package executionruntime

import (
	"context"
	"fmt"
	"strings"

	"kalita/internal/actionplan"
	"kalita/internal/eventcore"
	"kalita/internal/executioncontrol"
)

type executionContextKey struct{}

type ExecutionContext struct{ ExecutionID, CorrelationID, CausationID string }

func ContextWithExecution(ctx context.Context, meta ExecutionContext) context.Context {
	return context.WithValue(ctx, executionContextKey{}, meta)
}
func executionFromContext(ctx context.Context) ExecutionContext {
	meta, _ := ctx.Value(executionContextKey{}).(ExecutionContext)
	return meta
}

type DefaultRunner struct {
	repo     ExecutionRepository
	wal      WAL
	executor ActionExecutor
	log      eventcore.EventLog
	clock    eventcore.Clock
	ids      eventcore.IDGenerator
}

func NewRunner(repo ExecutionRepository, wal WAL, executor ActionExecutor, log eventcore.EventLog, clock eventcore.Clock, ids eventcore.IDGenerator) *DefaultRunner {
	if clock == nil {
		clock = eventcore.RealClock{}
	}
	if ids == nil {
		ids = eventcore.NewULIDGenerator()
	}
	return &DefaultRunner{repo: repo, wal: wal, executor: executor, log: log, clock: clock, ids: ids}
}

