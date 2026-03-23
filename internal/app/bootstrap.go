package app

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"kalita/internal/actionplan"
	"kalita/internal/blob"
	"kalita/internal/capability"
	"kalita/internal/caseruntime"
	"kalita/internal/catalog"
	"kalita/internal/command"
	"kalita/internal/config"
	"kalita/internal/controlplane"
	"kalita/internal/employee"
	"kalita/internal/eventcore"
	"kalita/internal/executioncontrol"
	"kalita/internal/executionruntime"
	"kalita/internal/integration"
	"kalita/internal/persistence"
	"kalita/internal/policy"
	"kalita/internal/postgres"
	"kalita/internal/profile"
	"kalita/internal/proposal"
	"kalita/internal/runtime"
	"kalita/internal/schema"
	"kalita/internal/trust"
	"kalita/internal/workplan"
)

// BootstrapResult holds the initialized application components
type BootstrapResult struct {
	Storage            *runtime.Storage
	EventLog           eventcore.EventLog
	CommandBus         command.CommandBus
	CaseRepo           caseruntime.CaseRepository
	CaseResolver       caseruntime.CaseResolver
	CaseService        *caseruntime.Service
	QueueRepo          workplan.QueueRepository
	PlanRepo           workplan.PlanRepository
	CoordinationRepo   workplan.CoordinationRepository
	AssignmentRouter   workplan.AssignmentRouter
	Planner            workplan.Planner
	Coordinator        workplan.Coordinator
	WorkService        *workplan.Service
	PolicyRepo         policy.PolicyRepository
	PolicyEvaluator    policy.Evaluator
	PolicyService      policy.Service
	ConstraintsRepo    executioncontrol.ConstraintsRepository
	ConstraintsPlanner executioncontrol.ConstraintsPlanner
	ConstraintsService executioncontrol.ConstraintsService
	ActionRegistry     actionplan.Registry
	ActionCompiler     actionplan.Compiler
	ActionValidator    actionplan.Validator
	ActionPlanService  actionplan.Service
	ProposalRepo       proposal.Repository
	ProposalValidator  proposal.Validator
	ProposalCompiler   proposal.CompilerAdapter
	ProposalService    proposal.Service
	EmployeeDirectory  employee.Directory
	AssignmentRepo     employee.AssignmentRepository
	EmployeeSelector   employee.Selector
	EmployeeService    employee.Service
	TrustRepo          trust.Repository
	TrustScorer        trust.Scorer
	TrustService       trust.Service
	ExecutionRepo      executionruntime.ExecutionRepository
	ExecutionWAL       executionruntime.WAL
	ActionExecutor     executionruntime.ActionExecutor
	ExecutionRunner    executionruntime.Runner
	ExecutionRuntime   executionruntime.Service
	ControlPlane       controlplane.Service
	IntegrationService integration.IncidentService
	Config             config.Config
}

