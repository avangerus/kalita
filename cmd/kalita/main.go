// Command kalita is the single-binary entry point for the Kalita node.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/avangerus/kalita/internal/api"
	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
	"github.com/avangerus/kalita/internal/mcp"
	"github.com/avangerus/kalita/internal/webui"
)

var version = "0.1.0-dev"

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	switch os.Args[1] {
	case "version":
		fmt.Printf("kalita %s\n", version)
	case "serve":
		serve(os.Args[2:])
	case "check":
		check(os.Args[2:])
	case "agent":
		agentCmd(os.Args[2:])
	default:
		usage()
	}
}

// agentCmd registers an agent and prints its bearer token (once).
// v0: run while the node is stopped — projections are rebuilt at serve start.
func agentCmd(args []string) {
	if len(args) < 1 || args[0] != "add" {
		fmt.Println("usage: kalita agent add --id <id> --role <Role> (KALITA_PG_DSN required)")
		os.Exit(1)
	}
	fs := flag.NewFlagSet("agent add", flag.ExitOnError)
	id := fs.String("id", "", "agent id")
	role := fs.String("role", "", "role from the pack")
	_ = fs.Parse(args[1:])
	if *id == "" || *role == "" {
		log.Fatal("--id and --role are required")
	}
	dsn := os.Getenv("KALITA_PG_DSN")
	if dsn == "" {
		log.Fatal("KALITA_PG_DSN is required: agent identities live in the journal")
	}
	ctx := context.Background()
	store, err := eventstore.NewPGStore(ctx, dsn, "", nil)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()
	reg := identity.NewRegistry(store)
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "node-admin", Role: "Owner"}
	token, err := reg.RegisterWithToken(ctx, registrar, *id, eventstore.ActorAgent, *role, nil,
		&eventstore.Basis{Type: "human", ID: "node-admin"})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("agent %s (role %s) registered\n", *id, *role)
	fmt.Printf("bearer token (shown once, only the hash is journaled):\n%s\n", token)
	fmt.Println("restart the node to pick up the registration (v0)")
}

func usage() {
	fmt.Println("kalita: an executable runtime for business systems in the agent era")
	fmt.Println("usage:")
	fmt.Println("  kalita serve --pack <dir> [--listen :8080]   run a node (KALITA_PG_DSN for postgres, else in-memory)")
	fmt.Println("  kalita check --pack <dir>                    compile a pack and print diagnostics")
	fmt.Println("  kalita version")
}

func loadPack(dir string) (*dsl.Model, []*dsl.Error) {
	files := map[string]string{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Fatalf("read pack dir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".kal" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			log.Fatal(err)
		}
		files[e.Name()] = string(raw)
	}
	if len(files) == 0 {
		log.Fatalf("no .kal files in %s", dir)
	}
	return dsl.Compile(files)
}

func check(args []string) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	pack := fs.String("pack", "", "pack directory")
	_ = fs.Parse(args)
	if *pack == "" {
		log.Fatal("--pack is required")
	}
	model, errs := loadPack(*pack)
	for _, e := range errs {
		fmt.Println(e.Error())
	}
	if len(errs) > 0 {
		os.Exit(1)
	}
	fmt.Printf("ok: pack %s, %d entities, %d roles\n",
		model.Manifest.Name, len(model.Entities), len(model.Roles))
}

func serve(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	pack := fs.String("pack", "", "genesis pack directory (optional: empty node accepts its first pack via propose_change)")
	listen := fs.String("listen", ":8080", "listen address")
	approver := fs.String("approver", "Owner", "role whose human signature applies definition changes")
	_ = fs.Parse(args)

	var model *dsl.Model
	if *pack != "" {
		var errs []*dsl.Error
		model, errs = loadPack(*pack)
		if len(errs) > 0 {
			for _, e := range errs {
				fmt.Println(e.Error())
			}
			os.Exit(1)
		}
	} else {
		model, _ = dsl.Compile(map[string]string{})
		log.Print("genesis: empty definition — the first pack arrives via propose_change + signature")
	}

	ctx := context.Background()
	var store eventstore.Store
	if dsn := os.Getenv("KALITA_PG_DSN"); dsn != "" {
		pg, err := eventstore.NewPGStore(ctx, dsn, "", nil)
		if err != nil {
			log.Fatalf("postgres: %v", err)
		}
		defer pg.Close()
		store = pg
		log.Print("journal: postgresql")
	} else {
		store = eventstore.NewMemStore(nil)
		log.Print("journal: IN-MEMORY (dev only, state is lost on exit; set KALITA_PG_DSN)")
	}

	reg := identity.NewRegistry(store)
	eng, err := engine.New(ctx, model, store,
		engine.WithVerifier(reg.VerifySignature),
		engine.WithDefinitionApprover(*approver))
	if err != nil {
		log.Fatalf("engine: %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", mcp.New(eng, reg))
	mux.Handle("/api/", api.New(eng))
	mux.Handle("/", webui.Handler())
	packName := "(genesis)"
	if m := eng.Model().Manifest; m != nil {
		packName = m.Name
	}
	log.Printf("pack %s: %d entities, def_version %d", packName, len(eng.Model().Entities), eng.DefVersion())
	log.Printf("listening on %s — REST (dev headers) + MCP at /mcp (agent bearer tokens)", *listen)
	log.Fatal(http.ListenAndServe(*listen, mux))
}
