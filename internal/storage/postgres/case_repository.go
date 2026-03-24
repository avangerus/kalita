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

type CaseRepository struct {
	pool *pgxpool.Pool
}

func NewCaseRepository(pool *pgxpool.Pool) *CaseRepository {
	return &CaseRepository{pool: pool}
}

func (r *CaseRepository) Save(ctx context.Context, c caseruntime.Case) error {
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

	_, err = r.pool.Exec(ctx, `
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
}

func (r *CaseRepository) GetByID(ctx context.Context, id string) (caseruntime.Case, bool, error) {
	return r.queryOne(ctx, `SELECT id, status, created_at, updated_at, metadata FROM cases WHERE id = $1`, id)
}

func (r *CaseRepository) List(ctx context.Context) ([]caseruntime.Case, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, status, created_at, updated_at, metadata FROM cases ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list cases: %w", err)
	}
	defer rows.Close()

	out := make([]caseruntime.Case, 0)
	for rows.Next() {
		c, err := scanCase(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list cases rows: %w", err)
	}
	return out, nil
}

func (r *CaseRepository) FindByCorrelation(ctx context.Context, correlationID string) (caseruntime.Case, bool, error) {
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

func (r *CaseRepository) FindBySubjectRef(ctx context.Context, subjectRef string) (caseruntime.Case, bool, error) {
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

func (r *CaseRepository) queryOne(ctx context.Context, query string, arg string) (caseruntime.Case, bool, error) {
	row := r.pool.QueryRow(ctx, query, arg)
	c, err := scanCase(row)
	if err != nil {
		if isNoRows(err) {
			return caseruntime.Case{}, false, nil
		}
		return caseruntime.Case{}, false, err
	}
	return c, true, nil
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
		OpenedAt:      createdAt,
		UpdatedAt:     updatedAt,
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
