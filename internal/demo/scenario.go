package demo

import (
	"context"
	"fmt"
	"sort"
	"time"

	"kalita/internal/actionplan"
	"kalita/internal/capability"
	"kalita/internal/caseruntime"
	"kalita/internal/command"
	"kalita/internal/controlplane"
	"kalita/internal/employee"
	"kalita/internal/eventcore"
	"kalita/internal/executioncontrol"
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
	ControlPlane       controlplane.Service
	EventLog           eventcore.EventLog
	CaseID             string
	WorkItemID         string
	CoordinationID     string
	PolicyDecisionID   string
	ApprovalRequestID  string
	CorrelationID      string
	ExecutionID        string
	BaseTime           time.Time
	CaseRepo           caseruntime.CaseRepository
	QueueRepo          *workplan.InMemoryQueueRepository
	CoordinationRepo   *workplan.InMemoryCoordinationRepository
	PolicyRepo         *policy.InMemoryRepository
	ExecutionRepo      *executionruntime.InMemoryExecutionRepository
	CaseIDs            []string
	WorkItemIDs        []string
	ApprovalRequestIDs []string
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
	trustService     trust.Service
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
	runtimeService   executionruntime.Service
}

type scenarioOutcome struct {
	caseID            string
	workItemID        string
	coordinationID    string
	policyDecisionID  string
	approvalRequestID string
	correlationID     string
	executionID       string
}

type actorSeed struct {
	ID                string
	Role              string
	TrustLevel        trust.TrustLevel
	AutonomyTier      trust.AutonomyTier
	SuccessCount      int
	FailureCount      int
	CompensationCount int
	PreserveTrust     bool
	MaxComplexity     int
	CapabilityLvl     int
}

type multiCaseSeed struct {
	label             string
	ids               scenarioDefinition
	metadata          map[string]any
	actors            []actorSeed
	executionActorID  string
	actionParams      map[string]any
	complexity        int
	approveAfterDefer bool
	promoteHighTrust  bool
	simulateExecuting bool
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

func RunAISOtkhodyMultiScenario(ctx context.Context) (*ScenarioResult, error) {
	runtime, err := newScenarioRuntime(defaultMultiDemoClock(), defaultDemoIDs())
	if err != nil {
		return nil, err
	}
	return runtime.runAISMultiScenario(ctx)
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
	trustService := trust.NewService(trustRepo, trust.NewDeterministicScorer(clock.Now))
	profileRepo := profile.NewInMemoryRepository()
	capRepo := capability.NewInMemoryRepository()
	executionRepo := executionruntime.NewInMemoryExecutionRepository()
	wal := executionruntime.NewInMemoryWAL()
	if err := seedDemoQueue(context.Background(), queueRepo); err != nil {
		return nil, err
	}
	commandBus := command.NewService(eventLog, command.PassThroughAdmissionPolicy{}, clock, ids)
	caseService := caseruntime.NewService(caseruntime.NewResolver(caseRepo, clock, ids), eventLog, clock, ids)
	planner := workplan.NewPlanner(workplan.NewInMemoryPlanRepository(), eventLog, clock, ids)
	coordinator := workplan.NewCoordinator(coordinationRepo, eventLog, clock, ids)
	workService := workplan.NewService(queueRepo, workplan.NewRouter(queueRepo, DemoQueueID), planner, coordinator, eventLog, clock, ids)
	policyService := policy.NewService(policyRepo, policy.NewEvaluator(), eventLog, clock, ids)
	runner := executionruntime.NewRunner(executionRepo, wal, executionruntime.NewLegacyWorkflowActionExecutor(), eventLog, clock, ids, trustService)
	runtimeService := executionruntime.NewService(runner)
	controlPlaneService := controlplane.NewService(caseRepo, queueRepo, coordinationRepo, policyRepo, proposalRepo, directory, trustRepo, profileRepo, capRepo, executionRepo, wal, eventLog, coordinator)
	return &scenarioRuntime{baseTime: baseTime, clock: clock, ids: ids, eventLog: eventLog, caseRepo: caseRepo, queueRepo: queueRepo, coordinationRepo: coordinationRepo, policyRepo: policyRepo, proposalRepo: proposalRepo, directory: directory, trustRepo: trustRepo, trustService: trustService, profileRepo: profileRepo, capRepo: capRepo, executionRepo: executionRepo, wal: wal, commandBus: commandBus, caseService: caseService, coordinator: coordinator, workService: workService, policyService: policyService, controlPlane: controlPlaneService, runtimeService: runtimeService}, nil
}

func defaultDemoClock() *scriptedClock {
	baseTime := time.Date(2026, 3, 23, 12, 0, 0, 0, time.UTC)
	times := make([]time.Time, 0, 64)
	for i := 0; i < 64; i++ {
		times = append(times, baseTime.Add(time.Duration(i)*time.Minute))
	}
	return &scriptedClock{times: times}
}

func defaultMultiDemoClock() *scriptedClock { return defaultDemoClock() }

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
		DemoCoordinationID, "execevt-policy",
		DemoPolicyDecisionID, "execevt-approval",
		DemoApprovalRequestID,
		"execevt-ais-command-admission",
		AISDemoCaseID, "execevt-ais-case-created",
		AISDemoWorkItemID, "execevt-ais-work-item-created", AISDemoPlanID, "execevt-ais-plan-intake",
		"demo-id-ais-default-coordination", "demo-id-ais-default-coordination-event",
		AISDemoCoordinationID, "execevt-ais-policy",
		AISDemoPolicyDecisionID, "execevt-ais-approval",
		AISDemoApprovalRequestID,
	}}
}

