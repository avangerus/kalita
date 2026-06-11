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
	stateSince map[string]map[string]time.Time // entity → id → entered current state
	taskTTL    time.Duration
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
		stateSince: map[string]map[string]time.Time{},
		taskTTL:    time.Hour,
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
	case eventstore.DefinitionApplied:
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

	for name, oldEnt := range e.model.Entities {
		newEnt, ok := next.Entities[name]
		if !ok {
			return invalid("", "entity "+name+" removed; only additive changes are allowed in v0",
				"keep the entity; destructive migrations require the manual procedure")
		}
		newFields := map[string]dsl.TypeRef{}
		for _, f := range newEnt.Fields {
			newFields[f.Name] = f.Type
		}
		for _, f := range oldEnt.Fields {
			nf, ok := newFields[f.Name]
			if !ok {
				return invalid(f.Name, fmt.Sprintf("field %s.%s removed; only additive changes are allowed in v0", name, f.Name),
					"keep the field; removals require the manual procedure")
			}
			if nf.Kind != f.Type.Kind || nf.Scalar != f.Type.Scalar || nf.RefTarget != f.Type.RefTarget {
				return invalid(f.Name, fmt.Sprintf("field %s.%s changed type; not allowed in v0", name, f.Name),
					"add a new field instead of changing the type")
			}
			if nf.Kind == dsl.TyEnum && len(nf.EnumValues) < len(f.Type.EnumValues) {
				return invalid(f.Name, "enum values may only be appended", "keep existing enum values, add new ones at the end")
			}
		}
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