func (r *DefaultRunner) RunPlan(ctx context.Context, plan actionplan.ActionPlan, constraints executioncontrol.ExecutionConstraints, metadata RunMetadata) (ExecutionSession, error) {
	if r.repo == nil {
		return ExecutionSession{}, fmt.Errorf("execution repository is nil")
	}
	if r.wal == nil {
		return ExecutionSession{}, fmt.Errorf("execution WAL is nil")
	}
	if r.executor == nil {
		return ExecutionSession{}, fmt.Errorf("action executor is nil")
	}
	now := r.clock.Now()
	session := ExecutionSession{ID: r.ids.NewID(), ActionPlanID: plan.ID, CaseID: metadata.CaseID, WorkItemID: metadata.WorkItemID, CoordinationDecisionID: metadata.CoordinationDecisionID, PolicyDecisionID: metadata.PolicyDecisionID, ExecutionConstraintsID: constraints.ID, Status: ExecutionSessionPending, CurrentStepIndex: -1, CreatedAt: now, UpdatedAt: now}
	if err := r.repo.SaveSession(ctx, session); err != nil {
		return ExecutionSession{}, err
	}
	if err := r.appendEvent(ctx, session.CaseID, "execution_session_created", string(session.Status), map[string]any{"execution_session_id": session.ID, "action_plan_id": session.ActionPlanID, "work_item_id": session.WorkItemID, "coordination_decision_id": session.CoordinationDecisionID, "policy_decision_id": session.PolicyDecisionID, "execution_constraints_id": session.ExecutionConstraintsID, "action_count": len(plan.Actions)}); err != nil {
		return ExecutionSession{}, err
	}
	steps := make([]StepExecution, 0, len(plan.Actions))
	for idx, action := range plan.Actions {
		step := StepExecution{ID: r.ids.NewID(), ExecutionSessionID: session.ID, ActionID: action.ID, StepIndex: idx, Status: StepPending}
		if err := r.repo.SaveStep(ctx, step); err != nil {
			return ExecutionSession{}, err
		}
		steps = append(steps, step)
	}
	session.Status = ExecutionSessionRunning
	session.UpdatedAt = r.clock.Now()
	if err := r.repo.SaveSession(ctx, session); err != nil {
		return ExecutionSession{}, err
	}
	for idx, action := range plan.Actions {
		step := steps[idx]
		if err := r.appendWAL(ctx, session, step, action, WALStepIntent, map[string]any{"step_index": idx}); err != nil {
			return ExecutionSession{}, err
		}
		startedAt := r.clock.Now()
		step.Status = StepRunning
		step.StartedAt = &startedAt
		if err := r.repo.SaveStep(ctx, step); err != nil {
			return ExecutionSession{}, err
		}
		session.CurrentStepIndex, session.UpdatedAt = idx, startedAt
		if err := r.repo.SaveSession(ctx, session); err != nil {
			return ExecutionSession{}, err
		}
		if err := r.appendEvent(ctx, session.CaseID, "execution_step_started", string(step.Status), map[string]any{"execution_session_id": session.ID, "step_execution_id": step.ID, "action_id": action.ID, "step_index": idx}); err != nil {
			return ExecutionSession{}, err
		}
		err := r.executor.ExecuteAction(ctx, action, constraints)
		finishedAt := r.clock.Now()
		step.FinishedAt = &finishedAt
		if err != nil {
			step.Status, step.FailureReason = StepFailed, err.Error()
			session.Status, session.FailureReason, session.UpdatedAt = ExecutionSessionFailed, fmt.Sprintf("step %d action %s failed: %v", idx, action.ID, err), finishedAt
			if saveErr := r.repo.SaveStep(ctx, step); saveErr != nil {
				return ExecutionSession{}, saveErr
			}
			if saveErr := r.repo.SaveSession(ctx, session); saveErr != nil {
				return ExecutionSession{}, saveErr
			}
			if appendErr := r.appendWAL(ctx, session, step, action, WALStepResult, map[string]any{"step_index": idx, "status": string(step.Status), "failure_reason": step.FailureReason}); appendErr != nil {
				return ExecutionSession{}, appendErr
			}
			if appendErr := r.appendEvent(ctx, session.CaseID, "execution_step_failed", string(step.Status), map[string]any{"execution_session_id": session.ID, "step_execution_id": step.ID, "action_id": action.ID, "step_index": idx, "failure_reason": step.FailureReason}); appendErr != nil {
				return ExecutionSession{}, appendErr
			}
			if compErr := r.compensate(ctx, &session, plan.Actions, steps[:idx], constraints); compErr != nil {
				session.Status, session.FailureReason, session.UpdatedAt = ExecutionSessionFailed, strings.TrimSpace(session.FailureReason+"; compensation failed: "+compErr.Error()), r.clock.Now()
				if saveErr := r.repo.SaveSession(ctx, session); saveErr != nil {
					return ExecutionSession{}, saveErr
				}
			}
			if appendErr := r.appendEvent(ctx, session.CaseID, "execution_session_failed", string(session.Status), map[string]any{"execution_session_id": session.ID, "failure_reason": session.FailureReason}); appendErr != nil {
				return ExecutionSession{}, appendErr
			}
			return session, nil
		}
		step.Status = StepSucceeded
		if saveErr := r.repo.SaveStep(ctx, step); saveErr != nil {
			return ExecutionSession{}, saveErr
		}
		steps[idx] = step
		if appendErr := r.appendWAL(ctx, session, step, action, WALStepResult, map[string]any{"step_index": idx, "status": string(step.Status)}); appendErr != nil {
			return ExecutionSession{}, appendErr
		}
		if appendErr := r.appendEvent(ctx, session.CaseID, "execution_step_succeeded", string(step.Status), map[string]any{"execution_session_id": session.ID, "step_execution_id": step.ID, "action_id": action.ID, "step_index": idx}); appendErr != nil {
			return ExecutionSession{}, appendErr
		}
	}
	session.Status, session.UpdatedAt = ExecutionSessionSucceeded, r.clock.Now()
	if err := r.repo.SaveSession(ctx, session); err != nil {
		return ExecutionSession{}, err
	}
	if err := r.appendEvent(ctx, session.CaseID, "execution_session_succeeded", string(session.Status), map[string]any{"execution_session_id": session.ID}); err != nil {
		return ExecutionSession{}, err
	}
	return session, nil
}