func (r *scenarioRuntime) runGenericScenario(ctx context.Context) (*ScenarioResult, error) {
	if err := r.replaceActors(ctx, defaultLowTrustActors()); err != nil {
		return nil, err
	}
	outcome, err := r.runScenarioFlow(ctx, scenarioDefinition{
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
	}, 1, "", nil)
	if err != nil {
		return nil, err
	}
	return r.resultFromOutcomes([]scenarioOutcome{outcome}), nil
}

func (r *scenarioRuntime) runAISScenario(ctx context.Context) (*ScenarioResult, error) {
	if err := r.replaceActors(ctx, defaultLowTrustActors()); err != nil {
		return nil, err
	}
	outcome, err := r.runScenarioFlow(ctx, scenarioDefinition{
		eventID:        AISDemoEventID,
		commandID:      AISDemoCommandID,
		correlationID:  AISDemoCorrelationID,
		executionID:    AISDemoExecutionID,
		eventType:      "missed_container_pickup_review",
		targetRef:      fmt.Sprintf("route:%s/container:%s", aisScenarioMetadata()["route_id"], aisScenarioMetadata()["container_site_id"]),
		eventPayload:   aisScenarioMetadata(),
		commandPayload: aisScenarioMetadata(),
		caseID:         AISDemoCaseID, workItemID: AISDemoWorkItemID, coordinationID: AISDemoCoordinationID,
		policyDecisionID: AISDemoPolicyDecisionID, approvalRequestID: AISDemoApprovalRequestID,
	}, 1, "", nil)
	if err != nil {
		return nil, err
	}
	return r.resultFromOutcomes([]scenarioOutcome{outcome}), nil
}

