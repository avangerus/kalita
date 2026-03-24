package workplan

import (
	"context"
	"sort"
	"strings"
)

type QueuePressureScorer interface {
	Score(ctx context.Context, capability string, queueLen int) float64
	ListQueuePressure(ctx context.Context) ([]QueuePressure, error)
}

type QueuePressure struct {
	DepartmentID   string
	PressureScore  float64
	WorkItemsCount int
}

type queuePressureScorer struct {
	config         CoordinationConfig
	queueRepo      QueueRepository
	departmentLoad DepartmentLoadProvider
}

func NewQueuePressureScorer(config CoordinationConfig, queueRepo QueueRepository, departmentLoad DepartmentLoadProvider) QueuePressureScorer {
	if config.QueueDepthThreshold <= 0 {
		config = defaultCoordinationConfig()
	}
	if departmentLoad == nil {
		departmentLoad = config.DepartmentLoadSource
	}
	return &queuePressureScorer{config: config, queueRepo: queueRepo, departmentLoad: departmentLoad}
}

func (s *queuePressureScorer) Score(_ context.Context, _ string, queueLen int) float64 {
	threshold := s.config.QueueDepthThreshold
	if threshold <= 0 {
		if queueLen > 0 {
			return 1.0
		}
		return 0.0
	}
	if queueLen <= threshold {
		return 0.0
	}
	pressure := float64(queueLen-threshold) / float64(threshold)
	if pressure > 1.0 {
		return 1.0
	}
	if pressure < 0.0 {
		return 0.0
	}
	return pressure
}

func (s *queuePressureScorer) ListQueuePressure(ctx context.Context) ([]QueuePressure, error) {
	if s.queueRepo == nil {
		return nil, nil
	}
	queues, err := s.queueRepo.ListQueues(ctx)
	if err != nil {
		return nil, err
	}
	items, err := s.queueRepo.ListWorkItems(ctx)
	if err != nil {
		return nil, err
	}

	departmentCounts := make(map[string]int)
	for _, queue := range queues {
		departmentID := strings.TrimSpace(queue.Department)
		if departmentID == "" {
			continue
		}
		if _, exists := departmentCounts[departmentID]; !exists {
			departmentCounts[departmentID] = 0
		}
	}
	for _, item := range items {
		if item.Status == string(WorkItemDone) {
			continue
		}
		queue, ok, err := s.queueRepo.GetQueue(ctx, item.QueueID)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		departmentID := strings.TrimSpace(queue.Department)
		if departmentID == "" {
			continue
		}
		departmentCounts[departmentID]++
	}

	out := make([]QueuePressure, 0, len(departmentCounts))
	for departmentID, workItemsCount := range departmentCounts {
		throughput := s.config.QueueDepthThreshold
		if s.departmentLoad != nil {
			load, err := s.departmentLoad.GetLoad(ctx, departmentID)
			if err != nil {
				return nil, err
			}
			if load.DepartmentExists && load.TotalActors > 0 {
				throughput = load.TotalActors
			}
		}
		out = append(out, QueuePressure{
			DepartmentID:   departmentID,
			PressureScore:  s.departmentPressureScore(ctx, departmentID, workItemsCount, throughput),
			WorkItemsCount: workItemsCount,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].DepartmentID < out[j].DepartmentID
	})
	return out, nil
}

func (s *queuePressureScorer) departmentPressureScore(ctx context.Context, departmentID string, workItemsCount int, throughput int) float64 {
	if workItemsCount <= 0 {
		return 0
	}
	if throughput <= 0 {
		throughput = 1
	}
	score := float64(workItemsCount) / float64(workItemsCount+throughput)
	if s.departmentLoad != nil {
		load, err := s.departmentLoad.GetLoad(ctx, departmentID)
		if err == nil && load.DepartmentExists && load.TotalActors > 0 {
			busyActors := load.BusyActors
			if busyActors < 0 {
				busyActors = 0
			}
			if busyActors > load.TotalActors {
				busyActors = load.TotalActors
			}
			utilization := float64(busyActors) / float64(load.TotalActors)
			score = score + ((1.0 - score) * utilization)
		}
	}
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}
