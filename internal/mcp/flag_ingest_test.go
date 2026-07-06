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

// TECHDEBT "Безопасность LLM-периметра" (b): the MCP tool `flag_ingest`, the
// `flag [kind]` permission verb, and its idempotency — the write-side mirror
// of emit_event_test.go's PR-5 acceptance tests.

// testFlagPack declares one entity (Doc) and two agent roles: Indexer holds
// `flag [ingest.flagged]` (plus ordinary read/update on Doc, to show the
// dedicated grant is what actually gates flag_ingest, not the entity grant),
// Reader holds only read — the minimal fixture for the permission-denied and
// idempotent-retry acceptance tests.
const testFlagPack = `pack testflag
version 0.1.0

entity Doc:
    name: string

roles:
    Indexer agent
    Reader agent

permissions:
    Indexer:
        read [Doc]
        update [Doc]
        create [Doc]
        flag [ingest.flagged]
        deny [delete *]
    Reader:
        read [Doc]
        deny [delete *]
`

// newFlagMCP boots a node from testFlagPack with two agent tokens (Indexer,
// holding the flag grant; Reader, not) plus one Doc record created under
// Indexer's identity, and returns the engine directly so tests can inspect
// IngestFlags() beyond what a single read_journal call shows.
func newFlagMCP(t *testing.T) (srv *httptest.Server, eng *engine.Engine, indexerToken, readerToken, docID string) {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"testflag.dsl": testFlagPack})
	if len(errs) > 0 {
		t.Fatalf("compile testFlagPack: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	indexerToken, err := reg.RegisterWithToken(context.Background(), registrar,
		"indexer-1", eventstore.ActorAgent, "Indexer", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	readerToken, err = reg.RegisterWithToken(context.Background(), registrar,
		"reader-1", eventstore.ActorAgent, "Reader", nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	eng, err = engine.New(context.Background(), model, store)
	if err != nil {
		t.Fatal(err)
	}
	srv = httptest.NewServer(New(eng, reg))
	t.Cleanup(srv.Close)

	out, isErr := call(t, srv.URL, indexerToken, "create_record", map[string]any{
		"entity": "Doc", "values": map[string]any{"name": "honeypot.txt"},
		"basis": map[string]any{"type": "task", "id": "t-1"},
	})
	if isErr {
		t.Fatalf("create_record Doc must succeed: %v", out)
	}
	docID, _ = out["id"].(string)
	if docID == "" {
		t.Fatalf("create_record Doc did not return an id: %v", out)
	}
	return srv, eng, indexerToken, readerToken, docID
}

// TestFlagIngestPermissionDenied: flag_ingest without the `flag [ingest.flagged]`
// grant is denied, and no ingest.flagged is journaled for that call — even
// though Reader can read the Doc, that read grant does not imply flag.
func TestFlagIngestPermissionDenied(t *testing.T) {
	srv, eng, _, readerToken, docID := newFlagMCP(t)

	out, isErr := call(t, srv.URL, readerToken, "flag_ingest", map[string]any{
		"entity": "Doc", "id": docID, "source": "honeypot.txt",
		"reasons": []string{"ignore-previous-instructions"}, "score": 0.6,
		"basis": map[string]any{"type": "task", "id": "t-2"},
	})
	if !isErr || out["code"] != "PERMISSION_DENIED" {
		t.Fatalf("flag_ingest without a flag grant must be denied: %v", out)
	}
	if rule, _ := out["rule"].(string); rule == "" {
		t.Fatalf("PERMISSION_DENIED must name the rule: %v", out)
	}
	if flags := eng.IngestFlags(); len(flags) != 0 {
		t.Fatalf("a denied flag_ingest must journal no ingest.flagged, got %v", flags)
	}
}

// TestFlagIngestJournalsUnderTheRecordSubject: a successful flag_ingest
// journals ingest.flagged{source, reasons, score} under Subject{Doc, docID}
// — the whole point of PR-6: the signal is visible via read_journal on the
// flagged record itself, not just IngestFlags()'s engine-level view.
func TestFlagIngestJournalsUnderTheRecordSubject(t *testing.T) {
	srv, eng, indexerToken, _, docID := newFlagMCP(t)

	out, isErr := call(t, srv.URL, indexerToken, "flag_ingest", map[string]any{
		"entity": "Doc", "id": docID, "source": "honeypot.txt",
		"reasons": []string{"ignore-previous-instructions", "reveal-prompt-request"}, "score": 0.75,
		"basis": map[string]any{"type": "task", "id": "t-3"},
	})
	if isErr {
		t.Fatalf("flag_ingest with the flag grant must succeed: %v", out)
	}

	flags := eng.IngestFlags()
	if len(flags) != 1 {
		t.Fatalf("want exactly 1 ingest.flagged fact, got %d: %v", len(flags), flags)
	}
	f := flags[0]
	if f.Entity != "Doc" || f.RecordID != docID || f.Source != "honeypot.txt" || f.Score != 0.75 {
		t.Fatalf("unexpected ingest.flagged fact: %+v", f)
	}
	if len(f.Reasons) != 2 {
		t.Fatalf("want 2 reasons carried through, got: %v", f.Reasons)
	}

	// The record's own journal (read_journal) must show it too — the actual
	// "visible in audit" contract, checked at the MCP layer, not just engine-internal.
	jout, isErr := call(t, srv.URL, indexerToken, "read_journal", map[string]any{"entity": "Doc", "id": docID})
	if isErr {
		t.Fatalf("read_journal must succeed: %v", jout)
	}
	events, _ := jout["events"].([]any)
	found := false
	for _, e := range events {
		ev, _ := e.(map[string]any)
		if ev["kind"] == "ingest.flagged" {
			found = true
			payload, _ := json.Marshal(ev["payload"])
			var p struct {
				Source  string   `json:"source"`
				Reasons []string `json:"reasons"`
				Score   float64  `json:"score"`
			}
			_ = json.Unmarshal(payload, &p)
			if p.Source != "honeypot.txt" || p.Score != 0.75 || len(p.Reasons) != 2 {
				t.Fatalf("ingest.flagged event in read_journal has wrong payload: %+v", p)
			}
		}
	}
	if !found {
		t.Fatalf("read_journal on the flagged Doc must show an ingest.flagged event, got: %v", events)
	}
}

// TestFlagIngestIdempotent: a repeat flag_ingest call with the same
// idempotency_key produces exactly one ingest.flagged, not a duplicate.
func TestFlagIngestIdempotent(t *testing.T) {
	srv, eng, indexerToken, _, docID := newFlagMCP(t)
	const key = "flag-retry-same-key-001"
	args := map[string]any{
		"entity": "Doc", "id": docID, "source": "honeypot.txt",
		"reasons": []string{"ignore-previous-instructions"}, "score": 0.6,
		"basis": map[string]any{"type": "task", "id": "t-4"}, "idempotency_key": key,
	}

	out1, isErr := call(t, srv.URL, indexerToken, "flag_ingest", args)
	if isErr {
		t.Fatalf("first flag_ingest must succeed: %v", out1)
	}
	out2, isErr := call(t, srv.URL, indexerToken, "flag_ingest", args)
	if isErr {
		t.Fatalf("retried flag_ingest must succeed (idempotent replay, not an error): %v", out2)
	}
	if out1["seq"] != out2["seq"] {
		t.Fatalf("retried flag_ingest must return the SAME journaled fact: first=%v second=%v", out1, out2)
	}
	if flags := eng.IngestFlags(); len(flags) != 1 {
		t.Fatalf("want exactly 1 ingest.flagged for the repeated idempotency_key, got %d: %v", len(flags), flags)
	}
}

// TestFlagIngestUnknownRecordNotFound: flag_ingest against a record that does
// not exist is NOT_FOUND, and nothing is journaled.
func TestFlagIngestUnknownRecordNotFound(t *testing.T) {
	srv, eng, indexerToken, _, _ := newFlagMCP(t)

	out, isErr := call(t, srv.URL, indexerToken, "flag_ingest", map[string]any{
		"entity": "Doc", "id": "does-not-exist", "source": "honeypot.txt",
		"reasons": []string{"ignore-previous-instructions"}, "score": 0.6,
		"basis": map[string]any{"type": "task", "id": "t-5"},
	})
	if !isErr || out["code"] != "NOT_FOUND" {
		t.Fatalf("flag_ingest against a missing record must be NOT_FOUND: %v", out)
	}
	if flags := eng.IngestFlags(); len(flags) != 0 {
		t.Fatalf("a NOT_FOUND flag_ingest must journal nothing, got %v", flags)
	}
}