func (r *scenarioRuntime) runAISMultiScenario(ctx context.Context) (*ScenarioResult, error) {
	seeds := []multiCaseSeed{
		{label: "A", ids: scenarioDefinition{eventID: "evt-ais-multi-a", commandID: "cmd-ais-multi-a", correlationID: "corr-ais-multi-a", executionID: "exec-ais-multi-a", caseID: "case-ais-multi-a", workItemID: "work-ais-multi-a", coordinationID: "coord-ais-multi-a", policyDecisionID: "policy-ais-multi-a", approvalRequestID: "approval-ais-multi-a"}, metadata: aisScenarioMetadataFor(0), actors: []actorSeed{{ID: "actor-growth-1", Role: "container_triage", TrustLevel: trust.TrustLow, AutonomyTier: trust.AutonomyRestricted, SuccessCount: 2, MaxComplexity: 3, CapabilityLvl: 2}}, executionActorID: "actor-growth-1", complexity: 1, approveAfterDefer: true, promoteHighTrust: true},
		{label: "B", ids: scenarioDefinition{eventID: "evt-ais-multi-b", commandID: "cmd-ais-multi-b", correlationID: "corr-ais-multi-b", executionID: "exec-ais-multi-b", caseID: "case-ais-multi-b", workItemID: "work-ais-multi-b", coordinationID: "coord-ais-multi-b", policyDecisionID: "policy-ais-multi-b", approvalRequestID: "approval-ais-multi-b"}, metadata: aisScenarioMetadataFor(1), actors: []actorSeed{{ID: "actor-growth-1", Role: "container_triage", PreserveTrust: true, MaxComplexity: 3, CapabilityLvl: 2}}, executionActorID: "actor-growth-1", complexity: 1, simulateExecuting: true},
		{label: "C", ids: scenarioDefinition{eventID: "evt-ais-multi-c", commandID: "cmd-ais-multi-c", correlationID: "corr-ais-multi-c", executionID: "exec-ais-multi-c", caseID: "case-ais-multi-c", workItemID: "work-ais-multi-c", coordinationID: "coord-ais-multi-c", policyDecisionID: "policy-ais-multi-c", approvalRequestID: "approval-ais-multi-c"}, metadata: aisScenarioMetadataFor(2), actors: []actorSeed{{ID: "actor-growth-1", Role: "container_triage", TrustLevel: trust.TrustMedium, AutonomyTier: trust.AutonomySupervised, SuccessCount: 3, FailureCount: 1, MaxComplexity: 3, CapabilityLvl: 2}}, executionActorID: "actor-growth-1", actionParams: map[string]any{"fail": true}, complexity: 1},
		{label: "D", ids: scenarioDefinition{eventID: "evt-ais-multi-d", commandID: "cmd-ais-multi-d", correlationID: "corr-ais-multi-d", executionID: "exec-ais-multi-d", caseID: "case-ais-multi-d", workItemID: "work-ais-multi-d", coordinationID: "coord-ais-multi-d", policyDecisionID: "policy-ais-multi-d", approvalRequestID: "approval-ais-multi-d"}, metadata: aisScenarioMetadataFor(3), actors: []actorSeed{{ID: "actor-growth-1", Role: "container_triage", PreserveTrust: true, MaxComplexity: 3, CapabilityLvl: 2}}, complexity: 1},
		{label: "E", ids: scenarioDefinition{eventID: "evt-ais-multi-e", commandID: "cmd-ais-multi-e", correlationID: "corr-ais-multi-e", executionID: "exec-ais-multi-e", caseID: "case-ais-multi-e", workItemID: "work-ais-multi-e", coordinationID: "coord-ais-multi-e", policyDecisionID: "policy-ais-multi-e", approvalRequestID: "approval-ais-multi-e"}, metadata: aisScenarioMetadataFor(4), actors: []actorSeed{{ID: "actor-comp-1", Role: "container_triage", TrustLevel: trust.TrustHigh, AutonomyTier: trust.AutonomyStandard, SuccessCount: 6, MaxComplexity: 3, CapabilityLvl: 2}}, executionActorID: "actor-comp-1", actionParams: map[string]any{"first_action_fail": true}, complexity: 2},
	}
	outcomes := make([]scenarioOutcome, 0, len(seeds))
	for _, seed := range seeds {
		if err := r.replaceActors(ctx, seed.actors); err != nil {
			return nil, err
		}
		def := seed.ids
		def.eventType = "missed_container_pickup_review"
		def.targetRef = fmt.Sprintf("route:%s/container:%s", seed.metadata["route_id"], seed.metadata["container_site_id"])
		def.eventPayload = cloneMap(seed.metadata)
		def.commandPayload = cloneMap(seed.metadata)
		outcome, err := r.runScenarioFlow(ctx, def, seed.complexity, seed.executionActorID, seed.actionParams)
		if err != nil {
			return nil, fmt.Errorf("run multi-scenario case %s: %w", seed.label, err)
		}
		if seed.approveAfterDefer {
			if seed.promoteHighTrust {
				if err := r.replaceActors(ctx, []actorSeed{{ID: "actor-high-approval", Role: "supervised_release", TrustLevel: trust.TrustHigh, AutonomyTier: trust.AutonomyStandard, MaxComplexity: 3, CapabilityLvl: 2}}); err != nil {
					return nil, err
				}
			}
			if _, err := r.controlPlane.ApproveApprovalRequest(ctx, outcome.approvalRequestID); err != nil {
				return nil, err
			}
			if err := r.evaluateLatestDecisionAndMaybeExecute(ctx, outcome.caseID, outcome.workItemID, outcome.executionID, seed.executionActorID, seed.actionParams, false); err != nil {
				return nil, err
			}
		}
		if seed.simulateExecuting {
			if err := r.simulateRunningExecution(ctx, outcome.workItemID, outcome.executionID); err != nil {
				return nil, err
			}
		}
		if seed.label == "D" {
			if err := r.appendEscalationWaitEvent(ctx, outcome); err != nil {
				return nil, err
			}
		}
		outcomes = append(outcomes, outcome)
	}
	sort.Slice(outcomes, func(i, j int) bool { return outcomes[i].caseID < outcomes[j].caseID })
	return r.resultFromOutcomes(outcomes), nil
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

func (r *scenarioRuntime) runScenarioFlow(ctx context.Context, def scenarioDefinition, complexity int, executionActorID string, actionParams map[string]any) (scenarioOutcome, error) {
	if err := r.eventLog.AppendEvent(ctx, eventcore.Event{ID: def.eventID, Type: def.eventType, OccurredAt: r.baseTime, Source: "demo.scenario", CorrelationID: def.correlationID, CausationID: def.eventID, ExecutionID: def.executionID, Payload: cloneMap(def.eventPayload)}); err != nil {
		return scenarioOutcome{}, fmt.Errorf("append demo event: %w", err)
	}
	if err := r.eventLog.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{ID: fmt.Sprintf("%s-detected", def.eventID), ExecutionID: def.executionID, Step: "incident_detected", Status: "recorded", OccurredAt: r.baseTime, CorrelationID: def.correlationID, CausationID: def.eventID, Payload: cloneMap(def.eventPayload)}); err != nil {
		return scenarioOutcome{}, fmt.Errorf("append demo detection event: %w", err)
	}
	cmd, err := r.commandBus.Submit(ctx, eventcore.Command{Type: def.eventType, ID: def.commandID, CorrelationID: def.correlationID, CausationID: def.eventID, ExecutionID: def.executionID, RequestedAt: r.baseTime.Add(30 * time.Second), TargetRef: def.targetRef, Payload: cloneMap(def.commandPayload)})
	if err != nil {
		return scenarioOutcome{}, fmt.Errorf("submit demo command: %w", err)
	}
	resolved, err := r.caseService.ResolveCommand(ctx, cmd)
	if err != nil {
		return scenarioOutcome{}, fmt.Errorf("resolve demo command: %w", err)
	}
	intake, err := r.workService.IntakeCommand(ctx, resolved)
	if err != nil {
		return scenarioOutcome{}, fmt.Errorf("intake demo command: %w", err)
	}
	intake.WorkItem, err = r.workService.AttachActionPlan(ctx, intake.WorkItem.ID, demoActionPlan(intake.WorkItem.ID, resolved.Case.ID, def.commandPayload["route_id"], actionParams))
	if err != nil {
		return scenarioOutcome{}, fmt.Errorf("attach demo action plan: %w", err)
	}
	actors, profiles, err := demoCoordinationInputs(ctx, r.directory, r.trustRepo, r.profileRepo)
	if err != nil {
		return scenarioOutcome{}, err
	}
	coordinationCtx := workplan.ContextWithPlanningExecution(ctx, workplan.PlanningExecutionContext{ExecutionID: cmd.ExecutionID, CorrelationID: cmd.CorrelationID, CausationID: cmd.ID})
	decision, err := r.coordinator.Decide(coordinationCtx, intake.WorkItem, workplan.CoordinationContext{ActionTypes: []string{"legacy_workflow_action"}, Complexity: complexity, Actors: actors, Profiles: profiles})
	if err != nil {
		return scenarioOutcome{}, fmt.Errorf("coordinate demo work item: %w", err)
	}
	policyCtx := policy.ContextWithExecution(ctx, policy.ExecutionContext{ExecutionID: cmd.ExecutionID, CorrelationID: cmd.CorrelationID, CausationID: cmd.ID})
	policyDecision, approvalRequest, err := r.policyService.EvaluateAndRecord(policyCtx, decision)
	if err != nil {
		return scenarioOutcome{}, fmt.Errorf("evaluate demo policy: %w", err)
	}
	if approvalRequest == nil && decision.DecisionType == workplan.CoordinationDefer {
		return scenarioOutcome{}, fmt.Errorf("demo scenario expected approval request")
	}
	if approvalRequest == nil && decision.DecisionType == workplan.CoordinationExecuteNow {
		if err := r.startExecutionForWorkItem(ctx, intake.WorkItem, decision, policyDecision, cmd.ExecutionID, executionActorID); err != nil {
			return scenarioOutcome{}, err
		}
	}
	approvalID := ""
	if approvalRequest != nil {
		approvalID = approvalRequest.ID
	}
	return scenarioOutcome{caseID: resolved.Case.ID, workItemID: intake.WorkItem.ID, coordinationID: decision.ID, policyDecisionID: policyDecision.ID, approvalRequestID: approvalID, correlationID: cmd.CorrelationID, executionID: cmd.ExecutionID}, nil
}

