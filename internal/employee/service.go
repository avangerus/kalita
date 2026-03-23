package employee

import (
	"context"
	"fmt"

	"kalita/internal/actionplan"
	"kalita/internal/eventcore"
	"kalita/internal/executioncontrol"
	"kalita/internal/executionruntime"
	"kalita/internal/trust"
	"kalita/internal/workplan"
)

type employeeService struct {
	assignments AssignmentRepository
	selector    Selector
	runtime     executionruntime.Service
	trust       trust.Service
	log         eventcore.EventLog
	clock       eventcore.Clock
	ids         eventcore.IDGenerator
}

func NewService(assignments AssignmentRepository, selector Selector, runtime executionruntime.Service, log eventcore.EventLog, clock eventcore.Clock, ids eventcore.IDGenerator, trustServices ...trust.Service) Service {
	if clock == nil {
		clock = eventcore.RealClock{}
	}
	if ids == nil {
		ids = eventcore.NewULIDGenerator()
	}
	var trustService trust.Service
	if len(trustServices) > 0 {
		trustService = trustServices[0]
	}
	return &employeeService{assignments: assignments, selector: selector, runtime: runtime, trust: trustService, log: log, clock: clock, ids: ids}
}

func (s *employeeService) AssignAndStartExecution(ctx context.Context, wi workplan.WorkItem, plan actionplan.ActionPlan, constraints executioncontrol.ExecutionConstraints, metadata RunMetadata) (Assignment, executionruntime.ExecutionSession, error) {
	if s.assignments == nil {
		return Assignment{}, executionruntime.ExecutionSession{}, fmt.Errorf("assignment repository is nil")
	}
	if s.selector == nil {
		return Assignment{}, executionruntime.ExecutionSession{}, fmt.Errorf("employee selector is nil")
	}
	if s.runtime == nil {
		return Assignment{}, executionruntime.ExecutionSession{}, fmt.Errorf("execution runtime service is nil")
	}
	employee, reason, err := s.selector.SelectForWorkItem(ctx, wi, plan)
	if err != nil {
		return Assignment{}, executionruntime.ExecutionSession{}, err
	}
	now := s.clock.Now()
	assignment := Assignment{ID: s.ids.NewID(), WorkItemID: wi.ID, CaseID: metadata.CaseID, QueueID: metadata.QueueID, EmployeeID: employee.ID, AssignedAt: now, Reason: reason}
	if err := s.assignments.SaveAssignment(ctx, assignment); err != nil {
		return Assignment{}, executionruntime.ExecutionSession{}, err
	}
	if s.log != nil {
		meta := executionruntime.ExecutionMetadataFromContext(ctx)
		if err := s.log.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{ID: s.ids.NewID(), ExecutionID: meta.ExecutionID, CaseID: metadata.CaseID, Step: "employee_assigned", Status: "assigned", OccurredAt: now, CorrelationID: meta.CorrelationID, CausationID: meta.CausationID, Payload: map[string]any{"employee_id": employee.ID, "work_item_id": wi.ID, "case_id": metadata.CaseID, "queue_id": metadata.QueueID, "assignment_id": assignment.ID}}); err != nil {
			return Assignment{}, executionruntime.ExecutionSession{}, err
		}
	}
	trustProfile := trust.DefaultTrustProfile(employee.ID, now)
	if s.trust != nil {
		profile, ok, err := s.trust.GetTrustProfile(ctx, employee.ID)
		if err != nil {
			return Assignment{}, executionruntime.ExecutionSession{}, err
		}
		if ok {
			trustProfile = profile
		}
	}
	constraints, adjustmentReason := executioncontrol.AdjustForTrust(constraints, trustProfile)
	if s.log != nil {
		meta := executionruntime.ExecutionMetadataFromContext(ctx)
		if err := s.log.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{ID: s.ids.NewID(), ExecutionID: meta.ExecutionID, CaseID: metadata.CaseID, Step: "execution_mode_adjusted_by_trust", Status: string(trustProfile.TrustLevel), OccurredAt: s.clock.Now(), CorrelationID: meta.CorrelationID, CausationID: meta.CausationID, Payload: map[string]any{"employee_id": employee.ID, "work_item_id": wi.ID, "execution_mode": constraints.ExecutionMode, "max_steps": constraints.MaxSteps, "max_duration_sec": constraints.MaxDurationSec, "trust_level": trustProfile.TrustLevel, "reason": adjustmentReason}}); err != nil {
			return Assignment{}, executionruntime.ExecutionSession{}, err
		}
	}
	session, err := s.runtime.StartExecution(ctx, plan, constraints, executionruntime.RunMetadata{CaseID: metadata.CaseID, WorkItemID: wi.ID, CoordinationDecisionID: metadata.CoordinationDecisionID, PolicyDecisionID: metadata.PolicyDecisionID})
	if err != nil {
		return Assignment{}, executionruntime.ExecutionSession{}, err
	}
	return assignment, session, nil
}
