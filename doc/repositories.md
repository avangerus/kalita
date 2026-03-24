# Repository Audit

## Scope

`internal/domain/` does not exist in the current repository state. This audit covers the domain-level repository interfaces that are actually declared under `internal/*` and used by the domain/application services.

Excluded from the main list:
- `eventcore.EventLog`
- `executionruntime.WAL`

They are storage interfaces, but not repository interfaces by name.

## Inventory

### `caseruntime.CaseRepository`

Source: `internal/caseruntime/types.go`

Methods:
- `Save(ctx, c)`
- `GetByID(ctx, id)`
- `List(ctx)`
- `FindByCorrelation(ctx, correlationID)`
- `FindBySubjectRef(ctx, subjectRef)`

Current in-memory implementation:
- `caseruntime.InMemoryCaseRepository` in `internal/caseruntime/repository.go`

### `workplan.QueueRepository`

Source: `internal/workplan/types.go`

Methods:
- `SaveQueue(ctx, q)`
- `GetQueue(ctx, id)`
- `ListQueues(ctx)`
- `SaveWorkItem(ctx, wi)`
- `GetWorkItem(ctx, id)`
- `ListWorkItemsByCase(ctx, caseID)`
- `ListWorkItemsByQueue(ctx, queueID)`
- `ListWorkItems(ctx)`

Current in-memory implementation:
- `workplan.InMemoryQueueRepository` in `internal/workplan/repository.go`

### `workplan.PlanRepository`

Source: `internal/workplan/daily_plan.go`

Methods:
- `SavePlan(ctx, p)`
- `GetPlan(ctx, id)`
- `FindPlanByQueueAndDate(ctx, queueID, planDate)`
- `ListPlansByQueue(ctx, queueID)`

Current in-memory implementation:
- `workplan.InMemoryPlanRepository` in `internal/workplan/daily_plan.go`

### `workplan.CoordinationRepository`

Source: `internal/workplan/coordination.go`

Methods:
- `SaveDecision(ctx, d)`
- `GetDecision(ctx, id)`
- `ListByWorkItem(ctx, workItemID)`
- `ListByCase(ctx, caseID)`
- `ListByQueue(ctx, queueID)`

Current in-memory implementation:
- `workplan.InMemoryCoordinationRepository` in `internal/workplan/coordination_repository.go`

### `policy.PolicyRepository`

Source: `internal/policy/types.go`

Methods:
- `SaveDecision(ctx, d)`
- `GetDecision(ctx, id)`
- `ListByCoordinationDecision(ctx, coordinationDecisionID)`
- `SaveApprovalRequest(ctx, r)`
- `GetApprovalRequest(ctx, id)`
- `ListApprovalRequests(ctx)`
- `ListApprovalRequestsByCoordinationDecision(ctx, coordinationDecisionID)`

Current in-memory implementation:
- `policy.InMemoryRepository` in `internal/policy/repository.go`

### `capability.CapabilityRepository`

Source: `internal/capability/types.go`

Methods:
- `SaveCapability(ctx, c)`
- `GetCapability(ctx, id)`
- `ListCapabilities(ctx)`

Current in-memory implementation:
- `capability.InMemoryCapabilityRepository` in `internal/capability/repository.go`

### `capability.ActorCapabilityRepository`

Source: `internal/capability/types.go`

Methods:
- `AssignCapability(ctx, ac)`
- `ListByActor(ctx, actorID)`

Current in-memory implementation:
- `capability.InMemoryCapabilityRepository` in `internal/capability/repository.go`

### `profile.Repository`

Source: `internal/profile/types.go`

Methods:
- `SaveProfile(ctx, p)`
- `GetProfile(ctx, id)`
- `GetProfileByActor(ctx, actorID)`
- `ListProfiles(ctx)`

Current in-memory implementation:
- `profile.InMemoryRepository` in `internal/profile/repository.go`

### `profile.RequirementRepository`

Source: `internal/profile/types.go`

Methods:
- `SaveRequirement(ctx, r)`
- `ListRequirements(ctx)`

Current in-memory implementation:
- `profile.InMemoryRepository` in `internal/profile/repository.go`

### `proposal.Repository`

Source: `internal/proposal/types.go`