func seedDemoQueue(ctx context.Context, queueRepo *workplan.InMemoryQueueRepository) error {
	if err := queueRepo.SaveQueue(ctx, workplan.WorkQueue{ID: DemoQueueID, Name: "Demo Container Incident Intake", Department: "operations", Purpose: "Deterministic demo queue for container incidents", AllowedCaseKinds: []string{"container_incident_detected", "missed_container_pickup_review"}}); err != nil {
		return fmt.Errorf("seed demo queue: %w", err)
	}
	return nil
}

func defaultLowTrustActors() []actorSeed {
	return []actorSeed{{ID: "actor-low-1", Role: "container_triage", TrustLevel: trust.TrustLow, AutonomyTier: trust.AutonomyRestricted, MaxComplexity: 2, CapabilityLvl: 1}, {ID: "actor-low-2", Role: "container_triage", TrustLevel: trust.TrustLow, AutonomyTier: trust.AutonomyRestricted, MaxComplexity: 2, CapabilityLvl: 1}}
}

func (r *scenarioRuntime) replaceActors(ctx context.Context, actors []actorSeed) error {
	existing, err := r.directory.ListEmployees(ctx)
	if err != nil {
		return err
	}
	for _, emp := range existing {
		emp.Enabled = false
		emp.UpdatedAt = r.clock.Now()
		if err := r.directory.SaveEmployee(ctx, emp); err != nil {
			return err
		}
	}
	if _, ok, err := r.capRepo.GetCapability(ctx, "cap-demo-container-ops"); err != nil {
		return err
	} else if !ok {
		if err := r.capRepo.SaveCapability(ctx, capability.Capability{ID: "cap-demo-container-ops", Code: "workflow.execute", Type: capability.CapabilitySkill, Level: 2}); err != nil {
			return fmt.Errorf("seed capability: %w", err)
		}
	}
	for _, actor := range actors {
		now := r.clock.Now()
		emp := employee.DigitalEmployee{ID: actor.ID, Code: actor.ID, Role: actor.Role, Enabled: true, QueueMemberships: []string{DemoQueueID}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action"}, AllowedCommandTypes: []string{"container_incident_detected", "missed_container_pickup_review"}, CreatedAt: now, UpdatedAt: now}
		if err := r.directory.SaveEmployee(ctx, emp); err != nil {
			return err
		}
		if err := r.capRepo.AssignCapability(ctx, capability.ActorCapability{ActorID: actor.ID, CapabilityID: "cap-demo-container-ops", Level: actor.CapabilityLvl}); err != nil {
			return err
		}
		if actor.PreserveTrust {
			if _, ok, err := r.trustRepo.GetByActor(ctx, actor.ID); err != nil {
				return err
			} else if !ok {
				actor.PreserveTrust = false
			}
		}
		if !actor.PreserveTrust {
			profile := trust.TrustProfile{
				ActorID:               actor.ID,
				Metrics:               trust.TrustMetrics{SuccessCount: actor.SuccessCount, FailureCount: actor.FailureCount, CompensationCount: actor.CompensationCount},
				CompletedExecutions:   actor.SuccessCount,
				FailedExecutions:      actor.FailureCount,
				CompensatedExecutions: actor.CompensationCount,
				TrustLevel:            actor.TrustLevel,
				AutonomyTier:          actor.AutonomyTier,
				UpdatedAt:             now,
			}
			if profile.TrustLevel == "" {
				profile.TrustLevel, profile.AutonomyTier = deriveSeedTrust(profile)
			}
			if err := r.trustRepo.Save(ctx, profile); err != nil {
				return err
			}
		}
		if err := r.profileRepo.SaveProfile(ctx, profile.CompetencyProfile{ID: "profile-" + actor.ID, ActorID: actor.ID, Name: actor.Role + " " + actor.ID, MaxComplexity: actor.MaxComplexity, PreferredWorkKinds: []string{"container_incident_detected", "missed_container_pickup_review"}}); err != nil {
			return err
		}
	}
	return nil
}

func (r *scenarioRuntime) evaluateLatestDecisionAndMaybeExecute(ctx context.Context, caseID, workItemID, executionID, actorID string, actionParams map[string]any, simulateRunning bool) error {
	decisions, err := r.coordinationRepo.ListByWorkItem(ctx, workItemID)
	if err != nil || len(decisions) == 0 {
		return err
	}
	decision := decisions[len(decisions)-1]
	policyDecision, approvalRequest, err := r.policyService.EvaluateAndRecord(policy.ContextWithExecution(ctx, policy.ExecutionContext{ExecutionID: executionID, CorrelationID: r.caseCorrelationID(ctx, caseID), CausationID: decision.ID}), decision)
	if err != nil {
		return err
	}
	if approvalRequest != nil {
		return nil
	}
	wi, ok, err := r.queueRepo.GetWorkItem(ctx, workItemID)
	if err != nil || !ok {
		return err
	}
	if len(actionParams) > 0 {
		plan := demoActionPlan(wi.ID, wi.CaseID, "", actionParams)
		wi.ActionPlan = &plan
	}
	if err := r.startExecutionForWorkItem(ctx, wi, decision, policyDecision, executionID, actorID); err != nil {
		return err
	}
	if simulateRunning {
		return r.simulateRunningExecution(ctx, workItemID, executionID)
	}
	return nil
}

func (r *scenarioRuntime) startExecutionForWorkItem(ctx context.Context, wi workplan.WorkItem, decision workplan.CoordinationDecision, policyDecision policy.PolicyDecision, executionID, actorID string) error {
	if wi.ActionPlan == nil {
		return fmt.Errorf("work item %s missing action plan", wi.ID)
	}
	session, err := r.runtimeService.StartExecution(executionruntime.ContextWithExecution(ctx, executionruntime.ExecutionContext{ExecutionID: executionID, CorrelationID: r.caseCorrelationID(ctx, wi.CaseID), CausationID: policyDecision.ID}), *wi.ActionPlan, executioncontrol.ExecutionConstraints{ID: "constraints-" + wi.ID, ExecutionMode: executioncontrol.ExecutionModeDeterministicAPI, MaxSteps: 8, MaxDurationSec: 300}, executionruntime.RunMetadata{CaseID: wi.CaseID, WorkItemID: wi.ID, CoordinationDecisionID: decision.ID, PolicyDecisionID: policyDecision.ID, ActorID: actorID})
	if err != nil {
		return fmt.Errorf("start execution for %s: %w", wi.ID, err)
	}
	_ = session
	return nil
}

func demoActionPlan(workItemID, caseID string, routeID any, actionParams map[string]any) actionplan.ActionPlan {
	params := map[string]any{"route_id": routeID, "case_id": caseID}
	for k, v := range actionParams {
		params[k] = v
	}
	actions := []actionplan.Action{
		{ID: "action-" + workItemID + "-1", Type: actionplan.ActionType("legacy_workflow_action"), Params: cloneMap(params)},
	}
	if first, _ := actionParams["first_action_fail"].(bool); first {
		actions[0].Reversibility = actionplan.ReversibilityCompensatable
		actions[0].Compensation = &actionplan.Action{ID: "compensation-" + workItemID + "-1", Type: actionplan.ActionType("legacy_workflow_action"), Params: map[string]any{"route_id": routeID, "case_id": caseID}}
		actions = append(actions, actionplan.Action{ID: "action-" + workItemID + "-2", Type: actionplan.ActionType("legacy_workflow_action"), Params: map[string]any{"route_id": routeID, "case_id": caseID, "fail": true}})
		delete(actions[0].Params, "first_action_fail")
	} else {
		delete(actions[0].Params, "first_action_fail")
	}
	return actionplan.ActionPlan{ID: "action-plan-" + workItemID, Actions: actions}
}

func deriveSeedTrust(profile trust.TrustProfile) (trust.TrustLevel, trust.AutonomyTier) {
	if profile.Metrics.SuccessCount >= 6 {
		return trust.TrustHigh, trust.AutonomyStandard
	}
	if profile.Metrics.SuccessCount >= 3 {
		return trust.TrustMedium, trust.AutonomySupervised
	}
	return trust.TrustLow, trust.AutonomyRestricted
}

func (r *scenarioRuntime) simulateRunningExecution(ctx context.Context, workItemID, executionID string) error {
	sessions, err := r.executionRepo.ListSessionsByWorkItem(ctx, workItemID)
	if err != nil || len(sessions) == 0 {
		return err
	}
	session := sessions[len(sessions)-1]
	session.Status = executionruntime.ExecutionSessionRunning
	session.UpdatedAt = session.CreatedAt.Add(2 * time.Minute)
	if err := r.executionRepo.SaveSession(ctx, session); err != nil {
		return err
	}
	steps, err := r.executionRepo.ListStepsBySession(ctx, session.ID)
	if err == nil && len(steps) > 0 {
		step := steps[0]
		step.Status = executionruntime.StepRunning
		started := session.CreatedAt.Add(time.Minute)
		step.StartedAt = &started
		step.FinishedAt = nil
		if err := r.executionRepo.SaveStep(ctx, step); err != nil {
			return err
		}
	}
	return r.eventLog.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{ID: fmt.Sprintf("%s-running", session.ID), ExecutionID: executionID, CaseID: session.CaseID, Step: "execution_session_heartbeat", Status: "running", OccurredAt: session.UpdatedAt, CorrelationID: r.caseCorrelationID(ctx, session.CaseID), CausationID: session.ID, Payload: map[string]any{"work_item_id": workItemID, "execution_session_id": session.ID}})
}

