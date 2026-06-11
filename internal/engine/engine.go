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
	mu         sync.RWMutex
	model      *dsl.Model
	store      eventstore.Store
	defVersion uint64
	records    map[string]map[string]*Record // entity → id → record
	approvals  map[string]*Approval
	tasks      map[string]*Task
	taskWake   chan struct{} // long-polling waiters, closed on new tasks
	proposals  map[string]*Proposal
	stateSince map[string]map[string]time.Time // entity → id → entered current state
	taskTTL    time.Duration
	defApprover string // role whose human signature applies definitions
	now        func() time.Time
	// verify checks an actor's signature (wired to identity.Registry by the
	// node). When set, approval decisions REQUIRE a valid signature.
	verify func(ctx context.Context, actorID string, msg, sig []byte) error
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

// Record is the projected current state of one row.
type Record struct {
	ID     string         `json:"id"`
	Entity string         `json:"entity"`
	Values map[string]any `json:"values"`
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
		model:      model,
		store:      store,
		defVersion: 1,
		records:    map[string]map[string]*Record{},
		approvals:  map[string]*Approval{},
		tasks:      map[string]*Task{},
		proposals:  map[string]*Proposal{},
		stateSince: map[string]map[string]time.Time{},
		taskTTL:    time.Hour,
		defApprover: "Owner",
		now:        time.Now,
	}
	for _, opt := range opts {
		opt(e)
	}
	events, err := store.All(ctx)
	if err != nil {
		return nil, err
	}
	for _, ev := range events {
		e.applyEvent(ev)
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
		e.tasks[ev.Subject.TaskID] = &Task{
			ID: ev.Subject.TaskID, Kind: p.Kind, Role: p.Role, Entity: p.Entity,
			RecordID: p.RecordID, Action: p.Action, Args: p.Args, Status: TaskOpen,
		}
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
		}
	case eventstore.TaskFailed:
		if t, ok := e.tasks[ev.Subject.TaskID]; ok {
			t.Status = TaskFailed
		}
	case eventstore.TaskExpired:
		if t, ok := e.tasks[ev.Subject.TaskID]; ok {
			t.Status, t.TakenBy = TaskOpen, ""
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
		out = append(out, &Record{ID: rec.ID, Entity: entity, Values: vals})
	}
	return out
}

// ImportRecord restores a record with its original id during an era import:
// types are validated against the NEW definition, but permissions, ref
// existence and the workflow-field guard are bypassed — the operator owns the
// data, state is restored as-is, refs may arrive out of order.
func (e *Engine) ImportRecord(ctx context.Context, actor eventstore.Actor, entity, id string, values map[string]any, basis *eventstore.Basis) error {
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
	payload, _ := json.Marshal(createdPayload{Values: values})
	ev, err := e.store.Append(ctx, eventstore.AppendInput{
		Actor:      actor,
		Kind:       eventstore.RecordCreated,
		Subject:    eventstore.Subject{Entity: entity, RecordID: id},
		Payload:    payload,
		Basis:      basis,
		DefVersion: e.defVersion,
	})
	if err != nil {
		return err
	}
	if e.records[entity] == nil {
		e.records[entity] = map[string]*Record{}
	}
	e.records[entity][id] = &Record{ID: id, Entity: entity, Values: values}
	e.setStateSince(entity, id, ev.TS)
	return nil
}

func findFieldDecl(decl *dsl.EntityDecl, name string) *dsl.FieldDecl {
	for _, f := range decl.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
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
