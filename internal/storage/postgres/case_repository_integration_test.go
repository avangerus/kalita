//go:build testcontainers

package postgres

import (
	"context"
	"testing"
	"time"

	"kalita/internal/caseruntime"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestPostgresCaseRepository_Integration(t *testing.T) {
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

	repo := NewPostgresCaseRepository(pool)
	base := time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC)

	caseOne := caseruntime.Case{
		ID:            "case-1",
		Kind:          "workflow.action",
		Status:        string(caseruntime.CaseOpen),
		Title:         "Review invoice",
		SubjectRef:    "billing.Invoice/inv-1",
		CorrelationID: "corr-1",
		OpenedAt:      base,
		UpdatedAt:     base.Add(5 * time.Minute),
		OwnerQueueID:  "queue-ops",
		CurrentPlanID: "plan-1",
		Attributes: map[string]any{
			"priority": "high",
			"retries":  float64(2),
		},
	}
	caseTwo := caseruntime.Case{
		ID:        "case-2",
		Kind:      "workflow.action",
		Status:    string(caseruntime.CaseClosed),
		Title:     "Closed case",
		OpenedAt:  base.Add(10 * time.Minute),
		UpdatedAt: base.Add(15 * time.Minute),
	}

	if err := repo.Save(ctx, caseOne); err != nil {
		t.Fatalf("Save(caseOne): %v", err)
	}
	if err := repo.Save(ctx, caseTwo); err != nil {
		t.Fatalf("Save(caseTwo): %v", err)
	}

	saved, ok, err := repo.FindByID(ctx, caseOne.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if !ok {
		t.Fatal("FindByID ok = false, want true")
	}
	if saved.Title != caseOne.Title || saved.SubjectRef != caseOne.SubjectRef || saved.OwnerQueueID != caseOne.OwnerQueueID {
		t.Fatalf("FindByID roundtrip mismatch: %#v", saved)
	}
	if saved.Attributes["priority"] != "high" {
		t.Fatalf("FindByID attributes mismatch: %#v", saved.Attributes)
	}

	all, err := repo.FindAll(ctx)
	if err != nil {
		t.Fatalf("FindAll: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("FindAll len = %d, want 2", len(all))
	}
	if all[0].ID != "case-1" || all[1].ID != "case-2" {
		t.Fatalf("FindAll order mismatch: %#v", all)
	}

	openCases, err := repo.FindByStatus(ctx, string(caseruntime.CaseOpen))
	if err != nil {
		t.Fatalf("FindByStatus: %v", err)
	}
	if len(openCases) != 1 || openCases[0].ID != caseOne.ID {
		t.Fatalf("FindByStatus result mismatch: %#v", openCases)
	}

	updated := caseOne
	updated.Status = string(caseruntime.CaseClosed)
	updated.Title = "Review invoice (closed)"
	updated.Attributes = map[string]any{"priority": "normal"}
	updated.UpdatedAt = base.Add(20 * time.Minute)
	if err := repo.Save(ctx, updated); err != nil {
		t.Fatalf("Save(updated): %v", err)
	}

	closedCases, err := repo.FindByStatus(ctx, string(caseruntime.CaseClosed))
	if err != nil {
		t.Fatalf("FindByStatus(closed): %v", err)
	}
	if len(closedCases) != 2 {
		t.Fatalf("FindByStatus(closed) len = %d, want 2", len(closedCases))
	}

	reloaded, ok, err := repo.FindByID(ctx, updated.ID)
	if err != nil {
		t.Fatalf("FindByID(updated): %v", err)
	}
	if !ok {
		t.Fatal("FindByID(updated) ok = false, want true")
	}
	if reloaded.Status != updated.Status || reloaded.Title != updated.Title {
		t.Fatalf("FindByID(updated) mismatch: %#v", reloaded)
	}
	if reloaded.Attributes["priority"] != "normal" {
		t.Fatalf("FindByID(updated) attributes mismatch: %#v", reloaded.Attributes)
	}
}
