// Package eventstore implements the append-only, tamper-evident journal that is
// the single source of truth in Kalita (docs/EVENT-STORE-v0.md). Everything else
// (entity state, task queues, approval queues) is a projection derived from it.
package eventstore

import (
	"encoding/json"
	"time"
)

// ActorType identifies who caused an event.
type ActorType string

const (
	ActorHuman  ActorType = "human"
	ActorAgent  ActorType = "agent"
	ActorSystem ActorType = "system"
)

// Actor is the identity behind an event. Anonymous actors do not exist.
type Actor struct {
	Type ActorType `json:"type"`
	ID   string    `json:"id"`
	Role string    `json:"role,omitempty"`
	// Attrs are the actor's attributes (region, department, clearance…), the "A"
	// in ABAC. They let permissions scope by who the actor IS, not just what they
	// own: `read Counterparty where region = $me.region`.
	Attrs map[string]any `json:"attrs,omitempty"`
}

// Kind is the closed taxonomy of event kinds (EVENT-STORE-v0 §2).
// New kinds may only ever be added, never removed or renamed.
type Kind string

const (
	RecordCreated Kind = "record.created"
	RecordUpdated Kind = "record.updated"
	RecordAction  Kind = "record.action"

	DefinitionProposed  Kind = "definition.proposed"
	DefinitionValidated Kind = "definition.validated"
	DefinitionApproved  Kind = "definition.approved"
	DefinitionRejected  Kind = "definition.rejected"
	DefinitionApplied   Kind = "definition.applied"
	DefinitionReverted  Kind = "definition.reverted"

	ApprovalRequested Kind = "approval.requested"
	ApprovalGranted   Kind = "approval.granted"
	ApprovalRejected  Kind = "approval.rejected"

	TaskCreated   Kind = "task.created"
	TaskTaken     Kind = "task.taken"
	TaskProgress  Kind = "task.progress"
	TaskCompleted Kind = "task.completed"
	TaskFailed    Kind = "task.failed"
	TaskExpired   Kind = "task.expired"

	ActorRegistered  Kind = "actor.registered"
	ActorRoleChanged Kind = "actor.role_changed"
	ActorKeyRotated  Kind = "actor.key_rotated"
	ActorDisabled    Kind = "actor.disabled"

	InviteCreated  Kind = "invite.created"
	InviteRedeemed Kind = "invite.redeemed"

	LinkAdded   Kind = "link.added"
	LinkRemoved Kind = "link.removed"

	CommentPosted Kind = "comment.posted"

	NodeStarted        Kind = "node.started"
	ProjectionRebuilt  Kind = "projection.rebuilt"
	CheckpointSealed   Kind = "checkpoint.sealed"

	// The kinds below are the closed list introduced by docs/lld/
	// LLD-01-journal-mechanics.md §1.2. All events of these kinds are written
	// with SchemaV 1 (see CurrentSchemaV and hashEnvelope in chain.go).
	OpBegin             Kind = "op.begin"             // brackets a multi-event operation (LLD-01 §2)
	OpEnd               Kind = "op.end"               // closes the bracket
	OpAborted           Kind = "op.aborted"            // written by startup recovery for an unclosed bracket
	AutomationFailed    Kind = "automation.failed"    // a fireActions branch returned an error (LLD-01 §3)
	AutomationAbandoned Kind = "automation.abandoned" // retries exhausted (LLD-01 §3)
	EventEmitted        Kind = "event.emitted"        // DSL `event` primitive (LLD-01 §4, PR-4)
	IngestFlagged       Kind = "ingest.flagged"        // perimeter detector (LLD-01 §6, PR-6)
)

// CurrentSchemaV is the highest envelope schema_v this binary understands.
// New()/replay refuses to start on a journal containing a strictly greater
// value (LLD-01 §1.1) — a hard stop, not a silent skip, until LLD-03 adds a
// downgrade path.
const CurrentSchemaV = 1

// Subject is what the event is about. Exactly the fields relevant to the
// subject type are set; the rest stay empty.
type Subject struct {
	Entity     string `json:"entity,omitempty"`
	RecordID   string `json:"record_id,omitempty"`
	ProposalID string `json:"proposal_id,omitempty"`
	TaskID     string `json:"task_id,omitempty"`
	ApprovalID string `json:"approval_id,omitempty"`
	ActorID    string `json:"actor_id,omitempty"`
}

// Basis is the provenance of a mutation: the rule, task, ADR, approval or
// human instruction the actor acted upon. Mutations without a basis are
// rejected at the API layer (MCP-CONTRACT-v0 §0.3).
type Basis struct {
	Type string `json:"type"` // task | rule | adr | human | approval
	ID   string `json:"id"`
}

// Event is a single journal entry. Hash and Signature are set by the store;
// callers provide everything else via Append.
type Event struct {
	EventID        string          `json:"event_id"`
	Seq            uint64          `json:"seq"`
	TS             time.Time       `json:"ts"`
	Actor          Actor           `json:"actor"`
	Kind           Kind            `json:"kind"`
	Subject        Subject         `json:"subject"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	Basis          *Basis          `json:"basis,omitempty"`
	DefVersion     uint64          `json:"def_version"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	PrevHash       []byte          `json:"prev_hash"`
	Hash           []byte          `json:"hash"`
	Signature      []byte          `json:"signature,omitempty"`
	// SchemaV versions the envelope (LLD-01 §1.1). Absent/0 is every event
	// that predates this field — canonicalJSON omits it in that case, so
	// those events hash exactly as before (I-1). Events introduced by
	// LLD-01 are written with SchemaV 1.
	SchemaV int `json:"schema_v,omitempty"`
}
