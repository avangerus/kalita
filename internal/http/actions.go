package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"kalita/internal/actionplan"
	"kalita/internal/caseruntime"
	"kalita/internal/command"
	"kalita/internal/employee"
	"kalita/internal/eventcore"
	"kalita/internal/executioncontrol"
	"kalita/internal/executionruntime"
	"kalita/internal/policy"
	"kalita/internal/proposal"
	"kalita/internal/runtime"
	"kalita/internal/validation"
	"kalita/internal/workplan"

	"github.com/gin-gonic/gin"
)

type actionRequest struct {
	Action        string `json:"action,omitempty"`
	RecordVersion int64  `json:"record_version"`
}

func ActionHandler(storage *runtime.Storage) gin.HandlerFunc {
	return ActionHandlerWithServices(storage, nil, nil, nil, nil, nil, nil, nil, nil, nil)
}

func ActionHandlerWithCommandBus(storage *runtime.Storage, commandBus command.CommandBus) gin.HandlerFunc {
	return ActionHandlerWithServices(storage, commandBus, nil, nil, nil, nil, nil, nil, nil, nil)
}

type commandCaseResolver interface {
	ResolveCommand(ctx context.Context, cmd eventcore.Command) (caseruntime.ResolutionResult, error)
}

type workItemIntakeService interface {
	IntakeCommand(ctx context.Context, resolved caseruntime.ResolutionResult) (workplan.IntakeResult, error)
	AttachActionPlan(ctx context.Context, workItemID string, plan actionplan.ActionPlan) (workplan.WorkItem, error)
}

type policyService interface {
	EvaluateAndRecord(ctx context.Context, d workplan.CoordinationDecision) (policy.PolicyDecision, *policy.ApprovalRequest, error)
}

type constraintsService interface {
	CreateAndRecord(ctx context.Context, coordination workplan.CoordinationDecision, policyDecision policy.PolicyDecision) (executioncontrol.ExecutionConstraints, error)
}

type actionPlanService interface {
	CreatePlan(ctx context.Context, workItemID string, caseID string, input map[string]any) (actionplan.ActionPlan, error)
}

type employeeService interface {
	AssignAndStartExecution(ctx context.Context, wi workplan.WorkItem, plan actionplan.ActionPlan, constraints executioncontrol.ExecutionConstraints, metadata employee.RunMetadata) (employee.Assignment, executionruntime.ExecutionSession, error)
}

type proposalService interface {
	CreateProposal(ctx context.Context, actor employee.DigitalEmployee, wi workplan.WorkItem, assignment employee.Assignment, payload map[string]any, justification string) (proposal.Proposal, error)
	ValidateProposal(ctx context.Context, proposalID string, actor employee.DigitalEmployee) (proposal.Proposal, error)
	CompileProposal(ctx context.Context, proposalID string) (proposal.Proposal, actionplan.ActionPlan, error)
}

