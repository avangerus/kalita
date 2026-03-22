package policy

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryRepositorySavesGetsAndListsPolicyDecisions(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := context.Background()
	first := PolicyDecision{ID: "policy-1", CoordinationDecisionID: "coord-1", Outcome: PolicyAllow, Reason: "allowed", CreatedAt: time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC)}
	second := PolicyDecision{ID: "policy-2", CoordinationDecisionID: "coord-1", Outcome: PolicyDeny, Reason: "denied", CreatedAt: time.Date(2026, 3, 22, 12, 1, 0, 0, time.UTC)}
	if err := repo.SaveDecision(ctx, first); err != nil {
		t.Fatalf("SaveDecision first error = %v", err)
	}
	if err := repo.SaveDecision(ctx, second); err != nil {
		t.Fatalf("SaveDecision second error = %v", err)
	}
	got, ok, err := repo.GetDecision(ctx, "policy-1")
	if err != nil || !ok {
		t.Fatalf("GetDecision ok=%v err=%v", ok, err)
	}
	if got.Reason != "allowed" {
		t.Fatalf("GetDecision reason = %q", got.Reason)
	}
	listed, err := repo.ListByCoordinationDecision(ctx, "coord-1")
	if err != nil {
		t.Fatalf("ListByCoordinationDecision error = %v", err)
	}
	if len(listed) != 2 || listed[0].ID != "policy-1" || listed[1].ID != "policy-2" {
		t.Fatalf("listed = %#v", listed)
	}
}

func TestInMemoryRepositorySavesGetsAndListsApprovalRequests(t *testing.T) {
	repo := NewInMemoryRepository()
	ctx := context.Background()
	first := ApprovalRequest{ID: "approval-1", CoordinationDecisionID: "coord-1", PolicyDecisionID: "policy-1", Status: ApprovalPending, RequestedFromRole: "manager", CreatedAt: time.Date(2026, 3, 22, 12, 0, 0, 0, time.UTC)}
	second := ApprovalRequest{ID: "approval-2", CoordinationDecisionID: "coord-1", PolicyDecisionID: "policy-2", Status: ApprovalRejected, RequestedFromRole: "director", CreatedAt: time.Date(2026, 3, 22, 12, 1, 0, 0, time.UTC)}
	if err := repo.SaveApprovalRequest(ctx, first); err != nil {
		t.Fatalf("SaveApprovalRequest first error = %v", err)
	}
	if err := repo.SaveApprovalRequest(ctx, second); err != nil {
		t.Fatalf("SaveApprovalRequest second error = %v", err)
	}
	got, ok, err := repo.GetApprovalRequest(ctx, "approval-1")
	if err != nil || !ok {
		t.Fatalf("GetApprovalRequest ok=%v err=%v", ok, err)
	}
	if got.RequestedFromRole != "manager" {
		t.Fatalf("GetApprovalRequest role = %q", got.RequestedFromRole)
	}
	listed, err := repo.ListApprovalRequestsByCoordinationDecision(ctx, "coord-1")
	if err != nil {
		t.Fatalf("ListApprovalRequestsByCoordinationDecision error = %v", err)
	}
	if len(listed) != 2 || listed[0].ID != "approval-1" || listed[1].ID != "approval-2" {
		t.Fatalf("listed = %#v", listed)
	}
}
