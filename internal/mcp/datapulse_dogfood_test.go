package mcp

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// ENH-01 dogfood: datapulse (MDM-lite / НСИ-аудитор) governed model. This is
// a DSL-model-only Sprint 1 demo asset — no matching/profiling/dedup LOGIC
// lives in packs/datapulse (that is ENH-02..04's job). What this test proves
// is the governance invariant itself: ConnectorAgent/ProfilerAgent/MatchAgent
// propose (create Source/MasterRecord/QualityMetric/MatchGroup), but NO agent
// role — not even the one that raises the task — can ever move a
// StewardshipTask into Approved/Rejected/Merged/Closed. Only a human can, and
// Merged additionally requires DataAdmin's own signature (workflows.dsl:
// `requires approval(DataAdmin)`), a role distinct from Steward who runs
// every other transition.

func denyPermission(t *testing.T, name string, r map[string]any, isErr bool) {
	t.Helper()
	if !isErr {
		t.Fatalf("%s: must be denied, got %v", name, r)
	}
	if code, _ := r["code"].(string); code != "PERMISSION_DENIED" {
		t.Fatalf("%s: want PERMISSION_DENIED, got %v", name, r)
	}
}

// denyStatusWrite accepts either PERMISSION_DENIED or VALIDATION_ERROR — a
// direct write to a workflow-governed field is schema-rejected one layer
// before ABAC even runs (see contracts_backstop_test.go's denyReject), so
// either outcome proves the same thing: the write never took effect.
func denyStatusWrite(t *testing.T, name string, r map[string]any, isErr bool) {
	t.Helper()
	if !isErr {
		t.Fatalf("%s: must be rejected, got %v", name, r)
	}
	if code, _ := r["code"].(string); code != "PERMISSION_DENIED" && code != "VALIDATION_ERROR" {
		t.Fatalf("%s: want PERMISSION_DENIED or VALIDATION_ERROR, got %v", name, r)
	}
}

