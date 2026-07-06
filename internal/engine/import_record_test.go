package engine

import (
	"encoding/json"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// TestImportedRecordsSemantics is acceptance test 5 of
// docs/lld/LLD-03-upgrade-eras.md §6: on-create automation does not fire for
// an imported row, the serial counter continues from the imported row's
// value (max+1), and the row's origin is preserved in the journal — verified
// both live and after a fresh replay of the same journal.
func TestImportedRecordsSemantics(t *testing.T) {
	files := map[string]string{"pack.dsl": `
pack test_import
version 0.1.0
requires kalita >= 0.1

entity Doc:
    number: serial format="DOC-{seq:3}"
    title:  string required

roles:
    Owner
    Worker agent

permissions:
    Owner:
        full [Doc]
    Worker:
        read [Doc]
        deny [delete *]

automation:
    on create Doc:
        agent Worker: notify_new()
`}
	model, errs := dsl.Compile(files)
	if len(errs) > 0 {
		t.Fatalf("test pack must compile: %v", errs[0])
	}
	store := eventstore.NewMemStore(nil)
	owner := eventstore.Actor{Type: eventstore.ActorHuman, ID: "root", Role: "Owner"}
	migrator := eventstore.Actor{Type: eventstore.ActorHuman, ID: "migration", Role: "Owner"}
	migrationBasis := &eventstore.Basis{Type: "human", ID: "migration:era-import"}

	e, err := New(ctx, model, store)
	if err != nil {
		t.Fatal(err)
	}

	// import a row carrying the highest serial the old era ever reached
	origin := eventstore.RecordOrigin{EraID: "era-1", ID: "old-doc-7"}
	if err := e.ImportRecord(ctx, migrator, "Doc", "old-doc-7",
		map[string]any{"number": "DOC-007", "title": "Imported Doc"}, origin, migrationBasis); err != nil {
		t.Fatalf("ImportRecord: %v", err)
	}

	// 1. on-create automation must NOT have fired for the import
	if tasks := e.Tasks("Worker", TaskOpen); len(tasks) != 0 {
		t.Fatalf("on-create automation must not fire on import, got tasks: %+v", tasks)
	}

	// 2. a LIVE create must continue the serial from the imported max (007+1
	// = 008), proving nextSerial scans the projection regardless of which
	// kind put a row there
	created, err := e.Create(ctx, owner, "Doc", map[string]any{"title": "New Doc"}, basis, "")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Values["number"] != "DOC-008" {
		t.Fatalf("serial must continue from the imported row's max+1: want DOC-008, got %v", created.Values["number"])
	}

	// ...and, as a control, on-create automation DOES fire for a real create —
	// proving its absence above was import-specific, not a broken test rig.
	if tasks := e.Tasks("Worker", TaskOpen); len(tasks) != 1 {
		t.Fatalf("on-create automation must fire for a live Create, got tasks: %+v", tasks)
	}

	// 3. origin must be preserved in the journal, under the record.imported
	// kind (not record.created), with SchemaV stamped
	events, err := store.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var found *eventstore.Event
	for _, ev := range events {
		if ev.Kind == eventstore.RecordImported && ev.Subject.RecordID == "old-doc-7" {
			found = ev
		}
	}
	if found == nil {
		t.Fatal("no record.imported event found for old-doc-7")
	}
	if found.SchemaV != eventstore.CurrentSchemaV {
		t.Fatalf("record.imported must be written with SchemaV %d, got %d", eventstore.CurrentSchemaV, found.SchemaV)
	}
	var p eventstore.RecordImportedPayload
	if err := json.Unmarshal(found.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.Origin != origin {
		t.Fatalf("origin not preserved: want %+v, got %+v", origin, p.Origin)
	}

	// 4. a fresh replay of the same journal folds the imported row identically
	replayed, err := New(ctx, model, store)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	got, err := replayed.Get(ctx, owner, "Doc", "old-doc-7")
	if err != nil {
		t.Fatalf("replayed engine must project the imported row: %v", err)
	}
	if got.Values["title"] != "Imported Doc" || got.Values["number"] != "DOC-007" {
		t.Fatalf("replayed imported row values wrong: %+v", got.Values)
	}
}
