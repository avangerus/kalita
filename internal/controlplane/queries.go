package controlplane

import "context"

type Service interface {
	GetSummary(ctx context.Context) (Summary, error)
	GetCaseTimeline(ctx context.Context, caseID string) ([]TimelineEntry, error)

	GetCaseOverview(ctx context.Context, caseID string) (CaseOverview, error)
	ListCases(ctx context.Context) ([]CaseOverview, error)

	GetWorkItemOverview(ctx context.Context, workItemID string) (WorkItemOverview, error)
	ListWorkItems(ctx context.Context) ([]WorkItemOverview, error)

	GetActorOverview(ctx context.Context, actorID string) (ActorOverview, error)
	ListActors(ctx context.Context) ([]ActorOverview, error)

	GetApprovalInbox(ctx context.Context) ([]ApprovalInboxItem, error)
	GetBlockedOrDeferredWork(ctx context.Context) ([]WorkItemOverview, error)
}
