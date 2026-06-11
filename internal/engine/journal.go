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
		if d := e.can(actor.Role, "read", entity, "", rec.Values, actor.ID); !d.allowed {
			ok = false
		}
	}
	e.mu.RUnlock()
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: entity + " " + id + " not found"}
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
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out, nil
}
