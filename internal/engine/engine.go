package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/eventstore"
)

// Engine executes a compiled model over the journal. State is an in-memory
// projection rebuilt by replay (ADR-001); the journal is the only truth.
type Engine struct {
	mu          sync.RWMutex
	model       *dsl.Model
	store       eventstore.Store
	defVersion  uint64
	records     map[string]map[string]*Record // entity → id → record
	approvals   map[string]*Approval
	tasks       map[string]*Task
	taskWake    chan struct{} // long-polling waiters, closed on new tasks
	proposals   map[string]*Proposal
	stateSince  map[string]map[string]time.Time // entity → id → entered current state
	links       map[string]linkPayload          // canonical link key → fact
	comments    map[string][]*Comment           // entity|id → thread
	taskTTL     time.Duration
	defApprover string // role whose human signature applies definitions
	requireSig  bool   // enforce signatures on HITL decisions (WebAuthn era)
	// requireHumanSig enforces a verified WebAuthn human_signature
	// specifically (LLD-02 §5) on every human HITL decision — stricter than
	// requireSig, which only requires SOME signature verified by the
	// generic Ed25519 `verify` hook above. See WithRequireHumanSignature.
	requireHumanSig bool
	blobs           BlobStore
	now             func() time.Time
	cal             businessCalendar        // working-time calendar for business_*_since
	idxMu           sync.Mutex              // guards idxCache (built lazily under e.mu read side)
	idxCache        map[string]*entityIndex // entity → secondary index for permitted-set queries
	// verify checks an actor's signature (wired to identity.Registry by the
	// node). When set, approval decisions REQUIRE a valid signature.
	verify func(ctx context.Context, actorID string, msg, sig []byte) error

	// failedActions is the retry projection for automation cascades (LLD-01
	// §3): key = "rule|record_id|action_index", built by folding
	// automation.failed/automation.abandoned. A successful retry clears its
	// entry live (see fireOneAction/retryFailedActions in automation.go).
	failedActions map[string]*actionFailState
	// openOps and currentOp track op-brackets (LLD-01 §2): openOps is the set
	// of op_id opened by op.begin and not yet closed by op.end/op.aborted —
	// built by fold, consumed by startup recovery. currentOp is the ONE
	// cascade the engine is inside right now (nesting is forbidden: secondary
	// cascades reuse it). Both live under e.mu, like every other projection.
	openOps   map[string]opInfo
	currentOp *opContext

	// emittedEvents is the folded log of event.emitted facts (LLD-01 §4). It is
	// observability only: subscriber effects are journaled as their own events,
	// so this never drives a record mutation and is not re-dispatched on replay.
	emittedEvents []EmittedFact

	// ingestFlags is the folded log of ingest.flagged facts (TECHDEBT
	// "Безопасность LLM-периметра" (b), LLD-01 §6 PR-6): the perimeter
	// detector's tripwire signal against ingested content. Observability
	// only, exactly like emittedEvents — it never drives a record mutation on
	// its own; the document is indexed regardless (tripwire, not a filter).
	ingestFlags []IngestFlagged

	// shadowRoles is the LLD-09 §1 shadow-mode overlay: the set of roles whose
	// create/update/act/emit is redirected into a MutationProposal instead of
	// applied directly. Folded from role.shadow_enabled/role.shadow_disabled.
	shadowRoles map[string]bool
	// mutationProposals is the shadow proposal projection (LLD-09 §1), folded
	// from mutation.proposed/approved/rejected — the record-mutation twin of
	// e.proposals (which is definition-change proposals only).
	mutationProposals map[string]*MutationProposal

	// scheduleTaskHistory bounds the in-memory growth of tasks created by
	// `on schedule` rules (TECHDEBT "Ядро: automation / schedule" P1: a
	// recurring rule used to pile up one task per bucket forever). Keyed
	// "ruleKey|entity|recordID" (the same lineage hasOpenRuleTask dedups on),
	// it holds the finished (completed/failed) task IDs for that lineage in
	// the order they finished. noteScheduleTaskFinished (automation.go)
	// appends to it and evicts the oldest entries past
	// scheduleTaskRetention — a function of fold ORDER (the sequence
	// replay/live processing sees events in), never of wall-clock time, so
	// replay reproduces the exact same projection (I-6 determinism).
	scheduleTaskHistory map[string][]string
	// scheduleTaskRetention is how many finished tasks per schedule-rule
	// lineage stay in the in-memory projection; older ones are pruned (the
	// journal itself is untouched — this is projection GC, not history loss).
	// 0 disables pruning. Default set in New(); override with
	// WithScheduleTaskRetention.
	scheduleTaskRetention int
}

