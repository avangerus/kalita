package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Named bidirectional links between records (Jira issue links). A link is one
// undirected fact stored once; both records see it under their respective
// name. Links live as journal events (link.added/removed) and as a projection;
// they are NOT entity fields, so they never bloat the record schema.

type linkPayload struct {
	Link string `json:"link"` // the LinkDecl forward name (canonical)
	From string `json:"from"` // canonical: the LinkDecl.From record
	To   string `json:"to"`   // canonical: the LinkDecl.To record
}

// LinkedRecord is one end of a link as seen from a record.
type LinkedRecord struct {
	Name     string `json:"name"`   // the name from THIS record's side
	Entity   string `json:"entity"`
	RecordID string `json:"record_id"`
}

// Link connects two records under a named relation. The name may be either the
// forward or the inverse; the engine stores it canonically.
func (e *Engine) Link(ctx context.Context, actor eventstore.Actor, entity, id, name, otherID string, basis *eventstore.Basis) error {
	if basis == nil {
		return &Err{Code: CodeBasisRequired, Message: "link without basis", FixHint: "reference the task or instruction"}
	}
	e.mu.Lock()
	defer e.mu.Unlock()

	decl, forward, ok := e.model.LinkByName(entity, name)
	if !ok {
		return &Err{Code: CodeValidation, Message: fmt.Sprintf("%s has no link named %s", entity, name),
			FixHint: "declare `link ... as " + name + " / ...` or use an existing link name"}
	}
	// permission: the actor must be able to update the side they act on
	if d := e.can(actor.Role, "update", entity, "", nil, actor.ID); !d.allowed {
		return denied(actor.Role, "update", entity, d.rule)
	}
	// canonical orientation: from = decl.From record, to = decl.To record
	fromID, toID := id, otherID
	if !forward {
		fromID, toID = otherID, id
	}
	if e.records[decl.From][fromID] == nil || e.records[decl.To][toID] == nil {
		return &Err{Code: CodeNotFound, Message: "one of the linked records does not exist"}
	}
	key := decl.Forward + "|" + fromID + "|" + toID
	if _, exists := e.links[key]; exists {
		return nil // the link already holds; one fact, idempotent
	}
	payload, _ := json.Marshal(linkPayload{Link: decl.Forward, From: fromID, To: toID})
	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:      actor,
		Kind:       eventstore.LinkAdded,
		Subject:    eventstore.Subject{Entity: entity, RecordID: id},
		Payload:    payload,
		Basis:      basis,
		DefVersion: e.defVersion,
	}); err != nil {
		return err
	}
	e.links[key] = linkPayload{Link: decl.Forward, From: fromID, To: toID}
	return nil
}

// Unlink removes a named link.
func (e *Engine) Unlink(ctx context.Context, actor eventstore.Actor, entity, id, name, otherID string, basis *eventstore.Basis) error {
	if basis == nil {
		return &Err{Code: CodeBasisRequired, Message: "unlink without basis"}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	decl, forward, ok := e.model.LinkByName(entity, name)
	if !ok {
		return &Err{Code: CodeValidation, Message: fmt.Sprintf("%s has no link named %s", entity, name)}
	}
	if d := e.can(actor.Role, "update", entity, "", nil, actor.ID); !d.allowed {
		return denied(actor.Role, "update", entity, d.rule)
	}
	fromID, toID := id, otherID
	if !forward {
		fromID, toID = otherID, id
	}
	key := decl.Forward + "|" + fromID + "|" + toID
	if _, exists := e.links[key]; !exists {
		return &Err{Code: CodeNotFound, Message: "no such link"}
	}
	payload, _ := json.Marshal(linkPayload{Link: decl.Forward, From: fromID, To: toID})
	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor: actor, Kind: eventstore.LinkRemoved,
		Subject: eventstore.Subject{Entity: entity, RecordID: id},
		Payload: payload, Basis: basis, DefVersion: e.defVersion,
	}); err != nil {
		return err
	}
	delete(e.links, key)
	return nil
}

// LinksOf returns the links visible from a record, each under the name that
// record sees (forward name on the From side, inverse on the To side).
func (e *Engine) LinksOf(actor eventstore.Actor, entity, id string) []LinkedRecord {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var out []LinkedRecord
	for _, lp := range e.links {
		var decl = e.findLinkDecl(lp.Link)
		if decl == nil {
			continue
		}
		switch {
		case decl.From == entity && lp.From == id:
			if e.canReadRecord(actor, decl.To, lp.To) {
				out = append(out, LinkedRecord{Name: decl.Forward, Entity: decl.To, RecordID: lp.To})
			}
		case decl.To == entity && lp.To == id:
			if e.canReadRecord(actor, decl.From, lp.From) {
				out = append(out, LinkedRecord{Name: decl.Inverse, Entity: decl.From, RecordID: lp.From})
			}
		}
	}
	return out
}

func (e *Engine) findLinkDecl(forward string) *linkDeclLite {
	for _, l := range e.model.Links {
		if l.Forward == forward {
			return &linkDeclLite{From: l.From, To: l.To, Forward: l.Forward, Inverse: l.Inverse}
		}
	}
	return nil
}

type linkDeclLite struct{ From, To, Forward, Inverse string }

func (e *Engine) canReadRecord(actor eventstore.Actor, entity, id string) bool {
	rec := e.records[entity][id]
	if rec == nil {
		return false
	}
	return e.can(actor.Role, "read", entity, "", rec.Values, actor.ID).allowed
}
