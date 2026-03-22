package app

import (
	"context"
	"fmt"
	"log"
	"strings"

	"kalita/internal/blob"
	"kalita/internal/caseruntime"
	"kalita/internal/catalog"
	"kalita/internal/command"
	"kalita/internal/config"
	"kalita/internal/eventcore"
	"kalita/internal/executioncontrol"
	"kalita/internal/policy"
	"kalita/internal/postgres"
	"kalita/internal/runtime"
	"kalita/internal/schema"
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
		Config:             cfg,
	}, nil
}

func tern[T any](cond bool, a, b T) T {
	if cond {
		return a
	}
	return b
}
