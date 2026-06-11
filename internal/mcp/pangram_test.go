package mcp

import (
	"os"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
)

// The pangram example pack exercises every type and construct. It is no longer
// served over MCP (the grammar is the teaching path), but it stays a compile
// guard: if it stops compiling, the language drifted from its own showcase.
func TestPangramCompiles(t *testing.T) {
	src, err := os.ReadFile("../../examples/pangram/pangram.kal")
	if err != nil {
		t.Fatalf("read pangram: %v", err)
	}
	_, errs := dsl.Compile(map[string]string{"pangram.kal": string(src)})
	if len(errs) > 0 {
		t.Fatalf("the pangram example must compile: %v", errs[0])
	}
}