func TestDogfoodDataPulseDenyMatrix(t *testing.T) {
	ctx := context.Background()
	model, errs := dsl.Compile(packFiles(t, "../../packs/datapulse"))
	if len(errs) > 0 {
		t.Fatalf("datapulse must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}

	dataAdmin, _ := reg.RegisterWithToken(ctx, root, "admin-1", eventstore.ActorHuman, "DataAdmin", nil, nil, nil)
	steward, _ := reg.RegisterWithToken(ctx, root, "steward-1", eventstore.ActorHuman, "Steward", nil, nil, nil)
	connector, _ := reg.RegisterWithToken(ctx, root, "connector-1", eventstore.ActorAgent, "ConnectorAgent", nil, nil, nil)
	profiler, _ := reg.RegisterWithToken(ctx, root, "profiler-1", eventstore.ActorAgent, "ProfilerAgent", nil, nil, nil)
	matcher, _ := reg.RegisterWithToken(ctx, root, "matcher-1", eventstore.ActorAgent, "MatchAgent", nil, nil, nil)
	explainer, _ := reg.RegisterWithToken(ctx, root, "explainer-1", eventstore.ActorAgent, "ExplainerAgent", nil, nil, nil)

	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(eng, reg))
	defer srv.Close()

	// autonomous-ingest provenance: no task/human decision drives the
	// connector's own ingest run, so the basis is the standing automation
	// rule itself (eventstore.Basis "rule" — same provenance kind
	// internal/engine/automation.go's scheduled rules use), not a task.
	ruleBasis := map[string]any{"type": "rule", "id": "datapulse-ingest"}
	stewardBasis := map[string]any{"type": "human", "id": "steward-1"}

	// --- positive flow: ConnectorAgent ingests a Source and its raw rows ---
	src, isErr := call(t, srv.URL, connector, "create_record", map[string]any{
		"entity": "Source", "basis": ruleBasis,
		"values": map[string]any{"name": "Customers.xlsx", "kind": "Excel", "path": "/incoming/customers.xlsx"},
	})
	if isErr {
		t.Fatalf("ConnectorAgent create Source: %v", src)
	}
	srcID := src["id"].(string)

	if _, isErr := call(t, srv.URL, connector, "update_record", map[string]any{
		"entity": "Source", "id": srcID, "basis": ruleBasis,
		"values": map[string]any{"ingest_status": "Ingesting"},
	}); isErr {
		t.Fatal("ConnectorAgent must be able to move Source.ingest_status")
	}
	mr, isErr := call(t, srv.URL, connector, "create_record", map[string]any{
		"entity": "MasterRecord", "basis": ruleBasis,
		"values": map[string]any{"source": srcID, "raw": map[string]any{"name": "Acme LLC", "inn": "7701234567"}, "display_name": "Acme LLC"},
	})
	if isErr {
		t.Fatalf("ConnectorAgent create MasterRecord: %v", mr)
	}
	mrID := mr["id"].(string)
	if _, isErr := call(t, srv.URL, connector, "update_record", map[string]any{
		"entity": "Source", "id": srcID, "basis": ruleBasis,
		"values": map[string]any{"ingest_status": "Ingested", "record_count": 1},
	}); isErr {
		t.Fatal("ConnectorAgent must be able to finish ingest")
	}

	// ConnectorAgent must never repoint an already-registered source.
	out, isErr := call(t, srv.URL, connector, "update_record", map[string]any{
		"entity": "Source", "id": srcID, "basis": ruleBasis,
		"values": map[string]any{"path": "/incoming/elsewhere.xlsx"},
	})
	denyPermission(t, "ConnectorAgent rewriting Source.path", out, isErr)

	// the steward raises a task on the ingested record
	task, isErr := call(t, srv.URL, steward, "create_record", map[string]any{
		"entity": "StewardshipTask", "basis": stewardBasis,
		"values": map[string]any{"type": "QualityIssue", "master_record": mrID, "proposed_action": "Verify INN format"},
	})
	if isErr {
		t.Fatalf("Steward create StewardshipTask: %v", task)
	}
	taskID := task["id"].(string)

	// --- scenario 1 (ENH-01): ConnectorAgent tries to force the task to
	// Approved, both as a direct field write and as a workflow act. Neither
	// may leave a mark, and the node must return a structured error, not
	// panic or silently apply.
	out, isErr = call(t, srv.URL, connector, "update_record", map[string]any{
		"entity": "StewardshipTask", "id": taskID, "basis": ruleBasis,
		"values": map[string]any{"status": "Approved"},
	})
	denyStatusWrite(t, "ConnectorAgent direct StewardshipTask.status write", out, isErr)
	out, isErr = call(t, srv.URL, connector, "act", map[string]any{
		"entity": "StewardshipTask", "id": taskID, "action": "approve", "basis": ruleBasis,
	})
	denyPermission(t, "ConnectorAgent act(approve) on StewardshipTask", out, isErr)
	out, isErr = call(t, srv.URL, connector, "act", map[string]any{
		"entity": "StewardshipTask", "id": taskID, "action": "merge", "basis": ruleBasis,
	})
	denyPermission(t, "ConnectorAgent act(merge) on StewardshipTask", out, isErr)
	// ConnectorAgent must not even be able to raise its own StewardshipTask.
	out, isErr = call(t, srv.URL, connector, "create_record", map[string]any{
		"entity": "StewardshipTask", "basis": ruleBasis,
		"values": map[string]any{"type": "QualityIssue", "master_record": mrID},
	})
	denyPermission(t, "ConnectorAgent create StewardshipTask", out, isErr)

	// --- scenario 2 (ENH-01): MatchAgent proposes a duplicate group but must
	// never rewrite the MasterRecord it grouped.
	mg, isErr := call(t, srv.URL, matcher, "create_record", map[string]any{
		"entity": "MatchGroup", "basis": ruleBasis,
		"values": map[string]any{"records": []any{mrID}, "score": 0.92},
	})
	if isErr {
		t.Fatalf("MatchAgent create MatchGroup: %v", mg)
	}
	out, isErr = call(t, srv.URL, matcher, "update_record", map[string]any{
		"entity": "MasterRecord", "id": mrID, "basis": ruleBasis,
		"values": map[string]any{"status": "Merged"},
	})
	denyPermission(t, "MatchAgent update MasterRecord", out, isErr)

	// ProfilerAgent may write QualityMetric but never touch MasterRecord.
	qm, isErr := call(t, srv.URL, profiler, "create_record", map[string]any{
		"entity": "QualityMetric", "basis": ruleBasis,
		"values": map[string]any{"source": srcID, "empties": 0, "format_anomalies": 1, "duplicates": 0},
	})
	if isErr {
		t.Fatalf("ProfilerAgent create QualityMetric: %v", qm)
	}
	out, isErr = call(t, srv.URL, profiler, "update_record", map[string]any{
		"entity": "MasterRecord", "id": mrID, "basis": ruleBasis,
		"values": map[string]any{"display_name": "renamed by profiler"},
	})
	denyPermission(t, "ProfilerAgent update MasterRecord", out, isErr)

	// --- scenario 3 (ENH-01): ExplainerAgent writes the narrative field but
	// can never move the workflow itself.
	if _, isErr := call(t, srv.URL, explainer, "update_record", map[string]any{
		"entity": "StewardshipTask", "id": taskID, "basis": ruleBasis,
		"values": map[string]any{"explanation": "High INN-format similarity across ingested rows from the same source."},
	}); isErr {
		t.Fatal("ExplainerAgent must be able to write StewardshipTask.explanation")
	}
	out, isErr = call(t, srv.URL, explainer, "update_record", map[string]any{
		"entity": "StewardshipTask", "id": taskID, "basis": ruleBasis,
		"values": map[string]any{"status": "Approved"},
	})
	denyStatusWrite(t, "ExplainerAgent direct StewardshipTask.status write", out, isErr)
	out, isErr = call(t, srv.URL, explainer, "act", map[string]any{
		"entity": "StewardshipTask", "id": taskID, "action": "approve", "basis": ruleBasis,
	})
	denyPermission(t, "ExplainerAgent act(approve) on StewardshipTask", out, isErr)

	// --- the hard invariant: Steward itself — the role that runs every
	// other transition — is denied the merge signature; only DataAdmin holds
	// act[merge]/approve[merge] (security.dsl, workflows.dsl SoD).
	out, isErr = call(t, srv.URL, steward, "act", map[string]any{
		"entity": "StewardshipTask", "id": taskID, "action": "merge", "basis": stewardBasis,
	})
	denyPermission(t, "Steward act(merge) on StewardshipTask (SoD: DataAdmin only)", out, isErr)

	// the steward runs the ordinary path: start review, then approve.
	if _, isErr := call(t, srv.URL, steward, "act", map[string]any{
		"entity": "StewardshipTask", "id": taskID, "action": "start_review", "basis": stewardBasis,
	}); isErr {
		t.Fatal("Steward start_review failed")
	}
	res, isErr := call(t, srv.URL, steward, "act", map[string]any{
		"entity": "StewardshipTask", "id": taskID, "action": "approve", "basis": stewardBasis,
	})
	if isErr || res["status"] != "applied" {
		t.Fatalf("Steward approve: %v", res)
	}

	// no escape attempt above left any mark: the record is exactly where the
	// human steward left it (Approved), never Merged/Closed by an agent.
	got, isErr := call(t, srv.URL, dataAdmin, "get_record", map[string]any{
		"entity": "StewardshipTask", "id": taskID,
	})
	if isErr {
		t.Fatalf("get_record StewardshipTask: %v", got)
	}
	gv := got["values"].(map[string]any)
	if gv["status"] != "Approved" {
		t.Errorf("StewardshipTask status = %v, want Approved (no agent escape may have moved it further)", gv["status"])
	}
	if gv["explanation"] == "" || gv["explanation"] == nil {
		t.Errorf("ExplainerAgent's explanation must persist, got %v", gv["explanation"])
	}

	// journal shows the denials as events, not a crash: the server answered
	// every escape attempt over HTTP the whole way through this test.
	j, isErr := call(t, srv.URL, dataAdmin, "read_journal", map[string]any{
		"entity": "StewardshipTask", "id": taskID,
	})
	if isErr {
		t.Fatalf("read_journal StewardshipTask: %v", j)
	}
	if len(j["events"].([]any)) < 2 {
		t.Fatalf("journal must show create + at least one transition: %v", j)
	}
}