Methods:
- `Save(ctx, p)`
- `Get(ctx, id)`
- `ListByWorkItem(ctx, workItemID)`
- `ListByActor(ctx, actorID)`

Current in-memory implementation:
- `proposal.InMemoryRepository` in `internal/proposal/repository.go`

### `executionruntime.ExecutionRepository`

Source: `internal/executionruntime/types.go`

Methods:
- `SaveSession(ctx, s)`
- `GetSession(ctx, id)`
- `ListSessionsByWorkItem(ctx, workItemID)`
- `SaveStep(ctx, s)`
- `GetStep(ctx, id)`
- `ListStepsBySession(ctx, sessionID)`

Current in-memory implementation:
- `executionruntime.InMemoryExecutionRepository` in `internal/executionruntime/repository.go`

### `executioncontrol.ConstraintsRepository`

Source: `internal/executioncontrol/types.go`

Methods:
- `Save(ctx, c)`
- `Get(ctx, id)`
- `ListByPolicyDecision(ctx, policyDecisionID)`
- `ListByCoordinationDecision(ctx, coordinationDecisionID)`
- `ListByCase(ctx, caseID)`

Current in-memory implementation:
- `executioncontrol.InMemoryConstraintsRepository` in `internal/executioncontrol/repository.go`

### `employee.Directory`

Source: `internal/employee/types.go`

Methods:
- `SaveEmployee(ctx, e)`
- `GetEmployee(ctx, id)`
- `ListEmployees(ctx)`
- `ListEmployeesByQueue(ctx, queueID)`

Current in-memory implementation:
- `employee.InMemoryDirectory` in `internal/employee/repository.go`

### `employee.AssignmentRepository`

Source: `internal/employee/types.go`

Methods:
- `SaveAssignment(ctx, a)`
- `GetAssignment(ctx, id)`
- `ListAssignmentsByWorkItem(ctx, workItemID)`
- `ListAssignmentsByEmployee(ctx, employeeID)`

Current in-memory implementation:
- `employee.InMemoryAssignmentRepository` in `internal/employee/repository.go`

### `trust.Repository`

Source: `internal/trust/types.go`

Methods:
- `Save(ctx, p)`
- `GetByActor(ctx, actorID)`
- `List(ctx)`

Current in-memory implementation:
- `trust.InMemoryRepository` in `internal/trust/repository.go`

## In-Memory Mapping Summary

- `caseruntime.InMemoryCaseRepository` implements `caseruntime.CaseRepository`
- `workplan.InMemoryQueueRepository` implements `workplan.QueueRepository`
- `workplan.InMemoryPlanRepository` implements `workplan.PlanRepository`
- `workplan.InMemoryCoordinationRepository` implements `workplan.CoordinationRepository`
- `policy.InMemoryRepository` implements `policy.PolicyRepository`
- `capability.InMemoryCapabilityRepository` implements both `capability.CapabilityRepository` and `capability.ActorCapabilityRepository`
- `profile.InMemoryRepository` implements both `profile.Repository` and `profile.RequirementRepository`
- `proposal.InMemoryRepository` implements `proposal.Repository`
- `executionruntime.InMemoryExecutionRepository` implements `executionruntime.ExecutionRepository`
- `executioncontrol.InMemoryConstraintsRepository` implements `executioncontrol.ConstraintsRepository`
- `employee.InMemoryDirectory` implements `employee.Directory`
- `employee.InMemoryAssignmentRepository` implements `employee.AssignmentRepository`
- `trust.InMemoryRepository` implements `trust.Repository`

## Interface Segregation Issues

### 1. `workplan.QueueRepository` mixes two aggregates: queues and work items

Interface definition:
- `internal/workplan/types.go:44`

Current consumers depend on different slices of the same interface:
- `workplan.Router` only needs queue reads: `ListQueues`, `GetQueue`
  - `internal/workplan/router.go:10`
- `workplan.Service` only needs work item persistence: `SaveWorkItem`, `GetWorkItem`
  - `internal/workplan/service.go:14`
- `workplan.InMemoryWorkQueueSnapshot` only needs `ListWorkItems`
  - `internal/workplan/snapshot.go:18`
