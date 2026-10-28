package engine

import (
	"context"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Journal returns the event history of a record, within read permissions:
// a record the actor cannot read has no history either (NOT_FOUND).
func (e *Engine) Journal(ctx context.Context, actor eventstore.Actor, entity, id string, limit int) ([]*eventstore.Event, error) {
	e.mu.RLock()
	rec, ok := e.records[entity][id]
	if ok {
		if d := e.can(actor, "read", entity, "", rec.Values); !d.allowed {
			ok = false
		}
	}
	e.mu.RUnlock()
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: entity + " " + id + " not found"}
	}

	out, err := e.recordEvents(ctx, entity, id)
	if err != nil {
		return nil, err
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}

// recordEvents returns one record's events ordered by seq — through the
// store's record index when it has one (LIM-3), else by filtering the journal.
func (e *Engine) recordEvents(ctx context.Context, entity, id string) ([]*eventstore.Event, error) {
	if rr, ok := e.store.(eventstore.RecordReader); ok {
		return rr.ForRecord(ctx, entity, id)
	}
	events, err := e.store.All(ctx)
	if err != nil {
		return nil, err
	}
	var out []*eventstore.Event
	for _, ev := range events {
		if ev.Subject.Entity == entity && ev.Subject.RecordID == id {
			out = append(out, ev)
		}
	}
	return out, nil
}
