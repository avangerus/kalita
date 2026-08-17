package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/avangerus/kalita/internal/eventstore"
)

// subjectErasedPayload is the journaled proof of an erasure. It carries NO
// personal data — only the subject id, the reason, and the timestamp — so the
// audit trail of "we executed the request" is itself lawful to retain (LLD-16
// §4).
type subjectErasedPayload struct {
	SubjectID string `json:"subject_id"`
	Reason    string `json:"reason"`
	At        string `json:"at"`
}

// EraseSubject is the governed crypto-shredding operation (LLD-16 §4): destroy
// the subject's key in the node pii store, then journal subject.erased — the
// proof the erasure was executed. Gated on an explicit `erase [subject]` grant
// (never inferred from any other verb); idempotent on a repeated idempotency_key.
// Shred-first order: the key is destroyed before the proof is appended, so a
// crash mid-way leaves the data shredded (safe) and the proof re-derivable by
// a same-key retry.
func (e *Engine) EraseSubject(ctx context.Context, actor eventstore.Actor, subjectID, reason string, basis *eventstore.Basis, idemKey string) error {
	if basis == nil {
		return &Err{Code: CodeBasisRequired, Message: "erase-subject without basis",
			FixHint: "reference the request (GDPR/152-ФЗ request, court order) this erasure is based on"}
	}
	if subjectID == "" {
		return &Err{Code: CodeValidation, Message: "erase-subject needs a subject id",
			FixHint: "pass the data subject's identifier"}
	}
	if e.pii == nil {
		return &Err{Code: CodeValidation, Message: "pii is not configured on this node",
			FixHint: "start the node with a pii key store (engine.WithPIIStore)"}
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if idemKey != "" {
		if prior, _ := e.store.ByIdemKey(ctx, idemKey); prior != nil {
			return nil // already erased under this key — idempotent
		}
	}

	if !e.canErase(actor.Role) {
		return &Err{Code: CodePermissionDenied,
			Message: fmt.Sprintf("role %s may not erase subjects", actor.Role),
			Rule:    "erase permissions grant the power to shred a data subject"}
	}

	if err := e.pii.Destroy(subjectID); err != nil {
		return err
	}

	payload, _ := json.Marshal(subjectErasedPayload{
		SubjectID: subjectID, Reason: reason, At: e.now().UTC().Format(time.RFC3339)})
	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:          actor,
		Kind:           eventstore.SubjectErased,
		Subject:        eventstore.Subject{}, // node-wide: no single record subject
		Payload:        payload,
		Basis:          basis,
		DefVersion:     e.defVersion,
		SchemaV:        eventstore.CurrentSchemaV,
		IdempotencyKey: idemKey,
	}); err != nil {
		return err
	}
	return nil
}
