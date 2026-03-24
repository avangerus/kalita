package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"kalita/internal/caseruntime"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresCaseRepository struct {
	pool *pgxpool.Pool
}

var _ caseruntime.CaseRepository = (*PostgresCaseRepository)(nil)

func NewPostgresCaseRepository(pool *pgxpool.Pool) *PostgresCaseRepository {
	return &PostgresCaseRepository{pool: pool}
}

func NewCaseRepository(pool *pgxpool.Pool) *PostgresCaseRepository {
	return NewPostgresCaseRepository(pool)
}

func (r *PostgresCaseRepository) Save(ctx context.Context, c caseruntime.Case) error {
	metadata, err := marshalCaseMetadata(c)
	if err != nil {
		return err
	}

	createdAt := c.OpenedAt.UTC()
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	updatedAt := c.UpdatedAt.UTC()
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}

	return r.withTx(ctx, pgx.TxOptions{}, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO cases (id, status, created_at, updated_at, metadata)
			VALUES ($1, $2, $3, $4, $5::jsonb)
			ON CONFLICT (id) DO UPDATE SET
				status = EXCLUDED.status,
				created_at = EXCLUDED.created_at,
				updated_at = EXCLUDED.updated_at,
				metadata = EXCLUDED.metadata
		`, c.ID, c.Status, createdAt, updatedAt, metadata)
		if err != nil {
			return fmt.Errorf("save case %s: %w", c.ID, err)
		}
		return nil
	})
}

func (r *PostgresCaseRepository) FindByID(ctx context.Context, id string) (caseruntime.Case, bool, error) {
	return r.queryOne(ctx, `SELECT id, status, created_at, updated_at, metadata FROM cases WHERE id = $1`, id)
}

func (r *PostgresCaseRepository) GetByID(ctx context.Context, id string) (caseruntime.Case, bool, error) {
	return r.FindByID(ctx, id)
}

func (r *PostgresCaseRepository) FindAll(ctx context.Context) ([]caseruntime.Case, error) {
	return r.queryMany(ctx, `
		SELECT id, status, created_at, updated_at, metadata
		FROM cases
		ORDER BY created_at, id
	`)
}

func (r *PostgresCaseRepository) List(ctx context.Context) ([]caseruntime.Case, error) {
	return r.FindAll(ctx)
}

func (r *PostgresCaseRepository) FindByStatus(ctx context.Context, status string) ([]caseruntime.Case, error) {
	return r.queryMany(ctx, `
		SELECT id, status, created_at, updated_at, metadata
		FROM cases
		WHERE status = $1
		ORDER BY created_at, id
	`, status)
}

func (r *PostgresCaseRepository) FindByCorrelation(ctx context.Context, correlationID string) (caseruntime.Case, bool, error) {
	if correlationID == "" {
		return caseruntime.Case{}, false, nil
	}
	return r.queryOne(ctx, `
		SELECT id, status, created_at, updated_at, metadata
		FROM cases
		WHERE metadata->>'correlation_id' = $1
		ORDER BY created_at, id
		LIMIT 1
	`, correlationID)
}

func (r *PostgresCaseRepository) FindBySubjectRef(ctx context.Context, subjectRef string) (caseruntime.Case, bool, error) {
	if subjectRef == "" {
		return caseruntime.Case{}, false, nil
	}
	return r.queryOne(ctx, `
		SELECT id, status, created_at, updated_at, metadata
		FROM cases
		WHERE metadata->>'subject_ref' = $1
		ORDER BY created_at, id
		LIMIT 1
	`, subjectRef)
}

type scanner interface {
	Scan(dest ...any) error
}

var _ scanner = (pgx.Row)(nil)

func (r *PostgresCaseRepository) withTx(ctx context.Context, opts pgx.TxOptions, fn func(tx pgx.Tx) error) error {
	tx, err := r.pool.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin case transaction: %w", err)
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit case transaction: %w", err)
	}
	return nil
}

func (r *PostgresCaseRepository) queryOne(ctx context.Context, query string, arg string) (caseruntime.Case, bool, error) {
	var (
		out   caseruntime.Case
		found bool
	)
	err := r.withTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		row := tx.QueryRow(ctx, query, arg)
		c, err := scanCase(row)
		if err != nil {
			if isNoRows(err) {
				return nil
			}
			return err
		}
		out = c
		found = true
		return nil
	})
	if err != nil {
		return caseruntime.Case{}, false, err
	}
	if !found {
		return caseruntime.Case{}, false, nil
	}
	return out, true, nil
}

func (r *PostgresCaseRepository) queryMany(ctx context.Context, query string, args ...any) ([]caseruntime.Case, error) {
	var out []caseruntime.Case
	err := r.withTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("query cases: %w", err)
		}
		defer rows.Close()

		out = make([]caseruntime.Case, 0)
		for rows.Next() {
			c, err := scanCase(rows)
			if err != nil {
				return err
			}
			out = append(out, c)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("iterate cases: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func scanCase(row scanner) (caseruntime.Case, error) {
	var (
		id        string
		status    string
		createdAt time.Time
		updatedAt time.Time
		metadata  []byte
	)
	if err := row.Scan(&id, &status, &createdAt, &updatedAt, &metadata); err != nil {
		return caseruntime.Case{}, err
	}

	var meta caseMetadata
	if err := json.Unmarshal(metadata, &meta); err != nil {
		return caseruntime.Case{}, fmt.Errorf("unmarshal case metadata for %s: %w", id, err)
	}

	return caseruntime.Case{
		ID:            id,
		Status:        status,
		Kind:          meta.Kind,
		Title:         meta.Title,
		SubjectRef:    meta.SubjectRef,
		CorrelationID: meta.CorrelationID,
		OpenedAt:      createdAt.UTC(),
		UpdatedAt:     updatedAt.UTC(),
		OwnerQueueID:  meta.OwnerQueueID,
		CurrentPlanID: meta.CurrentPlanID,
		Attributes:    cloneAttributes(meta.Attributes),
	}, nil
}

type caseMetadata struct {
	Kind          string         `json:"kind"`
	Title         string         `json:"title"`
	SubjectRef    string         `json:"subject_ref"`
	CorrelationID string         `json:"correlation_id"`
	OwnerQueueID  string         `json:"owner_queue_id"`
	CurrentPlanID string         `json:"current_plan_id"`
	Attributes    map[string]any `json:"attributes"`
}

func marshalCaseMetadata(c caseruntime.Case) ([]byte, error) {
	metadata, err := json.Marshal(caseMetadata{
		Kind:          c.Kind,
		Title:         c.Title,
		SubjectRef:    c.SubjectRef,
		CorrelationID: c.CorrelationID,
		OwnerQueueID:  c.OwnerQueueID,
		CurrentPlanID: c.CurrentPlanID,
		Attributes:    cloneAttributes(c.Attributes),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal case metadata %s: %w", c.ID, err)
	}
	return metadata, nil
}

func cloneAttributes(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
