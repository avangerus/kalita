package workplan

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryCoordinationRepositorySaveAndGetDecision(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryCoordinationRepository()
	decision := CoordinationDecision{ID: "coord-1", CaseID: "case-1", WorkItemID: "wi-1", DecisionType: CoordinationExecuteNow, Priority: CoordinationPriorityExecuteNow, Reason: "selected", CreatedAt: time.Date(2026, 3, 22, 16, 0, 0, 0, time.UTC)}
	if err := repo.SaveDecision(context.Background(), decision); err != nil {
		t.Fatalf("SaveDecision error = %v", err)
	}
	got, ok, err := repo.GetDecision(context.Background(), "coord-1")
	if err != nil || !ok {
		t.Fatalf("GetDecision = %#v ok=%v err=%v", got, ok, err)
	}
	if got != decision {
		t.Fatalf("decision = %#v, want %#v", got, decision)
	}
}

func TestInMemoryCoordinationRepositoryListByWorkItem(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryCoordinationRepository()
	seedCoordinationDecisions(t, repo)
	got, err := repo.ListByWorkItem(context.Background(), "wi-1")
	if err != nil {
		t.Fatalf("ListByWorkItem error = %v", err)
	}
	if len(got) != 2 || got[0].ID != "coord-1" || got[1].ID != "coord-2" {
		t.Fatalf("decisions = %#v", got)
	}
}

func TestInMemoryCoordinationRepositoryListByCase(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryCoordinationRepository()
	seedCoordinationDecisions(t, repo)
	got, err := repo.ListByCase(context.Background(), "case-1")
	if err != nil {
		t.Fatalf("ListByCase error = %v", err)
	}
	if len(got) != 2 || got[0].ID != "coord-1" || got[1].ID != "coord-2" {
		t.Fatalf("decisions = %#v", got)
	}
}

func TestInMemoryCoordinationRepositoryListByQueue(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryCoordinationRepository()
	seedCoordinationDecisions(t, repo)
	got, err := repo.ListByQueue(context.Background(), "queue-1")
	if err != nil {
		t.Fatalf("ListByQueue error = %v", err)
	}
	if len(got) != 2 || got[0].ID != "coord-1" || got[1].ID != "coord-3" {
		t.Fatalf("decisions = %#v", got)
	}
}

func seedCoordinationDecisions(t *testing.T, repo *InMemoryCoordinationRepository) {
	t.Helper()
	for _, d := range []CoordinationDecision{{ID: "coord-1", CaseID: "case-1", WorkItemID: "wi-1", QueueID: "queue-1"}, {ID: "coord-2", CaseID: "case-1", WorkItemID: "wi-1", QueueID: "queue-2"}, {ID: "coord-3", CaseID: "case-2", WorkItemID: "wi-2", QueueID: "queue-1"}} {
		if err := repo.SaveDecision(context.Background(), d); err != nil {
			t.Fatalf("SaveDecision(%s) error = %v", d.ID, err)
		}
	}
}
