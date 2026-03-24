package workplan

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInMemoryWorkQueueSnapshotStatusCounts(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryQueueRepository()
	base := time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC)
	require.NoError(t, repo.SaveWorkItem(context.Background(), WorkItem{ID: "wi-1", Status: string(WorkItemOpen), Type: "cap-a", CreatedAt: base}))
	require.NoError(t, repo.SaveWorkItem(context.Background(), WorkItem{ID: "wi-2", Status: string(WorkItemDone), Type: "cap-b", CreatedAt: base}))
	require.NoError(t, repo.SaveWorkItem(context.Background(), WorkItem{ID: "wi-3", Status: string(WorkItemOpen), Type: "cap-a", CreatedAt: base}))

	snapshot := NewInMemoryWorkQueueSnapshot(repo)
	snapshot.nowFn = func() time.Time { return base.Add(1 * time.Hour) }
	require.NoError(t, snapshot.Refresh(context.Background()))

	assert.Equal(t, map[WorkItemStatus]int{
		WorkItemOpen: 2,
		WorkItemDone: 1,
	}, snapshot.GetStatusCounts())
}

func TestInMemoryWorkQueueSnapshotAverageAge(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryQueueRepository()
	now := time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC)
	require.NoError(t, repo.SaveWorkItem(context.Background(), WorkItem{ID: "wi-1", Status: string(WorkItemOpen), Type: "cap-a", CreatedAt: now.Add(-10 * time.Minute)}))
	require.NoError(t, repo.SaveWorkItem(context.Background(), WorkItem{ID: "wi-2", Status: string(WorkItemDone), Type: "cap-b", CreatedAt: now.Add(-20 * time.Minute)}))

	snapshot := NewInMemoryWorkQueueSnapshot(repo)
	snapshot.nowFn = func() time.Time { return now }
	require.NoError(t, snapshot.Refresh(context.Background()))
	assert.Equal(t, 15*time.Minute, snapshot.GetAverageAge())
}

func TestInMemoryWorkQueueSnapshotAverageAgeEmptyQueue(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryQueueRepository()
	snapshot := NewInMemoryWorkQueueSnapshot(repo)
	snapshot.nowFn = func() time.Time { return time.Date(2026, 3, 24, 12, 0, 0, 0, time.UTC) }

	require.NoError(t, snapshot.Refresh(context.Background()))
	assert.Equal(t, time.Duration(0), snapshot.GetAverageAge())
}

func TestInMemoryWorkQueueSnapshotItemsByCapability(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryQueueRepository()
	base := time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC)
	require.NoError(t, repo.SaveWorkItem(context.Background(), WorkItem{ID: "wi-1", Status: string(WorkItemOpen), Type: "legacy_workflow_action", CreatedAt: base}))
	require.NoError(t, repo.SaveWorkItem(context.Background(), WorkItem{ID: "wi-2", Status: string(WorkItemOpen), Type: "external_incident_followup", CreatedAt: base}))
	require.NoError(t, repo.SaveWorkItem(context.Background(), WorkItem{ID: "wi-3", Status: string(WorkItemDone), Type: "legacy_workflow_action", CreatedAt: base}))

	snapshot := NewInMemoryWorkQueueSnapshot(repo)
	snapshot.nowFn = func() time.Time { return base.Add(30 * time.Minute) }
	require.NoError(t, snapshot.Refresh(context.Background()))

	assert.Equal(t, map[string]int{
		"legacy_workflow_action":     2,
		"external_incident_followup": 1,
	}, snapshot.GetItemsByCapability())
}

func TestInMemoryWorkQueueSnapshotRefreshIsIdempotentAndUpdatesCache(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryQueueRepository()
	base := time.Date(2026, 3, 24, 10, 0, 0, 0, time.UTC)
	require.NoError(t, repo.SaveWorkItem(context.Background(), WorkItem{ID: "wi-1", Status: string(WorkItemOpen), Type: "cap-a", CreatedAt: base}))

	snapshot := NewInMemoryWorkQueueSnapshot(repo)
	snapshot.nowFn = func() time.Time { return base.Add(1 * time.Hour) }
	require.NoError(t, snapshot.Refresh(context.Background()))
	assert.Equal(t, map[WorkItemStatus]int{WorkItemOpen: 1}, snapshot.GetStatusCounts())

	require.NoError(t, repo.SaveWorkItem(context.Background(), WorkItem{ID: "wi-2", Status: string(WorkItemDone), Type: "cap-b", CreatedAt: base.Add(10 * time.Minute)}))
	require.NoError(t, snapshot.Refresh(context.Background()))

	assert.Equal(t, map[WorkItemStatus]int{
		WorkItemOpen: 1,
		WorkItemDone: 1,
	}, snapshot.GetStatusCounts())
	assert.Equal(t, map[string]int{
		"cap-a": 1,
		"cap-b": 1,
	}, snapshot.GetItemsByCapability())
}