// Option configures the engine.
type Option func(*Engine)

// WithClock injects a deterministic clock (tests, replay).
func WithClock(now func() time.Time) Option { return func(e *Engine) { e.now = now } }

// WithVerifier wires signature verification for approval decisions.
func WithVerifier(v func(ctx context.Context, actorID string, msg, sig []byte) error) Option {
	return func(e *Engine) { e.verify = v }
}

// WithTaskTTL sets the task lease duration (default 1h).
func WithTaskTTL(d time.Duration) Option { return func(e *Engine) { e.taskTTL = d } }

// WithDefinitionApprover sets the role whose human signature applies
// definition changes (default Owner).
func WithDefinitionApprover(role string) Option { return func(e *Engine) { e.defApprover = role } }

// WithBusinessCalendar sets the working-time calendar used by business_*_since
// (default Mon-Fri 09:00-18:00, no holidays).
func WithBusinessCalendar(cal businessCalendar) Option { return func(e *Engine) { e.cal = cal } }

// WithRequireSignatures enforces a verified signature on every HITL decision
// (the WebAuthn era). Off by default: token-authenticated decisions are
// allowed until passkeys are deployed.
func WithRequireSignatures() Option { return func(e *Engine) { e.requireSig = true } }

// WithRequireHumanSignature enforces a verified WebAuthn human_signature
// (LLD-02 §5, DecideOption WithHumanSignature) on every HITL decision made
// by a human actor. This is the production gate — "approve without a
// passkey" — and is off by default so demo/dev nodes keep working exactly
// as before (I-1). It is independent of WithRequireSignatures: a node can
// run either, both, or neither.
func WithRequireHumanSignature() Option { return func(e *Engine) { e.requireHumanSig = true } }

// WithScheduleTaskRetention overrides how many finished tasks per
// schedule-rule lineage (ruleKey + entity + record) the in-memory projection
// keeps before pruning the oldest (TECHDEBT "Ядро: automation / schedule"
// P1). 0 disables pruning entirely — every finished schedule task stays in
// memory forever, the pre-fix behavior. Default (set in New) is 20.
func WithScheduleTaskRetention(n int) Option {
	return func(e *Engine) { e.scheduleTaskRetention = n }
}

// Record is the projected current state of one row.
type Record struct {
	ID     string         `json:"id"`
	Entity string         `json:"entity"`
	Values map[string]any `json:"values"`
	// Inconsistent marks a record left mid-operation by a crash (LLD-01 §2):
	// derived purely from the journal (op.aborted sets it, a confirm_consistent
	// action clears it), never itself journaled — "состояние выводимо". While
	// set, mutations are rejected with RECORD_INCONSISTENT.
	Inconsistent bool `json:"inconsistent,omitempty"`
	// ShadowProposalID is set instead of ID when the actor's role is in shadow
	// mode (LLD-09 §1): this create/update was NOT applied — Values is what
	// the actor asked for, held as a MutationProposal for a human to grant or
	// reject, not the record's real journaled state. Empty on every ordinary
	// call, so existing callers see no behavior change (I-1) until shadow
	// mode is explicitly turned on for their role.
	ShadowProposalID string `json:"shadow_proposal_id,omitempty"`
}

type createdPayload struct {
	Values map[string]any `json:"values"`
}

type change struct {
	Field string `json:"field"`
	Old   any    `json:"old"`
	New   any    `json:"new"`
}

type updatedPayload struct {
	Changes []change `json:"changes"`
}

