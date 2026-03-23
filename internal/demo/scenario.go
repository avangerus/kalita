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

	AISDemoEventID           = "evt-demo-missed-pickup-review"
	AISDemoCommandID         = "cmd-demo-missed-pickup-review"
	AISDemoCorrelationID     = "corr-demo-missed-pickup-review"
	AISDemoExecutionID       = "exec-demo-missed-pickup-review"
	AISDemoCaseID            = "case-demo-missed-pickup-review"
	AISDemoWorkItemID        = "work-demo-missed-pickup-review"
	AISDemoPlanID            = "plan-demo-missed-pickup-review"
	AISDemoCoordinationID    = "coord-demo-missed-pickup-review"
	AISDemoPolicyDecisionID  = "policy-demo-missed-pickup-review"
	AISDemoApprovalRequestID = "approval-demo-missed-pickup-review"
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

type scenarioRuntime struct {
	baseTime         time.Time
	clock            *scriptedClock
	ids              *fixedIDs
	eventLog         eventcore.EventLog
	caseRepo         caseruntime.CaseRepository
	queueRepo        *workplan.InMemoryQueueRepository
	coordinationRepo *workplan.InMemoryCoordinationRepository
	policyRepo       *policy.InMemoryRepository
	proposalRepo     *proposal.InMemoryRepository
	directory        *employee.InMemoryDirectory
	trustRepo        *trust.InMemoryRepository
	profileRepo      *profile.InMemoryRepository
	capRepo          *capability.InMemoryCapabilityRepository
	executionRepo    *executionruntime.InMemoryExecutionRepository
	wal              *executionruntime.InMemoryWAL
	commandBus       command.CommandBus
	caseService      *caseruntime.Service
	coordinator      workplan.Coordinator
	workService      *workplan.Service
	policyService    *policy.PolicyService
	controlPlane     controlplane.Service
}

func RunDemoScenario(ctx context.Context) (*ScenarioResult, error) {
	runtime, err := newScenarioRuntime(defaultDemoClock(), defaultDemoIDs())
	if err != nil {
		return nil, err
	}
	return runtime.runGenericScenario(ctx)
}

func RunAISOtkhodyDemoScenario(ctx context.Context) (*ScenarioResult, error) {
	runtime, err := newScenarioRuntime(defaultDemoClock(), defaultAISDemoIDs())
	if err != nil {
		return nil, err
	}
	return runtime.runAISScenario(ctx)
}

func newScenarioRuntime(clock *scriptedClock, ids *fixedIDs) (*scenarioRuntime, error) {
	baseTime := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
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
	if err := seedDemoScenario(context.Background(), baseTime, queueRepo, directory, trustRepo, profileRepo, capRepo); err != nil {
		return nil, err
	}
	commandBus := command.NewService(eventLog, command.PassThroughAdmissionPolicy{}, clock, ids)
	caseService := caseruntime.NewService(caseruntime.NewResolver(caseRepo, clock, ids), eventLog, clock, ids)
	planner := workplan.NewPlanner(workplan.NewInMemoryPlanRepository(), eventLog, clock, ids)
	coordinator := workplan.NewCoordinator(coordinationRepo, eventLog, clock, ids)
	workService := workplan.NewService(queueRepo, workplan.NewRouter(queueRepo, DemoQueueID), planner, coordinator, eventLog, clock, ids)
	policyService := policy.NewService(policyRepo, policy.NewEvaluator(), eventLog, clock, ids)
	controlPlaneService := controlplane.NewService(caseRepo, queueRepo, coordinationRepo, policyRepo, proposalRepo, directory, trustRepo, profileRepo, capRepo, executionRepo, wal, eventLog, coordinator)
	return &scenarioRuntime{baseTime: baseTime, clock: clock, ids: ids, eventLog: eventLog, caseRepo: caseRepo, queueRepo: queueRepo, coordinationRepo: coordinationRepo, policyRepo: policyRepo, proposalRepo: proposalRepo, directory: directory, trustRepo: trustRepo, profileRepo: profileRepo, capRepo: capRepo, executionRepo: executionRepo, wal: wal, commandBus: commandBus, caseService: caseService, coordinator: coordinator, workService: workService, policyService: policyService, controlPlane: controlPlaneService}, nil
}

func defaultDemoClock() *scriptedClock {
	baseTime := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	return &scriptedClock{times: []time.Time{
		baseTime,
		baseTime.Add(1 * time.Minute),
		baseTime.Add(2 * time.Minute),
		baseTime.Add(3 * time.Minute),
		baseTime.Add(4 * time.Minute),
		baseTime.Add(5 * time.Minute),
		baseTime.Add(6 * time.Minute),
		baseTime.Add(7 * time.Minute),
		baseTime.Add(8 * time.Minute),
		baseTime.Add(9 * time.Minute),
		baseTime.Add(10 * time.Minute),
		baseTime.Add(11 * time.Minute),
	}}
}

