package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"kalita/internal/runtime"

	"github.com/gin-gonic/gin"
)

func TestHealthEndpointReturnsOKForMemoryBackend(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	r := newRouterWithServices(&runtime.Storage{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "memory", nil)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /health status=%d body=%s", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got == "" {
		t.Fatal("health body is empty")
	}
}

func TestHealthEndpointReturnsServiceUnavailableWhenDBCheckFails(t *testing.T) {
	t.Parallel()
	gin.SetMode(gin.TestMode)

	r := newRouterWithServices(&runtime.Storage{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, "postgres", func(_ context.Context) error {
		return errors.New("db unavailable")
	})
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /health status=%d body=%s", w.Code, w.Body.String())
	}
}
