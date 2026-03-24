package integration

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"kalita/internal/actionplan"
	"kalita/internal/caseruntime"
	"kalita/internal/command"
	"kalita/internal/employee"
	"kalita/internal/eventcore"
	"kalita/internal/executioncontrol"
	"kalita/internal/executionruntime"
	"kalita/internal/policy"
	"kalita/internal/profile"
	"kalita/internal/trust"
	"kalita/internal/workplan"
)

type ExternalIncident struct {
	ExternalID    string         `json:"external_id"`
	Source        string         `json:"source"`
	RouteID       string         `json:"route_id"`
	ContainerSite string         `json:"container_site"`
	Timestamp     time.Time      `json:"timestamp"`
	Payload       map[string]any `json:"payload"`
}

type ProcessedIncidentStore interface {
	Seen(ctx context.Context, externalID string) (caseID string, processed bool, err error)
	Record(ctx context.Context, externalID string, caseID string) error
}

type IncidentService interface {
	IngestIncident(ctx context.Context, incident ExternalIncident) (IngestResult, error)
}

type ExternalEvent struct {
	ExternalID     string
	Source         string
	EventType      string
	OccurredAt     time.Time
	CorrelationID  string
	TargetRef      string
	EventPayload   map[string]any
	CommandPayload map[string]any
	PlanInput      map[string]any
}

type Service struct {
	events            eventcore.EventLog
	commandBus        command.CommandBus
	caseService       commandCaseResolver
	workService       workItemIntakeService
	coordinator       coordinator
	policyService     policyService
	constraints       constraintsService
	actionPlans       actionPlanService
	employeeDirectory employee.Directory
	employeeService   employeeService
	trustRepo         trust.Repository
	profileRepo       profile.Repository
	ids               eventcore.IDGenerator
	clock             eventcore.Clock
	processed         ProcessedIncidentStore
}

type IngestResult struct {
	Event            eventcore.Event
	Command          eventcore.Command
	Case             caseruntime.Case
	WorkItem         workplan.WorkItem
	Coordination     workplan.CoordinationDecision
	PolicyDecision   policy.PolicyDecision
	ApprovalRequest  *policy.ApprovalRequest
	Constraints      executioncontrol.ExecutionConstraints
	ExecutionSession *executionruntime.ExecutionSession
	Duplicate        bool
}

type commandCaseResolver interface {
	ResolveCommand(context.Context, eventcore.Command) (caseruntime.ResolutionResult, error)
}
type workItemIntakeService interface {
	IntakeCommand(context.Context, caseruntime.ResolutionResult) (workplan.IntakeResult, error)
	AttachActionPlan(context.Context, string, actionplan.ActionPlan) (workplan.WorkItem, error)
}
type coordinator interface {
	Decide(context.Context, workplan.WorkItem, workplan.CoordinationContext) (workplan.CoordinationDecision, error)
}
type policyService interface {
	EvaluateAndRecord(context.Context, workplan.CoordinationDecision) (policy.PolicyDecision, *policy.ApprovalRequest, error)
}
type constraintsService interface {
	CreateAndRecord(context.Context, workplan.CoordinationDecision, policy.PolicyDecision) (executioncontrol.ExecutionConstraints, error)
}
type actionPlanService interface {
	CreatePlan(context.Context, string, string, map[string]any) (actionplan.ActionPlan, error)
}
type employeeService interface {
	AssignAndStartExecution(context.Context, workplan.WorkItem, actionplan.ActionPlan, executioncontrol.ExecutionConstraints, employee.RunMetadata) (employee.Assignment, executionruntime.ExecutionSession, error)
}

func NewService(events eventcore.EventLog, commandBus command.CommandBus, caseService commandCaseResolver, workService workItemIntakeService, coordinator coordinator, policyService policyService, constraints constraintsService, actionPlans actionPlanService, employeeDirectory employee.Directory, employeeService employeeService, trustRepo trust.Repository, profileRepo profile.Repository, processed ProcessedIncidentStore, clock eventcore.Clock, ids eventcore.IDGenerator) *Service {
	if clock == nil {
		clock = eventcore.RealClock{}
	}
	if ids == nil {
		ids = eventcore.NewULIDGenerator()
	}
	if processed == nil {
		processed = NewInMemoryProcessedIncidentStore()
	}
	return &Service{events: events, commandBus: commandBus, caseService: caseService, workService: workService, coordinator: coordinator, policyService: policyService, constraints: constraints, actionPlans: actionPlans, employeeDirectory: employeeDirectory, employeeService: employeeService, trustRepo: trustRepo, profileRepo: profileRepo, processed: processed, clock: clock, ids: ids}
}

