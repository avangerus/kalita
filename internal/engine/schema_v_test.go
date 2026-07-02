package engine

import (
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// LLD-01 §1.1: "неизвестный БОЛЬШИЙ schema_v при replay = ошибка старта (не
// тихий пропуск)". This is the hard-stop half of the fold rule; the "accepts
// everything <= its own" half is exercised implicitly by every other test in
// this package replaying schema_v-0 journals successfully.

func compileTestModel(t *testing.T) *dsl.Model {
	t.Helper()
	model, errs := dsl.Compile(map[string]string{"test.dsl": testPack})
	if len(errs) > 0 {
		t.Fatalf("compile: %v", errs[0])
	}
	return model
}

func TestNewRefusesUnknownGreaterSchemaV(t *testing.T) {
	store := eventstore.NewMemStore(nil)
	if _, err := store.Append(ctx, eventstore.AppendInput{
		Actor:   eventstore.Actor{Type: eventstore.ActorSystem, ID: "future-node"},
		Kind:    eventstore.RecordCreated,
		Subject: eventstore.Subject{Entity: "Doc", RecordID: "d-1"},
		SchemaV: eventstore.CurrentSchemaV + 1,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := New(ctx, compileTestModel(t), store); err == nil {
		t.Fatal("New must refuse to start on a journal with schema_v greater than this binary supports")
	}
}

func TestNewAcceptsCurrentAndLowerSchemaV(t *testing.T) {
	store := eventstore.NewMemStore(nil)
	// schema_v 0 (absent — every pre-LLD-01 event) ...
	if _, err := store.Append(ctx, eventstore.AppendInput{
		Actor: eventstore.Actor{Type: eventstore.ActorHuman, ID: "a"}, Kind: eventstore.RecordCreated,
		Subject: eventstore.Subject{Entity: "Doc", RecordID: "d-1"},
	}); err != nil {
		t.Fatal(err)
	}
	// ... and schema_v == CurrentSchemaV must both replay without error.
	if _, err := store.Append(ctx, eventstore.AppendInput{
		Actor: eventstore.Actor{Type: eventstore.ActorSystem, ID: "engine"}, Kind: eventstore.RecordCreated,
		Subject: eventstore.Subject{Entity: "Doc", RecordID: "d-2"}, SchemaV: eventstore.CurrentSchemaV,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := New(ctx, compileTestModel(t), store); err != nil {
		t.Fatalf("New must accept schema_v <= CurrentSchemaV: %v", err)
	}
}
