package mcp

import (
	"os"
	"testing"
)

// TestMcpContractMatchesCode is the RELEASE-1.0 N8 §7.3 gate: docs/MCP-CONTRACT-v1.md
// must be byte-identical to what RenderContract() produces from tools.go's
// toolDefs right now. If this fails after a tool was added/changed/removed,
// regenerate the doc — do not hand-edit it:
//
//	kalita mcp contract > docs/MCP-CONTRACT-v1.md
//
// This is what makes "how many tools" and "which tools" honest by
// construction: RELEASE-1.0 recorded README claiming "~22 tools" against 27
// in the code, and this test exists so that specific drift cannot recur.
func TestMcpContractMatchesCode(t *testing.T) {
	want := RenderContract()
	got, err := os.ReadFile("../../docs/MCP-CONTRACT-v1.md")
	if err != nil {
		t.Fatalf("read docs/MCP-CONTRACT-v1.md: %v (generate it with `kalita mcp contract > docs/MCP-CONTRACT-v1.md`)", err)
	}
	if string(got) != want {
		t.Errorf("docs/MCP-CONTRACT-v1.md is stale relative to internal/mcp/tools.go — regenerate with `kalita mcp contract > docs/MCP-CONTRACT-v1.md`")
	}
}
