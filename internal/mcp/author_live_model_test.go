package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// TestAuthorLiveModelFullLoop closes the whole author loop WITH A REAL MODEL:
// an empty node + a business description -> a local llama.cpp model synthesizes
// a pack -> the node validates it (compose_pack) -> the agent proposes it -> a
// human signs -> it applies -> an agent then works in the system the model
// built. Skips when no local model is reachable (KALITA_LLM_URL, default
// http://127.0.0.1:1234/v1).

const authorPrompt = `Build a minimal kalita pack JSON for: "сотрудник подаёт заявку, менеджер одобряет".
Output ONLY one JSON object, no prose, no markdown fences.
Schema: {"pack":str,"entities":[{"name":str,"fields":[{"name":str,"type":str}]}],"workflows":[{"entity":str,"field":str,"transitions":[{"from":str,"to":str,"action":str,"requires_approval":str}]}],"roles":[{"name":str,"agent":bool}],"permissions":[{"role":str,"rules":[str]}]}
- Use type "enum" with values ["Pending","Approved"] for status.
- approve/act take ACTION names (workflow transition actions), not entity names.
- Every agent role needs a deny rule, e.g. "deny [delete *]".
- Example: {"pack":"mini","entities":[{"name":"Ticket","fields":[{"name":"title","type":"string"},{"name":"status","type":"enum","values":["Pending","Approved"],"default":"Pending"}]}],"workflows":[{"entity":"Ticket","field":"status","transitions":[{"from":"Pending","to":"Approved","action":"approve","requires_approval":"Manager"}]}],"roles":[{"name":"Manager"},{"name":"Worker","agent":true}],"permissions":[{"role":"Manager","rules":["full [Ticket]","approve [approve]"]},{"role":"Worker","rules":["create [Ticket]","deny [delete *]"]}]}`

func llmReachable(url string) bool {
	c := &http.Client{Timeout: 4 * time.Second}
	resp, err := c.Get(strings.TrimSuffix(url, "/v1") + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

func chatLLM(t *testing.T, url, model, content string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"model": model, "messages": []map[string]string{{"role": "user", "content": content}},
		"temperature": 0.2, "seed": 42, "max_tokens": 4000, "stream": false,
	})
	req, _ := http.NewRequest("POST", strings.TrimSuffix(url, "/")+"/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 300 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("model call: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out.Choices) == 0 {
		t.Fatalf("bad model reply: %s", raw)
	}
	return out.Choices[0].Message.Content
}

func modelSpec(t *testing.T, llmURL, llmModel string) map[string]any {
	t.Helper()
	text := chatLLM(t, llmURL, llmModel, authorPrompt)
	for attempt := 0; attempt < 8; attempt++ {
		i, j := strings.Index(text, "{"), strings.LastIndex(text, "}")
		if i >= 0 && j > i {
			var spec map[string]any
			if err := json.Unmarshal([]byte(text[i:j+1]), &spec); err == nil {
				return spec
			}
		}
		text = chatLLM(t, llmURL, llmModel,
			"Your JSON was truncated or incomplete (it stopped mid-object). Here was your output:\n"+
				text+"\nFinish it: return ONLY the complete, valid, single-line JSON object.")
	}
	t.Fatalf("model never returned valid JSON; last output: %.400s", text)
	return nil
}

// fixSpec is the author loop's error-feedback half: kalita's structured
// compose_pack errors go back to the model, which returns a corrected spec.
func fixSpec(t *testing.T, llmURL, llmModel string, prev map[string]any, errors []any) map[string]any {
	t.Helper()
	var brief strings.Builder
	for _, e := range errors {
		if m, ok := e.(map[string]any); ok {
			brief.WriteString(fmt.Sprintf("- %v: %v (fix: %v)\n", m["code"], m["message"], m["fix_hint"]))
		}
	}
	prevJSON, _ := json.Marshal(prev)
	text := chatLLM(t, llmURL, llmModel,
		"Your compose_pack spec failed kalita validation:\n"+brief.String()+
			"\nYour previous spec:\n"+string(prevJSON)+
			"\nFix ONLY the reported problems. Return ONLY the corrected, complete, single-line JSON object.")
	i, j := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if i < 0 || j <= i {
		t.Fatalf("model returned no JSON on fix: %.400s", text)
	}
	var spec map[string]any
	if err := json.Unmarshal([]byte(text[i:j+1]), &spec); err != nil {
		t.Fatalf("model returned invalid JSON on fix: %v", err)
	}
	return spec
}

