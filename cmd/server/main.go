// cmd/server/main.go
package main

import (
	"fmt"

	"kalita/internal/app"
	"kalita/internal/controlplane"
	"kalita/internal/http"
)

func main() {
	cfgPath := "./config/config.json"

	result, err := app.Bootstrap(cfgPath)
	if err != nil {
		panic(err)
	}

	// HTTP
	fmt.Printf("Стартуем сервер Kalita на :%s...\n", result.Config.Port)
	operatorControlPlane := controlplane.NewService(result.CaseRepo, result.QueueRepo, result.CoordinationRepo, result.PolicyRepo, result.ProposalRepo, result.ExecutionRepo, result.EmployeeDirectory, result.TrustRepo, result.EventLog)
	http.RunServerWithControlPlane(":"+result.Config.Port, result.Storage, result.CommandBus, result.CaseService, result.WorkService, result.Coordinator, result.PolicyService, result.ConstraintsService, result.ActionPlanService, result.ProposalService, result.EmployeeDirectory, operatorControlPlane, result.EmployeeService)
}