func (r *scenarioRuntime) appendEscalationWaitEvent(ctx context.Context, outcome scenarioOutcome) error {
	return r.eventLog.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{ID: outcome.caseID + "-escalated", ExecutionID: outcome.executionID, CaseID: outcome.caseID, Step: "escalation_waiting", Status: "awaiting_supervisor_capacity", OccurredAt: r.clock.Now(), CorrelationID: outcome.correlationID, CausationID: outcome.coordinationID, Payload: map[string]any{"case_id": outcome.caseID, "work_item_id": outcome.workItemID, "reason": "long_waiting_escalated"}})
}

func (r *scenarioRuntime) resultFromOutcomes(outcomes []scenarioOutcome) *ScenarioResult {
	res := &ScenarioResult{ControlPlane: r.controlPlane, EventLog: r.eventLog, BaseTime: r.baseTime, CaseRepo: r.caseRepo, QueueRepo: r.queueRepo, CoordinationRepo: r.coordinationRepo, PolicyRepo: r.policyRepo, ExecutionRepo: r.executionRepo}
	if len(outcomes) > 0 {
		first := outcomes[0]
		res.CaseID, res.WorkItemID, res.CoordinationID, res.PolicyDecisionID, res.ApprovalRequestID, res.CorrelationID, res.ExecutionID = first.caseID, first.workItemID, first.coordinationID, first.policyDecisionID, first.approvalRequestID, first.correlationID, first.executionID
	}
	for _, outcome := range outcomes {
		res.CaseIDs = append(res.CaseIDs, outcome.caseID)
		res.WorkItemIDs = append(res.WorkItemIDs, outcome.workItemID)
		if outcome.approvalRequestID != "" {
			res.ApprovalRequestIDs = append(res.ApprovalRequestIDs, outcome.approvalRequestID)
		}
	}
	return res
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

func aisScenarioMetadata() map[string]any { return aisScenarioMetadataFor(2048 - 2048) }

func aisScenarioMetadataFor(idx int) map[string]any {
	districts := []string{"Nevsky", "Tsentralny", "Vyborgsky", "Kalininsky", "Moskovsky"}
	zones := []string{"North-East", "Center", "North-West", "Industrial Belt", "South"}
	sources := []string{"photo/GPS", "dispatcher_call", "weight_sensor", "citizen_portal", "yard_scan"}
	reasons := []string{"Photo/GPS mismatch", "Dispatcher reported missed stop", "Weight sensor shows unemptied load", "Resident complaint requires verification", "Yard scan shows missing return"}
	return map[string]any{
		"route_id":                fmt.Sprintf("R-%04d", 1001+idx),
		"carrier_id":              fmt.Sprintf("CR-%02d", 17+idx),
		"container_site_id":       fmt.Sprintf("SITE-%03d", 881+idx),
		"container_id":            fmt.Sprintf("CNT-%03d-%02d", 881+idx, 4+idx),
		"district":                districts[idx%len(districts)],
		"zone":                    zones[idx%len(zones)],
		"incident_source":         sources[idx%len(sources)],
		"incident_reason":         reasons[idx%len(reasons)],
		"expected_service_window": "2026-03-23T09:00:00Z",
		"route_completed_at":      fmt.Sprintf("2026-03-23T10:%02d:00Z", 30+idx),
		"operator_report_id":      fmt.Sprintf("OP-%03d", 771+idx),
		"yard_id":                 fmt.Sprintf("YARD-%02d", 4+idx),
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
		id := fmt.Sprintf("demo-id-%02d", f.idx)
		f.idx++
		return id
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

func (r *scenarioRuntime) caseCorrelationID(ctx context.Context, caseID string) string {
	c, ok, err := r.caseRepo.GetByID(ctx, caseID)
	if err == nil && ok {
		return c.CorrelationID
	}
	return ""
}
