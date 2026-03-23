package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"kalita/internal/capability"
	"kalita/internal/caseruntime"
	"kalita/internal/employee"
	"kalita/internal/eventcore"
	"kalita/internal/executionruntime"
	"kalita/internal/policy"
	"kalita/internal/profile"
	"kalita/internal/proposal"
	"kalita/internal/trust"
	"kalita/internal/workplan"
)

type Event struct {
	Sequence int64           `json:"sequence"`
	Kind     string          `json:"kind"`
	Payload  json.RawMessage `json:"payload"`
}

type EventStore interface {
	Append(event Event) error
	List() ([]Event, error)
}

type SnapshotStore interface {
	SaveSnapshot(state SystemState) error
	LoadSnapshot() (SystemState, error)
}

type SystemState struct {
	LastSequence      int64                               `json:"last_sequence"`
	Cases             []caseruntime.Case                  `json:"cases"`
	Queues            []workplan.WorkQueue                `json:"queues"`
	WorkItems         []workplan.WorkItem                 `json:"work_items"`
	Coordinations     []workplan.CoordinationDecision     `json:"coordinations"`
	PolicyDecisions   []policy.PolicyDecision             `json:"policy_decisions"`
	ApprovalRequests  []policy.ApprovalRequest            `json:"approval_requests"`
	Proposals         []proposal.Proposal                 `json:"proposals"`
	Employees         []employee.DigitalEmployee          `json:"employees"`
	Assignments       []employee.Assignment               `json:"assignments"`
	TrustProfiles     []trust.TrustProfile                `json:"trust_profiles"`
	Profiles          []profile.CompetencyProfile         `json:"profiles"`
	Requirements      []profile.CapabilityRequirement     `json:"requirements"`
	Capabilities      []capability.Capability             `json:"capabilities"`
	ActorCapabilities []capability.ActorCapability        `json:"actor_capabilities"`
	ExecutionSessions []executionruntime.ExecutionSession `json:"execution_sessions"`
	StepExecutions    []executionruntime.StepExecution    `json:"step_executions"`
	WALRecords        []executionruntime.WALRecord        `json:"wal_records"`
	DomainEvents      []eventcore.Event                   `json:"domain_events"`
	ExecutionEvents   []eventcore.ExecutionEvent          `json:"execution_events"`
}

type FileEventStore struct {
	path string
	mu   sync.Mutex
}

func NewFileEventStore(dir string) *FileEventStore {
	return &FileEventStore{path: filepath.Join(dir, "events.jsonl")}
}
func (s *FileEventStore) Append(event Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(event)
}
func (s *FileEventStore) List() ([]Event, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lines := bytesSplitLines(b)
	out := make([]Event, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var evt Event
		if err := json.Unmarshal(line, &evt); err != nil {
			return nil, err
		}
		out = append(out, evt)
	}
	return out, nil
}

type FileSnapshotStore struct {
	path string
	mu   sync.Mutex
}

func NewFileSnapshotStore(dir string) *FileSnapshotStore {
	return &FileSnapshotStore{path: filepath.Join(dir, "snapshot.json")}
}
func (s *FileSnapshotStore) SaveSnapshot(state SystemState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o600)
}
func (s *FileSnapshotStore) LoadSnapshot() (SystemState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return SystemState{}, nil
	}
	if err != nil {
		return SystemState{}, err
	}
	var state SystemState
	if err := json.Unmarshal(b, &state); err != nil {
		return SystemState{}, err
	}
	return state, nil
}

type Collector interface {
	Collect(context.Context) (SystemState, error)
}

type Manager struct {
	mu            sync.Mutex
	enabled       bool
	events        EventStore
	snapshots     SnapshotStore
	collector     Collector
	snapshotEvery int
	seq           int64
}

