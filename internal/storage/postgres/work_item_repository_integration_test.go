//go:build testcontainers

package postgres

import (
	"context"
	"testing"
	"time"

	"kalita/internal/actionplan"
	"kalita/internal/caseruntime"
	"kalita/internal/workplan"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestPostgresWorkItemRepository_Integration(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	container, err := tcpostgres.Run(ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("kalita"),
		tcpostgres.WithUsername("postgres"),
		tcpostgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("postgres connection string: %v", err)
	}

	pool, err := OpenPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := RunMigrations(ctx, pool); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	caseRepo := NewPostgresCaseRepository(pool)
	workItemRepo := NewPostgresWorkItemRepository(pool)
	base := time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC)
	dueAt := base.Add(4 * time.Hour)

	caseOne := caseruntime.Case{ID: "case-1", Kind: "workflow.action", Status: string(caseruntime.CaseOpen), Title: "Case one", OpenedAt: base, UpdatedAt: base}
	caseTwo := caseruntime.Case{ID: "case-2", Kind: "workflow.action", Status: string(caseruntime.CaseOpen), Title: "Case two", OpenedAt: base.Add(10 * time.Minute), UpdatedAt: base.Add(10 * time.Minute)}
	if err := caseRepo.Save(ctx, caseOne); err != nil {
		t.Fatalf("Save(caseOne): %v", err)
	}
	if err := caseRepo.Save(ctx, caseTwo); err != nil {
		t.Fatalf("Save(caseTwo): %v", err)
	}

	itemOne := workplan.WorkItem{
		ID:                 "wi-1",
		CaseID:             caseOne.ID,
		QueueID:            "queue-1",
		Type:               "workflow.action",
		Status:             "open",
		Priority:           "high",
		Reason:             "first touch",
		AssignedEmployeeID: "actor-1",
		PlanID:             "plan-1",
		DueAt:              &dueAt,
		ActionPlan: &actionplan.ActionPlan{
			ID:         "plan-1",
			WorkItemID: "wi-1",
			CaseID:     caseOne.ID,
			CreatedAt:  base.Add(2 * time.Minute),
			Reason:     "auto",
			Actions: []actionplan.Action{
				{ID: "act-1", Type: actionplan.ActionType("call_customer"), Params: map[string]any{"channel": "phone"}, Reversibility: actionplan.ReversibilityCompensatable, Idempotency: actionplan.IdempotencyConditional},
			},
		},
		CreatedAt: base.Add(1 * time.Minute),
		UpdatedAt: base.Add(3 * time.Minute),
	}
	itemTwo := workplan.WorkItem{
		ID:                 "wi-2",
		CaseID:             caseOne.ID,
		QueueID:            "queue-1",
		Type:               "followup",
		Status:             "done",
		Priority:           "normal",
		AssignedEmployeeID: "actor-2",
		CreatedAt:          base.Add(5 * time.Minute),
		UpdatedAt:          base.Add(6 * time.Minute),
	}
	itemThree := workplan.WorkItem{
		ID:                 "wi-3",
		CaseID:             caseTwo.ID,
		QueueID:            "queue-2",
		Type:               "review",
		Status:             "open",
		Priority:           "urgent",
		AssignedEmployeeID: "actor-1",
		CreatedAt:          base.Add(7 * time.Minute),
		UpdatedAt:          base.Add(8 * time.Minute),
	}

	for _, item := range []workplan.WorkItem{itemOne, itemTwo, itemThree} {
		if err := workItemRepo.Save(ctx, item); err != nil {
			t.Fatalf("Save(%s): %v", item.ID, err)
		}
	}

	saved, ok, err := workItemRepo.FindByID(ctx, itemOne.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if !ok {
		t.Fatal("FindByID ok = false, want true")
	}
	if saved.CaseID != itemOne.CaseID || saved.AssignedEmployeeID != itemOne.AssignedEmployeeID || saved.PlanID != itemOne.PlanID {
		t.Fatalf("FindByID roundtrip mismatch: %#v", saved)
	}
	if saved.ActionPlan == nil || len(saved.ActionPlan.Actions) != 1 || saved.ActionPlan.Actions[0].Params["channel"] != "phone" {
		t.Fatalf("FindByID action plan mismatch: %#v", saved.ActionPlan)
	}

	byCase, err := workItemRepo.FindByCaseID(ctx, caseOne.ID)
	if err != nil {
		t.Fatalf("FindByCaseID: %v", err)
	}
	if len(byCase) != 2 || byCase[0].ID != itemOne.ID || byCase[1].ID != itemTwo.ID {
		t.Fatalf("FindByCaseID mismatch: %#v", byCase)
	}

	openItems, err := workItemRepo.FindByStatus(ctx, "open")
	if err != nil {
		t.Fatalf("FindByStatus: %v", err)
	}
	if len(openItems) != 2 || openItems[0].ID != itemOne.ID || openItems[1].ID != itemThree.ID {
		t.Fatalf("FindByStatus mismatch: %#v", openItems)
	}

	actorItems, err := workItemRepo.FindByActorID(ctx, "actor-1")
	if err != nil {
		t.Fatalf("FindByActorID: %v", err)
	}
	if len(actorItems) != 2 || actorItems[0].ID != itemOne.ID || actorItems[1].ID != itemThree.ID {
		t.Fatalf("FindByActorID mismatch: %#v", actorItems)
	}

	updated := itemOne
	updated.Status = "done"
	updated.Priority = "normal"
	updated.AssignedEmployeeID = "actor-3"
	updated.UpdatedAt = base.Add(9 * time.Minute)
	if err := workItemRepo.Save(ctx, updated); err != nil {
		t.Fatalf("Save(updated): %v", err)
	}

	reloaded, ok, err := workItemRepo.FindByID(ctx, updated.ID)
	if err != nil {
		t.Fatalf("FindByID(updated): %v", err)
	}
	if !ok {
		t.Fatal("FindByID(updated) ok = false, want true")
	}
	if reloaded.Status != updated.Status || reloaded.Priority != updated.Priority || reloaded.AssignedEmployeeID != updated.AssignedEmployeeID {
		t.Fatalf("FindByID(updated) mismatch: %#v", reloaded)
	}
}
