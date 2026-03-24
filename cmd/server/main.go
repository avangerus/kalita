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
	if result.PostgresPool != nil {
		defer result.PostgresPool.Close()
	}

	operatorService := result.ControlPlane
	integrationService := result.IntegrationService
	aisIngestionService := result.AISIngestionService
	if result.Config.DemoMode {
		demoResult, err := demo.RunAISOtkhodyDemoScenario(context.Background())
		if err != nil {
			panic(err)
		}
		operatorService = demoResult.ControlPlane
		integrationService = demoResult.IntegrationService
		aisIngestionService = nil
		fmt.Printf("Kalita demo mode enabled at /demo with seeded case %s and approval %s\n", demoResult.CaseID, demoResult.ApprovalRequestID)
	}
	if aisIngestionService != nil {
		aisIngestionService.Start(context.Background())
	}

	// HTTP
	fmt.Printf("Стартуем сервер Kalita на :%s...\n", result.Config.Port)
	http.RunServerWithServicesAndHealth(":"+result.Config.Port, result.Storage, result.CommandBus, result.CaseService, result.WorkService, result.Coordinator, result.PolicyService, result.ConstraintsService, result.ActionPlanService, result.ProposalService, result.EmployeeDirectory, operatorService, integrationService, aisIngestionService, result.DBBackend, result.DBHealthCheck, result.EmployeeService)
}
