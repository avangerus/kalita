package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"kalita/internal/executionruntime"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresExecutionSessionRepository struct {
	pool *pgxpool.Pool
}

var _ executionruntime.ExecutionRepository = (*PostgresExecutionSessionRepository)(nil)
var _ executionruntime.WAL = (*PostgresExecutionSessionRepository)(nil)

func NewPostgresExecutionSessionRepository(pool *pgxpool.Pool) *PostgresExecutionSessionRepository {
	return &PostgresExecutionSessionRepository{pool: pool}
}

func NewExecutionSessionRepository(pool *pgxpool.Pool) *PostgresExecutionSessionRepository {
	return NewPostgresExecutionSessionRepository(pool)
}

func (r *PostgresExecutionSessionRepository) SaveSession(ctx context.Context, s executionruntime.ExecutionSession) error {
	createdAt := s.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := s.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}

	return r.withTx(ctx, pgx.TxOptions{}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO execution_sessions (
				id, action_plan_id, case_id, work_item_id, coordination_decision_id,
				policy_decision_id, execution_constraints_id, status, current_step_index,
				created_at, updated_at, failure_reason
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (id) DO UPDATE SET
				action_plan_id = EXCLUDED.action_plan_id,
				case_id = EXCLUDED.case_id,
				work_item_id = EXCLUDED.work_item_id,
				coordination_decision_id = EXCLUDED.coordination_decision_id,
				policy_decision_id = EXCLUDED.policy_decision_id,
				execution_constraints_id = EXCLUDED.execution_constraints_id,
				status = EXCLUDED.status,
				current_step_index = EXCLUDED.current_step_index,
				created_at = EXCLUDED.created_at,
				updated_at = EXCLUDED.updated_at,
				failure_reason = EXCLUDED.failure_reason
		`, s.ID, s.ActionPlanID, s.CaseID, s.WorkItemID, s.CoordinationDecisionID, s.PolicyDecisionID, s.ExecutionConstraintsID, s.Status, s.CurrentStepIndex, createdAt, updatedAt, s.FailureReason)
		if err != nil {
			return fmt.Errorf("save execution session %s: %w", s.ID, err)
		}
		return nil
	})
}

func (r *PostgresExecutionSessionRepository) GetSession(ctx context.Context, id string) (executionruntime.ExecutionSession, bool, error) {
	return r.querySessionOne(ctx, `
		SELECT id, action_plan_id, case_id, work_item_id, coordination_decision_id,
			policy_decision_id, execution_constraints_id, status, current_step_index,
			created_at, updated_at, failure_reason
		FROM execution_sessions
		WHERE id = $1
	`, id)
}

func (r *PostgresExecutionSessionRepository) ListSessionsByWorkItem(ctx context.Context, workItemID string) ([]executionruntime.ExecutionSession, error) {
	return r.querySessions(ctx, `
		SELECT id, action_plan_id, case_id, work_item_id, coordination_decision_id,
			policy_decision_id, execution_constraints_id, status, current_step_index,
			created_at, updated_at, failure_reason
		FROM execution_sessions
		WHERE work_item_id = $1
		ORDER BY created_at, id
	`, workItemID)
}

func (r *PostgresExecutionSessionRepository) SaveStep(ctx context.Context, s executionruntime.StepExecution) error {
	return r.withTx(ctx, pgx.TxOptions{}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO step_executions (
				id, execution_session_id, action_id, step_index, status,
				started_at, finished_at, failure_reason
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (id) DO UPDATE SET
				execution_session_id = EXCLUDED.execution_session_id,
				action_id = EXCLUDED.action_id,
				step_index = EXCLUDED.step_index,
				status = EXCLUDED.status,
				started_at = EXCLUDED.started_at,
				finished_at = EXCLUDED.finished_at,
				failure_reason = EXCLUDED.failure_reason
		`, s.ID, s.ExecutionSessionID, s.ActionID, s.StepIndex, s.Status, nullableTime(s.StartedAt), nullableTime(s.FinishedAt), s.FailureReason)
		if err != nil {
			return fmt.Errorf("save step execution %s: %w", s.ID, err)
		}
		return nil
	})
}