// New builds an engine over a model and journal, replaying existing events.
func New(ctx context.Context, model *dsl.Model, store eventstore.Store, opts ...Option) (*Engine, error) {
	e := &Engine{
		model:                 model,
		store:                 store,
		defVersion:            1,
		records:               map[string]map[string]*Record{},
		approvals:             map[string]*Approval{},
		tasks:                 map[string]*Task{},
		proposals:             map[string]*Proposal{},
		stateSince:            map[string]map[string]time.Time{},
		links:                 map[string]linkPayload{},
		comments:              map[string][]*Comment{},
		taskTTL:               time.Hour,
		defApprover:           "Owner",
		now:                   time.Now,
		cal:                   defaultCalendar(),
		failedActions:         map[string]*actionFailState{},
		openOps:               map[string]opInfo{},
		shadowRoles:           map[string]bool{},
		mutationProposals:     map[string]*MutationProposal{},
		scheduleTaskHistory:   map[string][]string{},
		scheduleTaskRetention: 20,
	}
	for _, opt := range opts {
		opt(e)
	}
	events, err := store.All(ctx)
	if err != nil {
		return nil, err
	}
	for _, ev := range events {
		// LLD-01 §1.1: a binary must fold every schema_v <= its own. An
		// unknown GREATER schema_v is a hard startup refusal, not a silent
		// skip — running an old binary on a newer journal must not proceed
		// until LLD-03 adds a proper downgrade path.
		if ev.SchemaV > eventstore.CurrentSchemaV {
			return nil, fmt.Errorf("engine: journal contains event %s (seq %d) with schema_v %d, this binary supports up to %d — refusing to start (LLD-01 §1.1)",
				ev.EventID, ev.Seq, ev.SchemaV, eventstore.CurrentSchemaV)
		}
		e.applyEvent(ev)
	}
	if err := e.recoverUnclosedOps(ctx); err != nil {
		return nil, err
	}
	return e, nil
}

