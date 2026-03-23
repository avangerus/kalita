package demo

import (
	"context"
	"fmt"
	"time"

	"kalita/internal/actionplan"
	"kalita/internal/capability"
	"kalita/internal/caseruntime"
	"kalita/internal/command"
	"kalita/internal/controlplane"
	"kalita/internal/employee"
	"kalita/internal/eventcore"
	"kalita/internal/executionruntime"
	"kalita/internal/policy"
	"kalita/internal/profile"
	"kalita/internal/proposal"
	"kalita/internal/trust"
	"kalita/internal/workplan"
)

const (
	DemoEventID           = "evt-demo-container-incident"
	DemoCommandID         = "cmd-demo-container-incident"
	DemoCorrelationID     = "corr-demo-container-incident"
	DemoExecutionID       = "exec-demo-container-incident"
	DemoCaseID            = "case-demo-container-incident"
	DemoWorkItemID        = "work-demo-container-incident"
	DemoPlanID            = "plan-demo-container-incident"
	DemoCoordinationID    = "coord-demo-container-incident"
	DemoPolicyDecisionID  = "policy-demo-container-incident"
	DemoApprovalRequestID = "approval-demo-container-incident"
	DemoQueueID           = "queue-demo-container-incidents"
)

type ScenarioResult struct {
	ControlPlane      controlplane.Service
	EventLog          eventcore.EventLog
	CaseID            string
	WorkItemID        string
	CoordinationID    string
	PolicyDecisionID  string
	ApprovalRequestID string
	CorrelationID     string
	ExecutionID       string
	BaseTime          time.Time
	CaseRepo          caseruntime.CaseRepository
	QueueRepo         *workplan.InMemoryQueueRepository
	CoordinationRepo  *workplan.InMemoryCoordinationRepository
	PolicyRepo        *policy.InMemoryRepository
	ExecutionRepo     *executionruntime.InMemoryExecutionRepository
}