func ActionHandlerWithServices(storage *runtime.Storage, commandBus command.CommandBus, caseService commandCaseResolver, workService workItemIntakeService, policyService policyService, constraintsService constraintsService, actionPlanService actionPlanService, proposalService proposalService, employeeDirectory employee.Directory, employeeServices ...employeeService) gin.HandlerFunc {
	var employeeService employeeService
	if len(employeeServices) > 0 {
		employeeService = employeeServices[0]
	}
	return func(c *gin.Context) {
		fqn, action, req, ok := parseActionRequest(c, storage)
		if !ok {
			return
		}

		if commandBus != nil {
			cmd := buildWorkflowActionCommand(c, fqn, action, req)
			admitted, err := commandBus.Submit(c.Request.Context(), cmd)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"errors": []validation.FieldError{{
					Code:    validation.ErrTypeMismatch,
					Field:   "action",
					Message: err.Error(),
				}}})
				return
			}
			if caseService != nil {
				resolved, err := caseService.ResolveCommand(c.Request.Context(), admitted)
				if err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"errors": []validation.FieldError{{
						Code:    validation.ErrTypeMismatch,
						Field:   "action",
						Message: err.Error(),
					}}})
					return
				}
				if workService != nil {
					intakeResult, err := workService.IntakeCommand(c.Request.Context(), resolved)
					if err != nil {
						c.JSON(http.StatusBadRequest, gin.H{"errors": []validation.FieldError{{
							Code:    validation.ErrTypeMismatch,
							Field:   "action",
							Message: err.Error(),
						}}})
						return
					}
					if policyService != nil {
						policyCtx := policy.ContextWithExecution(c.Request.Context(), policy.ExecutionContext{
							ExecutionID:   intakeResult.Command.ExecutionID,
							CorrelationID: intakeResult.Command.CorrelationID,
							CausationID:   intakeResult.Command.ID,
						})
						policyDecision, approvalRequest, err := policyService.EvaluateAndRecord(policyCtx, intakeResult.CoordinationDecision)
						if err != nil {
							c.JSON(http.StatusBadRequest, gin.H{"errors": []validation.FieldError{{
								Code:    validation.ErrTypeMismatch,
								Field:   "action",
								Message: err.Error(),
							}}})
							return
						}
						var constraints executioncontrol.ExecutionConstraints
						if constraintsService != nil && policyDecision.Outcome != policy.PolicyDeny {
							constraintsCtx := executioncontrol.ContextWithExecution(c.Request.Context(), executioncontrol.ExecutionContext{
								ExecutionID:   intakeResult.Command.ExecutionID,
								CorrelationID: intakeResult.Command.CorrelationID,
								CausationID:   intakeResult.Command.ID,
							})
							var err error
							constraints, err = constraintsService.CreateAndRecord(constraintsCtx, intakeResult.CoordinationDecision, policyDecision)
							if err != nil {
								c.JSON(http.StatusBadRequest, gin.H{"errors": []validation.FieldError{{
									Code:    validation.ErrTypeMismatch,
									Field:   "action",
									Message: err.Error(),
								}}})
								return
							}
						}
						if policyDecision.Outcome != policy.PolicyAllow {
							message := policyDecision.Reason
							if approvalRequest != nil {
								message = fmt.Sprintf("%s (approval_request_id=%s)", message, approvalRequest.ID)
							}
							c.JSON(http.StatusBadRequest, gin.H{"errors": []validation.FieldError{{
								Code:    validation.ErrTypeMismatch,
								Field:   "action",
								Message: message,
							}}})
							return
						}
						if actionPlanService != nil {
							planCtx := actionplan.ContextWithExecution(c.Request.Context(), actionplan.ExecutionContext{
								ExecutionID:   intakeResult.Command.ExecutionID,
								CorrelationID: intakeResult.Command.CorrelationID,
								CausationID:   intakeResult.Command.ID,
							})
							planInput := actionPlanInput(fqn, c.Param("id"), action, req)
							plan, err := createGovernedPlan(planCtx, actionPlanService, proposalService, employeeDirectory, intakeResult.WorkItem, planInput)
							if err != nil {
								c.JSON(http.StatusBadRequest, gin.H{"errors": []validation.FieldError{{
									Code:    validation.ErrTypeMismatch,
									Field:   "action",
									Message: err.Error(),
								}}})
								return
							}
							if _, err := workService.AttachActionPlan(c.Request.Context(), intakeResult.WorkItem.ID, plan); err != nil {
								c.JSON(http.StatusBadRequest, gin.H{"errors": []validation.FieldError{{
									Code:    validation.ErrTypeMismatch,
									Field:   "action",
									Message: err.Error(),
								}}})
								return
							}
							if employeeService != nil {
								runtimeCtx := executionruntime.ContextWithExecution(c.Request.Context(), executionruntime.ExecutionContext{ExecutionID: intakeResult.Command.ExecutionID, CorrelationID: intakeResult.Command.CorrelationID, CausationID: intakeResult.Command.ID})
								if _, _, err := employeeService.AssignAndStartExecution(runtimeCtx, intakeResult.WorkItem, plan, constraints, employee.RunMetadata{CaseID: intakeResult.Case.ID, QueueID: intakeResult.WorkItem.QueueID, CoordinationDecisionID: intakeResult.CoordinationDecision.ID, PolicyDecisionID: policyDecision.ID}); err != nil {
									c.JSON(http.StatusBadRequest, gin.H{"errors": []validation.FieldError{{Code: validation.ErrTypeMismatch, Field: "action", Message: err.Error()}}})
									return
								}
							}
						}
					}
				}
			}
		}

		result, errs := runtime.ExecuteWorkflowAction(storage, fqn, c.Param("id"), action, req.RecordVersion)
		if len(errs) > 0 {
			verrs := toValidationErrors(errs)
			c.JSON(statusForErrors(verrs), gin.H{"errors": verrs})
			return
		}

		c.Header("ETag", fmt.Sprintf(`"%d"`, result.Version))
		c.JSON(http.StatusOK, gin.H{
			"id":           result.ID,
			"entity":       result.Entity,
			"action":       result.Action,
			"status_field": result.StatusField,
			"from":         result.From,
			"to":           result.To,
			"version":      result.Version,
			"committed":    result.Committed,
			"record":       result.Record,
		})
	}
}

