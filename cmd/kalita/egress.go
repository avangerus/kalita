// Operator commands for the dead-lettered webhook deliveries (D-011 hardening,
// English-first release): `kalita egress dlq` lists deliveries that exhausted
// their retries; `kalita egress retry <id>` journals a fresh delivery of the
// SAME per-firing identity. Same one-shot family as `kalita shadow`/
// `kalita lockdown`: replays the journal into a fresh engine and exits. A
// RUNNING `kalita serve` holds its own in-memory Engine and does NOT re-fold
// the new task event — restart it to pick up a CLI-journaled retry (the same
// v0 limitation `kalita agent add`/`kalita lockdown` carry). To retry on a
// LIVE node without a restart, use the approver-gated REST/UI path
// (POST /api/egress/retry/{id}), which journals the retry through the running
// engine and folds it live.
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

func egressCmd(args []string) {
	if len(args) < 1 || (args[0] != "dlq" && args[0] != "retry") {
		fmt.Println("usage: kalita egress dlq [--pack <dir>] [--approver Owner]")
		fmt.Println("       kalita egress retry <task-id> [--pack <dir>] [--approver Owner]")
		fmt.Println("  dlq   list dead-lettered webhook deliveries (KALITA_PG_DSN required)")
		fmt.Println("  retry journal a fresh delivery of a dead-lettered webhook under the SAME idempotency identity")
		os.Exit(1)
	}
	action := args[0]
	fs := flag.NewFlagSet("egress "+action, flag.ExitOnError)
	pack := fs.String("pack", "", "pack directory (or comma-separated list) this node runs — needed to replay state")
	approverRole := fs.String("approver", "Owner", "role whose human signature applies definition changes AND may retry deliveries (must match how the node was started)")
	_ = fs.Parse(args[1:])
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
		log.Fatal("KALITA_PG_DSN is required: deliveries live in the journal")
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
	case "dlq":
		egressDlqCmd(eng)
	case "retry":
		if len(fs.Args()) < 1 {
			log.Fatal("task id required: kalita egress retry <task-id>")
		}
		taskID := fs.Args()[0]
		task, err := eng.RetryWebhookTask(ctx, registrar, taskID, basis)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("retry journaled: task %s -> %s (delivery identity %s, destination %s)\n",
			taskID, task.ID, task.DeliveryID, task.Args)
		fmt.Println("restart the node to pick up the CLI-journaled retry (v0); on a LIVE node use the Integrations page Retry button instead")
	}
}

// egressDlqCmd prints the dead-letter listing: one line per failed webhook
// delivery, newest first, with its per-firing identity and destination.
func egressDlqCmd(eng *engine.Engine) {
	tasks := eng.FailedWebhookTasks()
	if len(tasks) == 0 {
		fmt.Println("dead-letter queue is empty")
		return
	}
	fmt.Printf("%d dead-lettered webhook delivery(s):\n", len(tasks))
	for _, t := range tasks {
		created := ""
		if !t.CreatedAt.IsZero() {
			created = t.CreatedAt.UTC().Format("2006-01-02 15:04:05")
		}
		fmt.Printf("  %s  created %s  %s/%s -> %s  (delivery identity %s)\n",
			t.ID, created, t.Entity, t.RecordID, t.Args, t.DeliveryID)
		fmt.Printf("    retry: kalita egress retry %s\n", t.ID)
	}
}
