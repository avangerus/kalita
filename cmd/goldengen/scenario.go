package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// scenarioResult bundles what the caller needs to dump: the folded engine,
// the actor registry folded over the same store, and the store itself (to
// read back the recorded journal).
type scenarioResult struct {
	Engine   *engine.Engine
	Registry *identity.Registry
	Store    eventstore.Store
}

// buildScenario runs the fixed v0.1 golden scenario end to end on a MemStore
// with a fixed, manually-advanced clock (LLD-03 §3). It touches only public
// engine/eventstore/dsl/identity API — no production code under internal/ is
// modified for this.
//
// Coverage by design: record creation, workflow transitions (plain, auto-
// assignee, approval-gated, direct-reject), HITL approve (granted signed,
// rejected unsigned, left pending), automation (on create, on update, on
// schedule, on stuck — all four trigger kinds reachable via the engine's
// public API; "on delete" is skipped because there is no Delete API to
// trigger it), the full task lifecycle (created/taken/progress/completed/
// failed/expired), undo via Revert (field edit AND transition) and RewindTo
// (multi-step), comments (public and internal), a file field (set at create
// and via update), and additive definition evolution mid-journal (proposed/
// approved and proposed/rejected).
func buildScenario(ctx context.Context) (*scenarioResult, error) {
	clock := time.Date(2026, 1, 5, 9, 0, 0, 0, time.UTC)
	now := func() time.Time { return clock }
	advance := func(d time.Duration) { clock = clock.Add(d) }

	model, errs := dsl.Compile(map[string]string{"pack.dsl": packV1Source})
	if len(errs) > 0 {
		return nil, fmt.Errorf("genesis pack does not compile: %v", errs[0])
	}

	store := eventstore.NewMemStore(now)
	reg := identity.NewRegistry(store)
	eng, err := engine.New(ctx, model, store,
		engine.WithClock(now),
		engine.WithVerifier(reg.VerifySignature),
		engine.WithTaskTTL(30*time.Minute))
	if err != nil {
		return nil, err
	}

	// --- actors ------------------------------------------------------------
	root := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	bootBasis := &eventstore.Basis{Type: "human", ID: "root"}

	ownerSeed := sha256.Sum256([]byte("kalita-golden-v0.1-owner-1"))
	ownerPriv := ed25519.NewKeyFromSeed(ownerSeed[:])
	ownerPub := ownerPriv.Public().(ed25519.PublicKey)

	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "owner-1", Role: "Owner"}
	reviewer := eventstore.Actor{Type: eventstore.ActorHuman, ID: "reviewer-1", Role: "Reviewer"}
	worker := eventstore.Actor{Type: eventstore.ActorAgent, ID: "worker-1", Role: "Worker"}

	if err := reg.Register(ctx, root, owner.ID, owner.Type, owner.Role, ownerPub, bootBasis); err != nil {
		return nil, fmt.Errorf("register owner-1: %w", err)
	}
	if err := reg.Register(ctx, root, reviewer.ID, reviewer.Type, reviewer.Role, nil, bootBasis); err != nil {
		return nil, fmt.Errorf("register reviewer-1: %w", err)
	}
	if err := reg.Register(ctx, root, worker.ID, worker.Type, worker.Role, nil, bootBasis); err != nil {
		return nil, fmt.Errorf("register worker-1: %w", err)
	}

	humanBasis := func(id string) *eventstore.Basis { return &eventstore.Basis{Type: "human", ID: id} }
	taskBasis := func(id string) *eventstore.Basis { return &eventstore.Basis{Type: "task", ID: id} }
	ruleBasis := func(id string) *eventstore.Basis { return &eventstore.Basis{Type: "rule", ID: id} }
	adrBasis := func(id string) *eventstore.Basis { return &eventstore.Basis{Type: "adr", ID: id} }

	// --- definition evolution ------------------------------------------------
	advance(time.Minute)
	propA, cerrs, err := eng.ProposeChange(ctx, reviewer, map[string]string{"pack.dsl": packV2Source},
		eng.DefVersion(), "add Case.resolution for closing notes", humanBasis(reviewer.ID), "")
	if err != nil {
		return nil, fmt.Errorf("propose A: %w", err)
	}
	if len(cerrs) > 0 {
		return nil, fmt.Errorf("proposal A does not compile: %v", cerrs[0])
	}
	sigA := ed25519.Sign(ownerPriv, engine.DefinitionMessage(propA.ID, "granted"))
	if _, err := eng.DecideProposal(ctx, owner, propA.ID, true, sigA, humanBasis(owner.ID)); err != nil {
		return nil, fmt.Errorf("decide A: %w", err)
	}

	advance(time.Minute)
	propB, cerrs, err := eng.ProposeChange(ctx, reviewer, map[string]string{"pack.dsl": packV3Source},
		eng.DefVersion(), "add Memo.tag (speculative)", humanBasis(reviewer.ID), "")
	if err != nil {
		return nil, fmt.Errorf("propose B: %w", err)
	}
	if len(cerrs) > 0 {
		return nil, fmt.Errorf("proposal B does not compile: %v", cerrs[0])
	}
	if _, err := eng.DecideProposal(ctx, owner, propB.ID, false, nil, humanBasis(owner.ID)); err != nil {
		return nil, fmt.Errorf("decide B: %w", err)
	}

	// --- file refs (no BlobStore needed: only the ref is journaled) --------
	file1 := engine.MarshalFileRef(&engine.FileRef{
		Hash: sha256hex("golden-fixture:brief.pdf:v1"), Name: "brief.pdf", Size: 48213, Mime: "application/pdf",
	})
	file2 := engine.MarshalFileRef(&engine.FileRef{
		Hash: sha256hex("golden-fixture:audit-notes.txt:v1"), Name: "audit-notes.txt", Size: 2048, Mime: "text/plain",
	})

	// --- create cases --------------------------------------------------------
	advance(2 * time.Minute)
	case1, err := eng.Create(ctx, reviewer, "Case", map[string]any{
		"title": "Q1 Vendor Onboarding", "notes": "initial intake", "opened_on": "2026-01-05",
		"amount": 12000.0, "attachment": file1,
	}, humanBasis(reviewer.ID), "")
	if err != nil {
		return nil, fmt.Errorf("create case1: %w", err)
	}

	advance(30 * time.Second)
	case2, err := eng.Create(ctx, owner, "Case", map[string]any{
		"title": "Facilities Repair Request", "opened_on": "2026-01-05",
		"amount": map[string]any{"amount": 3400.5, "currency": "USD"},
	}, humanBasis(owner.ID), "")
	if err != nil {
		return nil, fmt.Errorf("create case2: %w", err)
	}

	advance(30 * time.Second)
	case3, err := eng.Create(ctx, reviewer, "Case", map[string]any{
		"title": "Annual Audit Prep", "opened_on": "2026-01-05", "amount": 0.0,
	}, humanBasis(reviewer.ID), "")
	if err != nil {
		return nil, fmt.Errorf("create case3: %w", err)
	}

	advance(30 * time.Second)
	if _, err := eng.Update(ctx, reviewer, "Case", case3.ID, map[string]any{"attachment": file2}, humanBasis(reviewer.ID), ""); err != nil {
		return nil, fmt.Errorf("update case3 attachment: %w", err)
	}

	advance(30 * time.Second)
	case4, err := eng.Create(ctx, owner, "Case", map[string]any{
		"title": "Vendor Contract Renewal", "opened_on": "2026-01-05", "amount": 8600.0,
	}, humanBasis(owner.ID), "")
	if err != nil {
		return nil, fmt.Errorf("create case4: %w", err)
	}

	advance(30 * time.Second)
	case5, err := eng.Create(ctx, reviewer, "Case", map[string]any{
		"title": "Office Lease Amendment", "opened_on": "2026-01-05", "amount": 0.0,
	}, humanBasis(reviewer.ID), "")
	if err != nil {
		return nil, fmt.Errorf("create case5: %w", err)
	}

	advance(30 * time.Second)
	case6, err := eng.Create(ctx, owner, "Case", map[string]any{
		"title": "Insurance Policy Review", "notes": "n0", "opened_on": "2026-01-05", "amount": 0.0,
	}, humanBasis(owner.ID), "")
	if err != nil {
		return nil, fmt.Errorf("create case6: %w", err)
	}
	createSeq6, _, err := store.Head(ctx)
	if err != nil {
		return nil, err
	}

	// --- worker handles case1's auto-queued tasks; memos --------------------
	advance(time.Minute)
	openTasksWorker := eng.Tasks("Worker", engine.TaskOpen)
	openTask1 := findTask(openTasksWorker, case1.ID, "open")
	triageTask1 := findTask(openTasksWorker, case1.ID, "triage")
	if openTask1 == nil || triageTask1 == nil {
		return nil, fmt.Errorf("expected case1's on-create tasks to exist, got %d open Worker tasks", len(openTasksWorker))
	}
	if _, err := eng.TakeTask(ctx, worker, openTask1.ID, ""); err != nil {
		return nil, fmt.Errorf("take case1 open task: %w", err)
	}
	if _, err := eng.Act(ctx, worker, "Case", case1.ID, "open", taskBasis(openTask1.ID), ""); err != nil {
		return nil, fmt.Errorf("act open case1: %w", err)
	}
	if err := eng.CompleteTask(ctx, worker, openTask1.ID, "opened", ""); err != nil {
		return nil, fmt.Errorf("complete case1 open task: %w", err)
	}

	if _, err := eng.TakeTask(ctx, worker, triageTask1.ID, ""); err != nil {
		return nil, fmt.Errorf("take case1 triage task: %w", err)
	}
	if _, err := eng.Create(ctx, worker, "Memo", map[string]any{
		"case": case1.ID, "body": "vendor W9 received", "author": "worker-1",
	}, taskBasis(triageTask1.ID), ""); err != nil {
		return nil, fmt.Errorf("create memo1: %w", err)
	}
	if err := eng.ReportProgress(ctx, worker, triageTask1.ID, "vendor W9 received, memo filed", ""); err != nil {
		return nil, fmt.Errorf("report progress case1 triage: %w", err)
	}
	if err := eng.CompleteTask(ctx, worker, triageTask1.ID, "triaged", ""); err != nil {
		return nil, fmt.Errorf("complete case1 triage task: %w", err)
	}

	if _, err := eng.Create(ctx, owner, "Memo", map[string]any{
		"case": case4.ID, "body": "facilities vendor shortlist attached to intake checklist", "author": "owner-1",
	}, ruleBasis("case-intake-checklist"), ""); err != nil {
		return nil, fmt.Errorf("create memo2: %w", err)
	}
	if _, err := eng.Create(ctx, owner, "Memo", map[string]any{
		"case": case5.ID, "body": "lease amendment follows the standard template", "author": "owner-1",
	}, adrBasis("ADR-014-case-intake"), ""); err != nil {
		return nil, fmt.Errorf("create memo3: %w", err)
	}

	// --- task expiry: case2's open task is taken, then left to expire ------
	advance(time.Minute)
	openTask2 := findTask(eng.Tasks("Worker", engine.TaskOpen), case2.ID, "open")
	if openTask2 == nil {
		return nil, fmt.Errorf("expected case2's open task to exist")
	}
	if _, err := eng.TakeTask(ctx, worker, openTask2.ID, ""); err != nil {
		return nil, fmt.Errorf("take case2 open task: %w", err)
	}

	advance(40 * time.Minute) // past the 30m task TTL
	if err := eng.Tick(ctx); err != nil {
		return nil, fmt.Errorf("tick (lease sweep): %w", err)
	}

	advance(time.Minute)
	if _, err := eng.Act(ctx, worker, "Case", case2.ID, "open", humanBasis(worker.ID), ""); err != nil {
		return nil, fmt.Errorf("act open case2: %w", err)
	}

	triageTask3 := findTask(eng.Tasks("Worker", engine.TaskOpen), case3.ID, "triage")
	if triageTask3 == nil {
		return nil, fmt.Errorf("expected case3's triage task to exist")
	}
	if _, err := eng.TakeTask(ctx, worker, triageTask3.ID, ""); err != nil {
		return nil, fmt.Errorf("take case3 triage task: %w", err)
	}
	if err := eng.FailTask(ctx, worker, triageTask3.ID, "cannot identify auditor of record", ""); err != nil {
		return nil, fmt.Errorf("fail case3 triage task: %w", err)
	}

	if _, err := eng.Act(ctx, worker, "Case", case3.ID, "open", humanBasis(worker.ID), ""); err != nil {
		return nil, fmt.Errorf("act open case3: %w", err)
	}
	if _, err := eng.Act(ctx, worker, "Case", case4.ID, "open", humanBasis(worker.ID), ""); err != nil {
		return nil, fmt.Errorf("act open case4: %w", err)
	}
	if _, err := eng.Act(ctx, worker, "Case", case5.ID, "open", humanBasis(worker.ID), ""); err != nil {
		return nil, fmt.Errorf("act open case5: %w", err)
	}

	// --- update-triggered automation + start of the case6 rewind flurry ----
	advance(time.Minute)
	if _, err := eng.Update(ctx, reviewer, "Case", case3.ID, map[string]any{"priority": "High"}, humanBasis(reviewer.ID), ""); err != nil {
		return nil, fmt.Errorf("update case3 priority: %w", err)
	}
	if _, err := eng.Update(ctx, owner, "Case", case6.ID, map[string]any{"notes": "n1"}, humanBasis(owner.ID), ""); err != nil {
		return nil, fmt.Errorf("update case6 notes n1: %w", err)
	}

	advance(time.Minute)
	if _, err := eng.Update(ctx, owner, "Case", case6.ID, map[string]any{
		"notes": "n2", "title": "Insurance Policy Review — Renewal",
	}, humanBasis(owner.ID), ""); err != nil {
		return nil, fmt.Errorf("update case6 notes n2: %w", err)
	}
	if _, err := eng.Act(ctx, worker, "Case", case6.ID, "open", humanBasis(worker.ID), ""); err != nil {
		return nil, fmt.Errorf("act open case6: %w", err)
	}
	if _, err := eng.Act(ctx, reviewer, "Case", case6.ID, "submit", humanBasis(reviewer.ID), ""); err != nil {
		return nil, fmt.Errorf("act submit case6: %w", err)
	}

	// --- schedule automation: case1..case5 are all Active here --------------
	advance(5 * time.Minute)
	if err := eng.Tick(ctx); err != nil {
		return nil, fmt.Errorf("tick (schedule): %w", err)
	}

	advance(time.Minute)
	if _, err := eng.RewindTo(ctx, owner, "Case", case6.ID, createSeq6, humanBasis(owner.ID), ""); err != nil {
		return nil, fmt.Errorf("rewind case6: %w", err)
	}

	// --- submit / finalize / reject ------------------------------------------
	advance(time.Minute)
	for _, c := range []*engine.Record{case1, case2, case3, case5} {
		if _, err := eng.Act(ctx, reviewer, "Case", c.ID, "submit", humanBasis(reviewer.ID), ""); err != nil {
			return nil, fmt.Errorf("act submit %s: %w", c.ID, err)
		}
	}

	advance(time.Minute)
	res1, err := eng.Act(ctx, reviewer, "Case", case1.ID, "finalize", humanBasis(reviewer.ID), "")
	if err != nil {
		return nil, fmt.Errorf("act finalize case1: %w", err)
	}
	res2, err := eng.Act(ctx, reviewer, "Case", case2.ID, "finalize", humanBasis(reviewer.ID), "")
	if err != nil {
		return nil, fmt.Errorf("act finalize case2: %w", err)
	}
	if _, err := eng.Act(ctx, reviewer, "Case", case5.ID, "finalize", humanBasis(reviewer.ID), ""); err != nil {
		return nil, fmt.Errorf("act finalize case5: %w", err) // left pending forever
	}
	if _, err := eng.Act(ctx, reviewer, "Case", case3.ID, "reject_case", humanBasis(reviewer.ID), ""); err != nil {
		return nil, fmt.Errorf("act reject_case case3: %w", err)
	}

	advance(time.Minute)
	sig1 := ed25519.Sign(ownerPriv, engine.ApprovalMessage(res1.ApprovalID, "granted"))
	if _, err := eng.Decide(ctx, owner, res1.ApprovalID, true, sig1, humanBasis(owner.ID)); err != nil {
		return nil, fmt.Errorf("decide case1 approval: %w", err)
	}
	if _, err := eng.Decide(ctx, owner, res2.ApprovalID, false, nil, humanBasis(owner.ID)); err != nil {
		return nil, fmt.Errorf("decide case2 approval: %w", err)
	}

	// --- resolution field (from the evolved definition) + comments ---------
	advance(time.Minute)
	if _, err := eng.Update(ctx, owner, "Case", case1.ID, map[string]any{
		"resolution": "Approved after W9 and banking details verified.",
	}, humanBasis(owner.ID), ""); err != nil {
		return nil, fmt.Errorf("update case1 resolution: %w", err)
	}
	if _, err := eng.Comment(ctx, reviewer, "Case", case1.ID, "Please confirm vendor banking details on file.", false, humanBasis(reviewer.ID), ""); err != nil {
		return nil, fmt.Errorf("comment case1 public: %w", err)
	}
	if _, err := eng.Comment(ctx, owner, "Case", case1.ID, "Approved after W9 and banking details verified against the ledger.", true, humanBasis(owner.ID), ""); err != nil {
		return nil, fmt.Errorf("comment case1 internal: %w", err)
	}

	// --- undo: a transition (case3) and a field edit (case4) ----------------
	advance(time.Minute)
	if _, err := eng.Revert(ctx, owner, "Case", case3.ID, humanBasis(owner.ID), ""); err != nil {
		return nil, fmt.Errorf("revert case3 reject_case: %w", err)
	}

	advance(time.Minute)
	if _, err := eng.Act(ctx, owner, "Case", case4.ID, "close", humanBasis(owner.ID), ""); err != nil {
		return nil, fmt.Errorf("act close case4: %w", err)
	}
	if _, err := eng.Update(ctx, owner, "Case", case4.ID, map[string]any{"notes": "wrap-up complete, no further action"}, humanBasis(owner.ID), ""); err != nil {
		return nil, fmt.Errorf("update case4 notes: %w", err)
	}

	advance(time.Minute)
	if _, err := eng.Revert(ctx, owner, "Case", case4.ID, humanBasis(owner.ID), ""); err != nil {
		return nil, fmt.Errorf("revert case4 notes: %w", err)
	}

	// --- stuck automation: case2/case3/case5 have been sitting in Review ---
	advance(72 * time.Hour)
	if err := eng.Tick(ctx); err != nil {
		return nil, fmt.Errorf("tick (stuck): %w", err)
	}

	return &scenarioResult{Engine: eng, Registry: reg, Store: store}, nil
}

// findTask locates an open task for a given record + workflow/agent action —
// deterministic lookup instead of relying on map/slice iteration order.
func findTask(tasks []*engine.Task, recordID, action string) *engine.Task {
	for _, t := range tasks {
		if t.RecordID == recordID && t.Action == action {
			return t
		}
	}
	return nil
}

func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
