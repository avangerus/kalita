package engine

import (
	"context"
	"strings"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/pii"
)

// LLD-16 acceptance (TestPiiFieldStoredEncrypted + TestEraseMakesUnreadableChainIntact
// at the engine level): a pii field is sealed in the journal (hash over
// ciphertext), read back decrypted, and after shredding the subject it reads as
// a tombstone while the hash chain stays intact.
func TestPIIEncryptAtRestAndShred(t *testing.T) {
	model, errs := dsl.Compile(map[string]string{"p.dsl": `
pack pii
version 0.1.0

entity Contract:
    counterparty: ref[core.User] required
    terms: text pii subject=counterparty

roles:
    Lawyer agent
    Lead

permissions:
    Lawyer:
        create [Contract]
        read [Contract]
        update [Contract]
        deny [delete *]
    Lead:
        full [Contract]
`})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	store := eventstore.NewMemStore(nil)
	piiStore, err := pii.NewStore("")
	if err != nil {
		t.Fatal(err)
	}
	eng, err := New(context.Background(), model, store, WithPIIStore(piiStore))
	if err != nil {
		t.Fatal(err)
	}
	actor := eventstore.Actor{Type: eventstore.ActorAgent, ID: "lawyer-1", Role: "Lawyer"}
	basis := &eventstore.Basis{Type: "task", ID: "t-1"}

	rec, err := eng.Create(context.Background(), actor, "Contract",
		map[string]any{"counterparty": "user-42", "terms": "ФИО Иванов, паспорт 1234"},
		basis, "idem-1")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Values["terms"] != "ФИО Иванов, паспорт 1234" {
		t.Fatalf("create must return decrypted terms: %v", rec.Values["terms"])
	}

	// the journal stores ciphertext, never the plaintext, and still verifies
	events, err := store.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var raw string
	for _, ev := range events {
		raw += string(ev.Payload)
	}
	if strings.Contains(raw, "Иванов") || strings.Contains(raw, "1234") {
		t.Fatalf("journal leaks pii plaintext: %s", raw)
	}
	if !strings.Contains(raw, "encrypted") {
		t.Fatalf("journal must store the sealed shape: %s", raw)
	}
	if err := store.Verify(context.Background()); err != nil {
		t.Fatalf("chain must verify (hash over ciphertext): %v", err)
	}

	// Get decrypts
	got, err := eng.Get(context.Background(), actor, "Contract", rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Values["terms"] != "ФИО Иванов, паспорт 1234" {
		t.Fatalf("Get must decrypt terms: %v", got.Values["terms"])
	}

	// shred the subject -> tombstone, chain still intact
	if err := piiStore.Destroy("user-42"); err != nil {
		t.Fatal(err)
	}
	got, err = eng.Get(context.Background(), actor, "Contract", rec.ID)
	if err != nil {
		t.Fatal(err)
	}
	terms, _ := got.Values["terms"].(string)
	if !strings.HasPrefix(terms, "[shredded:") {
		t.Fatalf("shredded subject must read as tombstone: %v", got.Values["terms"])
	}
	if err := store.Verify(context.Background()); err != nil {
		t.Fatalf("chain must stay intact after shred: %v", err)
	}
}

// TestPIIFieldTransplantBlocked (SEC-4, RC-fix): a sealed blob is bound to
// (subject, entity.field). Transplanting it to ANOTHER field of the same
// subject must not open — the pre-fix AAD bound only the subject, so an
// attacker with read access could move a sealed value between fields.
func TestPIIFieldTransplantBlocked(t *testing.T) {
	piiStore, err := pii.NewStore("")
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := piiStore.Seal("user-42", "Contract.terms", []byte("secret terms"))
	if err != nil {
		t.Fatal(err)
	}

	// the legitimate location opens
	if pt, ok := piiStore.Open(sealed, "Contract.terms"); !ok || string(pt) != "secret terms" {
		t.Fatalf("sealed value must open at its own location, got %q ok=%v", pt, ok)
	}
	// a transplant to another field of the SAME subject must fail
	if _, ok := piiStore.Open(sealed, "Contract.notes"); ok {
		t.Fatal("field transplant must NOT open (SEC-4)")
	}
	// a transplant to another entity must fail
	if _, ok := piiStore.Open(sealed, "Other.terms"); ok {
		t.Fatal("entity transplant must NOT open")
	}
	// a transplant to another subject must fail (pre-existing behavior)
	if _, ok := piiStore.Open(sealed, "user-43"); ok {
		t.Fatal("subject transplant must NOT open")
	}
}
