package workplan

import (
	"context"
	"testing"
)

func TestInMemoryQueueRepositorySaveGetListQueue(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryQueueRepository()
	queue := WorkQueue{ID: "queue-1", Name: "Operations", AllowedCaseKinds: []string{"workflow.action"}}
	if err := repo.SaveQueue(context.Background(), queue); err != nil {
		t.Fatalf("SaveQueue error = %v", err)
	}
	got, ok, err := repo.GetQueue(context.Background(), "queue-1")
	if err != nil || !ok {
		t.Fatalf("GetQueue = %#v ok=%v err=%v", got, ok, err)
	}
	got.AllowedCaseKinds[0] = "mutated"
	reloaded, _, _ := repo.GetQueue(context.Background(), "queue-1")
	if reloaded.AllowedCaseKinds[0] != "workflow.action" {
		t.Fatalf("queue clone failed: %#v", reloaded)
	}
	queues, err := repo.ListQueues(context.Background())
	if err != nil {
		t.Fatalf("ListQueues error = %v", err)
	}
	if len(queues) != 1 || queues[0].ID != "queue-1" {
		t.Fatalf("queues = %#v", queues)
	}
}

func TestInMemoryQueueRepositorySaveGetListWorkItemsByCaseAndQueue(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryQueueRepository()
	items := []WorkItem{{ID: "wi-1", CaseID: "case-1", QueueID: "queue-1", Type: "workflow.action"}, {ID: "wi-2", CaseID: "case-1", QueueID: "queue-2", Type: "followup"}, {ID: "wi-3", CaseID: "case-2", QueueID: "queue-1", Type: "other"}}
	for _, item := range items {
		if err := repo.SaveWorkItem(context.Background(), item); err != nil {
			t.Fatalf("SaveWorkItem(%s) error = %v", item.ID, err)
		}
	}
	got, ok, err := repo.GetWorkItem(context.Background(), "wi-1")
	if err != nil || !ok {
		t.Fatalf("GetWorkItem = %#v ok=%v err=%v", got, ok, err)
	}
	byCase, err := repo.ListWorkItemsByCase(context.Background(), "case-1")
	if err != nil {
		t.Fatalf("ListWorkItemsByCase error = %v", err)
	}
	if len(byCase) != 2 || byCase[0].ID != "wi-1" || byCase[1].ID != "wi-2" {
		t.Fatalf("byCase = %#v", byCase)
	}
	byQueue, err := repo.ListWorkItemsByQueue(context.Background(), "queue-1")
	if err != nil {
		t.Fatalf("ListWorkItemsByQueue error = %v", err)
	}
	if len(byQueue) != 2 || byQueue[0].ID != "wi-1" || byQueue[1].ID != "wi-3" {
		t.Fatalf("byQueue = %#v", byQueue)
	}
}