func (r *PostgresExecutionSessionRepository) GetStep(ctx context.Context, id string) (executionruntime.StepExecution, bool, error) {
	return r.queryStepOne(ctx, `
		SELECT id, execution_session_id, action_id, step_index, status,
			started_at, finished_at, failure_reason
		FROM step_executions
		WHERE id = $1
	`, id)
}

func (r *PostgresExecutionSessionRepository) ListStepsBySession(ctx context.Context, sessionID string) ([]executionruntime.StepExecution, error) {
	return r.querySteps(ctx, `
		SELECT id, execution_session_id, action_id, step_index, status,
			started_at, finished_at, failure_reason
		FROM step_executions
		WHERE execution_session_id = $1
		ORDER BY step_index, id
	`, sessionID)
}

func (r *PostgresExecutionSessionRepository) Append(ctx context.Context, record executionruntime.WALRecord) error {
	payload, err := marshalWALPayload(record)
	if err != nil {
		return err
	}

	return r.withTx(ctx, pgx.TxOptions{}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO wal_entries (
				id, execution_session_id, step_execution_id, action_id,
				entry_type, created_at, payload
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		`, record.ID, record.ExecutionSessionID, nullableString(record.StepExecutionID), record.ActionID, record.Type, record.CreatedAt.UTC(), payload)
		if err != nil {
			return fmt.Errorf("append wal entry %s: %w", record.ID, err)
		}

		if !isCompensationRecord(record.Type) {
			return nil
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO compensation_log (
				wal_entry_id, execution_session_id, step_execution_id, action_id,
				entry_type, created_at, payload
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		`, record.ID, record.ExecutionSessionID, nullableString(record.StepExecutionID), record.ActionID, record.Type, record.CreatedAt.UTC(), payload)
		if err != nil {
			return fmt.Errorf("append compensation log %s: %w", record.ID, err)
		}
		return nil
	})
}

