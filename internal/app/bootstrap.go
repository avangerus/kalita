package app

import (
	"context"
	"fmt"
	"log"
	"strings"

	"kalita/internal/actionplan"
	"kalita/internal/blob"
	"kalita/internal/caseruntime"
	"kalita/internal/catalog"
	"kalita/internal/command"
	"kalita/internal/config"
	"kalita/internal/employee"
	"kalita/internal/eventcore"
	"kalita/internal/executioncontrol"
	"kalita/internal/executionruntime"
	"kalita/internal/policy"
	"kalita/internal/postgres"
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
	Config             config.Config
}

// Bootstrap initializes the application with all required components
func Bootstrap(cfgPath string) (*BootstrapResult, error) {
	cfg := config.LoadWithPath(cfgPath)

	fmt.Printf("Kalita starting on :%s (db=%s, autoMigrate=%v, blob=%s)\n",
		cfg.Port, tern(cfg.DBURL != "", "pg", "memory"), cfg.AutoMigrate, cfg.BlobDriver)

	// DSL
	entityMap, err := schema.LoadAllEntities(cfg.DSLDir)
	if err != nil {
		return nil, fmt.Errorf("ошибка загрузки DSL из %q: %w", cfg.DSLDir, err)
	}
	fmt.Printf("Загружено сущностей: %d\n", len(entityMap))
	entities := make([]*schema.Entity, 0, len(entityMap))
	for _, e := range entityMap {
		entities = append(entities, e)
	}

	// --- PostgreSQL: подключение + (опц.) add-only DDL
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

	// Enums
	enumCatalog, err := catalog.LoadEnumCatalog(cfg.EnumsDir)
	if err != nil {
		return nil, fmt.Errorf("ошибка загрузки enum-справочников из %q: %w", cfg.EnumsDir, err)
	}
	fmt.Printf("Загружено enum-справочников: %d\n", len(enumCatalog))

	// In-memory API (данные пока в памяти; PG — только схема)
	st := runtime.NewStorage(entities, enumCatalog)
	eventLog := eventcore.NewInMemoryEventLog()
	clock := eventcore.RealClock{}
	ids := eventcore.NewULIDGenerator()
	commandBus := command.NewService(eventLog, command.PassThroughAdmissionPolicy{}, clock, ids)
	caseRepo := caseruntime.NewInMemoryCaseRepository()
	caseResolver := caseruntime.NewResolver(caseRepo, clock, ids)
	caseService := caseruntime.NewService(caseResolver, eventLog, clock, ids)
	queueRepo := workplan.NewInMemoryQueueRepository()
	planRepo := workplan.NewInMemoryPlanRepository()
	defaultQueue := workplan.WorkQueue{
		ID:               "default-intake",
		Name:             "Default Intake",
		Department:       "operations",
		Purpose:          "Default operational intake for resolved cases",
		AllowedCaseKinds: []string{"workflow.action"},
	}
	if err := queueRepo.SaveQueue(context.Background(), defaultQueue); err != nil {
		return nil, fmt.Errorf("seed default queue: %w", err)
	}
	assignmentRouter := workplan.NewRouter(queueRepo, defaultQueue.ID)
	planner := workplan.NewPlanner(planRepo, eventLog, clock, ids)
	coordinationRepo := workplan.NewInMemoryCoordinationRepository()
	coordinator := workplan.NewCoordinator(coordinationRepo, eventLog, clock, ids)
	workService := workplan.NewService(queueRepo, assignmentRouter, planner, coordinator, eventLog, clock, ids)
	policyRepo := policy.NewInMemoryRepository()
	policyEvaluator := policy.NewEvaluator()
	policyService := policy.NewService(policyRepo, policyEvaluator, eventLog, clock, ids)
	constraintsRepo := executioncontrol.NewInMemoryConstraintsRepository()
	constraintsPlanner := executioncontrol.NewPlanner()
	constraintsService := executioncontrol.NewService(constraintsRepo, constraintsPlanner, eventLog, clock, ids)
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
	actionCompiler := actionplan.NewCompiler(actionRegistry, clock, ids)
	actionValidator := actionplan.NewValidator(actionRegistry)
	actionPlanService := actionplan.NewService(actionCompiler, actionValidator, eventLog, clock, ids)
	proposalRepo := proposal.NewInMemoryRepository()
	proposalValidator := proposal.NewValidator()
	proposalCompiler := proposal.NewCompilerAdapter(actionPlanService)
	proposalService := proposal.NewService(proposalRepo, proposalValidator, proposalCompiler, eventLog, clock, ids)
	executionRepo := executionruntime.NewInMemoryExecutionRepository()
	executionWAL := executionruntime.NewInMemoryWAL()
	actionExecutor := executionruntime.NewStubExecutor()
	executionRunner := executionruntime.NewRunner(executionRepo, executionWAL, actionExecutor, eventLog, clock, ids)
	executionRuntime := executionruntime.NewService(executionRunner)
	employeeDirectory := employee.NewInMemoryDirectory()
	assignmentRepo := employee.NewInMemoryAssignmentRepository()
	employeeSelector := employee.NewSelector(employeeDirectory)
	employeeService := employee.NewService(assignmentRepo, employeeSelector, executionRuntime, eventLog, clock, ids)
	trustRepo := trust.NewInMemoryRepository()
	trustScorer := trust.NewDeterministicScorer(clock.Now)
	trustService := trust.NewService(trustRepo, trustScorer)
	defaultEmployee := employee.DigitalEmployee{
		ID:                  "employee-legacy-operator",
		Code:                "legacy_operator_default",
		Role:                "legacy_operator",
		Enabled:             true,
		QueueMemberships:    []string{defaultQueue.ID},
		AllowedActionTypes:  []actionplan.ActionType{"legacy_workflow_action"},
		AllowedCommandTypes: []string{"workflow.action"},
		PolicyProfile:       "default",
		ExecutionProfile:    "default",
		CreatedAt:           clock.Now(),
		UpdatedAt:           clock.Now(),
	}
	if err := employeeDirectory.SaveEmployee(context.Background(), defaultEmployee); err != nil {
		return nil, fmt.Errorf("seed default employee: %w", err)
	}
	if strings.EqualFold(cfg.BlobDriver, "s3") {
		log.Printf("[warn] blob=s3 ещё не подключён — используем локальное хранилище (root=%q)\n", cfg.FilesRoot)
	}
	st.Blob = &blob.LocalBlobStore{Root: cfg.FilesRoot}

	return &BootstrapResult{
		Storage:            st,
		EventLog:           eventLog,
		CommandBus:         commandBus,
		CaseRepo:           caseRepo,
		CaseResolver:       caseResolver,
		CaseService:        caseService,
		QueueRepo:          queueRepo,
		PlanRepo:           planRepo,
		CoordinationRepo:   coordinationRepo,
		AssignmentRouter:   assignmentRouter,
		Planner:            planner,
		Coordinator:        coordinator,
		WorkService:        workService,
		PolicyRepo:         policyRepo,
		PolicyEvaluator:    policyEvaluator,
		PolicyService:      policyService,
		ConstraintsRepo:    constraintsRepo,
		ConstraintsPlanner: constraintsPlanner,
		ConstraintsService: constraintsService,
		ActionRegistry:     actionRegistry,
		ActionCompiler:     actionCompiler,
		ActionValidator:    actionValidator,
		ActionPlanService:  actionPlanService,
		ProposalRepo:       proposalRepo,
		ProposalValidator:  proposalValidator,
		ProposalCompiler:   proposalCompiler,
		ProposalService:    proposalService,
		EmployeeDirectory:  employeeDirectory,
		AssignmentRepo:     assignmentRepo,
		EmployeeSelector:   employeeSelector,
		EmployeeService:    employeeService,
		TrustRepo:          trustRepo,
		TrustScorer:        trustScorer,
		TrustService:       trustService,
		ExecutionRepo:      executionRepo,
		ExecutionWAL:       executionWAL,
		ActionExecutor:     actionExecutor,
		ExecutionRunner:    executionRunner,
		ExecutionRuntime:   executionRuntime,
		Config:             cfg,
	}, nil
}

func tern[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}

func stringValue(v any) string {
	s, _ := v.(string)
	return s
}
