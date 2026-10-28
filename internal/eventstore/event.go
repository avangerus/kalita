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

// Reserved kinds: definition.validated, definition.reverted, actor.role_changed
// and projection.rebuilt are part of the taxonomy (AllKinds — DSL `tail`
// grants may name them) but no code path writes them yet.
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
	// ActorPasswordSet (LLD-05 §3, PK-4): a password credential set for an
	// EXISTING actor — a UX wrapper over the same bearer-token mechanism, not
	// a new identity model. Only the bcrypt hash is journaled (I-4); the raw
	// password never touches the journal.
	ActorPasswordSet Kind = "actor.password_set"

	// ActorCredentialRegistered/_Revoked/_Warning (LLD-02 §1): the WebAuthn/
	// passkey credential registry for human non-repudiation. Only public
	// material (COSE public key, credential id) ever lands in the journal —
	// never a private key (I-4). Written with SchemaV CurrentSchemaV, like
	// ActorPasswordSet: these are new kinds, not a pre-schema_v carryover.
	ActorCredentialRegistered Kind = "actor.credential_registered"
	ActorCredentialRevoked    Kind = "actor.credential_revoked"
	ActorCredentialWarning    Kind = "actor.credential_warning"

	InviteCreated  Kind = "invite.created"
	InviteRedeemed Kind = "invite.redeemed"

	LinkAdded   Kind = "link.added"
	LinkRemoved Kind = "link.removed"

	CommentPosted Kind = "comment.posted"

	NodeStarted       Kind = "node.started"
	ProjectionRebuilt Kind = "projection.rebuilt"
	CheckpointSealed  Kind = "checkpoint.sealed"

	// NodeLockdown/NodeLockdownLifted (LLD-10 §3): an emergency curtain over
	// Authenticate, not a mass token revoke — see internal/identity/lockdown.go.
	// Written with SchemaV CurrentSchemaV, like the other operational-hardening
	// kinds below.
	NodeLockdown       Kind = "node.lockdown"
	NodeLockdownLifted Kind = "node.lockdown_lifted"
	// AuthThrottled (LLD-10 §4): one event per threshold CROSSING for a given
	// IP, never one per failed attempt (that would itself be the journal-flood
	// DoS this guards against, T6).
	AuthThrottled Kind = "auth.throttled"

	// The kinds below are the closed list introduced by docs/lld/
	// LLD-01-journal-mechanics.md §1.2. All events of these kinds are written
	// with SchemaV 1 (see CurrentSchemaV and hashEnvelope in chain.go).
	OpBegin             Kind = "op.begin"             // brackets a multi-event operation (LLD-01 §2)
	OpEnd               Kind = "op.end"               // closes the bracket
	OpAborted           Kind = "op.aborted"           // written by startup recovery for an unclosed bracket
	AutomationFailed    Kind = "automation.failed"    // a fireActions branch returned an error (LLD-01 §3)
	AutomationAbandoned Kind = "automation.abandoned" // retries exhausted (LLD-01 §3)
	EventEmitted        Kind = "event.emitted"        // DSL `event` primitive (LLD-01 §4, PR-4)
	IngestFlagged       Kind = "ingest.flagged"       // perimeter detector (LLD-01 §6, PR-6)
	SubjectErased       Kind = "subject.erased"       // crypto-shredding: a data subject's key was destroyed (LLD-16 §4)

	// The kinds below are LLD-03-upgrade-eras.md's closed additions. All
	// events of these kinds are written with SchemaV CurrentSchemaV.
	// NodeUpgraded/NodeDowngraded (§1) version-gate the running binary
	// against the journal's high-water mark; EraGenesis/RecordImported (§4)
	// are the destructive-migration ("era") procedure.
	NodeUpgraded   Kind = "node.upgraded"   // written after a successful `kalita upgrade` (§2 step 7)
	NodeDowngraded Kind = "node.downgraded" // written only via an explicit --force-downgrade (§1)
	EraGenesis     Kind = "era.genesis"     // first event of a new era's journal, chains to the old era's manifest (§4)
	RecordImported Kind = "record.imported" // NOT record.created: provenance differs, on-create automation must not fire (§4)

	// The kinds below are LLD-09-deployment-product.md §1's closed additions
	// (shadow mode). All events of these kinds are written with SchemaV
	// CurrentSchemaV. RoleShadowEnabled/Disabled toggle a role's membership in
	// the shadow set (a runtime overlay, not a new execution path — I-2). A
	// shadow actor's create/update/act/emit is redirected into a
	// MutationProposed instead of its normal record.created/updated/action/
	// event.emitted — MutationApproved applies the SAME validated mutation
	// through the ordinary path (any HITL gate the mutation itself carries,
	// e.g. an act's approval(Role), still applies — shadow defers, never
	// bypasses); MutationRejected discards it.
	RoleShadowEnabled  Kind = "role.shadow_enabled"
	RoleShadowDisabled Kind = "role.shadow_disabled"
	MutationProposed   Kind = "mutation.proposed"
	MutationApproved   Kind = "mutation.approved"
	MutationRejected   Kind = "mutation.rejected"

	// WebhookAttempt (English-first industrial release, D-011 hardening):
	// one delivery-attempt receipt per outbound webhook attempt. Purely
	// observability (the durable intent is task.created; settlement is
	// task.completed/task.failed): {attempt, outcome, category, destination,
	// latency_ms}. Written with SchemaV CurrentSchemaV — no schema_v bump, so
	// older binaries fold it as an unknown kind (silent no-op, receipts are
	// a projection, not state). Category vocabulary: http_4xx, http_5xx,
	// redirect, network, timeout, auth_missing, allowlist, payload, delivered,
	// retry_exhausted.
	WebhookAttempt Kind = "webhook.attempt"
)

// AllKinds is the closed taxonomy in declaration order — the single source of
// truth the DSL compiler validates `tail [kind,...]` permission grants against
// (LLD-08 §2: closed list, did-you-mean, exactly like §4c does for action
// names). Append-only, mirroring the Kind consts above: a kind added here
// without a matching const (or vice versa) is a compile-time-invisible bug,
// so keep the two lists in lockstep by hand.
var AllKinds = []Kind{
	RecordCreated, RecordUpdated, RecordAction,
	DefinitionProposed, DefinitionValidated, DefinitionApproved, DefinitionRejected, DefinitionApplied, DefinitionReverted,
	ApprovalRequested, ApprovalGranted, ApprovalRejected,
	TaskCreated, TaskTaken, TaskProgress, TaskCompleted, TaskFailed, TaskExpired,
	ActorRegistered, ActorRoleChanged, ActorKeyRotated, ActorDisabled, ActorPasswordSet,
	ActorCredentialRegistered, ActorCredentialRevoked, ActorCredentialWarning,
	InviteCreated, InviteRedeemed,
	LinkAdded, LinkRemoved,
	CommentPosted,
	NodeStarted, ProjectionRebuilt, CheckpointSealed,
	OpBegin, OpEnd, OpAborted,
	AutomationFailed, AutomationAbandoned,
	EventEmitted,
	IngestFlagged,
	SubjectErased,
	NodeLockdown, NodeLockdownLifted,
	AuthThrottled,
	NodeUpgraded, NodeDowngraded,
	EraGenesis, RecordImported,
	RoleShadowEnabled, RoleShadowDisabled,
	MutationProposed, MutationApproved, MutationRejected,
	WebhookAttempt,
}

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