func parseActionRequest(c *gin.Context, storage *runtime.Storage) (string, string, actionRequest, bool) {
	mod := c.Param("module")
	ent := c.Param("entity")
	action := c.Param("action")

	fqn, ok := storage.NormalizeEntityName(mod, ent)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Entity not found"})
		return "", "", actionRequest{}, false
	}

	var req actionRequest
	dec := json.NewDecoder(bytes.NewReader(mustReadBody(c)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid JSON"})
		return "", "", actionRequest{}, false
	}
	if req.Action != "" && req.Action != action {
		c.JSON(http.StatusBadRequest, gin.H{"errors": []validation.FieldError{{
			Code:    validation.ErrTypeMismatch,
			Field:   "action",
			Message: fmt.Sprintf("body action %q does not match path action %q", req.Action, action),
		}}})
		return "", "", actionRequest{}, false
	}
	if req.RecordVersion <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"errors": []validation.FieldError{{
			Code:    validation.ErrRequired,
			Field:   "record_version",
			Message: "record_version is required for workflow actions",
		}}})
		return "", "", actionRequest{}, false
	}

	return fqn, action, req, true
}

func mustReadBody(c *gin.Context) []byte {
	if c.Request.Body == nil {
		return []byte("{}")
	}
	body, err := c.GetRawData()
	if err != nil || len(body) == 0 {
		return []byte("{}")
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(body))
	return body
}

func buildWorkflowActionCommand(c *gin.Context, fqn, action string, req actionRequest) eventcore.Command {
	requestID := c.GetHeader("X-Request-Id")
	if requestID == "" {
		requestID = c.GetHeader("X-Request-ID")
	}

	return eventcore.Command{
		Type:           "workflow.action",
		CaseID:         c.Param("id"),
		TargetRef:      fmt.Sprintf("%s/%s", fqn, c.Param("id")),
		IdempotencyKey: fmt.Sprintf("%s:%s:%d", fqn, action, req.RecordVersion),
		Actor: eventcore.ActorContext{
			ActorType: eventcore.ActorHuman,
			RequestID: requestID,
		},
		Payload: map[string]any{
			"entity":         fqn,
			"record_id":      c.Param("id"),
			"action":         action,
			"record_version": req.RecordVersion,
		},
	}
}

func actionPlanInput(fqn, recordID, action string, req actionRequest) map[string]any {
	return map[string]any{
		"reason": "legacy workflow action approved for execution",
		"actions": []any{map[string]any{
			"type": "legacy_workflow_action",
			"params": map[string]any{
				"entity":         fqn,
				"record_id":      recordID,
				"action":         action,
				"record_version": req.RecordVersion,
			},
		}},
	}
}

