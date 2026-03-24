package persistence

import (
	"context"

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

type persistentEventLog struct {
	base *eventcore.InMemoryEventLog
	mgr  *Manager
}

func WrapEventLog(base *eventcore.InMemoryEventLog, mgr *Manager) eventcore.EventLog {
	if mgr == nil || !mgr.Enabled() {
		return base
	}
	return &persistentEventLog{base: base, mgr: mgr}
}
func (l *persistentEventLog) AppendEvent(ctx context.Context, e eventcore.Event) error {
	if err := l.base.AppendEvent(ctx, e); err != nil {
		return err
	}
	return l.mgr.Record("domain_event_appended", e)
}
func (l *persistentEventLog) AppendExecutionEvent(ctx context.Context, e eventcore.ExecutionEvent) error {
	if err := l.base.AppendExecutionEvent(ctx, e); err != nil {
		return err
	}
	return l.mgr.Record("execution_event_appended", e)
}
func (l *persistentEventLog) ListByCorrelation(ctx context.Context, correlationID string) ([]eventcore.Event, []eventcore.ExecutionEvent, error) {
	return l.base.ListByCorrelation(ctx, correlationID)
}

type PersistentCaseRepository struct {
	base *caseruntime.InMemoryCaseRepository
	mgr  *Manager
}

func WrapCaseRepository(base *caseruntime.InMemoryCaseRepository, mgr *Manager) caseruntime.CaseRepository {
	if mgr == nil || !mgr.Enabled() {
		return base
	}
	return &PersistentCaseRepository{base: base, mgr: mgr}
}
func (r *PersistentCaseRepository) Save(ctx context.Context, c caseruntime.Case) error {
	if err := r.base.Save(ctx, c); err != nil {
		return err
	}
	return r.mgr.Record("case_saved", c)
}
func (r *PersistentCaseRepository) GetByID(ctx context.Context, id string) (caseruntime.Case, bool, error) {
	return r.base.GetByID(ctx, id)
}
func (r *PersistentCaseRepository) FindByID(ctx context.Context, id string) (caseruntime.Case, bool, error) {
	return r.base.FindByID(ctx, id)
}
func (r *PersistentCaseRepository) List(ctx context.Context) ([]caseruntime.Case, error) {
	return r.base.List(ctx)
}
func (r *PersistentCaseRepository) FindAll(ctx context.Context) ([]caseruntime.Case, error) {
	return r.base.FindAll(ctx)
}
func (r *PersistentCaseRepository) FindByStatus(ctx context.Context, status string) ([]caseruntime.Case, error) {
	return r.base.FindByStatus(ctx, status)
}
func (r *PersistentCaseRepository) FindByCorrelation(ctx context.Context, correlationID string) (caseruntime.Case, bool, error) {
	return r.base.FindByCorrelation(ctx, correlationID)
}
func (r *PersistentCaseRepository) FindBySubjectRef(ctx context.Context, subjectRef string) (caseruntime.Case, bool, error) {
	return r.base.FindBySubjectRef(ctx, subjectRef)
}

type PersistentQueueRepository struct {
	base *workplan.InMemoryQueueRepository
	mgr  *Manager
}

func WrapQueueRepository(base *workplan.InMemoryQueueRepository, mgr *Manager) *PersistentQueueRepository {
	if mgr == nil || !mgr.Enabled() {
		return &PersistentQueueRepository{base: base}
	}
	return &PersistentQueueRepository{base: base, mgr: mgr}
}
func (r *PersistentQueueRepository) SaveQueue(ctx context.Context, q workplan.WorkQueue) error {
	if err := r.base.SaveQueue(ctx, q); err != nil {
		return err
	}
	if r.mgr != nil {
		return r.mgr.Record("queue_saved", q)
	}
	return nil
}
func (r *PersistentQueueRepository) GetQueue(ctx context.Context, id string) (workplan.WorkQueue, bool, error) {
	return r.base.GetQueue(ctx, id)
}
func (r *PersistentQueueRepository) ListQueues(ctx context.Context) ([]workplan.WorkQueue, error) {
	return r.base.ListQueues(ctx)
}
func (r *PersistentQueueRepository) SaveWorkItem(ctx context.Context, wi workplan.WorkItem) error {
	if err := r.base.SaveWorkItem(ctx, wi); err != nil {
		return err
	}
	if r.mgr != nil {
		return r.mgr.Record("work_item_saved", wi)
	}
	return nil
}
func (r *PersistentQueueRepository) GetWorkItem(ctx context.Context, id string) (workplan.WorkItem, bool, error) {
	return r.base.GetWorkItem(ctx, id)
}
func (r *PersistentQueueRepository) ListWorkItemsByCase(ctx context.Context, caseID string) ([]workplan.WorkItem, error) {
	return r.base.ListWorkItemsByCase(ctx, caseID)
}
func (r *PersistentQueueRepository) ListWorkItemsByQueue(ctx context.Context, queueID string) ([]workplan.WorkItem, error) {
	return r.base.ListWorkItemsByQueue(ctx, queueID)
}
func (r *PersistentQueueRepository) ListWorkItems(ctx context.Context) ([]workplan.WorkItem, error) {
	return r.base.ListWorkItems(ctx)
}

type PersistentCoordinationRepository struct {
	base *workplan.InMemoryCoordinationRepository
	mgr  *Manager
}

func WrapCoordinationRepository(base *workplan.InMemoryCoordinationRepository, mgr *Manager) workplan.CoordinationRepository {
	if mgr == nil || !mgr.Enabled() {
		return base
	}
	return &PersistentCoordinationRepository{base: base, mgr: mgr}
}
func (r *PersistentCoordinationRepository) SaveDecision(ctx context.Context, d workplan.CoordinationDecision) error {
	if err := r.base.SaveDecision(ctx, d); err != nil {
		return err
	}
	return r.mgr.Record("coordination_saved", d)
}
func (r *PersistentCoordinationRepository) GetDecision(ctx context.Context, id string) (workplan.CoordinationDecision, bool, error) {
	return r.base.GetDecision(ctx, id)
}
func (r *PersistentCoordinationRepository) ListByWorkItem(ctx context.Context, workItemID string) ([]workplan.CoordinationDecision, error) {
	return r.base.ListByWorkItem(ctx, workItemID)
}
func (r *PersistentCoordinationRepository) ListByCase(ctx context.Context, caseID string) ([]workplan.CoordinationDecision, error) {
	return r.base.ListByCase(ctx, caseID)
}
func (r *PersistentCoordinationRepository) ListByQueue(ctx context.Context, queueID string) ([]workplan.CoordinationDecision, error) {
	return r.base.ListByQueue(ctx, queueID)
}

type PersistentPolicyRepository struct {
	base *policy.InMemoryRepository
	mgr  *Manager
}

func WrapPolicyRepository(base *policy.InMemoryRepository, mgr *Manager) policy.PolicyRepository {
	if mgr == nil || !mgr.Enabled() {
		return base
	}
	return &PersistentPolicyRepository{base: base, mgr: mgr}
}
func (r *PersistentPolicyRepository) SaveDecision(ctx context.Context, d policy.PolicyDecision) error {
	if err := r.base.SaveDecision(ctx, d); err != nil {
		return err
	}
	return r.mgr.Record("policy_decision_saved", d)
}
func (r *PersistentPolicyRepository) GetDecision(ctx context.Context, id string) (policy.PolicyDecision, bool, error) {
	return r.base.GetDecision(ctx, id)
}
func (r *PersistentPolicyRepository) ListByCoordinationDecision(ctx context.Context, id string) ([]policy.PolicyDecision, error) {
	return r.base.ListByCoordinationDecision(ctx, id)
}
func (r *PersistentPolicyRepository) SaveApprovalRequest(ctx context.Context, a policy.ApprovalRequest) error {
	if err := r.base.SaveApprovalRequest(ctx, a); err != nil {
		return err
	}
	return r.mgr.Record("approval_request_saved", a)
}
func (r *PersistentPolicyRepository) GetApprovalRequest(ctx context.Context, id string) (policy.ApprovalRequest, bool, error) {
	return r.base.GetApprovalRequest(ctx, id)
}
func (r *PersistentPolicyRepository) ListApprovalRequests(ctx context.Context) ([]policy.ApprovalRequest, error) {
	return r.base.ListApprovalRequests(ctx)
}
func (r *PersistentPolicyRepository) ListApprovalRequestsByCoordinationDecision(ctx context.Context, id string) ([]policy.ApprovalRequest, error) {
	return r.base.ListApprovalRequestsByCoordinationDecision(ctx, id)
}

type PersistentProposalRepository struct {
	base *proposal.InMemoryRepository
	mgr  *Manager
}

func WrapProposalRepository(base *proposal.InMemoryRepository, mgr *Manager) proposal.Repository {
	if mgr == nil || !mgr.Enabled() {
		return base
	}
	return &PersistentProposalRepository{base: base, mgr: mgr}
}
func (r *PersistentProposalRepository) Save(ctx context.Context, p proposal.Proposal) error {
	if err := r.base.Save(ctx, p); err != nil {
		return err
	}
	return r.mgr.Record("proposal_saved", p)
}
func (r *PersistentProposalRepository) Get(ctx context.Context, id string) (proposal.Proposal, bool, error) {
	return r.base.Get(ctx, id)
}
func (r *PersistentProposalRepository) ListByWorkItem(ctx context.Context, workItemID string) ([]proposal.Proposal, error) {
	return r.base.ListByWorkItem(ctx, workItemID)
}
func (r *PersistentProposalRepository) ListByActor(ctx context.Context, actorID string) ([]proposal.Proposal, error) {
	return r.base.ListByActor(ctx, actorID)
}

type PersistentDirectory struct {
	base *employee.InMemoryDirectory
	mgr  *Manager
}

func WrapDirectory(base *employee.InMemoryDirectory, mgr *Manager) employee.Directory {
	if mgr == nil || !mgr.Enabled() {
		return base
	}
	return &PersistentDirectory{base: base, mgr: mgr}
}
func (r *PersistentDirectory) SaveEmployee(ctx context.Context, e employee.DigitalEmployee) error {
	if err := r.base.SaveEmployee(ctx, e); err != nil {
		return err
	}
	return r.mgr.Record("employee_saved", e)
}
func (r *PersistentDirectory) GetEmployee(ctx context.Context, id string) (employee.DigitalEmployee, bool, error) {
	return r.base.GetEmployee(ctx, id)
}
func (r *PersistentDirectory) ListEmployees(ctx context.Context) ([]employee.DigitalEmployee, error) {
	return r.base.ListEmployees(ctx)
}
func (r *PersistentDirectory) ListEmployeesByQueue(ctx context.Context, queueID string) ([]employee.DigitalEmployee, error) {
	return r.base.ListEmployeesByQueue(ctx, queueID)
}

type PersistentAssignments struct {
	base *employee.InMemoryAssignmentRepository
	mgr  *Manager
}

func WrapAssignments(base *employee.InMemoryAssignmentRepository, mgr *Manager) employee.AssignmentRepository {
	if mgr == nil || !mgr.Enabled() {
		return base
	}
	return &PersistentAssignments{base: base, mgr: mgr}
}
func (r *PersistentAssignments) SaveAssignment(ctx context.Context, a employee.Assignment) error {
	if err := r.base.SaveAssignment(ctx, a); err != nil {
		return err
	}
	return r.mgr.Record("assignment_saved", a)
}
func (r *PersistentAssignments) GetAssignment(ctx context.Context, id string) (employee.Assignment, bool, error) {
	return r.base.GetAssignment(ctx, id)
}
func (r *PersistentAssignments) ListAssignmentsByWorkItem(ctx context.Context, workItemID string) ([]employee.Assignment, error) {
	return r.base.ListAssignmentsByWorkItem(ctx, workItemID)
}
func (r *PersistentAssignments) ListAssignmentsByEmployee(ctx context.Context, employeeID string) ([]employee.Assignment, error) {
	return r.base.ListAssignmentsByEmployee(ctx, employeeID)
}

type PersistentTrustRepository struct {
	base *trust.InMemoryRepository
	mgr  *Manager
}

func WrapTrustRepository(base *trust.InMemoryRepository, mgr *Manager) trust.Repository {
	if mgr == nil || !mgr.Enabled() {
		return base
	}
	return &PersistentTrustRepository{base: base, mgr: mgr}
}
func (r *PersistentTrustRepository) Save(ctx context.Context, p trust.TrustProfile) error {
	if err := r.base.Save(ctx, p); err != nil {
		return err
	}
	return r.mgr.Record("trust_saved", p)
}
func (r *PersistentTrustRepository) GetByActor(ctx context.Context, actorID string) (trust.TrustProfile, bool, error) {
	return r.base.GetByActor(ctx, actorID)
}
func (r *PersistentTrustRepository) List(ctx context.Context) ([]trust.TrustProfile, error) {
	return r.base.List(ctx)
}

type PersistentProfileRepository struct {
	base *profile.InMemoryRepository
	mgr  *Manager
}

func WrapProfileRepository(base *profile.InMemoryRepository, mgr *Manager) *PersistentProfileRepository {
	if mgr == nil || !mgr.Enabled() {
		return &PersistentProfileRepository{base: base}
	}
	return &PersistentProfileRepository{base: base, mgr: mgr}
}
func (r *PersistentProfileRepository) SaveProfile(ctx context.Context, p profile.CompetencyProfile) error {
	if err := r.base.SaveProfile(ctx, p); err != nil {
		return err
	}
	return r.mgr.Record("profile_saved", p)
}
func (r *PersistentProfileRepository) GetProfile(ctx context.Context, id string) (profile.CompetencyProfile, bool, error) {
	return r.base.GetProfile(ctx, id)
}
func (r *PersistentProfileRepository) GetProfileByActor(ctx context.Context, actorID string) (profile.CompetencyProfile, bool, error) {
	return r.base.GetProfileByActor(ctx, actorID)
}
func (r *PersistentProfileRepository) ListProfiles(ctx context.Context) ([]profile.CompetencyProfile, error) {
	return r.base.ListProfiles(ctx)
}
func (r *PersistentProfileRepository) SaveRequirement(ctx context.Context, req profile.CapabilityRequirement) error {
	if err := r.base.SaveRequirement(ctx, req); err != nil {
		return err
	}
	return r.mgr.Record("requirement_saved", req)
}
func (r *PersistentProfileRepository) ListRequirements(ctx context.Context) ([]profile.CapabilityRequirement, error) {
	return r.base.ListRequirements(ctx)
}

type PersistentCapabilityRepository struct {
	base *capability.InMemoryCapabilityRepository
	mgr  *Manager
}

func WrapCapabilityRepository(base *capability.InMemoryCapabilityRepository, mgr *Manager) *PersistentCapabilityRepository {
	if mgr == nil || !mgr.Enabled() {
		return &PersistentCapabilityRepository{base: base}
	}
	return &PersistentCapabilityRepository{base: base, mgr: mgr}
}
func (r *PersistentCapabilityRepository) SaveCapability(ctx context.Context, c capability.Capability) error {
	if err := r.base.SaveCapability(ctx, c); err != nil {
		return err
	}
	if r.mgr != nil {
		return r.mgr.Record("capability_saved", c)
	}
	return nil
}
func (r *PersistentCapabilityRepository) GetCapability(ctx context.Context, id string) (capability.Capability, bool, error) {
	return r.base.GetCapability(ctx, id)
}
func (r *PersistentCapabilityRepository) ListCapabilities(ctx context.Context) ([]capability.Capability, error) {
	return r.base.ListCapabilities(ctx)
}
func (r *PersistentCapabilityRepository) AssignCapability(ctx context.Context, ac capability.ActorCapability) error {
	if err := r.base.AssignCapability(ctx, ac); err != nil {
		return err
	}
	if r.mgr != nil {
		return r.mgr.Record("actor_capability_saved", ac)
	}
	return nil
}
func (r *PersistentCapabilityRepository) ListByActor(ctx context.Context, actorID string) ([]capability.ActorCapability, error) {
	return r.base.ListByActor(ctx, actorID)
}

type PersistentExecutionRepository struct {
	base *executionruntime.InMemoryExecutionRepository
	mgr  *Manager
}

func WrapExecutionRepository(base *executionruntime.InMemoryExecutionRepository, mgr *Manager) executionruntime.ExecutionRepository {
	if mgr == nil || !mgr.Enabled() {
		return base
	}
	return &PersistentExecutionRepository{base: base, mgr: mgr}
}
func (r *PersistentExecutionRepository) SaveSession(ctx context.Context, s executionruntime.ExecutionSession) error {
	if err := r.base.SaveSession(ctx, s); err != nil {
		return err
	}
	return r.mgr.Record("execution_session_saved", s)
}
func (r *PersistentExecutionRepository) GetSession(ctx context.Context, id string) (executionruntime.ExecutionSession, bool, error) {
	return r.base.GetSession(ctx, id)
}
func (r *PersistentExecutionRepository) ListSessionsByWorkItem(ctx context.Context, workItemID string) ([]executionruntime.ExecutionSession, error) {
	return r.base.ListSessionsByWorkItem(ctx, workItemID)
}
func (r *PersistentExecutionRepository) SaveStep(ctx context.Context, s executionruntime.StepExecution) error {
	if err := r.base.SaveStep(ctx, s); err != nil {
		return err
	}
	return r.mgr.Record("step_execution_saved", s)
}
func (r *PersistentExecutionRepository) GetStep(ctx context.Context, id string) (executionruntime.StepExecution, bool, error) {
	return r.base.GetStep(ctx, id)
}
func (r *PersistentExecutionRepository) ListStepsBySession(ctx context.Context, sessionID string) ([]executionruntime.StepExecution, error) {
	return r.base.ListStepsBySession(ctx, sessionID)
}

type PersistentWAL struct {
	base *executionruntime.InMemoryWAL
	mgr  *Manager
}

func WrapWAL(base *executionruntime.InMemoryWAL, mgr *Manager) executionruntime.WAL {
	if mgr == nil || !mgr.Enabled() {
		return base
	}
	return &PersistentWAL{base: base, mgr: mgr}
}
func (w *PersistentWAL) Append(ctx context.Context, r executionruntime.WALRecord) error {
	if err := w.base.Append(ctx, r); err != nil {
		return err
	}
	return w.mgr.Record("wal_appended", r)
}
func (w *PersistentWAL) ListBySession(ctx context.Context, sessionID string) ([]executionruntime.WALRecord, error) {
	return w.base.ListBySession(ctx, sessionID)
}
