package proposal

import (
	"context"
	"testing"
)

func TestRepositorySaveGetAndList(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryRepository()
	first := Proposal{ID: "p-1", WorkItemID: "w-1", ActorID: "a-1", Payload: map[string]any{"actions": []any{map[string]any{"type": "legacy_workflow_action"}}}}
	second := Proposal{ID: "p-2", WorkItemID: "w-1", ActorID: "a-2", Payload: map[string]any{"actions": []any{map[string]any{"type": "legacy_workflow_action"}}}}
	if err := repo.Save(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	got, ok, err := repo.Get(context.Background(), "p-1")
	if err != nil || !ok || got.ID != "p-1" {
		t.Fatalf("got=%#v ok=%v err=%v", got, ok, err)
	}
	listed, err := repo.ListByWorkItem(context.Background(), "w-1")
	if err != nil || len(listed) != 2 || listed[0].ID != "p-1" || listed[1].ID != "p-2" {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
}

func TestRepositoryListByActor(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryRepository()
	_ = repo.Save(context.Background(), Proposal{ID: "p-1", WorkItemID: "w-1", ActorID: "a-1"})
	_ = repo.Save(context.Background(), Proposal{ID: "p-2", WorkItemID: "w-2", ActorID: "a-1"})
	listed, err := repo.ListByActor(context.Background(), "a-1")
	if err != nil || len(listed) != 2 {
		t.Fatalf("listed=%#v err=%v", listed, err)
	}
}
