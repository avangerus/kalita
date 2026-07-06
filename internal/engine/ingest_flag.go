package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/avangerus/kalita/internal/eventstore"
)

// FlagIngest backs the MCP tool `flag_ingest` (TECHDEBT "Безопасность
// LLM-периметра" (b)): the ONLY path any actor has to Append the
// core-reserved kind ingest.flagged (eventstore.IngestFlagged exists as a
// Kind since LLD-01 §6 PR-6, but until now nothing could Append it —
// emit_event is deliberately restricted to pack-declared domain events, not
// this closed core kind).
//
// This is the write-side mirror of read_events' `tail [kind]` gate: gated on
// its own dedicated `flag [kind]` grant (LLD-01 §6, internal/dsl's `flag`
// verb), never inferred from an ordinary create/update/act grant the actor
// holds on the subject entity — so e.g. KnowVault's Indexer role, which
// already holds `update [Source]` for entirely unrelated reasons, gains
// NOTHING here unless it also explicitly holds `flag [ingest.flagged]`
// (packs/knowvault's permissions block).
//
// Journaled under Subject{entity, id} — the flagged record's own subject, the
// same scheme CommentPosted already uses — so the signal is discoverable via
// read_journal(entity, id) by anyone who can already read that record: no new
// query surface, no separate "security event" API. This is the point of PR-6:
// the tripwire lives in the journal itself, not only in a comment thread.
//
// Like emit_event, this NEVER blocks or otherwise changes the subject
// record's state — it only journals the fact. The document/record stays
// exactly as it would have without a flag; the real backstop is ABAC on
// whatever the flagged content later touches (HLD §H4), not this tool.
type IngestFlagged struct {
	Entity   string   `json:"entity"`
	RecordID string   `json:"record_id"`
	Source   string   `json:"source"`
	Reasons  []string `json:"reasons"`
	Score    float64  `json:"score"`
	Seq      uint64   `json:"seq"`
}

type ingestFlaggedPayload struct {
	Source  string   `json:"source"`
	Reasons []string `json:"reasons"`
	Score   float64  `json:"score"`
}

// FlagIngest journals ingest.flagged{source, reasons, score} under
// Subject{entity, id}. Requires basis (I-2 choke point discipline, like every
// other mutation) and the `flag [ingest.flagged]` grant; idempotent on a
// repeated idempotency_key exactly like emit_event.
func (e *Engine) FlagIngest(ctx context.Context, actor eventstore.Actor, entity, id, source string, reasons []string, score float64, basis *eventstore.Basis, idemKey string) (*IngestFlagged, error) {
	if basis == nil {
		return nil, &Err{Code: CodeBasisRequired, Message: "flag_ingest without basis",
			FixHint: "reference the task that performed the ingest"}
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if idemKey != "" {
		if prior, _ := e.store.ByIdemKey(ctx, idemKey); prior != nil {
			if prior.Kind != eventstore.IngestFlagged {
				return nil, &Err{Code: CodeConflict,
					Message: "idempotency_key was already used for a different mutation",
					FixHint: "generate a fresh idempotency_key per task attempt"}
			}
			var p ingestFlaggedPayload
			if json.Unmarshal(prior.Payload, &p) != nil {
				return nil, &Err{Code: CodeConflict, Message: "cannot read the prior flag_ingest for this idempotency_key"}
			}
			return &IngestFlagged{Entity: prior.Subject.Entity, RecordID: prior.Subject.RecordID,
				Source: p.Source, Reasons: p.Reasons, Score: p.Score, Seq: prior.Seq}, nil
		}
	}

	if !e.canFlag(actor.Role, string(eventstore.IngestFlagged)) {
		return nil, &Err{Code: CodePermissionDenied,
			Message: fmt.Sprintf("role %s may not flag %s", actor.Role, eventstore.IngestFlagged),
			Rule:    "flag permissions list the core-reserved kinds a role may Append directly"}
	}
	if _, ok := e.records[entity][id]; !ok {
		return nil, &Err{Code: CodeNotFound, Message: entity + " " + id + " not found"}
	}

	payload, _ := json.Marshal(ingestFlaggedPayload{Source: source, Reasons: reasons, Score: score})
	ev, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:          actor,
		Kind:           eventstore.IngestFlagged,
		Subject:        eventstore.Subject{Entity: entity, RecordID: id},
		Payload:        payload,
		Basis:          basis,
		DefVersion:     e.defVersion,
		SchemaV:        eventstore.CurrentSchemaV,
		IdempotencyKey: idemKey,
	})
	if err != nil {
		return nil, err
	}
	return e.applyIngestFlagged(ev), nil
}

// applyIngestFlagged folds one ingest.flagged event into the ingestFlags
// projection — called both from FlagIngest (live path, above) and from
// engine.go's replay switch, exactly like applyEventEmitted folds
// event.emitted on both paths. Assumes e.mu is already held (write lock on
// the live path via FlagIngest, the replay-time exclusive lock during
// engine.New/rebuild).
func (e *Engine) applyIngestFlagged(ev *eventstore.Event) *IngestFlagged {
	var p ingestFlaggedPayload
	if json.Unmarshal(ev.Payload, &p) != nil {
		return nil
	}
	fact := IngestFlagged{Entity: ev.Subject.Entity, RecordID: ev.Subject.RecordID,
		Source: p.Source, Reasons: p.Reasons, Score: p.Score, Seq: ev.Seq}
	e.ingestFlags = append(e.ingestFlags, fact)
	return &fact
}

// IngestFlags returns every ingest.flagged fact journaled so far (audit/test
// view) — the flag_ingest analogue of EmittedEvents.
func (e *Engine) IngestFlags() []IngestFlagged {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]IngestFlagged, len(e.ingestFlags))
	copy(out, e.ingestFlags)
	return out
}
