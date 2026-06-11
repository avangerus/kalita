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
	default:
		usage()
	}
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
	pack := fs.String("pack", "", "pack directory")
	listen := fs.String("listen", ":8080", "listen address")
	_ = fs.Parse(args)
	if *pack == "" {
		log.Fatal("--pack is required")
	}

	model, errs := loadPack(*pack)
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Println(e.Error())
		}
		os.Exit(1)
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

	eng, err := engine.New(ctx, model, store)
	if err != nil {
		log.Fatalf("engine: %v", err)
	}
	log.Printf("pack %s: %d entities, %d roles, def_version %d",
		model.Manifest.Name, len(model.Entities), len(model.Roles), eng.DefVersion())
	log.Printf("listening on %s (v0 dev auth: X-Actor-Id / X-Actor-Role headers)", *listen)
	log.Fatal(http.ListenAndServe(*listen, api.New(eng)))
}