func createGovernedPlan(ctx context.Context, actionPlanService actionPlanService, proposalSvc proposalService, directory employee.Directory, wi workplan.WorkItem, input map[string]any) (actionplan.ActionPlan, error) {
	if proposalSvc == nil || directory == nil {
		return actionPlanService.CreatePlan(ctx, wi.ID, wi.CaseID, input)
	}
	actor, assignment, err := selectProposalActor(ctx, directory, wi, input)
	if err != nil {
		return actionplan.ActionPlan{}, err
	}
	meta := actionplan.ExecutionMetadataFromContext(ctx)
	proposalCtx := proposal.ContextWithExecution(ctx, proposal.ExecutionContext{ExecutionID: meta.ExecutionID, CorrelationID: meta.CorrelationID, CausationID: meta.CausationID})
	created, err := proposalSvc.CreateProposal(proposalCtx, actor, wi, assignment, input, legacyProposalJustification(input))
	if err != nil {
		return actionplan.ActionPlan{}, err
	}
	validated, err := proposalSvc.ValidateProposal(proposalCtx, created.ID, actor)
	if err != nil {
		return actionplan.ActionPlan{}, err
	}
	if validated.Status != proposal.ProposalValidated {
		if validated.RejectionReason == "" {
			return actionplan.ActionPlan{}, fmt.Errorf("proposal %s rejected", validated.ID)
		}
		return actionplan.ActionPlan{}, fmt.Errorf("proposal rejected: %s", validated.RejectionReason)
	}
	_, plan, err := proposalSvc.CompileProposal(proposalCtx, validated.ID)
	if err != nil {
		return actionplan.ActionPlan{}, err
	}
	return plan, nil
}

func selectProposalActor(ctx context.Context, directory employee.Directory, wi workplan.WorkItem, payload map[string]any) (employee.DigitalEmployee, employee.Assignment, error) {
	if directory == nil {
		return employee.DigitalEmployee{}, employee.Assignment{}, fmt.Errorf("employee directory is nil")
	}
	actions, err := proposalActionTypes(payload)
	if err != nil {
		return employee.DigitalEmployee{}, employee.Assignment{}, err
	}
	employees, err := directory.ListEmployeesByQueue(ctx, wi.QueueID)
	if err != nil {
		return employee.DigitalEmployee{}, employee.Assignment{}, err
	}
	for _, actor := range employees {
		if !actor.Enabled || !allowsProposalActions(actor, actions) {
			continue
		}
		return actor, employee.Assignment{ID: fmt.Sprintf("proposal-%s-%s", wi.ID, actor.ID), WorkItemID: wi.ID, CaseID: wi.CaseID, QueueID: wi.QueueID, EmployeeID: actor.ID}, nil
	}
	return employee.DigitalEmployee{}, employee.Assignment{}, fmt.Errorf("no eligible proposal actor for queue %s and work item %s", wi.QueueID, wi.ID)
}

func proposalActionTypes(payload map[string]any) ([]actionplan.ActionType, error) {
	rawActions, ok := payload["actions"]
	if !ok {
		return nil, fmt.Errorf("proposal payload actions are required")
	}
	items, err := proposalActionItems(rawActions)
	if err != nil {
		return nil, err
	}
	out := make([]actionplan.ActionType, 0, len(items))
	for i, item := range items {
		rawType, _ := item["type"].(string)
		typeName := actionplan.ActionType(rawType)
		if typeName == "" {
			return nil, fmt.Errorf("proposal action %d type is required", i)
		}
		out = append(out, typeName)
	}
	return out, nil
}

func proposalActionItems(raw any) ([]map[string]any, error) {
	switch v := raw.(type) {
	case []any:
		out := make([]map[string]any, 0, len(v))
		for i, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("actions[%d] must be an object", i)
			}
			out = append(out, m)
		}
		return out, nil
	case []map[string]any:
		return v, nil
	default:
		return nil, fmt.Errorf("actions must be an array")
	}
}

func allowsProposalActions(actor employee.DigitalEmployee, actions []actionplan.ActionType) bool {
	allowed := map[actionplan.ActionType]struct{}{}
	for _, actionType := range actor.AllowedActionTypes {
		allowed[actionType] = struct{}{}
	}
	for _, actionType := range actions {
		if _, ok := allowed[actionType]; !ok {
			return false
		}
	}
	return true
}

func legacyProposalJustification(input map[string]any) string {
	reason, _ := input["reason"].(string)
	if reason == "" {
		return "legacy workflow action approved for execution"
	}
	return reason
}