func defaultAISDemoIDs() *fixedIDs {
	return &fixedIDs{ids: []string{
		"execevt-ais-command-admission",
		AISDemoCaseID, "execevt-ais-case-created",
		AISDemoWorkItemID, "execevt-ais-work-item-created", AISDemoPlanID, "execevt-ais-plan-intake",
		"demo-id-ais-default-coordination", "demo-id-ais-default-coordination-event",
		AISDemoCoordinationID, "execevt-ais-coordination",
		AISDemoPolicyDecisionID, "execevt-ais-policy",
		AISDemoApprovalRequestID, "execevt-ais-approval",
	}}
}

func defaultDemoIDs() *fixedIDs {
	return &fixedIDs{ids: []string{
		"execevt-command-admission",
		DemoCaseID, "execevt-case-created",
		DemoWorkItemID, "execevt-work-item-created", DemoPlanID, "execevt-plan-intake",
		"demo-id-intake-default-coordination", "demo-id-intake-default-coordination-event",
		DemoCoordinationID, "execevt-coordination",
		DemoPolicyDecisionID, "execevt-policy",
		DemoApprovalRequestID, "execevt-approval",

		"execevt-ais-command-admission",
		AISDemoCaseID, "execevt-ais-case-created",
		AISDemoWorkItemID, "execevt-ais-work-item-created", AISDemoPlanID, "execevt-ais-plan-intake",
		"demo-id-ais-default-coordination", "demo-id-ais-default-coordination-event",
		AISDemoCoordinationID, "execevt-ais-coordination",
		AISDemoPolicyDecisionID, "execevt-ais-policy",
		AISDemoApprovalRequestID, "execevt-ais-approval",
	}}
}

func (r *scenarioRuntime) runGenericScenario(ctx context.Context) (*ScenarioResult, error) {
	return r.runScenario(ctx, scenarioDefinition{
		eventID:        DemoEventID,
		commandID:      DemoCommandID,
		correlationID:  DemoCorrelationID,
		executionID:    DemoExecutionID,
		eventType:      "container_incident_detected",
		targetRef:      "container-demo-001",
		eventPayload:   map[string]any{"container_id": "container-demo-001", "severity": "high", "namespace": "payments"},
		commandPayload: map[string]any{"namespace": "payments", "severity": "high"},
		caseID:         DemoCaseID, workItemID: DemoWorkItemID, coordinationID: DemoCoordinationID,
		policyDecisionID: DemoPolicyDecisionID, approvalRequestID: DemoApprovalRequestID,
	})
}

func (r *scenarioRuntime) runAISScenario(ctx context.Context) (*ScenarioResult, error) {
	payload := aisScenarioMetadata()
	return r.runScenario(ctx, scenarioDefinition{
		eventID:        AISDemoEventID,
		commandID:      AISDemoCommandID,
		correlationID:  AISDemoCorrelationID,
		executionID:    AISDemoExecutionID,
		eventType:      "missed_container_pickup_review",
		targetRef:      fmt.Sprintf("route:%s/container:%s", payload["route_id"], payload["container_site_id"]),
		eventPayload:   payload,
		commandPayload: payload,
		caseID:         AISDemoCaseID, workItemID: AISDemoWorkItemID, coordinationID: AISDemoCoordinationID,
		policyDecisionID: AISDemoPolicyDecisionID, approvalRequestID: AISDemoApprovalRequestID,
	})
}

type scenarioDefinition struct {
	eventID           string
	commandID         string
	correlationID     string
	executionID       string
	eventType         string
	targetRef         string
	eventPayload      map[string]any
	commandPayload    map[string]any
	caseID            string
	workItemID        string
	coordinationID    string
	policyDecisionID  string
	approvalRequestID string
}

