package persistence

import (
	"context"

	"kalita/internal/capability"
	"kalita/internal/caseruntime"
	"kalita/internal/employee"
	"kalita/internal/eventcore"
	"kalita/internal/executionruntime"
	"kalita/internal/policy"
	"kalita/internal/profile"
	"kalita/internal/proposal"
	"kalita/internal/trust"
	"kalita/internal/workplan"
)

type StateCollector struct {
	Cases         *caseruntime.InMemoryCaseRepository
	Queues        *workplan.InMemoryQueueRepository
	Coordinations *workplan.InMemoryCoordinationRepository
	Policies      *policy.InMemoryRepository
	Proposals     *proposal.InMemoryRepository
	Employees     *employee.InMemoryDirectory
	Assignments   *employee.InMemoryAssignmentRepository
	Trust         *trust.InMemoryRepository
	Profiles      *profile.InMemoryRepository
	Capabilities  *capability.InMemoryCapabilityRepository
	Executions    *executionruntime.InMemoryExecutionRepository
	WAL           *executionruntime.InMemoryWAL
	EventLog      *eventcore.InMemoryEventLog
}

func (c *StateCollector) Collect(ctx context.Context) (SystemState, error) {
	cases, err := c.Cases.List(ctx)
	if err != nil {
		return SystemState{}, err
	}
	queues, err := c.Queues.ListQueues(ctx)
	if err != nil {
		return SystemState{}, err
	}
	workItems, err := c.Queues.ListWorkItems(ctx)
	if err != nil {
		return SystemState{}, err
	}
	coordinations, err := c.Coordinations.ListAll(ctx)
	if err != nil {
		return SystemState{}, err
	}
	policyDecisions, err := c.Policies.ListDecisions(ctx)
	if err != nil {
		return SystemState{}, err
	}
	approvalRequests, err := c.Policies.ListApprovalRequests(ctx)
	if err != nil {
		return SystemState{}, err
	}
	proposals, err := c.Proposals.ListAll(ctx)
	if err != nil {
		return SystemState{}, err
	}
	employees, err := c.Employees.ListEmployees(ctx)
	if err != nil {
		return SystemState{}, err
	}
	assignments, err := c.Assignments.ListAssignments(ctx)
	if err != nil {
		return SystemState{}, err
	}
	trustProfiles, err := c.Trust.List(ctx)
	if err != nil {
		return SystemState{}, err
	}
	profiles, err := c.Profiles.ListProfiles(ctx)
	if err != nil {
		return SystemState{}, err
	}
	requirements, err := c.Profiles.ListRequirements(ctx)
	if err != nil {
		return SystemState{}, err
	}
	capabilities, err := c.Capabilities.ListCapabilities(ctx)
	if err != nil {
		return SystemState{}, err
	}
	actorCapabilities, err := c.Capabilities.ListActorCapabilities(ctx)
	if err != nil {
		return SystemState{}, err
	}
	sessions, err := c.Executions.ListSessions(ctx)
	if err != nil {
		return SystemState{}, err
	}
	steps, err := c.Executions.ListSteps(ctx)
	if err != nil {
		return SystemState{}, err
	}
	walRecords, err := c.WAL.ListAll(ctx)
	if err != nil {
		return SystemState{}, err
	}
	domainEvents, executionEvents, err := c.EventLog.ListAll(ctx)
	if err != nil {
		return SystemState{}, err
	}
	return SystemState{
		Cases: cases, Queues: queues, WorkItems: workItems, Coordinations: coordinations,
		PolicyDecisions: policyDecisions, ApprovalRequests: approvalRequests, Proposals: proposals,
		Employees: employees, Assignments: assignments, TrustProfiles: trustProfiles,
		Profiles: profiles, Requirements: requirements, Capabilities: capabilities, ActorCapabilities: actorCapabilities,
		ExecutionSessions: sessions, StepExecutions: steps, WALRecords: walRecords,
		DomainEvents: domainEvents, ExecutionEvents: executionEvents,
	}, nil
}
