package mcp

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// TestDispatchRecoversToolPanic proves RELEASE-1.0 N8 §7.7 for the MCP
// gateway: a tool panicking mid-request must not drop the agent's
// connection (which net/http's own recover would turn into an opaque
// disconnect — indistinguishable from a network blip, and unlike every
// other tool failure, not something the agent can read and self-correct
// from) or otherwise disturb the server. dispatch must instead answer with
// the same structured error shape any other unexpected failure gets
// (toolErr's "code":"INTERNAL" map), and the server must keep serving
// normally on the very next call.
func TestDispatchRecoversToolPanic(t *testing.T) {
	model, errs := dsl.Compile(map[string]string{})
	if len(errs) > 0 {
		t.Fatalf("empty model must compile: %v", errs[0])
	}
	ctx := context.Background()
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	eng, err := engine.New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}
	s := New(eng, reg)
	actor := eventstore.Actor{Type: eventstore.ActorAgent, ID: "bot-1", Role: "Bot"}
	req := httptest.NewRequest("POST", "/", nil)

	// Arm the fault-injection seam (dispatch_panic_test-only — see
	// panicOnDispatchForTest in tools.go): the very next call to this tool
	// panics before running any real logic.
	panicOnDispatchForTest = "describe_system"
	defer func() { panicOnDispatchForTest = "" }()

	result, terr := s.dispatch(req, actor, "describe_system", json.RawMessage("{}"))
	if terr == nil {
		t.Fatal("a panicking tool must return a non-nil tool error, not succeed")
	}
	if result != nil {
		t.Errorf("result must be nil on a recovered panic, got %v", result)
	}
	errMap, ok := terr.(map[string]any)
	if !ok {
		t.Fatalf("recovered-panic error must be the map[string]any shape toolErr uses, got %T: %v", terr, terr)
	}
	if errMap["code"] != "INTERNAL" {
		t.Errorf(`code = %v, want "INTERNAL" (toolErr's shape for an unexpected failure)`, errMap["code"])
	}
	if msg, _ := errMap["message"].(string); msg == "" {
		t.Error("message must be non-empty")
	}
	if hint, _ := errMap["fix_hint"].(string); hint == "" {
		t.Error("fix_hint must be non-empty (every tool error carries one)")
	}

	// Disarm the fault and prove the server is still healthy: the SAME tool,
	// called again right after the panic, must work normally.
	panicOnDispatchForTest = ""
	result2, terr2 := s.dispatch(req, actor, "describe_system", json.RawMessage("{}"))
	if terr2 != nil {
		t.Fatalf("the tool must work normally on the next call after a recovered panic, got error: %v", terr2)
	}
	sysInfo, ok := result2.(map[string]any)
	if !ok {
		t.Fatalf("describe_system must return its normal map result, got %T", result2)
	}
	if _, ok := sysInfo["def_version"]; !ok {
		t.Error("describe_system's normal result shape must be intact (def_version missing)")
	}
}