- `workplan.queuePressureScorer` needs cross-cutting read access: `ListQueues`, `ListWorkItems`, `GetQueue`
  - `internal/workplan/queue_pressure.go:20`
- `workplan.DefaultCoordinator` uses only queue/work-item read methods
  - `internal/workplan/coordination_service.go:20`
- `controlplane.service` uses only read methods over work items
  - `internal/controlplane/service.go:37`

Why this is an ISP violation:
- clients are forced to depend on queue write methods, queue read methods, and work item methods together even when they only need one subset

Natural split:
- `QueueCatalog`
- `WorkItemRepository`

### 2. `policy.PolicyRepository` combines policy decisions and approval requests

Interface definition:
- `internal/policy/types.go:51`

Current consumers:
- `policy.PolicyService` writes decisions and approval requests
  - `internal/policy/service.go:29`
- `controlplane.service` reads decisions and approval requests, and also updates only approval requests during operator actions
  - `internal/controlplane/service.go:41`

Why this is an ISP violation:
- policy-decision storage and approval-request storage are separate concerns
- read-only reporting and approval mutation are coupled to decision persistence in one interface

Natural split:
- `PolicyDecisionRepository`
- `ApprovalRequestRepository`
- optionally separate command/query views

### 3. `executionruntime.ExecutionRepository` combines session writes, step writes, and read models

Interface definition:
- `internal/executionruntime/types.go:78`

Current consumers:
- `executionruntime.DefaultRunner` only uses write/update behavior for sessions and steps
  - `internal/executionruntime/runner.go:30`
- `controlplane.service` only reads `ListSessionsByWorkItem` and `ListStepsBySession`
  - `internal/controlplane/service.go:48`

Why this is an ISP violation:
- the runner depends on query methods it never calls
- the control plane depends on write methods it never calls

Natural split:
- `ExecutionSessionWriter`
- `ExecutionStepWriter`
- `ExecutionSessionReader`
- `ExecutionStepReader`

### 4. `profile.Repository` is wider than every current consumer needs

Interface definition:
- `internal/profile/types.go:38`

Current consumers:
- `profile.profileService` only uses `GetProfileByActor`
  - `internal/profile/service.go:5`
- `profile.deterministicMatcher` only uses `GetProfileByActor`
  - `internal/profile/matcher.go:15`
- `controlplane.service` only uses `GetProfileByActor`
  - `internal/controlplane/service.go:46`

Why this is an ISP violation:
- all runtime consumers depend on create/get-by-id/list methods they do not use

Natural split:
- `ActorProfileReader`
- `ProfileRepository` for full CRUD if still needed

### 5. `trust.Repository` is wider than current service and control-plane usage

Interface definition:
- `internal/trust/types.go:52`

Current consumers:
- `trustService` only uses `GetByActor` and `Save`
  - `internal/trust/service.go:9`
- `controlplane.service` only uses `GetByActor`
  - `internal/controlplane/service.go:45`

Why this is an ISP violation:
- `List` is unrelated to the runtime trust update path
- read-only consumers should not depend on write capability

Natural split:
- `TrustProfileReader`
- `TrustProfileWriter`

### 6. `capability.CapabilityRepository` is too broad for current business services

Interface definition:
- `internal/capability/types.go:32`

Current consumers:
- `capabilityService` only uses `GetCapability`
  - `internal/capability/service.go:5`
- `capability.deterministicMatcher` only uses `GetCapability`
  - `internal/capability/matcher.go:13`

Why this is an ISP violation:
- current runtime clients do not need capability writes or full listing

Natural split:
- `CapabilityLookup`
- `CapabilityCatalogWriter`
- `CapabilityCatalogReader`

### 7. `controlplane.service` is concretely coupled to `capability.InMemoryCapabilityRepository`

Current dependency:
- `internal/controlplane/service.go:47`
- constructor parameter is `*capability.InMemoryCapabilityRepository`
  - `internal/controlplane/service.go:63`

Why this is an ISP violation:
- the service needs the union of `ListByActor` and `ListCapabilities`, but expresses that need as a concrete in-memory type
- this prevents substituting another implementation without matching the concrete struct

Natural split:
- define a small read interface for actor capability summaries, for example a composition of:
  - `ListByActor`
  - `ListCapabilities`

