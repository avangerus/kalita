package mcp

import (
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
)

// The pangram is the teaching artifact — it MUST compile, or it would teach
// wrong syntax. This pins it to the real grammar/compiler.
func TestPangramCompiles(t *testing.T) {
	_, errs := dsl.Compile(map[string]string{"pangram.kal": pangramExample})
	if len(errs) > 0 {
		t.Fatalf("the pangram must compile (it teaches the language): %v", errs[0])
	}
}