func NewManager(events EventStore, snapshots SnapshotStore, snapshotEvery int) *Manager {
	if snapshotEvery <= 0 {
		snapshotEvery = 50
	}
	return &Manager{enabled: events != nil && snapshots != nil, events: events, snapshots: snapshots, snapshotEvery: snapshotEvery}
}
func (m *Manager) Enabled() bool { return m != nil && m.enabled }
func (m *Manager) BindCollector(c Collector) {
	if m != nil {
		m.collector = c
	}
}
func (m *Manager) Record(kind string, payload any) error {
	if !m.Enabled() {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.seq++
	seq := m.seq
	m.mu.Unlock()
	if err := m.events.Append(Event{Sequence: seq, Kind: kind, Payload: raw}); err != nil {
		return err
	}
	if m.collector != nil && m.snapshotEvery > 0 && seq%int64(m.snapshotEvery) == 0 {
		return m.SaveSnapshot(context.Background())
	}
	return nil
}
func (m *Manager) SaveSnapshot(ctx context.Context) error {
	if !m.Enabled() || m.collector == nil {
		return nil
	}
	state, err := m.collector.Collect(ctx)
	if err != nil {
		return err
	}
	m.mu.Lock()
	state.LastSequence = m.seq
	m.mu.Unlock()
	return m.snapshots.SaveSnapshot(state)
}
func (m *Manager) Restore(ctx context.Context, r *Restorer) error {
	if !m.Enabled() {
		return nil
	}
	snap, err := m.snapshots.LoadSnapshot()
	if err != nil {
		return err
	}
	if err := r.ApplyState(ctx, snap); err != nil {
		return err
	}
	events, err := m.events.List()
	if err != nil {
		return err
	}
	last := snap.LastSequence
	for _, evt := range events {
		if evt.Sequence <= snap.LastSequence {
			continue
		}
		if err := r.ApplyEvent(ctx, evt); err != nil {
			return fmt.Errorf("apply event %d (%s): %w", evt.Sequence, evt.Kind, err)
		}
		last = evt.Sequence
	}
	m.mu.Lock()
	if last > m.seq {
		m.seq = last
	}
	m.mu.Unlock()
	return nil
}

type Restorer struct {
	Cases         *caseruntime.InMemoryCaseRepository
	Queues        *workplan.InMemoryQueueRepository
	Coordinations *workplan.InMemoryCoordinationRepository
	Policies      *policy.InMemoryRepository
	Proposals     *proposal.InMemoryRepository
	Employees     *employee.InMemoryDirectory
	Assignments   *employee.InMemoryAssignmentRepository
	Trust         *trust.InMemoryRepository
	Profiles      *profile.InMemoryRepository
	Capabilities  *capability.InMemoryCapabilityRepository
	Executions    *executionruntime.InMemoryExecutionRepository
	WAL           *executionruntime.InMemoryWAL
	EventLog      *eventcore.InMemoryEventLog
}

func (r *Restorer) ApplyState(ctx context.Context, s SystemState) error {
	for _, item := range s.Cases {
		if err := r.Cases.Save(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.Queues {
		if err := r.Queues.SaveQueue(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.WorkItems {
		if err := r.Queues.SaveWorkItem(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.Coordinations {
		if err := r.Coordinations.SaveDecision(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.PolicyDecisions {
		if err := r.Policies.SaveDecision(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.ApprovalRequests {
		if err := r.Policies.SaveApprovalRequest(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.Proposals {
		if err := r.Proposals.Save(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.Employees {
		if err := r.Employees.SaveEmployee(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.Assignments {
		if err := r.Assignments.SaveAssignment(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.TrustProfiles {
		if err := r.Trust.Save(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.Profiles {
		if err := r.Profiles.SaveProfile(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.Requirements {
		if err := r.Profiles.SaveRequirement(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.Capabilities {
		if err := r.Capabilities.SaveCapability(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.ActorCapabilities {
		if err := r.Capabilities.AssignCapability(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.ExecutionSessions {
		if err := r.Executions.SaveSession(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.StepExecutions {
		if err := r.Executions.SaveStep(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.WALRecords {
		if err := r.WAL.Append(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.DomainEvents {
		if err := r.EventLog.AppendEvent(ctx, item); err != nil {
			return err
		}
	}
	for _, item := range s.ExecutionEvents {
		if err := r.EventLog.AppendExecutionEvent(ctx, item); err != nil {
			return err
		}
	}
	return nil
}
func (r *Restorer) ApplyEvent(ctx context.Context, evt Event) error {
	switch evt.Kind {
	case "case_saved":
		var v caseruntime.Case
		return unmarshalApply(evt.Payload, &v, func() error { return r.Cases.Save(ctx, v) })
	case "queue_saved":
		var v workplan.WorkQueue
		return unmarshalApply(evt.Payload, &v, func() error { return r.Queues.SaveQueue(ctx, v) })
	case "work_item_saved":
		var v workplan.WorkItem
		return unmarshalApply(evt.Payload, &v, func() error { return r.Queues.SaveWorkItem(ctx, v) })
	case "coordination_saved":
		var v workplan.CoordinationDecision
		return unmarshalApply(evt.Payload, &v, func() error { return r.Coordinations.SaveDecision(ctx, v) })
	case "policy_decision_saved":
		var v policy.PolicyDecision
		return unmarshalApply(evt.Payload, &v, func() error { return r.Policies.SaveDecision(ctx, v) })
	case "approval_request_saved":
		var v policy.ApprovalRequest
		return unmarshalApply(evt.Payload, &v, func() error { return r.Policies.SaveApprovalRequest(ctx, v) })
	case "proposal_saved":
		var v proposal.Proposal
		return unmarshalApply(evt.Payload, &v, func() error { return r.Proposals.Save(ctx, v) })
	case "employee_saved":
		var v employee.DigitalEmployee
		return unmarshalApply(evt.Payload, &v, func() error { return r.Employees.SaveEmployee(ctx, v) })
	case "assignment_saved":
		var v employee.Assignment
		return unmarshalApply(evt.Payload, &v, func() error { return r.Assignments.SaveAssignment(ctx, v) })
	case "trust_saved":
		var v trust.TrustProfile
		return unmarshalApply(evt.Payload, &v, func() error { return r.Trust.Save(ctx, v) })
	case "profile_saved":
		var v profile.CompetencyProfile
		return unmarshalApply(evt.Payload, &v, func() error { return r.Profiles.SaveProfile(ctx, v) })
	case "requirement_saved":
		var v profile.CapabilityRequirement
		return unmarshalApply(evt.Payload, &v, func() error { return r.Profiles.SaveRequirement(ctx, v) })
	case "capability_saved":
		var v capability.Capability
		return unmarshalApply(evt.Payload, &v, func() error { return r.Capabilities.SaveCapability(ctx, v) })
	case "actor_capability_saved":
		var v capability.ActorCapability
		return unmarshalApply(evt.Payload, &v, func() error { return r.Capabilities.AssignCapability(ctx, v) })
	case "execution_session_saved":
		var v executionruntime.ExecutionSession
		return unmarshalApply(evt.Payload, &v, func() error { return r.Executions.SaveSession(ctx, v) })
	case "step_execution_saved":
		var v executionruntime.StepExecution
		return unmarshalApply(evt.Payload, &v, func() error { return r.Executions.SaveStep(ctx, v) })
	case "wal_appended":
		var v executionruntime.WALRecord
		return unmarshalApply(evt.Payload, &v, func() error { return r.WAL.Append(ctx, v) })
	case "domain_event_appended":
		var v eventcore.Event
		return unmarshalApply(evt.Payload, &v, func() error { return r.EventLog.AppendEvent(ctx, v) })
	case "execution_event_appended":
		var v eventcore.ExecutionEvent
		return unmarshalApply(evt.Payload, &v, func() error { return r.EventLog.AppendExecutionEvent(ctx, v) })
	default:
		return fmt.Errorf("unknown persistence event kind %q", evt.Kind)
	}
}
func unmarshalApply[T any](raw []byte, target *T, apply func() error) error {
	if err := json.Unmarshal(raw, target); err != nil {
		return err
	}
	return apply()
}
func bytesSplitLines(b []byte) [][]byte {
	out := make([][]byte, 0)
	start := 0
	for i, c := range b {
		if c == '\n' {
			out = append(out, b[start:i])
			start = i + 1
		}
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}