func RunDemoScenario(ctx context.Context) (*ScenarioResult, error) {
	baseTime := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	clock := &scriptedClock{times: []time.Time{
		baseTime,
		baseTime.Add(1 * time.Minute),
		baseTime.Add(2 * time.Minute),
		baseTime.Add(3 * time.Minute),
		baseTime.Add(4 * time.Minute),
		baseTime.Add(5 * time.Minute),
		baseTime.Add(6 * time.Minute),
		baseTime.Add(7 * time.Minute),
	}}
	ids := &fixedIDs{ids: []string{
		"execevt-command-admission",
		DemoCaseID, "execevt-case-created",
		DemoWorkItemID, "execevt-work-item-created", DemoPlanID, "execevt-plan-intake",
		"demo-id-intake-default-coordination", "demo-id-intake-default-coordination-event",
		DemoCoordinationID, "execevt-coordination",
		DemoPolicyDecisionID, "execevt-policy",
		DemoApprovalRequestID, "execevt-approval",
	}}

	eventLog := eventcore.NewInMemoryEventLog()
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	queueRepo := workplan.NewInMemoryQueueRepository()
	coordinationRepo := workplan.NewInMemoryCoordinationRepository()
	policyRepo := policy.NewInMemoryRepository()
	proposalRepo := proposal.NewInMemoryRepository()
	directory := employee.NewInMemoryDirectory()
	trustRepo := trust.NewInMemoryRepository()
	profileRepo := profile.NewInMemoryRepository()
	capRepo := capability.NewInMemoryRepository()
	executionRepo := executionruntime.NewInMemoryExecutionRepository()
	wal := executionruntime.NewInMemoryWAL()

	if err := seedDemoScenario(ctx, baseTime, queueRepo, directory, trustRepo, profileRepo, capRepo); err != nil {
		return nil, err
	}

	commandBus := command.NewService(eventLog, command.PassThroughAdmissionPolicy{}, clock, ids)
	caseService := caseruntime.NewService(caseruntime.NewResolver(caseRepo, clock, ids), eventLog, clock, ids)
	planner := workplan.NewPlanner(workplan.NewInMemoryPlanRepository(), eventLog, clock, ids)
	coordinator := workplan.NewCoordinator(coordinationRepo, eventLog, clock, ids)
	workService := workplan.NewService(queueRepo, workplan.NewRouter(queueRepo, DemoQueueID), planner, coordinator, eventLog, clock, ids)
	policyService := policy.NewService(policyRepo, policy.NewEvaluator(), eventLog, clock, ids)
	controlPlaneService := controlplane.NewService(caseRepo, queueRepo, coordinationRepo, policyRepo, proposalRepo, directory, trustRepo, profileRepo, capRepo, executionRepo, wal, eventLog)

	if err := eventLog.AppendEvent(ctx, eventcore.Event{ID: DemoEventID, Type: "container_incident_detected", OccurredAt: baseTime, Source: "demo.scenario", CorrelationID: DemoCorrelationID, CausationID: DemoEventID, ExecutionID: DemoExecutionID, Payload: map[string]any{"container_id": "container-demo-001", "severity": "high", "namespace": "payments"}}); err != nil {
		return nil, fmt.Errorf("append demo event: %w", err)
	}

	cmd, err := commandBus.Submit(ctx, eventcore.Command{Type: "container_incident_detected", ID: DemoCommandID, CorrelationID: DemoCorrelationID, CausationID: DemoEventID, ExecutionID: DemoExecutionID, RequestedAt: baseTime.Add(30 * time.Second), TargetRef: "container-demo-001", Payload: map[string]any{"namespace": "payments", "severity": "high"}})
	if err != nil {
		return nil, fmt.Errorf("submit demo command: %w", err)
	}
	resolved, err := caseService.ResolveCommand(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("resolve demo command: %w", err)
	}
	intake, err := workService.IntakeCommand(ctx, resolved)
	if err != nil {
		return nil, fmt.Errorf("intake demo command: %w", err)
	}

	actors, profiles, err := demoCoordinationInputs(ctx, directory, trustRepo, profileRepo)
	if err != nil {
		return nil, err
	}
	coordinationCtx := workplan.ContextWithPlanningExecution(ctx, workplan.PlanningExecutionContext{ExecutionID: cmd.ExecutionID, CorrelationID: cmd.CorrelationID, CausationID: cmd.ID})
	decision, err := coordinator.Decide(coordinationCtx, intake.WorkItem, workplan.CoordinationContext{ActionTypes: []string{"legacy_workflow_action"}, Complexity: 1, Actors: actors, Profiles: profiles})
	if err != nil {
		return nil, fmt.Errorf("coordinate demo work item: %w", err)
	}
	policyCtx := policy.ContextWithExecution(ctx, policy.ExecutionContext{ExecutionID: cmd.ExecutionID, CorrelationID: cmd.CorrelationID, CausationID: cmd.ID})
	policyDecision, approvalRequest, err := policyService.EvaluateAndRecord(policyCtx, decision)
	if err != nil {
		return nil, fmt.Errorf("evaluate demo policy: %w", err)
	}
	if approvalRequest == nil {
		return nil, fmt.Errorf("demo scenario expected approval request")
	}

	return &ScenarioResult{ControlPlane: controlPlaneService, EventLog: eventLog, CaseID: resolved.Case.ID, WorkItemID: intake.WorkItem.ID, CoordinationID: decision.ID, PolicyDecisionID: policyDecision.ID, ApprovalRequestID: approvalRequest.ID, CorrelationID: cmd.CorrelationID, ExecutionID: cmd.ExecutionID, BaseTime: baseTime, CaseRepo: caseRepo, QueueRepo: queueRepo, CoordinationRepo: coordinationRepo, PolicyRepo: policyRepo, ExecutionRepo: executionRepo}, nil
}