// applyEvent folds one journal event into the projection.
func (e *Engine) applyEvent(ev *eventstore.Event) {
	switch ev.Kind {
	case eventstore.RecordCreated:
		var p createdPayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			return
		}
		if e.records[ev.Subject.Entity] == nil {
			e.records[ev.Subject.Entity] = map[string]*Record{}
		}
		e.records[ev.Subject.Entity][ev.Subject.RecordID] = &Record{
			ID: ev.Subject.RecordID, Entity: ev.Subject.Entity, Values: p.Values,
		}
		e.setStateSince(ev.Subject.Entity, ev.Subject.RecordID, ev.TS)
	case eventstore.RecordImported:
		// LLD-03 §4: folds exactly like RecordCreated (same projection shape)
		// — the distinct kind exists for provenance and to keep on-create
		// automation from firing on replay, not for a different projection.
		// ImportRecord (below) is the only writer of this kind.
		var ip eventstore.RecordImportedPayload
		if json.Unmarshal(ev.Payload, &ip) != nil {
			return
		}
		if e.records[ev.Subject.Entity] == nil {
			e.records[ev.Subject.Entity] = map[string]*Record{}
		}
		e.records[ev.Subject.Entity][ev.Subject.RecordID] = &Record{
			ID: ev.Subject.RecordID, Entity: ev.Subject.Entity, Values: ip.Values,
		}
		e.setStateSince(ev.Subject.Entity, ev.Subject.RecordID, ev.TS)
	case eventstore.RecordUpdated:
		var p updatedPayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			return
		}
		if rec, ok := e.records[ev.Subject.Entity][ev.Subject.RecordID]; ok {
			for _, ch := range p.Changes {
				rec.Values[ch.Field] = ch.New
			}
		}
	case eventstore.RecordAction:
		var p actionPayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			return
		}
		if rec, ok := e.records[ev.Subject.Entity][ev.Subject.RecordID]; ok {
			if wf, ok := e.model.Workflows[ev.Subject.Entity]; ok {
				rec.Values[wf.Field] = p.To
			}
			// confirm_consistent (LLD-01 §2) is a kernel-synthesized action
			// name, not a declared transition — the supervisor's way out of
			// RECORD_INCONSISTENT. It never moves the workflow field (From==To).
			if p.Action == "confirm_consistent" {
				rec.Inconsistent = false
			}
		}
		e.setStateSince(ev.Subject.Entity, ev.Subject.RecordID, ev.TS)
	case eventstore.ApprovalRequested:
		var p approvalPayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			return
		}
		e.approvals[ev.Subject.ApprovalID] = &Approval{
			ID: ev.Subject.ApprovalID, Entity: p.Entity, RecordID: p.RecordID,
			Action: p.Action, From: p.From, To: p.To, Role: p.Role,
			RequestedBy: ev.Actor, Status: ApprovalPending,
		}
	case eventstore.ApprovalGranted:
		if a, ok := e.approvals[ev.Subject.ApprovalID]; ok {
			a.Status = ApprovalGrantedStatus
		}
	case eventstore.ApprovalRejected:
		if a, ok := e.approvals[ev.Subject.ApprovalID]; ok {
			a.Status = ApprovalRejectedStatus
		}
	case eventstore.TaskCreated:
		var p taskPayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			return
		}
		t := &Task{
			ID: ev.Subject.TaskID, Kind: p.Kind, Role: p.Role, Entity: p.Entity,
			RecordID: p.RecordID, Action: p.Action, Args: p.Args, Status: TaskOpen,
		}
		if ev.Basis != nil && ev.Basis.Type == "rule" {
			t.ruleKey = ev.Basis.ID
		}
		e.tasks[ev.Subject.TaskID] = t
	case eventstore.TaskTaken:
		var p taskPayload
		_ = json.Unmarshal(ev.Payload, &p)
		if t, ok := e.tasks[ev.Subject.TaskID]; ok {
			t.Status, t.TakenBy = TaskTaken, ev.Actor.ID
			if lease, err := time.Parse(time.RFC3339, p.Lease); err == nil {
				t.LeaseUntil = lease
			}
		}
	case eventstore.TaskCompleted:
		if t, ok := e.tasks[ev.Subject.TaskID]; ok {
			t.Status = TaskCompleted
			e.noteScheduleTaskFinished(t)
		}
	case eventstore.TaskFailed:
		if t, ok := e.tasks[ev.Subject.TaskID]; ok {
			t.Status = TaskFailed
			e.noteScheduleTaskFinished(t)
		}
	case eventstore.TaskExpired:
		if t, ok := e.tasks[ev.Subject.TaskID]; ok {
			t.Status, t.TakenBy = TaskOpen, ""
		}
	case eventstore.TaskProgress:
		var p taskPayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			return
		}
		if t, ok := e.tasks[ev.Subject.TaskID]; ok {
			t.Note = p.Note
			ts := ev.TS
			t.NoteAt = &ts
		}
	case eventstore.DefinitionProposed:
		var p proposalPayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			return
		}
		e.proposals[ev.Subject.ProposalID] = &Proposal{
			ID: ev.Subject.ProposalID, Description: p.Description, Author: ev.Actor,
			BaseDefVersion: p.BaseDefVersion, Files: p.Files, Plan: p.Plan, Status: ProposalPending,
		}
	case eventstore.DefinitionApproved:
		if p, ok := e.proposals[ev.Subject.ProposalID]; ok {
			p.Status = ProposalApproved
		}
	case eventstore.DefinitionRejected:
		if p, ok := e.proposals[ev.Subject.ProposalID]; ok {
			p.Status = ProposalRejected
		}
	case eventstore.LinkAdded:
		var lp linkPayload
		if json.Unmarshal(ev.Payload, &lp) == nil {
			e.links[lp.Link+"|"+lp.From+"|"+lp.To] = lp
		}
	case eventstore.LinkRemoved:
		var lp linkPayload
		if json.Unmarshal(ev.Payload, &lp) == nil {
			delete(e.links, lp.Link+"|"+lp.From+"|"+lp.To)
		}
	case eventstore.CommentPosted:
		e.applyCommentEvent(ev)
	case eventstore.DefinitionApplied:
		// definitions replay from the journal: the pack directory is only the
		// genesis seed
		if p, ok := e.proposals[ev.Subject.ProposalID]; ok && len(p.Files) > 0 {
			if next, errs := dsl.Compile(p.Files); len(errs) == 0 {
				e.model = next
			}
			p.Status = ProposalApplied
		}
		e.defVersion++

	// --- LLD-01 §1.2/§2/§3: op-brackets and automation retry projection -----
	case eventstore.OpBegin:
		e.applyOpBegin(ev)
	case eventstore.OpEnd:
		e.applyOpEnd(ev)
	case eventstore.OpAborted:
		e.applyOpAborted(ev)
	case eventstore.AutomationFailed:
		e.applyAutomationFailed(ev)
	case eventstore.AutomationAbandoned:
		e.applyAutomationAbandoned(ev)

	// --- LLD-01 §1.2: kinds reserved for later PRs of this LLD. Kind constants
	// and schema_v exist now (PR-1); fold logic lands with their own PR — the
	// switch falls through to no-op for them exactly as it already does for
	// any kind this engine does not recognize.
	case eventstore.EventEmitted:
		// LLD-01 §4: fold the domain-event fact. Subscribers' effects are their
		// own journaled events (replayed separately), so this does NOT
		// re-dispatch — it only records that the emit happened.
		e.applyEventEmitted(ev)
	case eventstore.IngestFlagged:
		// PR-6 (LLD-01 §6): perimeter detector tripwire. Folds into the
		// audit-only ingestFlags projection (see applyIngestFlagged in
		// ingest_flag.go) — the flagged record itself is untouched (the
		// event lives in ITS journal via Subject, which is the visible
		// signal: read_journal on the record surfaces it directly).
		e.applyIngestFlagged(ev)

	// --- LLD-09 §1: shadow mode ---------------------------------------------
	case eventstore.RoleShadowEnabled:
		e.applyShadowRoleEvent(ev, true)
	case eventstore.RoleShadowDisabled:
		e.applyShadowRoleEvent(ev, false)
	case eventstore.MutationProposed:
		e.applyMutationProposed(ev)
	case eventstore.MutationApproved:
		e.applyMutationDecided(ev, ProposalApplied)
	case eventstore.MutationRejected:
		e.applyMutationDecided(ev, ProposalRejected)
	}
}

