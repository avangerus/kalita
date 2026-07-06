// Shadow mode toggle (LLD-09 §1): `kalita shadow enable|disable --role R
// --pack <dir>`. A one-shot operator command in the same family as `kalita
// lockdown`/`kalita agent add` — it replays the journal into a fresh engine,
// appends role.shadow_enabled/disabled as the node's Owner, then exits. A
// currently RUNNING `kalita serve` process holds its own in-memory Engine and
// does not re-poll the store for this kind of event (unlike identity.Registry,
// which always re-reads on demand) — restart it to pick up the change, the
// same v0 limitation `kalita agent add`/`kalita lockdown` already carry.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
)

func shadowCmd(args []string) {
	if len(args) < 1 || (args[0] != "enable" && args[0] != "disable") {
		fmt.Println("usage: kalita shadow enable|disable --role <Role> --pack <dir> (KALITA_PG_DSN required)")
		os.Exit(1)
	}
	action := args[0]
	fs := flag.NewFlagSet("shadow "+action, flag.ExitOnError)
	role := fs.String("role", "", "role to toggle shadow mode for")
	pack := fs.String("pack", "", "pack directory (or comma-separated list) this node runs — needed to validate the role and replay state")
	approverRole := fs.String("approver", "Owner", "role whose human signature applies definition changes AND toggles shadow (must match how the node was started)")
	_ = fs.Parse(args[1:])
	if *role == "" {
		log.Fatal("--role is required")
	}
	if *pack == "" {
		log.Fatal("--pack is required")
	}

	model, errs := loadPacks(splitComma(*pack))
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Println(e.Error())
		}
		os.Exit(1)
	}

	dsn := resolveDSN()
	if dsn == "" {
		log.Fatal("KALITA_PG_DSN is required: shadow state lives in the journal")
	}
	ctx := context.Background()
	store, err := eventstore.NewPGStore(ctx, dsn, os.Getenv("KALITA_PG_SCHEMA"), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close()

	eng, err := engine.New(ctx, model, store, engine.WithDefinitionApprover(*approverRole))
	if err != nil {
		log.Fatal(err)
	}

	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "node-admin", Role: *approverRole}
	basis := &eventstore.Basis{Type: "human", ID: "node-admin"}

	switch action {
	case "enable":
		if err := eng.EnableShadow(ctx, registrar, *role, basis); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("shadow mode ENABLED for role %s: its create/update/act/emit now become proposals for a human to grant or reject.\n", *role)
	case "disable":
		if err := eng.DisableShadow(ctx, registrar, *role, basis); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("shadow mode DISABLED for role %s: its mutations apply directly again.\n", *role)
	}
	fmt.Println("restart the node to pick up the change (v0)")
}
