//go:build testcontainers

package postgres

import (
	"context"
	"testing"
	"time"

	"kalita/internal/executionruntime"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestPostgresExecutionSessionRepository_Integration(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := openExecutionRuntimeTestPool(t, ctx)
	repo := NewPostgresExecutionSessionRepository(pool)
	base := time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC)

	sessionOne := executionruntime.ExecutionSession{
		ID:                     "session-1",
		ActionPlanID:           "plan-1",
		CaseID:                 "case-1",
		WorkItemID:             "work-1",
		CoordinationDecisionID: "coord-1",
		PolicyDecisionID:       "policy-1",
		ExecutionConstraintsID: "constraints-1",
		Status:                 executionruntime.ExecutionSessionPending,
		CurrentStepIndex:       -1,
		CreatedAt:              base,
		UpdatedAt:              base,
	}
	sessionTwo := executionruntime.ExecutionSession{
		ID:               "session-2",
		WorkItemID:       "work-1",
		Status:           executionruntime.ExecutionSessionFailed,
		CurrentStepIndex: 1,
		CreatedAt:        base.Add(2 * time.Minute),
		UpdatedAt:        base.Add(3 * time.Minute),
		FailureReason:    "step failed",
	}

	for _, session := range []executionruntime.ExecutionSession{sessionOne, sessionTwo} {
		if err := repo.SaveSession(ctx, session); err != nil {
			t.Fatalf("SaveSession(%s): %v", session.ID, err)
		}
	}

	gotSession, ok, err := repo.GetSession(ctx, sessionOne.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !ok {
		t.Fatal("GetSession ok = false, want true")
	}
	if gotSession.ActionPlanID != sessionOne.ActionPlanID || gotSession.CaseID != sessionOne.CaseID || gotSession.CurrentStepIndex != sessionOne.CurrentStepIndex {
		t.Fatalf("GetSession mismatch: %#v", gotSession)
	}

	sessionsByWorkItem, err := repo.ListSessionsByWorkItem(ctx, "work-1")
	if err != nil {
		t.Fatalf("ListSessionsByWorkItem: %v", err)
	}
	if len(sessionsByWorkItem) != 2 || sessionsByWorkItem[0].ID != sessionOne.ID || sessionsByWorkItem[1].ID != sessionTwo.ID {
		t.Fatalf("ListSessionsByWorkItem mismatch: %#v", sessionsByWorkItem)
	}

	updatedSession := sessionOne
	updatedSession.Status = executionruntime.ExecutionSessionRunning
	updatedSession.CurrentStepIndex = 0
	updatedSession.UpdatedAt = base.Add(time.Minute)
	if err := repo.SaveSession(ctx, updatedSession); err != nil {
		t.Fatalf("SaveSession(updated): %v", err)
	}

	reloadedSession, ok, err := repo.GetSession(ctx, updatedSession.ID)
	if err != nil {
		t.Fatalf("GetSession(updated): %v", err)
	}
	if !ok {
		t.Fatal("GetSession(updated) ok = false, want true")
	}
	if reloadedSession.Status != updatedSession.Status || reloadedSession.CurrentStepIndex != updatedSession.CurrentStepIndex {
		t.Fatalf("GetSession(updated) mismatch: %#v", reloadedSession)
	}

	startedAt := base.Add(4 * time.Minute)
	finishedAt := base.Add(5 * time.Minute)
	steps := []executionruntime.StepExecution{
		{ID: "step-2", ExecutionSessionID: sessionOne.ID, ActionID: "action-2", StepIndex: 1, Status: executionruntime.StepRunning, StartedAt: &startedAt},
		{ID: "step-1", ExecutionSessionID: sessionOne.ID, ActionID: "action-1", StepIndex: 0, Status: executionruntime.StepSucceeded, StartedAt: &startedAt, FinishedAt: &finishedAt},
	}
	for _, step := range steps {
		if err := repo.SaveStep(ctx, step); err != nil {
			t.Fatalf("SaveStep(%s): %v", step.ID, err)
		}
	}

	gotStep, ok, err := repo.GetStep(ctx, "step-1")
	if err != nil {
		t.Fatalf("GetStep: %v", err)
	}
	if !ok {
		t.Fatal("GetStep ok = false, want true")
	}
	if gotStep.StepIndex != 0 || gotStep.ActionID != "action-1" {
		t.Fatalf("GetStep mismatch: %#v", gotStep)
	}

	sessionSteps, err := repo.ListStepsBySession(ctx, sessionOne.ID)
	if err != nil {
		t.Fatalf("ListStepsBySession: %v", err)
	}
	if len(sessionSteps) != 2 || sessionSteps[0].ID != "step-1" || sessionSteps[1].ID != "step-2" {
		t.Fatalf("ListStepsBySession mismatch: %#v", sessionSteps)
	}
}

func TestPostgresExecutionSessionRepository_WALAndCompensationLogAreAppendOnly(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx := context.Background()
	pool := openExecutionRuntimeTestPool(t, ctx)
	repo := NewPostgresExecutionSessionRepository(pool)
	base := time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC)

	session := executionruntime.ExecutionSession{
		ID:               "session-1",
		WorkItemID:       "work-1",
		Status:           executionruntime.ExecutionSessionRunning,
		CurrentStepIndex: 0,
		CreatedAt:        base,
		UpdatedAt:        base,
	}
	step := executionruntime.StepExecution{
		ID:                 "step-1",
		ExecutionSessionID: session.ID,
		ActionID:           "action-1",
		StepIndex:          0,
		Status:             executionruntime.StepSucceeded,
	}
	if err := repo.SaveSession(ctx, session); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	if err := repo.SaveStep(ctx, step); err != nil {
		t.Fatalf("SaveStep: %v", err)
	}

	records := []executionruntime.WALRecord{
		{ID: "wal-1", ExecutionSessionID: session.ID, StepExecutionID: step.ID, ActionID: step.ActionID, Type: executionruntime.WALStepIntent, CreatedAt: base.Add(time.Second), Payload: map[string]any{"step_index": 0}},
		{ID: "wal-2", ExecutionSessionID: session.ID, StepExecutionID: step.ID, ActionID: step.ActionID, Type: executionruntime.WALCompensationIntent, CreatedAt: base.Add(2 * time.Second), Payload: map[string]any{"step_index": 0, "reason": "rollback"}},
		{ID: "wal-3", ExecutionSessionID: session.ID, StepExecutionID: step.ID, ActionID: step.ActionID, Type: executionruntime.WALCompensationResult, CreatedAt: base.Add(3 * time.Second), Payload: map[string]any{"step_index": 0, "status": "compensated"}},
	}
	for _, record := range records {
		if err := repo.Append(ctx, record); err != nil {
			t.Fatalf("Append(%s): %v", record.ID, err)
		}
	}

	got, err := repo.ListBySession(ctx, session.ID)
	if err != nil {
		t.Fatalf("ListBySession: %v", err)
	}
	if len(got) != 3 || got[0].ID != "wal-1" || got[1].ID != "wal-2" || got[2].ID != "wal-3" {
		t.Fatalf("ListBySession mismatch: %#v", got)
	}

	var walCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM wal_entries WHERE execution_session_id = $1`, session.ID).Scan(&walCount); err != nil {
		t.Fatalf("count wal_entries: %v", err)
	}
	if walCount != 3 {
		t.Fatalf("wal_entries count = %d, want 3", walCount)
	}

	var compensationCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM compensation_log WHERE execution_session_id = $1`, session.ID).Scan(&compensationCount); err != nil {
		t.Fatalf("count compensation_log: %v", err)
	}
	if compensationCount != 2 {
		t.Fatalf("compensation_log count = %d, want 2", compensationCount)
	}

	if _, err := pool.Exec(ctx, `UPDATE wal_entries SET action_id = 'mutated' WHERE id = 'wal-1'`); err == nil {
		t.Fatal("UPDATE wal_entries succeeded, want append-only failure")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM wal_entries WHERE id = 'wal-1'`); err == nil {
		t.Fatal("DELETE wal_entries succeeded, want append-only failure")
	}
	if _, err := pool.Exec(ctx, `UPDATE compensation_log SET action_id = 'mutated' WHERE wal_entry_id = 'wal-2'`); err == nil {
		t.Fatal("UPDATE compensation_log succeeded, want append-only failure")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM compensation_log WHERE wal_entry_id = 'wal-2'`); err == nil {
		t.Fatal("DELETE compensation_log succeeded, want append-only failure")
	}
}

func openExecutionRuntimeTestPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

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
	return pool
}
