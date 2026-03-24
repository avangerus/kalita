package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"kalita/internal/actionplan"
	"kalita/internal/workplan"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresWorkItemRepository struct {
	pool *pgxpool.Pool
}

var _ workplan.WorkItemRepository = (*PostgresWorkItemRepository)(nil)

func NewPostgresWorkItemRepository(pool *pgxpool.Pool) *PostgresWorkItemRepository {
	return &PostgresWorkItemRepository{pool: pool}
}

func NewWorkItemRepository(pool *pgxpool.Pool) *PostgresWorkItemRepository {
	return NewPostgresWorkItemRepository(pool)
}

func (r *PostgresWorkItemRepository) Save(ctx context.Context, wi workplan.WorkItem) error {
	actionPlan, err := marshalWorkItemActionPlan(wi.ActionPlan)
	if err != nil {
		return err
	}

	createdAt := wi.CreatedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := wi.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}

	return r.withTx(ctx, pgx.TxOptions{}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO work_items (
				id, case_id, queue_id, type, status, priority, reason,
				assigned_employee_id, plan_id, due_at, action_plan, created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12, $13)
			ON CONFLICT (id) DO UPDATE SET
				case_id = EXCLUDED.case_id,
				queue_id = EXCLUDED.queue_id,
				type = EXCLUDED.type,
				status = EXCLUDED.status,
				priority = EXCLUDED.priority,
				reason = EXCLUDED.reason,
				assigned_employee_id = EXCLUDED.assigned_employee_id,
				plan_id = EXCLUDED.plan_id,
				due_at = EXCLUDED.due_at,
				action_plan = EXCLUDED.action_plan,
				created_at = EXCLUDED.created_at,
				updated_at = EXCLUDED.updated_at
		`, wi.ID, wi.CaseID, wi.QueueID, wi.Type, wi.Status, wi.Priority, wi.Reason, wi.AssignedEmployeeID, wi.PlanID, wi.DueAt, actionPlan, createdAt, updatedAt)
		if err != nil {
			return fmt.Errorf("save work item %s: %w", wi.ID, err)
		}
		return nil
	})
}

func (r *PostgresWorkItemRepository) FindByID(ctx context.Context, id string) (workplan.WorkItem, bool, error) {
	return r.queryOne(ctx, `
		SELECT id, case_id, queue_id, type, status, priority, reason,
			assigned_employee_id, plan_id, due_at, action_plan, created_at, updated_at
		FROM work_items
		WHERE id = $1
	`, id)
}

func (r *PostgresWorkItemRepository) FindByCaseID(ctx context.Context, caseID string) ([]workplan.WorkItem, error) {
	return r.queryMany(ctx, `
		SELECT id, case_id, queue_id, type, status, priority, reason,
			assigned_employee_id, plan_id, due_at, action_plan, created_at, updated_at
		FROM work_items
		WHERE case_id = $1
		ORDER BY created_at, id
	`, caseID)
}

func (r *PostgresWorkItemRepository) FindByStatus(ctx context.Context, status string) ([]workplan.WorkItem, error) {
	return r.queryMany(ctx, `
		SELECT id, case_id, queue_id, type, status, priority, reason,
			assigned_employee_id, plan_id, due_at, action_plan, created_at, updated_at
		FROM work_items
		WHERE status = $1
		ORDER BY created_at, id
	`, status)
}

func (r *PostgresWorkItemRepository) FindByActorID(ctx context.Context, actorID string) ([]workplan.WorkItem, error) {
	return r.queryMany(ctx, `
		SELECT id, case_id, queue_id, type, status, priority, reason,
			assigned_employee_id, plan_id, due_at, action_plan, created_at, updated_at
		FROM work_items
		WHERE assigned_employee_id = $1
		ORDER BY created_at, id
	`, actorID)
}

func (r *PostgresWorkItemRepository) ListWorkItemsByQueue(ctx context.Context, queueID string) ([]workplan.WorkItem, error) {
	return r.queryMany(ctx, `
		SELECT id, case_id, queue_id, type, status, priority, reason,
			assigned_employee_id, plan_id, due_at, action_plan, created_at, updated_at
		FROM work_items
		WHERE queue_id = $1
		ORDER BY created_at, id
	`, queueID)
}

func (r *PostgresWorkItemRepository) ListWorkItems(ctx context.Context) ([]workplan.WorkItem, error) {
	return r.queryMany(ctx, `
		SELECT id, case_id, queue_id, type, status, priority, reason,
			assigned_employee_id, plan_id, due_at, action_plan, created_at, updated_at
		FROM work_items
		ORDER BY created_at, id
	`)
}

func (r *PostgresWorkItemRepository) withTx(ctx context.Context, opts pgx.TxOptions, fn func(tx pgx.Tx) error) error {
	tx, err := r.pool.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin work item transaction: %w", err)
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit work item transaction: %w", err)
	}
	return nil
}

func (r *PostgresWorkItemRepository) queryOne(ctx context.Context, query string, arg string) (workplan.WorkItem, bool, error) {
	var (
		out   workplan.WorkItem
		found bool
	)
	err := r.withTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, query, arg)
		item, err := scanWorkItem(row)
		if err != nil {
			if isNoRows(err) {
				return nil
			}
			return err
		}
		out = item
		found = true
		return nil
	})
	if err != nil {
		return workplan.WorkItem{}, false, err
	}
	if !found {
		return workplan.WorkItem{}, false, nil
	}
	return out, true, nil
}

func (r *PostgresWorkItemRepository) queryMany(ctx context.Context, query string, args ...any) ([]workplan.WorkItem, error) {
	var out []workplan.WorkItem
	err := r.withTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("query work items: %w", err)
		}
		defer rows.Close()

		out = make([]workplan.WorkItem, 0)
		for rows.Next() {
			item, err := scanWorkItem(rows)
			if err != nil {
				return err
			}
			out = append(out, item)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate work items: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func scanWorkItem(row scanner) (workplan.WorkItem, error) {
	var (
		item               workplan.WorkItem
		assignedEmployeeID *string
		planID             *string
		dueAt              *time.Time
		actionPlan         []byte
	)
	if err := row.Scan(
		&item.ID,
		&item.CaseID,
		&item.QueueID,
		&item.Type,
		&item.Status,
		&item.Priority,
		&item.Reason,
		&assignedEmployeeID,
		&planID,
		&dueAt,
		&actionPlan,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return workplan.WorkItem{}, err
	}

	if assignedEmployeeID != nil {
		item.AssignedEmployeeID = *assignedEmployeeID
	}
	if planID != nil {
		item.PlanID = *planID
	}
	if dueAt != nil {
		due := dueAt.UTC()
		item.DueAt = &due
	}
	item.CreatedAt = item.CreatedAt.UTC()
	item.UpdatedAt = item.UpdatedAt.UTC()

	plan, err := unmarshalWorkItemActionPlan(item.ID, actionPlan)
	if err != nil {
		return workplan.WorkItem{}, err
	}
	item.ActionPlan = plan
	return item, nil
}

func marshalWorkItemActionPlan(plan *actionplan.ActionPlan) ([]byte, error) {
	if plan == nil {
		return []byte("null"), nil
	}
	payload, err := json.Marshal(plan)
	if err != nil {
		return nil, fmt.Errorf("marshal work item action plan %s: %w", plan.ID, err)
	}
	return payload, nil
}

func unmarshalWorkItemActionPlan(workItemID string, payload []byte) (*actionplan.ActionPlan, error) {
	if len(payload) == 0 || string(payload) == "null" {
		return nil, nil
	}
	var plan actionplan.ActionPlan
	if err := json.Unmarshal(payload, &plan); err != nil {
		return nil, fmt.Errorf("unmarshal work item action plan for %s: %w", workItemID, err)
	}
	return &plan, nil
}