func MapIncidentToEvent(incident ExternalIncident) eventcore.Event {
	occurredAt := incident.Timestamp.UTC()
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	payload := cloneMap(incident.Payload)
	payload["external_id"] = incident.ExternalID
	payload["source"] = incident.Source
	payload["route_id"] = incident.RouteID
	payload["container_site"] = incident.ContainerSite
	payload["timestamp"] = occurredAt
	return eventcore.Event{Type: "container_incident_detected", OccurredAt: occurredAt, Source: incident.Source, CorrelationID: incidentCorrelationID(incident.ExternalID), Payload: payload}
}

func (s *Service) IngestIncident(ctx context.Context, incident ExternalIncident) (IngestResult, error) {
	if err := validateIncident(incident); err != nil {
		return IngestResult{}, err
	}
	return s.IngestExternalEvent(ctx, externalEventFromIncident(incident))
}

func (s *Service) IngestExternalEvent(ctx context.Context, event ExternalEvent) (IngestResult, error) {
	if err := validateExternalEvent(event); err != nil {
		return IngestResult{}, err
	}
	if existingCaseID, processed, err := s.processed.Seen(ctx, event.ExternalID); err != nil {
		return IngestResult{}, err
	} else if processed {
		return IngestResult{Case: caseruntime.Case{ID: existingCaseID}, Duplicate: true}, nil
	}
	if existingCaseID, processed, err := s.alreadyIngested(ctx, event); err != nil {
		return IngestResult{}, err
	} else if processed {
		_ = s.processed.Record(ctx, event.ExternalID, existingCaseID)
		return IngestResult{Case: caseruntime.Case{ID: existingCaseID}, Duplicate: true}, nil
	}

	evt := eventcore.Event{
		ID:            s.ids.NewID(),
		Type:          event.EventType,
		OccurredAt:    event.OccurredAt.UTC(),
		Source:        strings.TrimSpace(event.Source),
		CorrelationID: strings.TrimSpace(event.CorrelationID),
		Payload:       cloneMap(event.EventPayload),
	}
	evt.ExecutionID = s.ids.NewID()
	evt.CausationID = evt.ID
	if s.events != nil {
		if err := s.events.AppendEvent(ctx, evt); err != nil {
			return IngestResult{}, err
		}
		if err := s.events.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{ID: s.ids.NewID(), ExecutionID: evt.ExecutionID, CaseID: "", Step: "incident_detected", Status: "recorded", OccurredAt: evt.OccurredAt, CorrelationID: evt.CorrelationID, CausationID: evt.ID, Payload: cloneMap(evt.Payload)}); err != nil {
			return IngestResult{}, err
		}
	}
	cmd := eventcore.Command{
		Type:           evt.Type,
		RequestedAt:    evt.OccurredAt,
		CorrelationID:  evt.CorrelationID,
		CausationID:    evt.ID,
		ExecutionID:    evt.ExecutionID,
		TargetRef:      strings.TrimSpace(event.TargetRef),
		IdempotencyKey: strings.TrimSpace(event.ExternalID),
		Actor:          eventcore.ActorContext{ActorType: eventcore.ActorService, DisplayName: strings.TrimSpace(event.Source)},
		Payload:        cloneMap(event.CommandPayload),
	}
	admitted, err := s.commandBus.Submit(ctx, cmd)
	if err != nil {
		return IngestResult{}, err
	}
	resolved, err := s.caseService.ResolveCommand(ctx, admitted)
	if err != nil {
		return IngestResult{}, err
	}
	if resolved.Existed && strings.TrimSpace(resolved.Case.CorrelationID) == strings.TrimSpace(admitted.CorrelationID) {
		if err := s.processed.Record(ctx, event.ExternalID, resolved.Case.ID); err != nil {
			return IngestResult{}, err
		}
		return IngestResult{Event: evt, Command: admitted, Case: resolved.Case, Duplicate: true}, nil
	}
	if err := s.processed.Record(ctx, event.ExternalID, resolved.Case.ID); err != nil {
		return IngestResult{}, err
	}
	intake, err := s.workService.IntakeCommand(ctx, resolved)
	if err != nil {
		return IngestResult{}, err
	}
	planCtx := actionplan.ContextWithExecution(ctx, actionplan.ExecutionContext{ExecutionID: intake.Command.ExecutionID, CorrelationID: intake.Command.CorrelationID, CausationID: intake.Command.ID})
	plan, err := s.actionPlans.CreatePlan(planCtx, intake.WorkItem.ID, intake.Case.ID, cloneMap(event.PlanInput))
	if err != nil {
		return IngestResult{}, err
	}
	updatedWorkItem, err := s.workService.AttachActionPlan(ctx, intake.WorkItem.ID, plan)
	if err != nil {
		return IngestResult{}, err
	}
	coordinationCtx, err := s.buildCoordinationContext(ctx, updatedWorkItem, plan)
	if err != nil {
		return IngestResult{}, err
	}
	planningCtx := workplan.ContextWithPlanningExecution(ctx, workplan.PlanningExecutionContext{ExecutionID: intake.Command.ExecutionID, CorrelationID: intake.Command.CorrelationID, CausationID: intake.Command.ID})
	decision, err := s.coordinator.Decide(planningCtx, updatedWorkItem, coordinationCtx)
	if err != nil {
		return IngestResult{}, err
	}
	policyCtx := policy.ContextWithExecution(ctx, policy.ExecutionContext{ExecutionID: intake.Command.ExecutionID, CorrelationID: intake.Command.CorrelationID, CausationID: intake.Command.ID})
	policyDecision, approvalRequest, err := s.policyService.EvaluateAndRecord(policyCtx, decision)
	if err != nil {
		return IngestResult{}, err
	}
	result := IngestResult{Event: evt, Command: admitted, Case: intake.Case, WorkItem: updatedWorkItem, Coordination: decision, PolicyDecision: policyDecision, ApprovalRequest: approvalRequest}
	if policyDecision.Outcome != policy.PolicyAllow {
		return result, nil
	}
	constraintsCtx := executioncontrol.ContextWithExecution(ctx, executioncontrol.ExecutionContext{ExecutionID: intake.Command.ExecutionID, CorrelationID: intake.Command.CorrelationID, CausationID: intake.Command.ID})
	constraints, err := s.constraints.CreateAndRecord(constraintsCtx, decision, policyDecision)
	if err != nil {
		return IngestResult{}, err
	}
	result.Constraints = constraints
	if s.employeeService != nil {
		runtimeCtx := executionruntime.ContextWithExecution(ctx, executionruntime.ExecutionContext{ExecutionID: intake.Command.ExecutionID, CorrelationID: intake.Command.CorrelationID, CausationID: intake.Command.ID})
		_, session, err := s.employeeService.AssignAndStartExecution(runtimeCtx, updatedWorkItem, plan, constraints, employee.RunMetadata{CaseID: intake.Case.ID, QueueID: updatedWorkItem.QueueID, CoordinationDecisionID: decision.ID, PolicyDecisionID: policyDecision.ID})
		if err != nil {
			return IngestResult{}, err
		}
		result.ExecutionSession = &session
	}
	return result, nil
}

