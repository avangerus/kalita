package workplan

import (
	"context"
	"strings"
	"sync"
)

type DepartmentLoad struct {
	DepartmentID     string
	TotalActors      int
	BusyActors       int
	DepartmentExists bool
}

type DepartmentLoadProvider interface {
	GetLoad(ctx context.Context, departmentID string) (DepartmentLoad, error)
}

type InMemoryDepartmentLoadProvider struct {
	mu     sync.RWMutex
	byDept map[string]DepartmentLoad
}

func NewInMemoryDepartmentLoadProvider() *InMemoryDepartmentLoadProvider {
	return &InMemoryDepartmentLoadProvider{byDept: make(map[string]DepartmentLoad)}
}

func (p *InMemoryDepartmentLoadProvider) SaveLoad(_ context.Context, load DepartmentLoad) {
	departmentID := strings.TrimSpace(load.DepartmentID)
	if departmentID == "" {
		return
	}
	load.DepartmentID = departmentID
	p.mu.Lock()
	defer p.mu.Unlock()
	p.byDept[departmentID] = load
}

func (p *InMemoryDepartmentLoadProvider) GetLoad(_ context.Context, departmentID string) (DepartmentLoad, error) {
	departmentID = strings.TrimSpace(departmentID)
	if departmentID == "" {
		return DepartmentLoad{}, nil
	}
	p.mu.RLock()
	defer p.mu.RUnlock()
	load, ok := p.byDept[departmentID]
	if !ok {
		return DepartmentLoad{DepartmentID: departmentID, DepartmentExists: false}, nil
	}
	return load, nil
}
