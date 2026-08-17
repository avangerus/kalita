package engine

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/pii"
)

// Regression: EraseSubject must timestamp the journaled subject.erased proof
// with the ENGINE's injectable clock (e.now, set via WithClock), not the wall
// clock — the old time.Now().UTC() made the event timestamp uncontrollable in
// tests and replay. The clock is far from "now" so a wall-clock implementation
// can never accidentally satisfy the assertion.
func TestEraseSubjectUsesEngineClock(t *testing.T) {
	model, errs := dsl.Compile(map[string]string{"p.dsl": `
pack erase-test
version 0.1.0

roles:
    Owner

permissions:
    Owner:
        erase [subject]
`})
	if len(errs) > 0 {
		t.Fatal(errs[0])
	}
	clock := time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC)
	store := eventstore.NewMemStore(nil)
	piiStore, err := pii.NewStore("")
	if err != nil {
		t.Fatal(err)
	}
	eng, err := New(context.Background(), model, store,
		WithClock(func() time.Time { return clock }), WithPIIStore(piiStore))
	if err != nil {
		t.Fatal(err)
	}
	actor := eventstore.Actor{Type: eventstore.ActorHuman, ID: "owner-1", Role: "Owner"}
	basis := &eventstore.Basis{Type: "human", ID: "owner-1"}
	if err := eng.EraseSubject(context.Background(), actor, "subject-1", "GDPR", basis, ""); err != nil {
		t.Fatal(err)
	}

	events, err := store.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range events {
		if ev.Kind != eventstore.SubjectErased {
			continue
		}
		found = true
		var p struct {
			At string `json:"at"`
		}
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			t.Fatal(err)
		}
		if p.At != clock.Format(time.RFC3339) {
			t.Fatalf("subject.erased at = %q, want engine clock %q (e.now, not time.Now)", p.At, clock.Format(time.RFC3339))
		}
	}
	if !found {
		t.Fatal("no subject.erased event journaled")
	}
}