func externalEventFromIncident(incident ExternalIncident) ExternalEvent {
	evt := MapIncidentToEvent(incident)
	return ExternalEvent{
		ExternalID:     incident.ExternalID,
		Source:         incident.Source,
		EventType:      evt.Type,
		OccurredAt:     evt.OccurredAt,
		CorrelationID:  evt.CorrelationID,
		TargetRef:      incidentTargetRef(incident),
		EventPayload:   evt.Payload,
		CommandPayload: map[string]any{"external_id": incident.ExternalID, "source": incident.Source, "route_id": incident.RouteID, "container_site": incident.ContainerSite, "payload": cloneMap(incident.Payload)},
		PlanInput:      incidentPlanInput(incident),
	}
}

func (s *Service) alreadyIngested(ctx context.Context, event ExternalEvent) (string, bool, error) {
	if s.events == nil {
		return "", false, nil
	}
	events, executionEvents, err := s.events.ListByCorrelation(ctx, strings.TrimSpace(event.CorrelationID))
	if err != nil {
		return "", false, err
	}
	if len(events) == 0 && len(executionEvents) == 0 {
		return "", false, nil
	}
	for _, executionEvent := range executionEvents {
		if executionEvent.CaseID != "" {
			return executionEvent.CaseID, true, nil
		}
		if caseID, ok := stringFromMap(executionEvent.Payload, "case_id"); ok {
			return caseID, true, nil
		}
	}
	return "", true, nil
}