func (e *Engine) setStateSince(entity, id string, ts time.Time) {
	if e.stateSince[entity] == nil {
		e.stateSince[entity] = map[string]time.Time{}
	}
	e.stateSince[entity][id] = ts
}

// ApplyAdditive swaps in a new model after verifying the change is purely
// additive (DSL-SPEC-v0 §9): nothing existing may disappear or change type.
// This is the week-3 prototype of the migration engine — risk #1 of the HLD,
// deliberately exercised early.
func (e *Engine) ApplyAdditive(ctx context.Context, actor eventstore.Actor, next *dsl.Model, basis *eventstore.Basis) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if err := validateAdditive(e.model, next); err != nil {
		return err
	}

	if _, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:      actor,
		Kind:       eventstore.DefinitionApplied,
		Payload:    json.RawMessage(fmt.Sprintf(`{"def_version":%d}`, e.defVersion+1)),
		Basis:      basis,
		DefVersion: e.defVersion,
	}); err != nil {
		return err
	}
	e.model = next
	e.defVersion++
	return nil
}

// Export dumps all records of an entity without permission filtering — the
// operator-only era-migration path (cmd export, node stopped).
func (e *Engine) Export(entity string) []*Record {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var out []*Record
	for _, id := range sortedIDs(e.records[entity]) {
		rec := e.records[entity][id]
		vals := make(map[string]any, len(rec.Values))
		for k, v := range rec.Values {
			vals[k] = v
		}
		out = append(out, &Record{ID: rec.ID, Entity: entity, Values: vals, Inconsistent: rec.Inconsistent})
	}
	return out
}

