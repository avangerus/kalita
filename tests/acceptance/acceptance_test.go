//go:build acceptance

// Package acceptance is the automated release acceptance (docs/ACCEPTANCE.md):
// the REAL kalita binary, built from this checkout, against a REAL Postgres,
// driven over REST and MCP exactly as an operator would — identities from the
// CLI, node restarts, a crash mid-delivery, backup → verify → restore.
//
//	KALITA_ACCEPT_PG_DSN='postgres://postgres:kalita@127.0.0.1:55432/postgres?sslmode=disable' \
//	  go test -tags acceptance ./tests/acceptance -v -count=1 -timeout 15m
//
// The DSN must allow CREATE/DROP DATABASE: every run uses a fresh database and
// drops it at the end. The destination ("system Y") is an in-process mock on
// 127.0.0.1. Steps that need a human in a browser (UI walkthrough, WebAuthn,
// the Ask conversation) stay manual in docs/ACCEPTANCE.md.
package acceptance

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// secretBearer is the destination credential the node injects at runtime; the
// hygiene step proves it never lands in the journal, the backup or the pack.
const secretBearer = "Bearer acceptance-destination-credential-7f3a"

// ---------------------------------------------------------------- mock Y ---

// mockY is "system Y": it records every POST by idempotency key and can be
// told to fail the next N requests. A key with ≥1 acknowledged POST is one
// business effect — a correct receiver collapses redeliveries on the key.
type mockY struct {
	mu       sync.Mutex
	failNext int
	posts    int
	attempts map[string]int // key -> POSTs seen (acknowledged or not)
	acked    map[string]int // key -> POSTs answered 2xx
	auth     map[string]string
	entity   map[string]string // key -> record_id
	srv      *http.Server
	url      string
}

