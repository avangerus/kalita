package engine

import (
	"crypto/ed25519"
	"os"
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// The week-4 acceptance test: the full debtor lifecycle with auto transitions,
// guards, an agent acting within its permissions, and a human signing the
// critical transition with a key — verified offline afterwards.

func collectionsEngine(t *testing.T) (*Engine, eventstore.Store, *identity.Registry, ed25519.PrivateKey) {
	t.Helper()
	files := map[string]string{}
	for _, name := range []string{"pack.kal", "collections.kal"} {
		raw, err := os.ReadFile("../../examples/collections/" + name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(raw)
	}
	model, errs := dsl.Compile(files)
	if len(errs) > 0 {
		t.Fatalf("collections must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	reg := identity.NewRegistry(store)

	// FinDirector registers with a key — approvals must be signed
	pub, priv, _ := identity.GenerateKey()
	registrar := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	if err := reg.Register(ctx, registrar, "fin-dir", eventstore.ActorHuman, "FinDirector", pub, nil); err != nil {
		t.Fatal(err)
	}

	clock := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	e, err := New(ctx, model, store,
		WithClock(func() time.Time { return clock }),
		WithVerifier(reg.VerifySignature), WithRequireSignatures())
	if err != nil {
		t.Fatal(err)
	}
	return e, store, reg, priv
}

var (
	anna      = eventstore.Actor{Type: eventstore.ActorHuman, ID: "anna", Role: "Accountant"}
	collector = eventstore.Actor{Type: eventstore.ActorAgent, ID: "collector-1", Role: "Collector"}
	finDir    = eventstore.Actor{Type: eventstore.ActorHuman, ID: "fin-dir", Role: "FinDirector"}
)

func TestDebtorLifecycle(t *testing.T) {
	e, store, _, priv := collectionsEngine(t)

	// overdue contract → debtor auto-moves OnTime → Overdue on creation
	contract, err := e.Create(ctx, anna, "Contract",
		map[string]any{"company": "Vector LLC", "due_date": "2026-05-01", "amount": 100000.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	debtor, err := e.Create(ctx, anna, "Debtor",
		map[string]any{"company": "Vector LLC", "contract": contract.ID, "debt": 100000.0}, basis, "")
	if err != nil {
		t.Fatal(err)
	}
	got, _ := e.Get(ctx, anna, "Debtor", debtor.ID)
	if got.Values["status"] != "Overdue" {
		t.Fatalf("auto transition must fire on create: want Overdue, got %v", got.Values["status"])
	}
	if days, ok := got.Values["overdue_days"].(float64); !ok || days < 40 {
		t.Fatalf("computed overdue_days must be ~42, got %v", got.Values["overdue_days"])
	}

	// the state field cannot be written directly — by anyone
	if _, err := e.Update(ctx, anna, "Debtor", debtor.ID, map[string]any{"status": "Settled"}, basis, ""); err == nil {
		t.Fatal("workflow field must reject direct writes")
	}

	// agent acts within its permissions: send_claim
	res, err := e.Act(ctx, collector, "Debtor", debtor.ID, "send_claim", basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "applied" || res.Record.Values["status"] != "Claim" {
		t.Fatalf("send_claim must apply: %+v", res)
	}

	// agent may NOT escalate (no act permission)
	if _, err := e.Act(ctx, collector, "Debtor", debtor.ID, "escalate", basis, ""); err == nil {
		t.Fatal("collector must not escalate")
	}

	// accountant initiates escalation → parked for approval
	res, err = e.Act(ctx, anna, "Debtor", debtor.ID, "escalate", basis, "")
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != "pending_approval" || res.ApprovalID == "" {
		t.Fatalf("escalate must park for approval: %+v", res)
	}
	mid, _ := e.Get(ctx, anna, "Debtor", debtor.ID)
	if mid.Values["status"] != "Claim" {
		t.Fatal("state must not move while approval is pending")
	}

	// approval visible in the FinDirector queue
	queue := e.PendingApprovals("FinDirector")
	if len(queue) != 1 || queue[0].ID != res.ApprovalID {
		t.Fatalf("approval queue: %+v", queue)
	}

	// accountant cannot sign it
	if _, err := e.Decide(ctx, anna, res.ApprovalID, true, nil, basis); err == nil {
		t.Fatal("accountant must not approve escalate")
	}
	// FinDirector without signature — rejected (verifier is wired)
	if _, err := e.Decide(ctx, finDir, res.ApprovalID, true, nil, basis); err == nil {
		t.Fatal("approval without signature must be rejected")
	}
	// FinDirector signs — transition applies
	sig := ed25519.Sign(priv, ApprovalMessage(res.ApprovalID, "granted"))
	rec, err := e.Decide(ctx, finDir, res.ApprovalID, true, sig, basis)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Values["status"] != "Legal" {
		t.Fatalf("after approval status must be Legal, got %v", rec.Values["status"])
	}

	// double-decide is final
	if _, err := e.Decide(ctx, finDir, res.ApprovalID, true, sig, basis); err == nil {
		t.Fatal("approval decisions are final")
	}

	// debt paid → auto any -> Settled
	if _, err := e.Update(ctx, anna, "Debtor", debtor.ID, map[string]any{"debt": 0.0}, basis, ""); err != nil {
		t.Fatal(err)
	}
	final, _ := e.Get(ctx, anna, "Debtor", debtor.ID)
	if final.Values["status"] != "Settled" {
		t.Fatalf("auto any->Settled must fire, got %v", final.Values["status"])
	}

	// offline verification: the granted event carries the signature and
	// verifies with only the journal and the public key
	events, _ := store.All(ctx)
	var grantedSig []byte
	var grantedID string
	for _, ev := range events {
		if ev.Kind == eventstore.ApprovalGranted {
			grantedSig = ev.Signature
			grantedID = ev.Subject.ApprovalID
		}
	}
	if grantedID == "" {
		t.Fatal("approval.granted must be in the journal")
	}
	pub := priv.Public().(ed25519.PublicKey)
	if !ed25519.Verify(pub, ApprovalMessage(grantedID, "granted"), grantedSig) {
		t.Fatal("approval signature must verify offline")
	}
	if err := eventstore.VerifyChain(events); err != nil {
		t.Fatalf("chain must verify: %v", err)
	}
}

func TestGuardFailed(t *testing.T) {
	src := `
entity Job:
    score: int
    status: enum[New, Done] default=New

roles:
    Owner

permissions:
    Owner:
        full [Job]
        act  [finish]

workflow Job on status:
    New -> Done: finish when score >= 100
`
	model, errs := dsl.Compile(map[string]string{"t.kal": src})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	e, err := New(ctx, model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "o", Role: "Owner"}
	job, _ := e.Create(ctx, owner, "Job", map[string]any{"score": 50.0}, basis, "")

	_, errAct := e.Act(ctx, owner, "Job", job.ID, "finish", basis, "")
	ge := wantCode(t, errAct, CodeGuardFailed)
	if ge.FixHint == "" {
		t.Fatal("guard failures must explain themselves")
	}

	if _, err := e.Update(ctx, owner, "Job", job.ID, map[string]any{"score": 150.0}, basis, ""); err != nil {
		t.Fatal(err)
	}
	if res, err := e.Act(ctx, owner, "Job", job.ID, "finish", basis, ""); err != nil || res.Record.Values["status"] != "Done" {
		t.Fatalf("guard satisfied must apply: %v %+v", err, res)
	}
}

func TestWorkflowReplay(t *testing.T) {
	e, store, _, priv := collectionsEngine(t)
	contract, _ := e.Create(ctx, anna, "Contract",
		map[string]any{"company": "V", "due_date": "2026-05-01"}, basis, "")
	debtor, _ := e.Create(ctx, anna, "Debtor",
		map[string]any{"company": "V", "contract": contract.ID, "debt": 1.0}, basis, "")
	_, _ = e.Act(ctx, collector, "Debtor", debtor.ID, "send_claim", basis, "")
	res, _ := e.Act(ctx, anna, "Debtor", debtor.ID, "escalate", basis, "")
	sig := ed25519.Sign(priv, ApprovalMessage(res.ApprovalID, "granted"))
	_, _ = e.Decide(ctx, finDir, res.ApprovalID, true, sig, basis)

	// rebuild from journal: states and approvals identical
	e2, err := New(ctx, e.Model(), store)
	if err != nil {
		t.Fatal(err)
	}
	rec, _ := e2.Get(ctx, anna, "Debtor", debtor.ID)
	if rec.Values["status"] != "Legal" {
		t.Fatalf("replayed status must be Legal, got %v", rec.Values["status"])
	}
	if len(e2.PendingApprovals("FinDirector")) != 0 {
		t.Fatal("decided approvals must not replay as pending")
	}
}
