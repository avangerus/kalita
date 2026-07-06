package main

import (
	"fmt"
	"os"

	"github.com/avangerus/kalita/internal/mcp"
)

// mcpCmd is the `kalita mcp` command family. `contract` prints the tool
// contract generated from internal/mcp's own toolDefs (RELEASE-1.0 N8 §7.3):
// an operator redirects it into docs/MCP-CONTRACT-v1.md, and
// internal/mcp.TestMcpContractMatchesCode gates that the committed file
// matches. No engine/journal is needed — this is pure code introspection.
func mcpCmd(args []string) {
	if len(args) < 1 || args[0] != "contract" {
		fmt.Println("usage: kalita mcp contract > docs/MCP-CONTRACT-v1.md")
		os.Exit(1)
	}
	fmt.Print(mcp.RenderContract())
}
