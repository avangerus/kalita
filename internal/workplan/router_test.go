package workplan

import (
	"context"
	"testing"

	"kalita/internal/caseruntime"
)

func TestRouterRoutesByCaseKind(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryQueueRepository()
	_ = repo.SaveQueue(context.Background(), WorkQueue{ID: "queue-1", AllowedCaseKinds: []string{"workflow.action"}})
	_ = repo.SaveQueue(context.Background(), WorkQueue{ID: "queue-2", AllowedCaseKinds: []string{"other"}})
	router := NewRouter(repo, "")
	queue, err := router.RouteCase(context.Background(), caseruntime.Case{Kind: "workflow.action"})
	if err != nil {
		t.Fatalf("RouteCase error = %v", err)
	}
	if queue.ID != "queue-1" {
		t.Fatalf("queue = %#v", queue)
	}
}

func TestRouterFallsBackToDefaultQueue(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryQueueRepository()
	_ = repo.SaveQueue(context.Background(), WorkQueue{ID: "default-queue", Name: "Default"})
	router := NewRouter(repo, "default-queue")
	queue, err := router.RouteCase(context.Background(), caseruntime.Case{Kind: "missing"})
	if err != nil {
		t.Fatalf("RouteCase error = %v", err)
	}
	if queue.ID != "default-queue" {
		t.Fatalf("queue = %#v", queue)
	}
}

func TestRouterReturnsErrorWhenNoRouteExists(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryQueueRepository()
	router := NewRouter(repo, "")
	if _, err := router.RouteCase(context.Background(), caseruntime.Case{Kind: "missing"}); err == nil {
		t.Fatal("RouteCase error = nil, want non-nil")
	}
}