func Bootstrap(cfgPath string) (*BootstrapResult, error) {
	cfg := config.LoadWithPath(cfgPath)

	fmt.Printf("Kalita starting on :%s (db=%s, autoMigrate=%v, blob=%s, persistence=%v)\n",
		cfg.Port, tern(cfg.DBURL != "", "pg", "memory"), cfg.AutoMigrate, cfg.BlobDriver, cfg.PersistenceEnabled)

	entityMap, err := schema.LoadAllEntities(cfg.DSLDir)
	if err != nil {
		return nil, fmt.Errorf("ошибка загрузки DSL из %q: %w", cfg.DSLDir, err)
	}
	fmt.Printf("Загружено сущностей: %d\n", len(entityMap))
	entities := make([]*schema.Entity, 0, len(entityMap))
	for _, e := range entityMap {
		entities = append(entities, e)
	}

	if cfg.DBURL != "" {
		db, err := postgres.Open(cfg.DBURL)
		if err != nil {
			return nil, fmt.Errorf("PG connect failed: %w", err)
		}
		defer db.Close()
		log.Printf("PG connected")
		if cfg.AutoMigrate {
			ddl, err := postgres.GenerateDDL(entityMap)
			if err != nil {
				return nil, fmt.Errorf("DDL generate failed: %w", err)
			}
			if err := postgres.ApplyDDL(db, ddl); err != nil {
				return nil, fmt.Errorf("DDL apply failed: %w", err)
			}
			log.Printf("DDL applied (add-only)")
		}
	}

	enumCatalog, err := catalog.LoadEnumCatalog(cfg.EnumsDir)
	if err != nil {
		return nil, fmt.Errorf("ошибка загрузки enum-справочников из %q: %w", cfg.EnumsDir, err)
	}
	fmt.Printf("Загружено enum-справочников: %d\n", len(enumCatalog))

	st := runtime.NewStorage(entities, enumCatalog)
	clock := eventcore.RealClock{}
	ids := eventcore.NewULIDGenerator()
	planRepo := workplan.NewInMemoryPlanRepository()
	baseEventLog := eventcore.NewInMemoryEventLog()
	baseCaseRepo := caseruntime.NewInMemoryCaseRepository()
	baseQueueRepo := workplan.NewInMemoryQueueRepository()
	baseCoordinationRepo := workplan.NewInMemoryCoordinationRepository()
	basePolicyRepo := policy.NewInMemoryRepository()
	baseProposalRepo := proposal.NewInMemoryRepository()
	baseExecutionRepo := executionruntime.NewInMemoryExecutionRepository()
	baseExecutionWAL := executionruntime.NewInMemoryWAL()
	baseEmployeeDirectory := employee.NewInMemoryDirectory()
	baseAssignmentRepo := employee.NewInMemoryAssignmentRepository()
	baseTrustRepo := trust.NewInMemoryRepository()
	baseCapabilityRepo := capability.NewInMemoryRepository()
	baseProfileRepo := profile.NewInMemoryRepository()

	var persistMgr *persistence.Manager
	if cfg.PersistenceEnabled {
		persistDir := cfg.PersistenceDir
		if strings.TrimSpace(persistDir) == "" {
			persistDir = filepath.Join(filepath.Dir(cfgPath), ".kalita-persistence")
		}
		persistMgr = persistence.NewManager(persistence.NewFileEventStore(persistDir), persistence.NewFileSnapshotStore(persistDir), cfg.SnapshotEvery)
	}

	eventLog := persistence.WrapEventLog(baseEventLog, persistMgr)
	caseRepo := persistence.WrapCaseRepository(baseCaseRepo, persistMgr)
	queueRepo := persistence.WrapQueueRepository(baseQueueRepo, persistMgr)
	coordinationRepo := persistence.WrapCoordinationRepository(baseCoordinationRepo, persistMgr)
	policyRepo := persistence.WrapPolicyRepository(basePolicyRepo, persistMgr)
	proposalRepo := persistence.WrapProposalRepository(baseProposalRepo, persistMgr)
	executionRepo := persistence.WrapExecutionRepository(baseExecutionRepo, persistMgr)
	executionWAL := persistence.WrapWAL(baseExecutionWAL, persistMgr)
	employeeDirectory := persistence.WrapDirectory(baseEmployeeDirectory, persistMgr)
	assignmentRepo := persistence.WrapAssignments(baseAssignmentRepo, persistMgr)
	trustRepo := persistence.WrapTrustRepository(baseTrustRepo, persistMgr)
	capabilityRepo := persistence.WrapCapabilityRepository(baseCapabilityRepo, persistMgr)
	profileRepo := persistence.WrapProfileRepository(baseProfileRepo, persistMgr)

	if persistMgr != nil && persistMgr.Enabled() {
		persistMgr.BindCollector(&persistence.StateCollector{
			Cases: baseCaseRepo, Queues: baseQueueRepo, Coordinations: baseCoordinationRepo, Policies: basePolicyRepo,
			Proposals: baseProposalRepo, Employees: baseEmployeeDirectory, Assignments: baseAssignmentRepo,
			Trust: baseTrustRepo, Profiles: baseProfileRepo, Capabilities: baseCapabilityRepo,
			Executions: baseExecutionRepo, WAL: baseExecutionWAL, EventLog: baseEventLog,
		})
		if err := persistMgr.Restore(context.Background(), &persistence.Restorer{
			Cases: baseCaseRepo, Queues: baseQueueRepo, Coordinations: baseCoordinationRepo, Policies: basePolicyRepo,
			Proposals: baseProposalRepo, Employees: baseEmployeeDirectory, Assignments: baseAssignmentRepo,
			Trust: baseTrustRepo, Profiles: baseProfileRepo, Capabilities: baseCapabilityRepo,
			Executions: baseExecutionRepo, WAL: baseExecutionWAL, EventLog: baseEventLog,
		}); err != nil {
			return nil, fmt.Errorf("restore persisted runtime state: %w", err)
		}
	}

	commandBus := command.NewService(eventLog, command.PassThroughAdmissionPolicy{}, clock, ids)
	caseResolver := caseruntime.NewResolver(caseRepo, clock, ids)
	caseService := caseruntime.NewService(caseResolver, eventLog, clock, ids)
	policyEvaluator := policy.NewEvaluator()
	policyService := policy.NewService(policyRepo, policyEvaluator, eventLog, clock, ids)
	constraintsRepo := executioncontrol.NewInMemoryConstraintsRepository()
	constraintsPlanner := executioncontrol.NewPlanner()
	constraintsService := executioncontrol.NewService(constraintsRepo, constraintsPlanner, eventLog, clock, ids)
	trustScorer := trust.NewDeterministicScorer(clock.Now)
	trustService := trust.NewService(trustRepo, trustScorer)
	actionRegistry := actionplan.NewRegistry()
	actionRegistry.Register(actionplan.ActionDefinition{
		Type:          "legacy_workflow_action",
		Reversibility: actionplan.ReversibilityIrreversible,
		Idempotency:   actionplan.IdempotencyConditional,
		Validate: func(params map[string]any) error {
			if strings.TrimSpace(stringValue(params["entity"])) == "" {
				return fmt.Errorf("entity is required")
			}
			if strings.TrimSpace(stringValue(params["record_id"])) == "" {
				return fmt.Errorf("record_id is required")
			}
			if strings.TrimSpace(stringValue(params["action"])) == "" {
				return fmt.Errorf("action is required")
			}
			return nil
		},
	})

	actionRegistry.Register(actionplan.ActionDefinition{
		Type:          "external_incident_followup",
		Reversibility: actionplan.ReversibilityIrreversible,
		Idempotency:   actionplan.IdempotencySafe,
		Validate: func(params map[string]any) error {
			if strings.TrimSpace(stringValue(params["external_id"])) == "" {
				return fmt.Errorf("external_id is required")
			}
			return nil
		},
	})

	actionCompiler := actionplan.NewCompiler(actionRegistry, clock, ids)
	actionValidator := actionplan.NewValidator(actionRegistry)
	actionPlanService := actionplan.NewService(actionCompiler, actionValidator, eventLog, clock, ids)
	proposalValidator := proposal.NewValidator()
	proposalCompiler := proposal.NewCompilerAdapter(actionPlanService)
	proposalService := proposal.NewService(proposalRepo, proposalValidator, proposalCompiler, eventLog, clock, ids)
	actionExecutor := executionruntime.NewStubExecutor()
	executionRunner := executionruntime.NewRunner(executionRepo, executionWAL, actionExecutor, eventLog, clock, ids, trustService)
	executionRuntime := executionruntime.NewService(executionRunner)

	defaultQueue := workplan.WorkQueue{ID: "default-intake", Name: "Default Intake", Department: "operations", Purpose: "Default operational intake for resolved cases", AllowedCaseKinds: []string{"workflow.action", "container_incident_detected"}}
	if _, ok, err := baseQueueRepo.GetQueue(context.Background(), defaultQueue.ID); err != nil {
		return nil, fmt.Errorf("load default queue: %w", err)
	} else if !ok {
		if err := queueRepo.SaveQueue(context.Background(), defaultQueue); err != nil {
			return nil, fmt.Errorf("seed default queue: %w", err)
		}
	}
	assignmentRouter := workplan.NewRouter(queueRepo, defaultQueue.ID)
	planner := workplan.NewPlanner(planRepo, eventLog, clock, ids)
	coordinator := workplan.NewCoordinator(coordinationRepo, eventLog, clock, ids)
	workService := workplan.NewService(queueRepo, assignmentRouter, planner, coordinator, eventLog, clock, ids)

	defaultEmployee := employee.DigitalEmployee{ID: "employee-legacy-operator", Code: "legacy_operator_default", Role: "legacy_operator", Enabled: true, QueueMemberships: []string{defaultQueue.ID}, AllowedActionTypes: []actionplan.ActionType{"legacy_workflow_action", "external_incident_followup"}, AllowedCommandTypes: []string{"workflow.action", "container_incident_detected"}, PolicyProfile: "default", ExecutionProfile: "default", CreatedAt: clock.Now(), UpdatedAt: clock.Now()}
	if _, ok, err := baseEmployeeDirectory.GetEmployee(context.Background(), defaultEmployee.ID); err != nil {
		return nil, fmt.Errorf("load default employee: %w", err)
	} else if !ok {
		if err := employeeDirectory.SaveEmployee(context.Background(), defaultEmployee); err != nil {
			return nil, fmt.Errorf("seed default employee: %w", err)
		}
	}
	if _, ok, err := baseCapabilityRepo.GetCapability(context.Background(), "cap-legacy-workflow"); err != nil {
		return nil, fmt.Errorf("load workflow capability: %w", err)
	} else if !ok {
		if err := capabilityRepo.SaveCapability(context.Background(), capability.Capability{ID: "cap-legacy-workflow", Code: "workflow.execute", Type: capability.CapabilitySkill, Level: 1}); err != nil {
			return nil, fmt.Errorf("seed workflow capability: %w", err)
		}
	}
	actorCapabilities, err := baseCapabilityRepo.ListByActor(context.Background(), defaultEmployee.ID)
	if err != nil {
		return nil, fmt.Errorf("load actor capabilities: %w", err)
	}
	if len(actorCapabilities) == 0 {
		if err := capabilityRepo.AssignCapability(context.Background(), capability.ActorCapability{ActorID: defaultEmployee.ID, CapabilityID: "cap-legacy-workflow", Level: 1}); err != nil {
			return nil, fmt.Errorf("assign workflow capability: %w", err)
		}
	}
	if _, ok, err := baseProfileRepo.GetProfileByActor(context.Background(), defaultEmployee.ID); err != nil {
		return nil, fmt.Errorf("load competency profile: %w", err)
	} else if !ok {
		if err := profileRepo.SaveProfile(context.Background(), profile.CompetencyProfile{ID: "profile-legacy-operator", ActorID: defaultEmployee.ID, Name: "Legacy Operator", MaxComplexity: 10, PreferredWorkKinds: []string{"workflow.action", "container_incident_detected"}}); err != nil {
			return nil, fmt.Errorf("seed competency profile: %w", err)
		}
	}
	requirements, err := baseProfileRepo.ListRequirements(context.Background())
	if err != nil {
		return nil, fmt.Errorf("load capability requirements: %w", err)
	}
	if len(requirements) == 0 {
		if err := profileRepo.SaveRequirement(context.Background(), profile.CapabilityRequirement{ActionType: "legacy_workflow_action", CapabilityCodes: []string{"workflow.execute"}, MinimumLevel: 1}); err != nil {
			return nil, fmt.Errorf("seed capability requirement: %w", err)
		}
		if err := profileRepo.SaveRequirement(context.Background(), profile.CapabilityRequirement{ActionType: "external_incident_followup", CapabilityCodes: []string{"workflow.execute"}, MinimumLevel: 1}); err != nil {
			return nil, fmt.Errorf("seed integration capability requirement: %w", err)
		}
	}

	employeeSelector := employee.NewSelectorWithMatcher(employeeDirectory, profile.NewMatcher(profileRepo, profileRepo, capabilityRepo, capabilityRepo, trustService))
	employeeService := employee.NewService(assignmentRepo, employeeSelector, executionRuntime, eventLog, clock, ids, trustService)
	controlPlaneService := controlplane.NewService(caseRepo, queueRepo, coordinationRepo, policyRepo, proposalRepo, employeeDirectory, trustRepo, profileRepo, baseCapabilityRepo, executionRepo, executionWAL, eventLog, coordinator)
	integrationService := integration.NewService(eventLog, commandBus, caseService, workService, coordinator, policyService, constraintsService, actionPlanService, employeeDirectory, employeeService, trustRepo, profileRepo, integration.NewInMemoryProcessedIncidentStore(), clock, ids)
	if persistMgr != nil && persistMgr.Enabled() {
		if err := persistMgr.SaveSnapshot(context.Background()); err != nil {
			return nil, fmt.Errorf("save runtime snapshot: %w", err)
		}
	}

	if strings.EqualFold(cfg.BlobDriver, "s3") {
		log.Printf("[warn] blob=s3 ещё не подключён — используем локальное хранилище (root=%q)\n", cfg.FilesRoot)
	}
	st.Blob = &blob.LocalBlobStore{Root: cfg.FilesRoot}

	return &BootstrapResult{Storage: st, EventLog: eventLog, CommandBus: commandBus, CaseRepo: caseRepo, CaseResolver: caseResolver, CaseService: caseService, QueueRepo: queueRepo, PlanRepo: planRepo, CoordinationRepo: coordinationRepo, AssignmentRouter: assignmentRouter, Planner: planner, Coordinator: coordinator, WorkService: workService, PolicyRepo: policyRepo, PolicyEvaluator: policyEvaluator, PolicyService: policyService, ConstraintsRepo: constraintsRepo, ConstraintsPlanner: constraintsPlanner, ConstraintsService: constraintsService, ActionRegistry: actionRegistry, ActionCompiler: actionCompiler, ActionValidator: actionValidator, ActionPlanService: actionPlanService, ProposalRepo: proposalRepo, ProposalValidator: proposalValidator, ProposalCompiler: proposalCompiler, ProposalService: proposalService, EmployeeDirectory: employeeDirectory, AssignmentRepo: assignmentRepo, EmployeeSelector: employeeSelector, EmployeeService: employeeService, TrustRepo: trustRepo, TrustScorer: trustScorer, TrustService: trustService, ExecutionRepo: executionRepo, ExecutionWAL: executionWAL, ActionExecutor: actionExecutor, ExecutionRunner: executionRunner, ExecutionRuntime: executionRuntime, ControlPlane: controlPlaneService, IntegrationService: integrationService, Config: cfg}, nil
}

func tern(condition bool, yes string, no string) string {
	if condition {
		return yes
	}
	return no
}

func stringValue(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
