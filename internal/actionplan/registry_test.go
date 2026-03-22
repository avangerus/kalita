package actionplan

import (
	"fmt"
	"testing"
)

func TestRegistryRegisterAndGet(t *testing.T) {
	registry := NewRegistry()
	registry.Register(ActionDefinition{Type: "send_notification", Validate: func(params map[string]any) error {
		if params["message"] == "" {
			return fmt.Errorf("message is required")
		}
		return nil
	}})

	got, ok := registry.Get("send_notification")
	if !ok || got.Type != "send_notification" {
		t.Fatalf("Get(send_notification) = %#v ok=%v", got, ok)
	}
}
