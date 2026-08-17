package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/avangerus/kalita/internal/eventstore"
)

// kalita trace — observability (Master Goal §10, LLD-09): render the journal as
// an execution timeline, per record or node-wide. Every substantive action is a
// journaled event (what / when / who / which agent / which tool-effect / which
// decision / who confirmed), so the timeline IS the trace — no separate tracing
// substrate needed. `--format json` emits machine-readable events for a
// cluster-level collector (federation pulls the same journal via bridge).

func traceCmd(args []string) {
	fs := flag.NewFlagSet("trace", flag.ExitOnError)
	entity := fs.String("entity", "", "entity to trace (with --id)")
	id := fs.String("id", "", "record id to trace (with --entity)")
	since := fs.Duration("since", 0, "node-wide: events newer than this duration (without --entity/--id)")
	format := fs.String("format", "text", "text | json")
	limit := fs.Int("limit", 500, "max events")
	_ = fs.Parse(args)

	dsn := resolveDSN()
	if dsn == "" {
		log.Fatal("KALITA_PG_DSN is required: trace reads the journal")
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

	// filter
	var out []*eventstore.Event
	if *entity != "" && *id != "" {
		for _, ev := range events {
			if ev.Subject.Entity == *entity && ev.Subject.RecordID == *id {
				out = append(out, ev)
			}
		}
	} else {
		cutoff := time.Now().Add(-*since)
		for _, ev := range events {
			if *since > 0 && ev.TS.Before(cutoff) {
				continue
			}
			out = append(out, ev)
		}
	}
	if len(out) > *limit {
		out = out[len(out)-*limit:]
	}

	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		for _, ev := range out {
			_ = enc.Encode(map[string]any{
				"seq": ev.Seq, "ts": ev.TS.UTC().Format(time.RFC3339), "kind": string(ev.Kind),
				"actor": ev.Actor, "subject": ev.Subject, "payload": json.RawMessage(ev.Payload),
				"basis": ev.Basis, "hash": fmt.Sprintf("%x", ev.Hash),
			})
		}
		return
	}

	for _, ev := range out {
		fmt.Printf("#%-6d %-24s %-28s %s\n", ev.Seq, ev.TS.UTC().Format("2006-01-02T15:04:05Z"),
			actorLabel(ev.Actor), describeEvent(ev))
	}
	if len(out) == 0 {
		fmt.Println("no events")
	}
}

func actorLabel(a eventstore.Actor) string {
	typ := string(a.Type)
	role := a.Role
	if role == "" {
		role = "?"
	}
	return fmt.Sprintf("%s:%s (%s)", typ, a.ID, role)
}

// describeEvent renders one journaled fact as a readable line.
func describeEvent(ev *eventstore.Event) string {
	var p struct {
		Action   string         `json:"action"`
		From     string         `json:"from"`
		To       string         `json:"to"`
		Status   string         `json:"status"`
		Approved bool           `json:"approved"`
		Result   string         `json:"result"`
		Reason   string         `json:"reason"`
		Note     string         `json:"note"`
		Subject  string         `json:"subject"`
		Values   map[string]any `json:"values"`
		Changes  []map[string]any `json:"changes"`
		Error    map[string]any `json:"error"`
	}
	_ = json.Unmarshal(ev.Payload, &p)
	subj := ev.Subject.Entity + "/" + shortID(ev.Subject.RecordID)

	short := map[string]string{
		"checkpoint.sealed": "checkpoint sealed (anchor)",
		"node.started":      "node started",
		"era.genesis":       "era genesis",
		"subject.erased":    "subject erased: " + p.Subject,
	}
	if s, ok := short[string(ev.Kind)]; ok {
		return s
	}
	switch ev.Kind {
	case eventstore.RecordCreated:
		return "created " + subj
	case eventstore.RecordUpdated:
		return "updated " + subj + " (" + plural(len(p.Changes), "field") + " changed)"
	case eventstore.RecordAction:
		return fmt.Sprintf("%s transition %s: %s -> %s (%s)", subj, p.Action, p.From, p.To, p.Status)
	case eventstore.ApprovalRequested:
		return "approval requested on " + subj + " (" + p.Action + ")"
	case eventstore.ApprovalGranted:
		return "APPROVED by " + actorLabel(ev.Actor) + " on " + subj + " (" + p.Action + ")"
	case eventstore.ApprovalRejected:
		return "REJECTED by " + actorLabel(ev.Actor) + " on " + subj + " (" + p.Action + ")"
	case eventstore.TaskCreated:
		return "task created for " + subj + " (" + p.Action + ")"
	case eventstore.TaskTaken:
		return "task taken by " + actorLabel(ev.Actor)
	case eventstore.TaskCompleted:
		return "task completed: " + truncate(p.Result, 60)
	case eventstore.TaskFailed:
		return "task FAILED: " + truncate(p.Reason, 60)
	case eventstore.CommentPosted:
		return "comment on " + subj + ": " + truncate(p.Note, 60)
	case eventstore.DefinitionProposed:
		return "pack proposal " + shortID(p.Subject) + " parked for signature"
	case eventstore.DefinitionApplied:
		return "pack applied (def_version " + itoa(ev.DefVersion) + ")"
	case eventstore.IngestFlagged:
		return "ingest flagged on " + subj
	case eventstore.ActorRegistered:
		return "actor registered: " + p.Subject
	case eventstore.ActorDisabled:
		return "actor disabled: " + p.Subject
	case eventstore.NodeLockdown:
		return "LOCKDOWN activated"
	case eventstore.NodeLockdownLifted:
		return "lockdown lifted"
	}
	return string(ev.Kind) + " " + subj
}

func shortID(s string) string {
	if len(s) <= 8 {
		return s
	}
	return s[:8]
}

func plural(n int, what string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", what)
	}
	return fmt.Sprintf("%d %ss", n, what)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func itoa(v uint64) string {
	return fmt.Sprintf("%d", v)
}