func stringFromMap(payload map[string]any, key string) (string, bool) {
	value, ok := payload[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	if !ok {
		return "", false
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", false
	}
	return text, true
}

func validateExternalEvent(event ExternalEvent) error {
	switch {
	case strings.TrimSpace(event.ExternalID) == "":
		return fmt.Errorf("external_id is required")
	case strings.TrimSpace(event.Source) == "":
		return fmt.Errorf("source is required")
	case strings.TrimSpace(event.EventType) == "":
		return fmt.Errorf("event_type is required")
	case event.OccurredAt.IsZero():
		return fmt.Errorf("timestamp is required")
	case strings.TrimSpace(event.CorrelationID) == "":
		return fmt.Errorf("correlation_id is required")
	case strings.TrimSpace(event.TargetRef) == "":
		return fmt.Errorf("target_ref is required")
	default:
		return nil
	}
}

func (s *Service) buildCoordinationContext(ctx context.Context, wi workplan.WorkItem, plan actionplan.ActionPlan) (workplan.CoordinationContext, error) {
	actors, err := s.employeeDirectory.ListEmployees(ctx)
	if err != nil {
		return workplan.CoordinationContext{}, err
	}
	outActors := make([]workplan.CoordinationActor, 0, len(actors))
	profiles := make(map[string]workplan.CoordinationActorProfile, len(actors))
	for _, actor := range actors {
		actionTypes := make([]string, 0, len(actor.AllowedActionTypes))
		for _, actionType := range actor.AllowedActionTypes {
			actionTypes = append(actionTypes, string(actionType))
		}
		outActors = append(outActors, workplan.CoordinationActor{ID: actor.ID, Enabled: actor.Enabled, QueueMemberships: append([]string(nil), actor.QueueMemberships...), AllowedActionTypes: actionTypes})
		profileView := workplan.CoordinationActorProfile{ActorID: actor.ID}
		if s.profileRepo != nil {
			if prof, ok, err := s.profileRepo.GetProfileByActor(ctx, actor.ID); err != nil {
				return workplan.CoordinationContext{}, err
			} else if ok {
				profileView.MaxComplexity = prof.MaxComplexity
			}
		}
		if s.trustRepo != nil {
			if trustProfile, ok, err := s.trustRepo.GetByActor(ctx, actor.ID); err != nil {
				return workplan.CoordinationContext{}, err
			} else if ok {
				profileView.TrustLevel = string(trustProfile.TrustLevel)
				profileView.TrustAvailable = true
			}
		}
		profiles[actor.ID] = profileView
	}
	actionTypes := make([]string, 0, len(plan.Actions))
	for _, action := range plan.Actions {
		actionTypes = append(actionTypes, string(action.Type))
	}
	return workplan.CoordinationContext{ActionTypes: actionTypes, Complexity: len(plan.Actions), Actors: outActors, Profiles: profiles}, nil
}

func incidentPlanInput(incident ExternalIncident) map[string]any {
	return map[string]any{"reason": fmt.Sprintf("external incident %s received from %s", incident.ExternalID, incident.Source), "actions": []any{map[string]any{"type": "external_incident_followup", "params": map[string]any{"external_id": incident.ExternalID, "source": incident.Source, "route_id": incident.RouteID, "container_site": incident.ContainerSite}}}}
}

func incidentTargetRef(incident ExternalIncident) string {
	return fmt.Sprintf("integration/incident/%s/%s/%s", strings.TrimSpace(incident.Source), strings.TrimSpace(incident.RouteID), strings.TrimSpace(incident.ContainerSite))
}
func incidentCorrelationID(externalID string) string {
	return "incident:" + strings.TrimSpace(externalID)
}
func validateIncident(incident ExternalIncident) error {
	switch {
	case strings.TrimSpace(incident.ExternalID) == "":
		return fmt.Errorf("external_id is required")
	case strings.TrimSpace(incident.Source) == "":
		return fmt.Errorf("source is required")
	case strings.TrimSpace(incident.RouteID) == "":
		return fmt.Errorf("route_id is required")
	case strings.TrimSpace(incident.ContainerSite) == "":
		return fmt.Errorf("container_site is required")
	case incident.Timestamp.IsZero():
		return fmt.Errorf("timestamp is required")
	default:
		return nil
	}
}
func cloneMap(in map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

type InMemoryProcessedIncidentStore struct {
	mu    sync.Mutex
	items map[string]string
}

func NewInMemoryProcessedIncidentStore() *InMemoryProcessedIncidentStore {
	return &InMemoryProcessedIncidentStore{items: map[string]string{}}
}

func (s *InMemoryProcessedIncidentStore) Seen(_ context.Context, externalID string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	caseID, ok := s.items[externalID]
	return caseID, ok, nil
}

func (s *InMemoryProcessedIncidentStore) Record(_ context.Context, externalID string, caseID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.items[externalID]; !ok {
		s.items[externalID] = caseID
	}
	return nil
}