func (r *PostgresExecutionSessionRepository) ListBySession(ctx context.Context, sessionID string) ([]executionruntime.WALRecord, error) {
	var out []executionruntime.WALRecord
	err := r.withTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id, execution_session_id, step_execution_id, action_id,
				entry_type, created_at, payload
			FROM wal_entries
			WHERE execution_session_id = $1
			ORDER BY seq
		`, sessionID)
		if err != nil {
			return fmt.Errorf("query wal entries: %w", err)
		}
		defer rows.Close()

		out = make([]executionruntime.WALRecord, 0)
		for rows.Next() {
			record, err := scanWALRecord(rows)
			if err != nil {
				return err
			}
			out = append(out, record)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate wal entries: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *PostgresExecutionSessionRepository) withTx(ctx context.Context, opts pgx.TxOptions, fn func(tx pgx.Tx) error) error {
	tx, err := r.pool.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin execution runtime transaction: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit execution runtime transaction: %w", err)
	}
	return nil
}

func (r *PostgresExecutionSessionRepository) querySessionOne(ctx context.Context, query string, arg string) (executionruntime.ExecutionSession, bool, error) {
	var (
		out   executionruntime.ExecutionSession
		found bool
	)
	err := r.withTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, query, arg)
		session, err := scanExecutionSession(row)
		if err != nil {
			if isNoRows(err) {
				return nil
			}
			return err
		}
		out = session
		found = true
		return nil
	})
	if err != nil {
		return executionruntime.ExecutionSession{}, false, err
	}
	if !found {
		return executionruntime.ExecutionSession{}, false, nil
	}
	return out, true, nil
}

func (r *PostgresExecutionSessionRepository) querySessions(ctx context.Context, query string, args ...any) ([]executionruntime.ExecutionSession, error) {
	var out []executionruntime.ExecutionSession
	err := r.withTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("query execution sessions: %w", err)
		}
		defer rows.Close()

		out = make([]executionruntime.ExecutionSession, 0)
		for rows.Next() {
			session, err := scanExecutionSession(rows)
			if err != nil {
				return err
			}
			out = append(out, session)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate execution sessions: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *PostgresExecutionSessionRepository) queryStepOne(ctx context.Context, query string, arg string) (executionruntime.StepExecution, bool, error) {
	var (
		out   executionruntime.StepExecution
		found bool
	)
	err := r.withTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, query, arg)
		step, err := scanStepExecution(row)
		if err != nil {
			if isNoRows(err) {
				return nil
			}
			return err
		}
		out = step
		found = true
		return nil
	})
	if err != nil {
		return executionruntime.StepExecution{}, false, err
	}
	if !found {
		return executionruntime.StepExecution{}, false, nil
	}
	return out, true, nil
}

func (r *PostgresExecutionSessionRepository) querySteps(ctx context.Context, query string, args ...any) ([]executionruntime.StepExecution, error) {
	var out []executionruntime.StepExecution
	err := r.withTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("query step executions: %w", err)
		}
		defer rows.Close()

		out = make([]executionruntime.StepExecution, 0)
		for rows.Next() {
			step, err := scanStepExecution(rows)
			if err != nil {
				return err
			}
			out = append(out, step)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate step executions: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func scanExecutionSession(row scanner) (executionruntime.ExecutionSession, error) {
	var session executionruntime.ExecutionSession
	if err := row.Scan(
		&session.ID,
		&session.ActionPlanID,
		&session.CaseID,
		&session.WorkItemID,
		&session.CoordinationDecisionID,
		&session.PolicyDecisionID,
		&session.ExecutionConstraintsID,
		&session.Status,
		&session.CurrentStepIndex,
		&session.CreatedAt,
		&session.UpdatedAt,
		&session.FailureReason,
	); err != nil {
		return executionruntime.ExecutionSession{}, err
	}
	session.CreatedAt = session.CreatedAt.UTC()
	session.UpdatedAt = session.UpdatedAt.UTC()
	return session, nil
}

func scanStepExecution(row scanner) (executionruntime.StepExecution, error) {
	var (
		step       executionruntime.StepExecution
		startedAt  *time.Time
		finishedAt *time.Time
	)
	if err := row.Scan(
		&step.ID,
		&step.ExecutionSessionID,
		&step.ActionID,
		&step.StepIndex,
		&step.Status,
		&startedAt,
		&finishedAt,
		&step.FailureReason,
	); err != nil {
		return executionruntime.StepExecution{}, err
	}
	step.StartedAt = cloneNullableTime(startedAt)
	step.FinishedAt = cloneNullableTime(finishedAt)
	return step, nil
}

func scanWALRecord(row scanner) (executionruntime.WALRecord, error) {
	var (
		record          executionruntime.WALRecord
		stepExecutionID *string
		payload         []byte
	)
	if err := row.Scan(
		&record.ID,
		&record.ExecutionSessionID,
		&stepExecutionID,
		&record.ActionID,
		&record.Type,
		&record.CreatedAt,
		&payload,
	); err != nil {
		return executionruntime.WALRecord{}, err
	}
	if stepExecutionID != nil {
		record.StepExecutionID = *stepExecutionID
	}
	record.CreatedAt = record.CreatedAt.UTC()
	decodedPayload, err := unmarshalWALPayload(record.ID, payload)
	if err != nil {
		return executionruntime.WALRecord{}, err
	}
	record.Payload = decodedPayload
	return record, nil
}

func marshalWALPayload(record executionruntime.WALRecord) ([]byte, error) {
	if record.Payload == nil {
		return []byte("{}"), nil
	}
	payload, err := json.Marshal(record.Payload)
	if err != nil {
		return nil, fmt.Errorf("marshal wal payload %s: %w", record.ID, err)
	}
	return payload, nil
}

func unmarshalWALPayload(id string, payload []byte) (map[string]any, error) {
	if len(payload) == 0 || string(payload) == "null" {
		return nil, nil
	}
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, fmt.Errorf("unmarshal wal payload %s: %w", id, err)
	}
	return out, nil
}

func isCompensationRecord(kind executionruntime.WALRecordType) bool {
	return kind == executionruntime.WALCompensationIntent || kind == executionruntime.WALCompensationResult
}

func nullableTime(ts *time.Time) any {
	if ts == nil {
		return nil
	}
	return ts.UTC()
}

func cloneNullableTime(ts *time.Time) *time.Time {
	if ts == nil {
		return nil
	}
	v := ts.UTC()
	return &v
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