func TestAuthorLiveModelFullLoop(t *testing.T) {
	llmURL := os.Getenv("KALITA_LLM_URL")
	if llmURL == "" {
		llmURL = "http://127.0.0.1:1234/v1"
	}
	llmModel := os.Getenv("KALITA_LLM_MODEL")
	if llmModel == "" {
		llmModel = "gemma-4-E4B-it-Q4_K_M.gguf"
	}
	if os.Getenv("KALITA_LIVE_MODEL_TEST") != "1" {
		t.Skip("opt-in live-model test: set KALITA_LIVE_MODEL_TEST=1 (and KALITA_LLM_URL) to run; the deterministic full loop is TestAuthorComposePackLoop")
	}
	if !llmReachable(llmURL) {
		t.Skipf("local model not reachable at %s", llmURL)
	}

	// genesis: empty node
	model, errs := dsl.Compile(map[string]string{})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	ctx := context.Background()
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	builderToken, err := reg.RegisterWithToken(ctx, registrar, "builder", eventstore.ActorAgent, "Builder", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()

	// (1) the model builds the process from the description — the full author
	// loop: model -> compose_pack -> structured errors fed back -> regenerate,
	// bounded, deterministic (temperature=0), so the run is reproducible.
	spec := modelSpec(t, llmURL, llmModel)
	var composed map[string]any
	var isErr bool
	for attempt := 0; attempt < 8; attempt++ {
		composed, isErr = call(t, srv.URL, builderToken, "compose_pack", spec)
		if isErr {
			t.Fatalf("compose_pack error: %v", composed)
		}
		if composed["ok"] == true {
			break
		}
		spec = fixSpec(t, llmURL, llmModel, spec, composed["errors"].([]any))
	}
	if composed["ok"] != true {
		t.Fatalf("model never converged to a valid pack: %v", composed["errors"])
	}
	dslText := composed["dsl"].(string)
	entity := ""
	if ents, ok := spec["entities"].([]any); ok && len(ents) > 0 {
		if m, ok := ents[0].(map[string]any); ok {
			entity, _ = m["name"].(string)
		}
	}
	if entity == "" {
		t.Fatal("model's pack declares no entities")
	}

	// (2) propose, then a human signs.
	agentBasis := map[string]any{"type": "human", "id": "mike"}
	sys, _ := call(t, srv.URL, builderToken, "describe_system", map[string]any{})
	baseDef := sys["def_version"].(float64)
	prop, isErr := call(t, srv.URL, builderToken, "propose_change", map[string]any{
		"files":            map[string]string{spec["pack"].(string) + ".dsl": dslText},
		"base_def_version": baseDef, "description": "auto-built from a description", "basis": agentBasis,
	})
	if isErr || prop["ok"] != true {
		t.Fatalf("propose: %v", prop)
	}
	proposalID := prop["proposal_id"].(string)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "mike", Role: "Owner"}
	if _, err := eng.DecideProposal(ctx, owner, proposalID, true, nil, &eventstore.Basis{Type: "human", ID: "mike"}); err != nil {
		t.Fatalf("owner decision: %v", err)
	}
	st, _ := call(t, srv.URL, builderToken, "get_proposal", map[string]any{"proposal_id": proposalID})
	if st["status"] != "applied" {
		t.Fatalf("proposal must be applied after signature: %v", st)
	}

	// (3) an agent works in the system the model built: a worker with the
	// model's own agent role can read the new entity through the permission wall.
	workerRole := ""
	roles, _ := spec["roles"].([]any)
	for _, r := range roles {
		if m, ok := r.(map[string]any); ok && m["agent"] == true {
			workerRole, _ = m["name"].(string)
		}
	}
	if workerRole == "" && len(roles) > 0 {
		if m, ok := roles[0].(map[string]any); ok {
			workerRole, _ = m["name"].(string)
		}
	}
	if workerRole != "" {
		workerToken, err := reg.RegisterWithToken(ctx, registrar, "worker", eventstore.ActorAgent, workerRole, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, isErr := call(t, srv.URL, workerToken, "describe_entity", map[string]any{"entity": entity}); isErr {
			t.Fatalf("worker cannot see the entity the model built: entity=%s role=%s", entity, workerRole)
		}
	}
	t.Logf("LIVE SYNTHESIS (model -> process): entity %q built, proposed, human-signed, applied; worker role %q operates it; def_version %v -> %v",
		entity, workerRole, baseDef, baseDef+1)
}
