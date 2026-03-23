package executioncontrol

import (
	"context"
	"testing"
	"time"

	"kalita/internal/eventcore"
	"kalita/internal/policy"
	"kalita/internal/trust"
	"kalita/internal/workplan"
)

type fakeClock struct{ now time.Time }

func (f fakeClock) Now() time.Time { return f.now }

type fakeIDs struct {
	ids []string
	i   int
}

func (f *fakeIDs) NewID() string {
	id := f.ids[f.i]
	f.i++
	return id
}

func TestServiceCreateAndRecordCreatesConstraintsAndEvent(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryConstraintsRepository()
	log := eventcore.NewInMemoryEventLog()
	service := NewService(repo, NewPlanner(), log, fakeClock{now: time.Date(2026, 3, 22, 16, 0, 0, 0, time.UTC)}, &fakeIDs{ids: []string{"constraints-1", "event-1"}})
	ctx := ContextWithExecution(context.Background(), ExecutionContext{ExecutionID: "exec-1", CorrelationID: "corr-1", CausationID: "cause-1"})
	coordination := workplan.CoordinationDecision{ID: "coord-1", CaseID: "case-1", WorkItemID: "work-1", QueueID: "queue-1", DecisionType: workplan.CoordinationExecuteNow}
	decision := policy.PolicyDecision{ID: "pol-1", Outcome: policy.PolicyAllow}

	constraints, err := service.CreateAndRecord(ctx, coordination, decision)
	if err != nil {
		t.Fatalf("CreateAndRecord error = %v", err)
	}
	if constraints.ID != "constraints-1" || constraints.CreatedAt.IsZero() {
		t.Fatalf("constraints = %#v", constraints)
	}
	stored, ok, err := repo.Get(context.Background(), "constraints-1")
	if err != nil || !ok {
		t.Fatalf("repo.Get error = %v ok=%v", err, ok)
	}
	if stored.ExecutionMode != ExecutionModeDeterministicAPI {
		t.Fatalf("stored = %#v", stored)
	}
	_, executionEvents, err := log.ListByCorrelation(context.Background(), "corr-1")
	if err != nil {
		t.Fatalf("ListByCorrelation error = %v", err)
	}
	if len(executionEvents) != 1 {
		t.Fatalf("executionEvents len = %d", len(executionEvents))
	}
	if executionEvents[0].Step != "execution_constraints_created" || executionEvents[0].Status != "ready" {
		t.Fatalf("executionEvents[0] = %#v", executionEvents[0])
	}
}

func TestServiceDeniedPolicyDoesNotCreateConstraints(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryConstraintsRepository()
	service := NewService(repo, NewPlanner(), nil, fakeClock{now: time.Date(2026, 3, 22, 16, 0, 0, 0, time.UTC)}, &fakeIDs{ids: []string{"constraints-1"}})
	_, err := service.CreateAndRecord(context.Background(), workplan.CoordinationDecision{ID: "coord-1", DecisionType: workplan.CoordinationExecuteNow}, policy.PolicyDecision{ID: "pol-1", Outcome: policy.PolicyDeny})
	if err == nil {
		t.Fatal("expected error")
	}
	items, err := repo.ListByPolicyDecision(context.Background(), "pol-1")
	if err != nil {
		t.Fatalf("ListByPolicyDecision error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("items = %#v", items)
	}
}

func TestAdjustForTrustAppliesDeterministicExecutionModes(t *testing.T) {
	t.Parallel()

	base := ExecutionConstraints{ID: "constraints-1", ExecutionMode: ExecutionModeDeterministicAPI, MaxSteps: 10, MaxDurationSec: 300, Reason: "baseline"}

	low, _ := AdjustForTrust(base, trust.TrustProfile{ActorID: "emp-low", TrustLevel: trust.TrustLow})
	if low.ExecutionMode != ExecutionModeApprovalEachStep || low.MaxSteps != 1 || low.MaxDurationSec != 60 {
		t.Fatalf("low = %#v", low)
	}

	medium, _ := AdjustForTrust(base, trust.TrustProfile{ActorID: "emp-medium", TrustLevel: trust.TrustMedium})
	if medium.ExecutionMode != ExecutionModeSupervised || medium.MaxSteps != 5 || medium.MaxDurationSec != 180 {
		t.Fatalf("medium = %#v", medium)
	}

	high, _ := AdjustForTrust(base, trust.TrustProfile{ActorID: "emp-high", TrustLevel: trust.TrustHigh})
	if high.ExecutionMode != ExecutionModeStandard || high.MaxSteps != 10 || high.MaxDurationSec != 300 {
		t.Fatalf("high = %#v", high)
	}
}
