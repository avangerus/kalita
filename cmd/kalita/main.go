// Command kalita is the single-binary entry point for the Kalita node.
package main

import (
	"fmt"
	"os"
)

var version = "0.1.0-dev"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("kalita %s\n", version)
		return
	}
	fmt.Println("kalita: an executable runtime for business systems in the agent era")
	fmt.Println("usage: kalita version")
	fmt.Println("(week 1 of MVP — the node does not serve yet; see docs/BACKLOG-MVP.md)")
}