func seedDemoScenario(ctx context.Context, now time.Time, queueRepo *workplan.InMemoryQueueRepository, directory *employee.InMemoryDirectory, trustRepo *trust.InMemoryRepository, profileRepo *profile.InMemoryRepository, capRepo *capability.InMemoryCapabilityRepository) error {
	if err := queueRepo.SaveQueue(ctx, workplan.WorkQueue{ID: DemoQueueID, Name: "Demo Container Incident Intake", Department: "operations", Purpose: "Deterministic demo queue for container incidents", AllowedCaseKinds: []string{"container_incident_detected"}}); err != nil {
		return fmt.Errorf("seed demo queue: %w", err)
	}
	actors := []employee.DigitalEmployee{
		{ID: "actor-low-1", Code: "container_triage_low_1", Role: "container_triage", Enabled: true, QueueMemberships: []string{DemoQueueID}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}, AllowedCommandTypes: []string{"container_incident_detected"}, CreatedAt: now, UpdatedAt: now},
		{ID: "actor-low-2", Code: "container_triage_low_2", Role: "container_triage", Enabled: true, QueueMemberships: []string{DemoQueueID}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}, AllowedCommandTypes: []string{"container_incident_detected"}, CreatedAt: now, UpdatedAt: now},
	}
	for _, actor := range actors {
		if err := directory.SaveEmployee(ctx, actor); err != nil {
			return fmt.Errorf("seed actor %s: %w", actor.ID, err)
		}
	}
	if err := capRepo.SaveCapability(ctx, capability.Capability{ID: "cap-demo-container-ops", Code: "workflow.execute", Type: capability.CapabilitySkill, Level: 1}); err != nil {
		return fmt.Errorf("seed capability: %w", err)
	}
	for _, actorID := range []string{"actor-low-1", "actor-low-2"} {
		if err := capRepo.AssignCapability(ctx, capability.ActorCapability{ActorID: actorID, CapabilityID: "cap-demo-container-ops", Level: 1}); err != nil {
			return fmt.Errorf("assign capability %s: %w", actorID, err)
		}
		if err := trustRepo.Save(ctx, trust.TrustProfile{ActorID: actorID, TrustLevel: trust.TrustLow, AutonomyTier: trust.AutonomyRestricted, UpdatedAt: now}); err != nil {
			return fmt.Errorf("seed trust %s: %w", actorID, err)
		}
	}
	profiles := []profile.CompetencyProfile{
		{ID: "profile-low-1", ActorID: "actor-low-1", Name: "Low Trust Container Triage 1", MaxComplexity: 2, PreferredWorkKinds: []string{"container_incident_detected"}},
		{ID: "profile-low-2", ActorID: "actor-low-2", Name: "Low Trust Container Triage 2", MaxComplexity: 2, PreferredWorkKinds: []string{"container_incident_detected"}},
	}
	for _, prof := range profiles {
		if err := profileRepo.SaveProfile(ctx, prof); err != nil {
			return fmt.Errorf("seed profile %s: %w", prof.ID, err)
		}
	}
	return nil
}

func demoCoordinationInputs(ctx context.Context, directory *employee.InMemoryDirectory, trustRepo *trust.InMemoryRepository, profileRepo *profile.InMemoryRepository) ([]workplan.CoordinationActor, map[string]workplan.CoordinationActorProfile, error) {
	employees, err := directory.ListEmployees(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list demo employees: %w", err)
	}
	actors := make([]workplan.CoordinationActor, 0, len(employees))
	profiles := make(map[string]workplan.CoordinationActorProfile, len(employees))
	for _, emp := range employees {
		actions := make([]string, 0, len(emp.AllowedActionTypes))
		for _, actionType := range emp.AllowedActionTypes {
			actions = append(actions, string(actionType))
		}
		actors = append(actors, workplan.CoordinationActor{ID: emp.ID, Enabled: emp.Enabled, QueueMemberships: append([]string(nil), emp.QueueMemberships...), AllowedActionTypes: actions})
		coordProfile := workplan.CoordinationActorProfile{ActorID: emp.ID}
		if prof, ok, err := profileRepo.GetProfileByActor(ctx, emp.ID); err != nil {
			return nil, nil, fmt.Errorf("get profile for %s: %w", emp.ID, err)
		} else if ok {
			coordProfile.MaxComplexity = prof.MaxComplexity
		}
		if trustProfile, ok, err := trustRepo.GetByActor(ctx, emp.ID); err != nil {
			return nil, nil, fmt.Errorf("get trust for %s: %w", emp.ID, err)
		} else if ok {
			coordProfile.TrustLevel = string(trustProfile.TrustLevel)
			coordProfile.TrustAvailable = true
		}
		profiles[emp.ID] = coordProfile
	}
	return actors, profiles, nil
}

type fixedIDs struct {
	ids []string
	idx int
}

func (f *fixedIDs) NewID() string {
	if f.idx >= len(f.ids) {
		return fmt.Sprintf("demo-id-%02d", f.idx)
	}
	id := f.ids[f.idx]
	f.idx++
	return id
}

type scriptedClock struct {
	times []time.Time
	idx   int
}

func (c *scriptedClock) Now() time.Time {
	if len(c.times) == 0 {
		return time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	}
	if c.idx >= len(c.times) {
		return c.times[len(c.times)-1]
	}
	t := c.times[c.idx]
	c.idx++
	return t
}
