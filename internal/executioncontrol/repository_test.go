package executioncontrol

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryConstraintsRepositorySaveGetAndList(t *testing.T) {
	t.Parallel()

	repo := NewInMemoryConstraintsRepository()
	now := time.Date(2026, 3, 22, 15, 0, 0, 0, time.UTC)
	first := ExecutionConstraints{ID: "ec-1", PolicyDecisionID: "pol-1", CoordinationDecisionID: "coord-1", CaseID: "case-1", CreatedAt: now}
	second := ExecutionConstraints{ID: "ec-2", PolicyDecisionID: "pol-1", CoordinationDecisionID: "coord-2", CaseID: "case-2", CreatedAt: now.Add(time.Minute)}
	third := ExecutionConstraints{ID: "ec-3", PolicyDecisionID: "pol-2", CoordinationDecisionID: "coord-1", CaseID: "case-1", CreatedAt: now.Add(2 * time.Minute)}

	for _, item := range []ExecutionConstraints{first, second, third} {
		if err := repo.Save(context.Background(), item); err != nil {
			t.Fatalf("Save(%s) error = %v", item.ID, err)
		}
	}

	got, ok, err := repo.Get(context.Background(), "ec-1")
	if err != nil || !ok {
		t.Fatalf("Get error = %v ok=%v", err, ok)
	}
	if got.ID != first.ID || got.PolicyDecisionID != first.PolicyDecisionID {
		t.Fatalf("Get = %#v", got)
	}

	byPolicy, err := repo.ListByPolicyDecision(context.Background(), "pol-1")
	if err != nil {
		t.Fatalf("ListByPolicyDecision error = %v", err)
	}
	if len(byPolicy) != 2 || byPolicy[0].ID != "ec-1" || byPolicy[1].ID != "ec-2" {
		t.Fatalf("ListByPolicyDecision = %#v", byPolicy)
	}

	byCoordination, err := repo.ListByCoordinationDecision(context.Background(), "coord-1")
	if err != nil {
		t.Fatalf("ListByCoordinationDecision error = %v", err)
	}
	if len(byCoordination) != 2 || byCoordination[0].ID != "ec-1" || byCoordination[1].ID != "ec-3" {
		t.Fatalf("ListByCoordinationDecision = %#v", byCoordination)
	}

	byCase, err := repo.ListByCase(context.Background(), "case-1")
	if err != nil {
		t.Fatalf("ListByCase error = %v", err)
	}
	if len(byCase) != 2 || byCase[0].ID != "ec-1" || byCase[1].ID != "ec-3" {
		t.Fatalf("ListByCase = %#v", byCase)
	}
}