func (r *scenarioRuntime) runScenario(ctx context.Context, def scenarioDefinition) (*ScenarioResult, error) {
	if err := r.eventLog.AppendEvent(ctx, eventcore.Event{ID: def.eventID, Type: def.eventType, OccurredAt: r.baseTime, Source: "demo.scenario", CorrelationID: def.correlationID, CausationID: def.eventID, ExecutionID: def.executionID, Payload: cloneMap(def.eventPayload)}); err != nil {
		return nil, fmt.Errorf("append demo event: %w", err)
	}
	if err := r.eventLog.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{
		ID:            fmt.Sprintf("%s-detected", def.eventID),
		ExecutionID:   def.executionID,
		Step:          "incident_detected",
		Status:        "recorded",
		OccurredAt:    r.baseTime,
		CorrelationID: def.correlationID,
		CausationID:   def.eventID,
		Payload:       cloneMap(def.eventPayload),
	}); err != nil {
		return nil, fmt.Errorf("append demo detection event: %w", err)
	}
	cmd, err := r.commandBus.Submit(ctx, eventcore.Command{Type: def.eventType, ID: def.commandID, CorrelationID: def.correlationID, CausationID: def.eventID, ExecutionID: def.executionID, RequestedAt: r.baseTime.Add(30 * time.Second), TargetRef: def.targetRef, Payload: cloneMap(def.commandPayload)})
	if err != nil {
		return nil, fmt.Errorf("submit demo command: %w", err)
	}
	resolved, err := r.caseService.ResolveCommand(ctx, cmd)
	if err != nil {
		return nil, fmt.Errorf("resolve demo command: %w", err)
	}
	intake, err := r.workService.IntakeCommand(ctx, resolved)
	if err != nil {
		return nil, fmt.Errorf("intake demo command: %w", err)
	}
	actors, profiles, err := demoCoordinationInputs(ctx, r.directory, r.trustRepo, r.profileRepo)
	if err != nil {
		return nil, err
	}
	coordinationCtx := workplan.ContextWithPlanningExecution(ctx, workplan.PlanningExecutionContext{ExecutionID: cmd.ExecutionID, CorrelationID: cmd.CorrelationID, CausationID: cmd.ID})
	decision, err := r.coordinator.Decide(coordinationCtx, intake.WorkItem, workplan.CoordinationContext{ActionTypes: []string{"legacy_workflow_action"}, Complexity: 1, Actors: actors, Profiles: profiles})
	if err != nil {
		return nil, fmt.Errorf("coordinate demo work item: %w", err)
	}
	policyCtx := policy.ContextWithExecution(ctx, policy.ExecutionContext{ExecutionID: cmd.ExecutionID, CorrelationID: cmd.CorrelationID, CausationID: cmd.ID})
	policyDecision, approvalRequest, err := r.policyService.EvaluateAndRecord(policyCtx, decision)
	if err != nil {
		return nil, fmt.Errorf("evaluate demo policy: %w", err)
	}
	if approvalRequest == nil {
		return nil, fmt.Errorf("demo scenario expected approval request")
	}
	return &ScenarioResult{ControlPlane: r.controlPlane, EventLog: r.eventLog, CaseID: resolved.Case.ID, WorkItemID: intake.WorkItem.ID, CoordinationID: decision.ID, PolicyDecisionID: policyDecision.ID, ApprovalRequestID: approvalRequest.ID, CorrelationID: cmd.CorrelationID, ExecutionID: cmd.ExecutionID, BaseTime: r.baseTime, CaseRepo: r.caseRepo, QueueRepo: r.queueRepo, CoordinationRepo: r.coordinationRepo, PolicyRepo: r.policyRepo, ExecutionRepo: r.executionRepo}, nil
}

func seedDemoScenario(ctx context.Context, now time.Time, queueRepo *workplan.InMemoryQueueRepository, directory *employee.InMemoryDirectory, trustRepo *trust.InMemoryRepository, profileRepo *profile.InMemoryRepository, capRepo *capability.InMemoryCapabilityRepository) error {
	if err := queueRepo.SaveQueue(ctx, workplan.WorkQueue{ID: DemoQueueID, Name: "Demo Container Incident Intake", Department: "operations", Purpose: "Deterministic demo queue for container incidents", AllowedCaseKinds: []string{"container_incident_detected", "missed_container_pickup_review"}}); err != nil {
		return fmt.Errorf("seed demo queue: %w", err)
	}
	actors := []employee.DigitalEmployee{
		{ID: "actor-low-1", Code: "container_triage_low_1", Role: "container_triage", Enabled: true, QueueMemberships: []string{DemoQueueID}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}, AllowedCommandTypes: []string{"container_incident_detected", "missed_container_pickup_review"}, CreatedAt: now, UpdatedAt: now},
		{ID: "actor-low-2", Code: "container_triage_low_2", Role: "container_triage", Enabled: true, QueueMemberships: []string{DemoQueueID}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}, AllowedCommandTypes: []string{"container_incident_detected", "missed_container_pickup_review"}, CreatedAt: now, UpdatedAt: now},
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
		{ID: "profile-low-1", ActorID: "actor-low-1", Name: "Low Trust Container Triage 1", MaxComplexity: 2, PreferredWorkKinds: []string{"container_incident_detected", "missed_container_pickup_review"}},
		{ID: "profile-low-2", ActorID: "actor-low-2", Name: "Low Trust Container Triage 2", MaxComplexity: 2, PreferredWorkKinds: []string{"container_incident_detected", "missed_container_pickup_review"}},
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

func aisScenarioMetadata() map[string]any {
	return map[string]any{
		"route_id":                "R-2048",
		"carrier_id":              "CR-17",
		"container_site_id":       "SITE-881",
		"container_id":            "CNT-881-04",
		"district":                "Nevsky",
		"zone":                    "North-East",
		"incident_source":         "photo/GPS",
		"incident_reason":         "Photo/GPS mismatch",
		"expected_service_window": "2026-03-23T09:00:00Z",
		"route_completed_at":      "2026-03-23T10:46:00Z",
		"operator_report_id":      "OP-771",
		"yard_id":                 "YARD-04",
	}
}

func cloneMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
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