### 8. `profile.RequirementRepository` is wider than its matcher client needs

Interface definition:
- `internal/profile/types.go:45`

Current consumer:
- `profile.deterministicMatcher` only uses `ListRequirements`
  - `internal/profile/matcher.go:169`

Why this is an ISP violation:
- the matcher is forced to depend on write capability (`SaveRequirement`)

Natural split:
- `RequirementReader`
- `RequirementWriter`

### 9. `employee.Directory` mixes write, broad read, and queue-scoped read concerns

Interface definition:
- `internal/employee/types.go:37`

Current consumers:
- `employee.deterministicSelector` only uses `ListEmployeesByQueue`
  - `internal/employee/selector.go:15`
- `controlplane.service` uses `GetEmployee` and `ListEmployees`
  - `internal/controlplane/service.go:44`

Why this is an ISP violation:
- queue-scoped selection and actor overview/reporting are separate read concerns
- both are coupled to `SaveEmployee`

Natural split:
- `EmployeeQueueDirectory`
- `EmployeeReader`
- `EmployeeWriter`

### 10. `employee.AssignmentRepository` is wider than the runtime assignment service needs

Interface definition:
- `internal/employee/types.go:44`

Current consumer:
- `employeeService` only uses `SaveAssignment`
  - `internal/employee/service.go:15`

Why this is an ISP violation:
- assignment write flow does not need lookup methods

Natural split:
- `AssignmentWriter`
- `AssignmentReader`

### 11. `proposal.Repository` combines mutation flow and reporting queries

Interface definition:
- `internal/proposal/types.go:47`

Current consumers:
- `proposalService` uses `Save` and `Get`
  - `internal/proposal/service.go:13`
- `controlplane.service` uses `ListByWorkItem`
  - `internal/controlplane/service.go:43`

Why this is an ISP violation:
- command-side proposal lifecycle and read-side overview queries are different client needs

Natural split:
- `ProposalWriter`
- `ProposalReader`
- optionally a dedicated `ProposalByWorkItemReader`

### 12. `executioncontrol.ConstraintsRepository` is broader than the write path needs

Interface definition:
- `internal/executioncontrol/types.go:55`

Current consumer:
- `executioncontrol.Service` only uses `Save`
  - `internal/executioncontrol/service.go:29`

Why this is an ISP violation:
- the planning/write service depends on read/list methods it does not use

Natural split:
- `ConstraintsWriter`
- `ConstraintsReader`

### 13. `caseruntime.CaseRepository` still combines resolution, lookup, and listing concerns

Interface definition:
- `internal/caseruntime/types.go:31`

Current consumers:
- `caseruntime.Resolver` uses `FindByCorrelation`, `FindBySubjectRef`, `Save`
  - `internal/caseruntime/resolver.go:11`
- `controlplane.service` uses `GetByID` and separately probes `List` through a narrower local `CaseLister`
  - `internal/controlplane/service.go:35`
  - `internal/controlplane/service.go:69`

Why this is an ISP violation:
- correlation-based resolution and operator read models are different concerns
- the local `CaseLister` adapter inside control plane is already a sign that `CaseRepository` is too broad for some consumers

Natural split:
- `CaseResolverStore`
- `CaseReader`
- `CaseLister`

### 14. `workplan.CoordinationRepository` combines decision writes and operator/reporting reads

Interface definition:
- `internal/workplan/coordination.go:56`

Current consumers:
- `workplan.DefaultCoordinator` writes decisions
  - `internal/workplan/coordination_service.go:20`
- `controlplane.service` reads `GetDecision`, `ListByWorkItem`, `ListByCase`, `ListByQueue`
  - `internal/controlplane/service.go:40`

Why this is an ISP violation:
- the coordinator is a command-side writer
- the control plane is a read-side consumer

Natural split:
- `CoordinationDecisionWriter`
- `CoordinationDecisionReader`

## Summary

The most significant ISP problems are concentrated in:
- `workplan.QueueRepository`
- `policy.PolicyRepository`
- `executionruntime.ExecutionRepository`
- `profile.Repository`
- `trust.Repository`
- `employee.Directory`

The strongest concrete-coupling issue is:
- `controlplane.service -> *capability.InMemoryCapabilityRepository`
