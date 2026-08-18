package engine

import (
	"encoding/json"
	"os"
	"testing"
)

// TestWalkerGoldenCorpus pins the engine's evalWhere to the shared walker
// corpus (workers/builder/walker_golden.json). The builder's Python walker
// runs the SAME corpus through its subset evaluator
// (workers/builder/test_synthesizer.py) — this test is the Go half of the
// differential contract that keeps the walker from silently diverging from
// the real guard semantics (product-mission P-risk, Architect Pro Finding 3).
func TestWalkerGoldenCorpus(t *testing.T) {
	raw, err := os.ReadFile("../../workers/builder/walker_golden.json")
	if err != nil {
		t.Fatalf("read walker corpus: %v (repo layout changed?)", err)
	}
	var corpus struct {
		Cases []struct {
			Expr   string         `json:"expr"`
			Record map[string]any `json:"record"`
			Want   bool           `json:"want"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) == 0 {
		t.Fatal("corpus must not be empty")
	}
	for i, c := range corpus.Cases {
		got := evalWhere(c.Expr, evalCtx{values: c.Record})
		if got != c.Want {
			t.Errorf("case %d: evalWhere(%q, %v) = %v, want %v",
				i, c.Expr, c.Record, got, c.Want)
		}
	}
}
