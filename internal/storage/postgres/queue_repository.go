package postgres

import (
	"context"

	"kalita/internal/workplan"

	"github.com/jackc/pgx/v5/pgxpool"
)

// QueueRepository keeps queue definitions in memory while persisting work items in Postgres.
type QueueRepository struct {
	queues    *workplan.InMemoryQueueRepository
	workItems *PostgresWorkItemRepository
}

var _ workplan.QueueRepository = (*QueueRepository)(nil)

func NewHybridQueueRepository(queues *workplan.InMemoryQueueRepository, pool *pgxpool.Pool) *QueueRepository {
	return &QueueRepository{
		queues:    queues,
		workItems: NewPostgresWorkItemRepository(pool),
	}
}

func (r *QueueRepository) SaveQueue(ctx context.Context, q workplan.WorkQueue) error {
	return r.queues.SaveQueue(ctx, q)
}

func (r *QueueRepository) GetQueue(ctx context.Context, id string) (workplan.WorkQueue, bool, error) {
	return r.queues.GetQueue(ctx, id)
}

func (r *QueueRepository) ListQueues(ctx context.Context) ([]workplan.WorkQueue, error) {
	return r.queues.ListQueues(ctx)
}

func (r *QueueRepository) SaveWorkItem(ctx context.Context, wi workplan.WorkItem) error {
	return r.workItems.Save(ctx, wi)
}

func (r *QueueRepository) GetWorkItem(ctx context.Context, id string) (workplan.WorkItem, bool, error) {
	return r.workItems.FindByID(ctx, id)
}

func (r *QueueRepository) ListWorkItemsByCase(ctx context.Context, caseID string) ([]workplan.WorkItem, error) {
	return r.workItems.FindByCaseID(ctx, caseID)
}

func (r *QueueRepository) ListWorkItemsByQueue(ctx context.Context, queueID string) ([]workplan.WorkItem, error) {
	return r.workItems.ListWorkItemsByQueue(ctx, queueID)
}

func (r *QueueRepository) ListWorkItems(ctx context.Context) ([]workplan.WorkItem, error) {
	return r.workItems.ListWorkItems(ctx)
}
