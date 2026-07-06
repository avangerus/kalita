// Reports (LLD-09 §2/§3): `kalita report` ("week in numbers") and
// `kalita egress-report` ("what left the perimeter") are pure, read-only
// folds over the journal — zero writes, run as one-shot operator commands
// exactly like `kalita verify`/`kalita export` (KALITA_PG_DSN required: this
// is a separate-process tool reading the persisted journal, not the live
// server's in-memory projection).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// reportCmd is `kalita report --since <dur> [--role R] [--format md|html]
// [--pack <dir>] [--out <file>]` (LLD-09 §2). --pack is optional: without it
// every role's hours×rate is omitted with a note (BuildReport's own graceful
// degradation) rather than the command refusing to run.
func reportCmd(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	since := fs.String("since", "7d", "how far back to fold the journal (e.g. 7d, 48h, 30m)")
	role := fs.String("role", "", "restrict to one role (default: all roles)")
	format := fs.String("format", "md", "md or html")
	pack := fs.String("pack", "", "pack directory (or comma-separated list) — enables role rate/hours cost figures; omit and they are noted as unknown")
	out := fs.String("out", "", "write to this file instead of stdout")
	_ = fs.Parse(args)

	dur, err := engine.ParseDuration(*since)
	if err != nil {
		log.Fatalf("--since: %v", err)
	}
	if *format != "md" && *format != "html" {
		log.Fatal("--format must be md or html")
	}

	var roles map[string]*dsl.RoleDecl
	if *pack != "" {
		model, errs := loadPacks(splitComma(*pack))
		if len(errs) > 0 {
			for _, e := range errs {
				fmt.Println(e.Error())
			}
			os.Exit(1)
		}
		roles = model.Roles
	}

	dsn := resolveDSN()
	if dsn == "" {
		log.Fatal("KALITA_PG_DSN is required: kalita report reads the persisted journal")
	}
	ctx := context.Background()
	store, err := eventstore.NewPGStore(ctx, dsn, os.Getenv("KALITA_PG_SCHEMA"), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	events, err := store.All(ctx)
	if err != nil {
		log.Fatal(err)
	}
	now := time.Now()
	rep := engine.BuildReport(events, roles, now.Add(-dur), now, *role)

	var body string
	if *format == "html" {
		body = rep.RenderHTML()
	} else {
		body = rep.RenderMarkdown()
	}
	writeReportOutput(*out, body)
}

// egressReportCmd is `kalita egress-report --since <dur> [--format md|html]
// [--out <file>]` (LLD-09 §3): reconciles each worker's declared egress
// whitelist (identity.ActorMeta.EgressDomains, set via `kalita agent add
// --egress-domains`) against the EgressCalled facts it journaled itself.
func egressReportCmd(args []string) {
	fs := flag.NewFlagSet("egress-report", flag.ExitOnError)
	since := fs.String("since", "7d", "how far back to fold the journal (e.g. 7d, 48h, 30m)")
	format := fs.String("format", "md", "md or html")
	out := fs.String("out", "", "write to this file instead of stdout")
	_ = fs.Parse(args)

	dur, err := engine.ParseDuration(*since)
	if err != nil {
		log.Fatalf("--since: %v", err)
	}
	if *format != "md" && *format != "html" {
		log.Fatal("--format must be md or html")
	}

	dsn := resolveDSN()
	if dsn == "" {
		log.Fatal("KALITA_PG_DSN is required: kalita egress-report reads the persisted journal")
	}
	ctx := context.Background()
	store, err := eventstore.NewPGStore(ctx, dsn, os.Getenv("KALITA_PG_SCHEMA"), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	reg := identity.NewRegistry(store)
	actors, err := reg.List(ctx)
	if err != nil {
		log.Fatal(err)
	}
	whitelists := make(map[string][]string, len(actors))
	for _, a := range actors {
		if a.Meta != nil {
			whitelists[a.ID] = a.Meta.EgressDomains
		}
	}

	events, err := store.All(ctx)
	if err != nil {
		log.Fatal(err)
	}
	now := time.Now()
	rep := engine.BuildEgressReport(events, whitelists, now.Add(-dur), now)

	var body string
	if *format == "html" {
		body = rep.RenderHTML()
	} else {
		body = rep.RenderMarkdown()
	}
	writeReportOutput(*out, body)
}

func writeReportOutput(out, body string) {
	if out == "" {
		fmt.Print(body)
		return
	}
	if err := os.WriteFile(out, []byte(body), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s\n", out)
}
