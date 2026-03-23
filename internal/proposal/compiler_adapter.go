package proposal

import (
	"context"
	"fmt"

	"kalita/internal/actionplan"
)

type compilerAdapter struct{ service actionplan.Service }

func NewCompilerAdapter(service actionplan.Service) CompilerAdapter {
	return &compilerAdapter{service: service}
}

func (a *compilerAdapter) CompileToActionPlan(ctx context.Context, p Proposal) (actionplan.ActionPlan, error) {
	if a.service == nil {
		return actionplan.ActionPlan{}, fmt.Errorf("action plan service is nil")
	}
	return a.service.CreatePlan(ctx, p.WorkItemID, p.CaseID, cloneMap(p.Payload))
}