// ImportRecord restores a record with its original id during an era import:
// types are validated against the NEW definition, but permissions, ref
// existence and the workflow-field guard are bypassed — the operator owns the
// data, state is restored as-is, refs may arrive out of order. Written as
// record.imported — NOT record.created (LLD-03 §4): provenance (origin) is
// preserved, and because this method never calls runAutoTransitions /
// runEventTriggers, on-create automation does not fire for restored rows,
// whichever kind is used — the distinct kind is for audit distinguishability,
// a locked design decision (LLD-03 §4), not a functional requirement of this
// method's own behavior.
func (e *Engine) ImportRecord(ctx context.Context, actor eventstore.Actor, entity, id string, values map[string]any, origin eventstore.RecordOrigin, basis *eventstore.Basis) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	decl, errr := e.entityOrErr(entity)
	if errr != nil {
		return errr
	}
	for name, v := range values {
		f := findFieldDecl(decl, name)
		if f == nil {
			return invalid(name, "unknown field "+entity+"."+name,
				"transform the export to match the new pack before importing")
		}
		if v != nil && f.Computed == "" {
			if err := checkType(f, v); err != nil {
				return err
			}
		}
	}
	payload, _ := json.Marshal(eventstore.RecordImportedPayload{Values: values, Origin: origin})
	ev, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:      actor,
		Kind:       eventstore.RecordImported,
		Subject:    eventstore.Subject{Entity: entity, RecordID: id},
		Payload:    payload,
		Basis:      basis,
		DefVersion: e.defVersion,
		SchemaV:    eventstore.CurrentSchemaV,
	})
	if err != nil {
		return err
	}
	if e.records[entity] == nil {
		e.records[entity] = map[string]*Record{}
	}
	e.records[entity][id] = &Record{ID: id, Entity: entity, Values: values}
	e.dropIndex(entity)
	e.setStateSince(entity, id, ev.TS)
	return nil
}

// StateSince returns when entity/id entered its current state (a workflow
// transition, or its creation/import time if it has never transitioned). ok
// is false if the record is unknown. Used by `kalita export` (LLD-03 §4) to
// snapshot each row's state_since into era.jsonl.
func (e *Engine) StateSince(entity, id string) (time.Time, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	t, ok := e.stateSince[entity][id]
	return t, ok
}

func findFieldDecl(decl *dsl.EntityDecl, name string) *dsl.FieldDecl {
	for _, f := range decl.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// VisibleRecordIDs returns the ids of an entity's records the role can read —
// the permission boundary the search layer maps onto vector collections.
// Generic over entity so it is not knowvault-specific.
func (e *Engine) VisibleRecordIDs(ctx context.Context, actor eventstore.Actor, entity string) ([]string, error) {
	rows, err := e.Query(ctx, actor, entity, QueryOpts{})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids, nil
}

// CanCall reports whether a role may invoke a connector capability
// ("<connector>.<fn>"). It reuses the existing name-based ABAC check (the same
// machinery behind act/approve) — the node's permission boundary for /api/call.
func (e *Engine) CanCall(role, capability string) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.canNamed(role, "call", capability)
}

// LookupByField returns the values of the first record of entity whose field
// equals value, read with NODE authority (no actor ABAC). It exists for the
// node to resolve its OWN infrastructure config — e.g. a connector's backend
// URL — not for serving actor requests, which always go through Query. Generic
// over entity/field so the kernel stays domain-agnostic.
func (e *Engine) LookupByField(entity, field string, value any) (map[string]any, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, id := range sortedIDs(e.records[entity]) {
		rec := e.records[entity][id]
		if rec != nil && fmt.Sprint(rec.Values[field]) == fmt.Sprint(value) {
			return rec.Values, true
		}
	}
	return nil, false
}

// Model returns the active definition. Callers must not mutate it.
func (e *Engine) Model() *dsl.Model {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.model
}

func (e *Engine) DefVersion() uint64 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.defVersion
}

// HeadSeq returns the journal's current head seq (0 on an empty journal).
// LLD-02 §2/§4 needs this: the approval-challenge endpoint captures it at
// issuance time so the canonical string fixes "what the human saw" (state
// as of head_seq), and Decide's verification later reconstructs the SAME
// canonical from that captured value — never a value recomputed at decide
// time, which could have moved on.
func (e *Engine) HeadSeq(ctx context.Context) (uint64, error) {
	seq, _, err := e.store.Head(ctx)
	return seq, err
}

// entityOrErr resolves an entity declaration.
func (e *Engine) entityOrErr(entity string) (*dsl.EntityDecl, *Err) {
	decl, ok := e.model.Entities[entity]
	if !ok {
		return nil, &Err{Code: CodeNotFound, Message: "unknown entity " + entity,
			FixHint: "call describe_system for the list of entities"}
	}
	return decl, nil
}

// sortedIDs gives deterministic iteration for queries.
func sortedIDs(m map[string]*Record) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}