func startMockY(t *testing.T) *mockY {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	m := &mockY{attempts: map[string]int{}, acked: map[string]int{}, auth: map[string]string{}, entity: map[string]string{}}
	m.url = "http://" + ln.Addr().String() + "/hook"
	m.srv = &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Key      string `json:"idempotency_key"`
			RecordID string `json:"record_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		m.mu.Lock()
		defer m.mu.Unlock()
		m.posts++
		m.attempts[body.Key]++
		m.entity[body.Key] = body.RecordID
		m.auth[body.Key] = r.Header.Get("Authorization")
		if m.failNext > 0 {
			m.failNext--
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		m.acked[body.Key]++
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = m.srv.Serve(ln) }()
	t.Cleanup(func() { _ = m.srv.Close() })
	return m
}

func (m *mockY) fail(n int) { m.mu.Lock(); m.failNext = n; m.mu.Unlock() }

func (m *mockY) totalPosts() int { m.mu.Lock(); defer m.mu.Unlock(); return m.posts }

// keyFor returns the idempotency key Y saw for a record ("" if none yet).
func (m *mockY) keyFor(recordID string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, id := range m.entity {
		if id == recordID {
			return k
		}
	}
	return ""
}

func (m *mockY) ackedFor(key string) int   { m.mu.Lock(); defer m.mu.Unlock(); return m.acked[key] }
func (m *mockY) authFor(key string) string { m.mu.Lock(); defer m.mu.Unlock(); return m.auth[key] }

func (m *mockY) effects() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, c := range m.acked {
		if c > 0 {
			n++
		}
	}
	return n
}

// ------------------------------------------------------------------ node ---

type harness struct {
	t       *testing.T
	root    string // repo root
	work    string // per-run scratch dir
	bin     string
	version string
	dsn     string // the run's own database
	pack    string
	dataDir string
	base    string // http://127.0.0.1:port
	node    *exec.Cmd
	nodeLog *bytes.Buffer
	logMu   sync.Mutex
	tokens  map[string]string
	y       *mockY
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

func run(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return string(out)
}

// freshDatabase creates a uniquely named database next to the admin DSN and
// drops it when the test ends.
func freshDatabase(t *testing.T, adminDSN string) string {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("kalita_accept_%d", time.Now().UnixNano())
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatalf("KALITA_ACCEPT_PG_DSN: %v", err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() {
		c, err := pgx.Connect(ctx, adminDSN)
		if err != nil {
			return
		}
		defer c.Close(ctx)
		_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	u, err := url.Parse(adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}

func recreateDatabase(t *testing.T, adminDSN, dsn string) {
	t.Helper()
	ctx := context.Background()
	u, _ := url.Parse(dsn)
	name := strings.TrimPrefix(u.Path, "/")
	conn, err := pgx.Connect(ctx, adminDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, "DROP DATABASE "+name+" WITH (FORCE)"); err != nil {
		t.Fatalf("drop database: %v", err)
	}
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("recreate database: %v", err)
	}
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func (h *harness) env(extra ...string) []string {
	return append([]string{"KALITA_PG_DSN=" + h.dsn}, extra...)
}

// cli runs the built binary with the run's DSN.
func (h *harness) cli(args ...string) string {
	h.t.Helper()
	return run(h.t, h.work, h.env(), h.bin, args...)
}

var tokenLine = regexp.MustCompile(`(?m)^bearer token \(shown once.*\):\r?\n(\S+)`)

func (h *harness) addActor(kind, id, role string) {
	h.t.Helper()
	out := h.cli(kind, "add", "--id", id, "--role", role)
	m := tokenLine.FindStringSubmatch(out)
	if m == nil {
		h.t.Fatalf("%s add %s: no token in output:\n%s", kind, id, out)
	}
	h.tokens[id] = m[1]
}

type logWriter struct{ h *harness }

func (w logWriter) Write(p []byte) (int, error) {
	w.h.logMu.Lock()
	defer w.h.logMu.Unlock()
	return w.h.nodeLog.Write(p)
}

func (h *harness) logText() string {
	h.logMu.Lock()
	defer h.logMu.Unlock()
	return h.nodeLog.String()
}

// startNode boots `kalita serve` and waits for /readyz.
func (h *harness) startNode(egressDomains, secretsFile string) {
	h.t.Helper()
	port := freePort(h.t)
	h.base = fmt.Sprintf("http://127.0.0.1:%d", port)
	h.nodeLog = &bytes.Buffer{}
	cmd := exec.Command(h.bin, "serve", "--pack", h.pack, "--ui-dir", filepath.Join(h.root, "web"),
		"--listen", fmt.Sprintf("127.0.0.1:%d", port), "--data-dir", h.dataDir,
		"--egress-domains", egressDomains)
	cmd.Dir = h.work
	cmd.Env = append(os.Environ(), h.env("KALITA_EGRESS_SECRETS="+secretsFile)...)
	cmd.Stdout = logWriter{h}
	cmd.Stderr = logWriter{h}
	if err := cmd.Start(); err != nil {
		h.t.Fatal(err)
	}
	h.node = cmd
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := http.Get(h.base + "/readyz"); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	h.t.Fatalf("node did not become ready:\n%s", h.logText())
}

// killNode stops the node hard (no graceful drain): the journal is the only
// state that may survive, which is exactly what a crash leaves behind.
func (h *harness) killNode() {
	h.t.Helper()
	if h.node == nil {
		return
	}
	_ = h.node.Process.Kill()
	_ = h.node.Wait()
	h.node = nil
}

// ------------------------------------------------------------- transport ---

func (h *harness) rest(method, path, actor string, body any) (int, map[string]any) {
	h.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, h.base+path, rd)
	if actor != "" {
		req.Header.Set("Authorization", "Bearer "+h.tokens[actor])
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return resp.StatusCode, out
}

func (h *harness) restOK(method, path, actor string, body any) map[string]any {
	h.t.Helper()
	code, out := h.rest(method, path, actor, body)
	if code != http.StatusOK {
		h.t.Fatalf("%s %s as %s: HTTP %d %v", method, path, actor, code, out)
	}
	return out
}

func (h *harness) mcp(actor, tool string, args map[string]any) map[string]any {
	h.t.Helper()
	if !strings.HasPrefix(tool, "get_") && !strings.HasPrefix(tool, "list_") {
		if _, ok := args["idempotency_key"]; !ok { // every MCP mutation carries one
			args["idempotency_key"] = fmt.Sprintf("accept-%s-%d", tool, time.Now().UnixNano())
		}
	}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": tool, "arguments": args}})
	req, _ := http.NewRequest("POST", h.base+"/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+h.tokens[actor])
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	var rpc struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rpc); err != nil {
		h.t.Fatalf("mcp %s: %v", tool, err)
	}
	if rpc.Error != nil || len(rpc.Result.Content) == 0 {
		h.t.Fatalf("mcp %s as %s: protocol error %+v", tool, actor, rpc.Error)
	}
	out := map[string]any{}
	_ = json.Unmarshal([]byte(rpc.Result.Content[0].Text), &out)
	if rpc.Result.IsError {
		h.t.Fatalf("mcp %s as %s: tool error %v", tool, actor, out)
	}
	return out
}

func (h *harness) metric(name string) int {
	h.t.Helper()
	req, _ := http.NewRequest("GET", h.base+"/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+h.tokens["admin"])
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		var v int
		if n, _ := fmt.Sscanf(sc.Text(), name+" %d", &v); n == 1 {
			return v
		}
	}
	h.t.Fatalf("metric %s not exposed", name)
	return 0
}

func waitFor(t *testing.T, what string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for: %s", timeout, what)
}

// --------------------------------------------------------------- AP flow ---

func (h *harness) status(invID string) string {
	rec := h.mcp("clerk", "get_record", map[string]any{"entity": "VendorInvoice", "id": invID})
	return rec["values"].(map[string]any)["status"].(string)
}

func (h *harness) agentTask(invID, action string) string {
	h.t.Helper()
	mine := h.mcp("checker", "list_my_tasks", map[string]any{})
	for _, x := range mine["tasks"].([]any) {
		tm := x.(map[string]any)
		if tm["record_id"] == invID && tm["action"] == action {
			return tm["id"].(string)
		}
	}
	h.t.Fatalf("checker pool has no %s task for %s: %v", action, invID, mine)
	return ""
}

// agentAct runs one pool task the way an agent does: take, act under the
// task basis, complete.
func (h *harness) agentAct(invID, action string, values map[string]any) {
	h.t.Helper()
	tid := h.agentTask(invID, action)
	tb := map[string]any{"type": "task", "id": tid}
	h.mcp("checker", "take_task", map[string]any{"task_id": tid})
	if values != nil {
		h.mcp("checker", "update_record", map[string]any{"entity": "VendorInvoice", "id": invID, "basis": tb, "values": values})
	}
	res := h.mcp("checker", "act", map[string]any{"entity": "VendorInvoice", "id": invID, "action": action,
		"basis": tb, "idempotency_key": "accept-" + action + "-" + invID})
	if res["status"] != "applied" {
		h.t.Fatalf("%s: %v", action, res)
	}
	if action == "start_checking" {
		res = h.mcp("checker", "act", map[string]any{"entity": "VendorInvoice", "id": invID, "action": "submit_check",
			"basis": tb, "idempotency_key": "accept-submit-" + invID})
		if res["status"] != "applied" {
			h.t.Fatalf("submit_check: %v", res)
		}
	}
	h.mcp("checker", "complete_task", map[string]any{"task_id": tid, "result": action + " done"})
}

func (h *harness) newInvoice(vendorID string, amount int) string {
	h.t.Helper()
	inv := h.mcp("clerk", "create_record", map[string]any{"entity": "VendorInvoice",
		"basis":  map[string]any{"type": "human", "id": "clerk"},
		"values": map[string]any{"vendor": vendorID, "contract_no": "C-2026-001", "amount": amount, "due_date": "2026-12-15"}})
	return inv["id"].(string)
}

// throughApproval drives an invoice Received → Checking → ApprovalPending →
// (manager signs) Approved.
func (h *harness) throughApproval(invID string) {
	h.t.Helper()
	h.agentAct(invID, "start_checking", map[string]any{"contract_ok": true, "amount_ok": true,
		"requisites_ok": true, "check_notes": "Contract, amount and bank details match."})
	if got := h.status(invID); got != "ApprovalPending" {
		h.t.Fatalf("after the agent check: status %s, want ApprovalPending", got)
	}
	_, res := h.rest("POST", "/api/records/VendorInvoice/"+invID+"/act", "manager",
		map[string]any{"action": "approve_small", "basis": map[string]any{"type": "human", "id": "manager"}})
	approvalID, _ := res["approval_id"].(string)
	if res["status"] != "pending_approval" || approvalID == "" {
		h.t.Fatalf("approve_small must park for a signature: %v", res)
	}
	h.restOK("POST", "/api/approvals/"+approvalID+"/decide", "manager",
		map[string]any{"grant": true, "basis": map[string]any{"type": "human", "id": "manager"}})
	if got := h.status(invID); got != "Approved" {
		h.t.Fatalf("after the signature: status %s, want Approved", got)
	}
}

// sendToY hands an approved invoice to Y (the agent's send_to_payment task).
func (h *harness) sendToY(invID string) {
	h.t.Helper()
	h.agentAct(invID, "send_to_payment", nil)
	if got := h.status(invID); got != "SentToPayment" {
		h.t.Fatalf("status %s, want SentToPayment", got)
	}
}

type dlqItem struct {
	TaskID       string `json:"task_id"`
	DeliveryID   string `json:"delivery_id"`
	RecordID     string `json:"record_id"`
	Attempts     int    `json:"attempts"`
	LastCategory string `json:"last_category"`
}

func (h *harness) dlq() []dlqItem {
	h.t.Helper()
	out := h.restOK("GET", "/api/egress/dlq", "admin", nil)
	raw, _ := json.Marshal(out["items"])
	var items []dlqItem
	_ = json.Unmarshal(raw, &items)
	return items
}

func (h *harness) dlqFor(invID string) *dlqItem {
	for _, it := range h.dlq() {
		if it.RecordID == invID {
			it := it
			return &it
		}
	}
	return nil
}

// ------------------------------------------------------------------ test ---

func TestReleaseAcceptance(t *testing.T) {
	adminDSN := os.Getenv("KALITA_ACCEPT_PG_DSN")
	if adminDSN == "" {
		t.Skip("KALITA_ACCEPT_PG_DSN not set (a Postgres DSN allowed to CREATE/DROP DATABASE)")
	}
	h := &harness{t: t, root: repoRoot(t), work: t.TempDir(), tokens: map[string]string{}}
	t.Cleanup(func() {
		h.killNode()
		if t.Failed() && h.nodeLog != nil {
			t.Logf("node log:\n%s", h.logText())
		}
	})
	h.dataDir = filepath.Join(h.work, "data")
	h.dsn = freshDatabase(t, adminDSN)
	h.y = startMockY(t)

	// ---- 1. build, version identity
	h.version = strings.TrimSpace(run(t, h.root, nil, "git", "describe", "--tags", "--always", "--dirty"))
	h.bin = filepath.Join(h.work, "kalita")
	if runtime.GOOS == "windows" {
		h.bin += ".exe"
	}
	run(t, h.root, nil, "go", "build", "-ldflags", "-X main.version="+h.version, "-o", h.bin, "./cmd/kalita")
	if out := h.cli("version"); !strings.Contains(out, h.version) || strings.Contains(out, "0.1.0-dev") {
		t.Fatalf("step 1: kalita version = %q, want %s", out, h.version)
	}

	// ---- the AP pack, pointed at the local Y (the URL is per-deployment)
	h.pack = filepath.Join(h.work, "pack-ap")
	if err := os.MkdirAll(h.pack, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(h.root, "packs", "ap")
	entries, _ := os.ReadDir(src)
	for _, e := range entries {
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		b = bytes.ReplaceAll(b, []byte("https://payments.example.com/incoming-invoice"), []byte(h.y.url))
		if err := os.WriteFile(filepath.Join(h.pack, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h.cli("check", "--pack", h.pack)

	// ---- egress credentials (owner-only file)
	secretsDir := filepath.Join(h.work, "secrets")
	_ = os.MkdirAll(secretsDir, 0o700)
	secrets := filepath.Join(secretsDir, "egress.json")
	sb, _ := json.Marshal(map[string]string{"127.0.0.1": secretBearer})
	if err := os.WriteFile(secrets, sb, 0o600); err != nil {
		t.Fatal(err)
	}
	otherSecrets := filepath.Join(secretsDir, "egress-other.json")
	ob, _ := json.Marshal(map[string]string{"other.example.com": "Bearer unrelated"})
	if err := os.WriteFile(otherSecrets, ob, 0o600); err != nil {
		t.Fatal(err)
	}

	// ---- 3. identities from the CLI (only hashes are journaled)
	h.addActor("user", "admin", "Owner")
	h.addActor("user", "clerk", "APClerk")
	h.addActor("user", "manager", "Manager")
	h.addActor("agent", "checker", "APChecker")

	// ---- 4. node on Postgres with egress to 127.0.0.1
	h.startNode("127.0.0.1", secrets)
	log := h.logText()
	for _, want := range []string{"webhook runner: 1 allowed egress domain(s)", "egress secrets: loaded 1 destination credential(s)"} {
		if !strings.Contains(log, want) {
			t.Fatalf("step 4: boot log lacks %q:\n%s", want, log)
		}
	}

	// ---- 5. UI is served and English (the visual walkthrough stays manual)
	for _, path := range []string{"/", "/app.js"} {
		resp, err := http.Get(h.base + path)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || len(body) == 0 {
			t.Fatalf("step 5: GET %s = %d", path, resp.StatusCode)
		}
		if regexp.MustCompile(`[А-Яа-яЁё]`).Match(body) {
			t.Fatalf("step 5: %s contains Cyrillic text", path)
		}
	}

	// ---- 7. the full company model is reserved to the definition approver
	if code, _ := h.rest("GET", "/api/model", "manager", nil); code != http.StatusForbidden {
		t.Fatalf("step 7: manager GET /api/model = %d, want 403", code)
	}
	h.restOK("GET", "/api/model", "admin", nil)

	// ---- 8. the clerk registers a supplier and an invoice
	vendor := h.mcp("clerk", "create_record", map[string]any{"entity": "Vendor",
		"basis": map[string]any{"type": "human", "id": "clerk"},
		"values": map[string]any{"name": "Acme Supplies Ltd", "inn": "7712345678", "kpp": "771401001",
			"bank_bic": "044525225", "bank_account": "40702810000000012345"}})
	vendorID := vendor["id"].(string)
	inv1 := h.newInvoice(vendorID, 5000)
	if got := h.status(inv1); got != "Received" {
		t.Fatalf("step 8: status %s, want Received", got)
	}

	// ---- 9–10. agent check over MCP, manager signs
	// the agent may never sign or touch the amount
	tb := map[string]any{"type": "human", "id": "checker"}
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "act", "arguments": map[string]any{"entity": "VendorInvoice", "id": inv1, "action": "approve_small", "basis": tb}}})
	req, _ := http.NewRequest("POST", h.base+"/mcp", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+h.tokens["checker"])
	if resp, err := http.DefaultClient.Do(req); err == nil {
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !bytes.Contains(raw, []byte(`"isError":true`)) {
			t.Fatalf("step 9: the agent must not call an approval transition: %s", raw)
		}
	}
	h.throughApproval(inv1)

	// ---- 11. egress success: one POST, stable key, runtime-injected bearer
	h.sendToY(inv1)
	waitFor(t, "Y acknowledges invoice 1", 45*time.Second, func() bool {
		k := h.y.keyFor(inv1)
		return k != "" && h.y.ackedFor(k) > 0
	})
	key1 := h.y.keyFor(inv1)
	if !strings.HasPrefix(key1, "webhook:") {
		t.Fatalf("step 11: idempotency_key %q, want webhook:<delivery_id>", key1)
	}
	if got := h.y.authFor(key1); got != secretBearer {
		t.Fatalf("step 11: Authorization %q, want the configured credential", got)
	}
	waitFor(t, "kalita_egress_delivered = 1", 15*time.Second, func() bool { return h.metric("kalita_egress_delivered") == 1 })
	if h.metric("kalita_egress_latency_count") < 1 {
		t.Fatal("step 11: no delivered-attempt latency recorded")
	}

	// ---- 12. persistent failure → retries → DLQ
	h.y.fail(1000)
	inv2 := h.newInvoice(vendorID, 7000)
	h.throughApproval(inv2)
	h.sendToY(inv2)
	waitFor(t, "invoice 2 dead-letters", 90*time.Second, func() bool { return h.dlqFor(inv2) != nil })
	d2 := h.dlqFor(inv2)
	if d2.Attempts != 5 || d2.LastCategory != "retry_exhausted" {
		t.Fatalf("step 12: DLQ entry %+v, want 5 attempts, retry_exhausted", d2)
	}
	if h.metric("kalita_egress_dead_lettered") != 1 {
		t.Fatal("step 12: kalita_egress_dead_lettered != 1")
	}
	key2 := h.y.keyFor(inv2)

	// ---- 13. operator recovery: only the approver may retry; same key
	h.y.fail(0)
	if code, _ := h.rest("POST", "/api/egress/retry/"+d2.TaskID, "manager", nil); code != http.StatusForbidden {
		t.Fatalf("step 13: manager retry = %d, want 403", code)
	}
	retried := h.restOK("POST", "/api/egress/retry/"+d2.TaskID, "admin", nil)
	if retried["delivery_id"] != d2.DeliveryID {
		t.Fatalf("step 13: retry changed the delivery identity: %v vs %s", retried["delivery_id"], d2.DeliveryID)
	}
	waitFor(t, "invoice 2 delivered after retry", 45*time.Second, func() bool { return h.y.ackedFor(key2) > 0 })
	if h.y.keyFor(inv2) != key2 || h.y.effects() != 2 {
		t.Fatalf("step 13: want 2 business effects (one per invoice), got %d", h.y.effects())
	}
	waitFor(t, "the recovered delivery leaves the DLQ", 15*time.Second, func() bool { return h.dlqFor(inv2) == nil })
	if h.metric("kalita_egress_dead_lettered") != 0 {
		t.Fatal("step 13: a recovered delivery is still counted as dead-lettered")
	}

	// ---- 14. allowlist fail-closed: zero requests leave the node
	h.killNode()
	h.startNode("other.example.com", secrets)
	before := h.y.totalPosts()
	inv3 := h.newInvoice(vendorID, 3000)
	h.throughApproval(inv3)
	h.sendToY(inv3)
	waitFor(t, "invoice 3 dead-letters on the allowlist", 30*time.Second, func() bool { return h.dlqFor(inv3) != nil })
	if d := h.dlqFor(inv3); d.LastCategory != "allowlist" {
		t.Fatalf("step 14: category %q, want allowlist", d.LastCategory)
	}
	if h.y.totalPosts() != before {
		t.Fatal("step 14: a request reached Y despite the allowlist")
	}
	if !strings.Contains(h.logText(), "outside KALITA_EGRESS_DOMAINS") {
		t.Fatal("step 14: no allowlist denial in the node log")
	}

	// ---- 15. secrets fail-safe: no unauthenticated POST
	h.killNode()
	h.startNode("127.0.0.1", otherSecrets)
	inv4 := h.newInvoice(vendorID, 4000)
	h.throughApproval(inv4)
	h.sendToY(inv4)
	waitFor(t, "invoice 4 dead-letters without a credential", 30*time.Second, func() bool { return h.dlqFor(inv4) != nil })
	if d := h.dlqFor(inv4); d.LastCategory != "auth_missing" {
		t.Fatalf("step 15: category %q, want auth_missing", d.LastCategory)
	}
	if h.y.totalPosts() != before {
		t.Fatal("step 15: a request reached Y without its credential")
	}

	// ---- 16. crash mid-delivery: the intent survives, same identity after restart
	h.killNode()
	h.startNode("127.0.0.1", secrets)
	h.y.fail(1000)
	inv5 := h.newInvoice(vendorID, 6000)
	h.throughApproval(inv5)
	h.sendToY(inv5)
	waitFor(t, "a failing attempt for invoice 5", 30*time.Second, func() bool { return h.y.keyFor(inv5) != "" })
	key5 := h.y.keyFor(inv5)
	h.killNode()
	h.y.fail(0)
	h.startNode("127.0.0.1", secrets)
	retried5 := false
	waitFor(t, "invoice 5 delivered after the crash", 90*time.Second, func() bool {
		if h.y.ackedFor(key5) > 0 {
			return true
		}
		if d := h.dlqFor(inv5); d != nil && !retried5 { // the crash landed after the tick settled
			h.restOK("POST", "/api/egress/retry/"+d.TaskID, "admin", nil)
			retried5 = true
		}
		return false
	})
	if h.y.keyFor(inv5) != key5 || h.y.effects() != 3 {
		t.Fatalf("step 16: want 3 business effects under stable keys, got %d", h.y.effects())
	}
	dlqBefore := len(h.dlq())
	statuses := map[string]string{}
	for _, id := range []string{inv1, inv2, inv3, inv4, inv5} {
		statuses[id] = h.status(id)
	}

	// ---- 17–18. backup and offline verification
	h.killNode()
	backup := filepath.Join(h.work, "backup")
	h.cli("backup", "--out", backup)
	out := h.cli("verify", "--file", backup, "--pub", filepath.Join(h.dataDir, "node.pub"))
	if !strings.Contains(out, "chain ok") {
		t.Fatalf("step 18: verify output lacks 'chain ok':\n%s", out)
	}

	// ---- 19. restore into an empty database, replay, same state
	recreateDatabase(t, adminDSN, h.dsn)
	h.cli("backup", "restore", "--dir", backup)
	h.startNode("127.0.0.1", secrets)
	for id, want := range statuses {
		if got := h.status(id); got != want {
			t.Fatalf("step 19: %s status %s after restore, want %s", id, got, want)
		}
	}
	if got := len(h.dlq()); got != dlqBefore {
		t.Fatalf("step 19: DLQ has %d entries after restore, want %d", got, dlqBefore)
	}

	// ---- 20. hygiene: the destination credential is nowhere at rest
	cred := strings.TrimPrefix(secretBearer, "Bearer ")
	_ = filepath.Walk(backup, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if b, _ := os.ReadFile(p); bytes.Contains(b, []byte(cred)) {
				t.Fatalf("step 20: the egress credential leaked into %s", p)
			}
		}
		return nil
	})
	_ = filepath.Walk(h.pack, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if b, _ := os.ReadFile(p); bytes.Contains(b, []byte(cred)) {
				t.Fatalf("step 20: the egress credential leaked into the pack %s", p)
			}
		}
		return nil
	})
	if bytes.Contains([]byte(h.logText()), []byte(cred)) {
		t.Fatal("step 20: the egress credential appears in the node log")
	}
	t.Logf("release acceptance PASS: %s, %d business effects, %d DLQ entries", h.version, h.y.effects(), dlqBefore)
}
