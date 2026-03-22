package workplan

import (
	"context"
	"fmt"

	"kalita/internal/caseruntime"
)

type Router struct {
	repo           QueueRepository
	defaultQueueID string
}

func NewRouter(repo QueueRepository, defaultQueueID string) *Router {
	return &Router{repo: repo, defaultQueueID: defaultQueueID}
}

func (r *Router) RouteCase(ctx context.Context, c caseruntime.Case) (WorkQueue, error) {
	if r.repo == nil {
		return WorkQueue{}, fmt.Errorf("queue repository is nil")
	}
	queues, err := r.repo.ListQueues(ctx)
	if err != nil {
		return WorkQueue{}, err
	}
	for _, q := range queues {
		for _, allowed := range q.AllowedCaseKinds {
			if allowed == c.Kind {
				return q, nil
			}
		}
	}
	if r.defaultQueueID != "" {
		if q, ok, err := r.repo.GetQueue(ctx, r.defaultQueueID); err != nil {
			return WorkQueue{}, err
		} else if ok {
			return q, nil
		}
	}
	return WorkQueue{}, fmt.Errorf("no queue available for case kind %q", c.Kind)
}
