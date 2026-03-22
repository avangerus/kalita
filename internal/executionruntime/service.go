package executionruntime

import (
	"context"
	"fmt"

	"kalita/internal/actionplan"
	"kalita/internal/executioncontrol"
)

type runtimeService struct{ runner Runner }

func NewService(runner Runner) Service { return &runtimeService{runner: runner} }
func (s *runtimeService) StartExecution(ctx context.Context, plan actionplan.ActionPlan, constraints executioncontrol.ExecutionConstraints, metadata RunMetadata) (ExecutionSession, error) {
	if s.runner == nil {
		return ExecutionSession{}, fmt.Errorf("execution runner is nil")
	}
	return s.runner.RunPlan(ctx, plan, constraints, metadata)
}

type StubExecutor struct{}

func NewStubExecutor() *StubExecutor { return &StubExecutor{} }
func (e *StubExecutor) ExecuteAction(_ context.Context, action actionplan.Action, _ executioncontrol.ExecutionConstraints) error {
	if shouldFail(action.Params, "fail") {
		return fmt.Errorf("stub execution requested failure")
	}
	return nil
}
func (e *StubExecutor) CompensateAction(_ context.Context, action actionplan.Action, _ executioncontrol.ExecutionConstraints) error {
	if !isCompensatable(action) {
		return fmt.Errorf("action %s is not compensatable", action.ID)
	}
	params := action.Params
	if action.Compensation != nil {
		params = action.Compensation.Params
	}
	if shouldFail(params, "compensation_fail") {
		return fmt.Errorf("stub compensation requested failure")
	}
	return nil
}

type LegacyWorkflowActionExecutor struct{}

func NewLegacyWorkflowActionExecutor() *LegacyWorkflowActionExecutor {
	return &LegacyWorkflowActionExecutor{}
}
func (e *LegacyWorkflowActionExecutor) ExecuteAction(_ context.Context, action actionplan.Action, _ executioncontrol.ExecutionConstraints) error {
	if action.Type != "legacy_workflow_action" {
		return fmt.Errorf("unsupported action type %q", action.Type)
	}
	if shouldFail(action.Params, "fail") {
		return fmt.Errorf("legacy workflow adapter requested failure")
	}
	return nil
}
func (e *LegacyWorkflowActionExecutor) CompensateAction(_ context.Context, action actionplan.Action, _ executioncontrol.ExecutionConstraints) error {
	if !isCompensatable(action) {
		return fmt.Errorf("action %s is not compensatable", action.ID)
	}
	if shouldFail(action.Params, "compensation_fail") {
		return fmt.Errorf("legacy workflow adapter compensation requested failure")
	}
	return nil
}
func shouldFail(params map[string]any, key string) bool {
	if params == nil {
		return false
	}
	b, _ := params[key].(bool)
	return b
}
