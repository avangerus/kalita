package engine

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/google/uuid"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Comments: a polymorphic conversation thread attachable to ANY record — the
// dialogue surface for service desks, the discussion on a deal, the feedback on
// a candidate, and the way a human talks to an agent inside a task. Like links,
// comments live as journal events + a projection, NOT as entity fields, so they
// never bloat the schema and any entity gets a thread for free.
//
// Visibility: a comment is readable by anyone who can read its host record;
// an `internal` comment additionally requires the actor to have non-read
// rights on the host (i.e. staff, not the external customer) — the operator's
// private note the customer must not see.

type Comment struct {
	ID       string           `json:"id"`
	Entity   string           `json:"entity"`
	RecordID string           `json:"record_id"`
	Author   eventstore.Actor `json:"author"`
	Body     string           `json:"body"`
	Internal bool             `json:"internal"`
	TS       string           `json:"ts"`
}

type commentPayload struct {
	Entity   string `json:"entity"`
	RecordID string `json:"record_id"`
	Body     string `json:"body"`
	Internal bool   `json:"internal"`
}

// Comment posts a comment on a record. The actor must be able to read the host
// record; posting an internal comment requires write rights on the host.
func (e *Engine) Comment(ctx context.Context, actor eventstore.Actor, entity, id, body string, internal bool, basis *eventstore.Basis) (*Comment, error) {
	if basis == nil {
		return nil, &Err{Code: CodeBasisRequired, Message: "comment without basis",
			FixHint: "reference the task or human instruction"}
	}
	if body == "" {
		return nil, &Err{Code: CodeValidation, Message: "comment body is empty", FixHint: "write something"}
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	rec, ok := e.records[entity][id]
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: entity + " " + id + " not found"}
	}
	if d := e.can(actor.Role, "read", entity, "", rec.Values, actor.ID); !d.allowed {
		return nil, &Err{Code: CodeNotFound, Message: entity + " " + id + " not found"} // invisible host
	}
	if internal && !e.canWriteHost(actor, entity, rec.Values) {
		return nil, &Err{Code: CodePermissionDenied,
			Message: "only staff may post internal comments",
			Rule:    "internal comments require write rights on the host record"}
	}

	cid, err := uuid.NewV7()
	if err != nil {
		return nil, err
	}
	payload, _ := json.Marshal(commentPayload{Entity: entity, RecordID: id, Body: body, Internal: internal})
	ev, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:      actor,
		Kind:       eventstore.CommentPosted,
		Subject:    eventstore.Subject{Entity: entity, RecordID: id},
		Payload:    payload,
		Basis:      basis,
		DefVersion: e.defVersion,
	})
	if err != nil {
		return nil, err
	}
	c := &Comment{ID: cid.String(), Entity: entity, RecordID: id, Author: actor,
		Body: body, Internal: internal, TS: ev.TS.Format("2006-01-02T15:04:05Z07:00")}
	e.comments[entity+"|"+id] = append(e.comments[entity+"|"+id], c)
	return c, nil
}

// CommentsOf returns the thread on a record the actor may see: all comments if
// the actor has write rights on the host (staff), otherwise only the public
// ones (the external customer sees the conversation but not internal notes).
func (e *Engine) CommentsOf(actor eventstore.Actor, entity, id string) ([]*Comment, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	rec, ok := e.records[entity][id]
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: entity + " " + id + " not found"}
	}
	if d := e.can(actor.Role, "read", entity, "", rec.Values, actor.ID); !d.allowed {
		return nil, &Err{Code: CodeNotFound, Message: entity + " " + id + " not found"}
	}
	staff := e.canWriteHost(actor, entity, rec.Values)
	var out []*Comment
	for _, c := range e.comments[entity+"|"+id] {
		if c.Internal && !staff {
			continue
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// canWriteHost reports whether the actor can update the host record — the test
// that separates staff from an external customer for internal-comment access.
func (e *Engine) canWriteHost(actor eventstore.Actor, entity string, values map[string]any) bool {
	return e.can(actor.Role, "update", entity, "", values, actor.ID).allowed
}

// applyCommentEvent folds a comment into the projection on replay.
func (e *Engine) applyCommentEvent(ev *eventstore.Event) {
	var p commentPayload
	if json.Unmarshal(ev.Payload, &p) != nil {
		return
	}
	key := p.Entity + "|" + p.RecordID
	e.comments[key] = append(e.comments[key], &Comment{
		ID: ev.EventID, Entity: p.Entity, RecordID: p.RecordID, Author: ev.Actor,
		Body: p.Body, Internal: p.Internal, TS: ev.TS.Format("2006-01-02T15:04:05Z07:00"),
	})
}