func (r *DefaultRunner) compensate(ctx context.Context, session *ExecutionSession, actions []actionplan.Action, completed []StepExecution, constraints executioncontrol.ExecutionConstraints) error {
	var targets []int
	for i := len(completed) - 1; i >= 0; i-- {
		if completed[i].Status == StepSucceeded && isCompensatable(actions[completed[i].StepIndex]) {
			targets = append(targets, i)
		}
	}
	if len(targets) == 0 {
		return nil
	}
	session.Status, session.UpdatedAt = ExecutionSessionCompensating, r.clock.Now()
	if err := r.repo.SaveSession(ctx, *session); err != nil {
		return err
	}
	if err := r.appendEvent(ctx, session.CaseID, "execution_compensation_started", string(session.Status), map[string]any{"execution_session_id": session.ID, "compensation_count": len(targets)}); err != nil {
		return err
	}
	for _, idx := range targets {
		step, action := completed[idx], actions[completed[idx].StepIndex]
		step.Status = StepCompensating
		if err := r.repo.SaveStep(ctx, step); err != nil {
			return err
		}
		if err := r.appendWAL(ctx, *session, step, action, WALCompensationIntent, map[string]any{"step_index": step.StepIndex}); err != nil {
			return err
		}
		if err := r.executor.CompensateAction(ctx, action, constraints); err != nil {
			step.Status, step.FailureReason = StepFailed, fmt.Sprintf("compensation failed: %v", err)
			finishedAt := r.clock.Now()
			step.FinishedAt = &finishedAt
			if saveErr := r.repo.SaveStep(ctx, step); saveErr != nil {
				return saveErr
			}
			if appendErr := r.appendWAL(ctx, *session, step, action, WALCompensationResult, map[string]any{"step_index": step.StepIndex, "status": string(step.Status), "failure_reason": step.FailureReason}); appendErr != nil {
				return appendErr
			}
			return fmt.Errorf("action %s: %w", action.ID, err)
		}
		step.Status = StepCompensated
		finishedAt := r.clock.Now()
		step.FinishedAt = &finishedAt
		if err := r.repo.SaveStep(ctx, step); err != nil {
			return err
		}
		if err := r.appendWAL(ctx, *session, step, action, WALCompensationResult, map[string]any{"step_index": step.StepIndex, "status": string(step.Status)}); err != nil {
			return err
		}
		if err := r.appendEvent(ctx, session.CaseID, "execution_compensation_succeeded", string(step.Status), map[string]any{"execution_session_id": session.ID, "step_execution_id": step.ID, "action_id": action.ID, "step_index": step.StepIndex}); err != nil {
			return err
		}
	}
	session.Status, session.UpdatedAt = ExecutionSessionCompensated, r.clock.Now()
	if err := r.repo.SaveSession(ctx, *session); err != nil {
		return err
	}
	return r.appendEvent(ctx, session.CaseID, "execution_session_compensated", string(session.Status), map[string]any{"execution_session_id": session.ID})
}

func isCompensatable(action actionplan.Action) bool {
	return action.Reversibility == actionplan.ReversibilityCompensatable || action.Reversibility == actionplan.ReversibilityFullyReversible
}
func (r *DefaultRunner) appendWAL(ctx context.Context, session ExecutionSession, step StepExecution, action actionplan.Action, kind WALRecordType, payload map[string]any) error {
	return r.wal.Append(ctx, WALRecord{ID: r.ids.NewID(), ExecutionSessionID: session.ID, StepExecutionID: step.ID, ActionID: action.ID, Type: kind, CreatedAt: r.clock.Now(), Payload: payload})
}
func (r *DefaultRunner) appendEvent(ctx context.Context, caseID, step, status string, payload map[string]any) error {
	if r.log == nil {
		return nil
	}
	meta := executionFromContext(ctx)
	return r.log.AppendExecutionEvent(ctx, eventcore.ExecutionEvent{ID: r.ids.NewID(), ExecutionID: meta.ExecutionID, CaseID: caseID, Step: step, Status: status, OccurredAt: r.clock.Now(), CorrelationID: meta.CorrelationID, CausationID: meta.CausationID, Payload: payload})
}
