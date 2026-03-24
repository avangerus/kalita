package workplan

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

type WorkQueueSnapshot interface {
	GetStatusCounts() map[WorkItemStatus]int
	GetAverageAge() time.Duration
	GetItemsByCapability() map[string]int
	Refresh(ctx context.Context) error
}

type InMemoryWorkQueueSnapshot struct {
	repo QueueRepository

	mu                sync.RWMutex
	statusCounts      map[WorkItemStatus]int
	averageAge        time.Duration
	itemsByCapability map[string]int
	nowFn             func() time.Time
}

func NewInMemoryWorkQueueSnapshot(repo QueueRepository) *InMemoryWorkQueueSnapshot {
	return &InMemoryWorkQueueSnapshot{
		repo:              repo,
		statusCounts:      make(map[WorkItemStatus]int),
		itemsByCapability: make(map[string]int),
		nowFn:             time.Now,
	}
}

func (s *InMemoryWorkQueueSnapshot) GetStatusCounts() map[WorkItemStatus]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[WorkItemStatus]int, len(s.statusCounts))
	for status, count := range s.statusCounts {
		out[status] = count
	}
	return out
}

func (s *InMemoryWorkQueueSnapshot) GetAverageAge() time.Duration {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.averageAge
}

func (s *InMemoryWorkQueueSnapshot) GetItemsByCapability() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]int, len(s.itemsByCapability))
	for capability, count := range s.itemsByCapability {
		out[capability] = count
	}
	return out
}

func (s *InMemoryWorkQueueSnapshot) Refresh(ctx context.Context) error {
	if s.repo == nil {
		return fmt.Errorf("queue repository is nil")
	}
	items, err := s.repo.ListWorkItems(ctx)
	if err != nil {
		return err
	}

	sort.Slice(items, func(i, j int) bool {
		if items[i].ID == items[j].ID {
			return items[i].CreatedAt.Before(items[j].CreatedAt)
		}
		return items[i].ID < items[j].ID
	})

	now := s.nowFn()
	statusCounts := make(map[WorkItemStatus]int)
	itemsByCapability := make(map[string]int)
	totalAge := time.Duration(0)
	for _, item := range items {
		status := WorkItemStatus(item.Status)
		statusCounts[status]++
		capability := item.Type
		itemsByCapability[capability]++
		age := now.Sub(item.CreatedAt)
		if age < 0 {
			age = 0
		}
		totalAge += age
	}

	averageAge := time.Duration(0)
	if len(items) > 0 {
		averageAge = totalAge / time.Duration(len(items))
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.statusCounts = statusCounts
	s.itemsByCapability = itemsByCapability
	s.averageAge = averageAge
	return nil
}
