// cmd/server/main.go
package main

import (
	"context"
	"fmt"

	"kalita/internal/app"
	"kalita/internal/demo"
	"kalita/internal/http"
)

func main() {
	cfgPath := "./config/config.json"

	result, err := app.Bootstrap(cfgPath)
	if err != nil {
		panic(err)
	}

	operatorService := result.ControlPlane
	if result.Config.DemoMode {
		demoResult, err := demo.RunAISOtkhodyDemoScenario(context.Background())
		if err != nil {
			panic(err)
		}
		operatorService = demoResult.ControlPlane
		fmt.Printf("Kalita demo mode enabled at /demo with seeded case %s and approval %s\n", demoResult.CaseID, demoResult.ApprovalRequestID)
	}

	// HTTP
	fmt.Printf("Стартуем сервер Kalita на :%s...\n", result.Config.Port)
	http.RunServerWithServices(":"+result.Config.Port, result.Storage, result.CommandBus, result.CaseService, result.WorkService, result.Coordinator, result.PolicyService, result.ConstraintsService, result.ActionPlanService, result.ProposalService, result.EmployeeDirectory, operatorService, result.EmployeeService)
}
