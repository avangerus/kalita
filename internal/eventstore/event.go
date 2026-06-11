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

	NodeStarted        Kind = "node.started"
	ProjectionRebuilt  Kind = "projection.rebuilt"
	CheckpointSealed   Kind = "checkpoint.sealed"
)

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
}
