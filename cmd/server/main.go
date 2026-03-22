// cmd/server/main.go
package main

import (
	"fmt"

	"kalita/internal/app"
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
	http.RunServerWithCommandBus(":"+result.Config.Port, result.Storage, result.CommandBus)
}
