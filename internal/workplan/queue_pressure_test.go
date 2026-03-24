package workplan

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQueuePressureScorer_Score(t *testing.T) {
	t.Parallel()

	scorer := NewQueuePressureScorer(CoordinationConfig{QueueDepthThreshold: 4}, nil, nil)

	testCases := []struct {
		name     string
		queueLen int
		want     float64
	}{
		{name: "empty queue", queueLen: 0, want: 0},
		{name: "below threshold", queueLen: 3, want: 0},
		{name: "above threshold", queueLen: 6, want: 0.5},
		{name: "clamped at one", queueLen: 10, want: 1},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, scorer.Score(context.Background(), "workflow.action", tc.queueLen))
		})
	}
}

func TestQueuePressureScorer_ListQueuePressure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	queueRepo := NewInMemoryQueueRepository()
	loadProvider := NewInMemoryDepartmentLoadProvider()
	loadProvider.SaveLoad(ctx, DepartmentLoad{DepartmentID: "ops", TotalActors: 4, BusyActors: 1, DepartmentExists: true})
	loadProvider.SaveLoad(ctx, DepartmentLoad{DepartmentID: "support", TotalActors: 2, BusyActors: 2, DepartmentExists: true})

	require.NoError(t, queueRepo.SaveQueue(ctx, WorkQueue{ID: "queue-1", Department: "ops"}))
	require.NoError(t, queueRepo.SaveQueue(ctx, WorkQueue{ID: "queue-2", Department: "support"}))
	require.NoError(t, queueRepo.SaveQueue(ctx, WorkQueue{ID: "queue-3", Department: "ops"}))
	require.NoError(t, queueRepo.SaveWorkItem(ctx, WorkItem{ID: "wi-1", QueueID: "queue-1", Status: string(WorkItemOpen)}))
	require.NoError(t, queueRepo.SaveWorkItem(ctx, WorkItem{ID: "wi-2", QueueID: "queue-1", Status: string(WorkItemOpen)}))
	require.NoError(t, queueRepo.SaveWorkItem(ctx, WorkItem{ID: "wi-3", QueueID: "queue-2", Status: string(WorkItemOpen)}))
	require.NoError(t, queueRepo.SaveWorkItem(ctx, WorkItem{ID: "wi-4", QueueID: "queue-3", Status: string(WorkItemDone)}))

	scorer := NewQueuePressureScorer(CoordinationConfig{QueueDepthThreshold: 3}, queueRepo, loadProvider)

	got, err := scorer.ListQueuePressure(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)

	assert.Equal(t, "ops", got[0].DepartmentID)
	assert.Equal(t, 2, got[0].WorkItemsCount)
	assert.InDelta(t, 0.5, got[0].PressureScore, 0.001)

	assert.Equal(t, "support", got[1].DepartmentID)
	assert.Equal(t, 1, got[1].WorkItemsCount)
	assert.InDelta(t, 1.0, got[1].PressureScore, 0.001)
}
