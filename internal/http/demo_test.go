package http

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kalita/internal/demo"

	"github.com/gin-gonic/gin"
)

func demoRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	result, err := demo.RunDemoScenario(t.Context())
	if err != nil {
		t.Fatalf("RunDemoScenario error = %v", err)
	}
	r := gin.New()
	api := r.Group("/api")
	registerOperatorRoutes(api, result.ControlPlane)
	registerDemoRoutes(r)
	return r
}

func TestDemoDashboardRendersSummaryData(t *testing.T) {
	t.Parallel()
	r := demoRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/demo", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /demo status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"Active cases", ">1<", "Deferred work items", "Pending approvals", "low", "2"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestDemoCaseDetailRendersTimelineData(t *testing.T) {
	t.Parallel()
	r := demoRouter(t)

	path := fmt.Sprintf("/demo/cases/%s", demo.DemoCaseID)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"Case header", demo.DemoCaseID, "approval_requested", "coordination_decided", "Approve"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestDemoApprovalActionUsesExistingApprovalFlow(t *testing.T) {
	t.Parallel()
	r := demoRouter(t)

	path := fmt.Sprintf("/demo/approvals/%s/approve", demo.DemoApprovalRequestID)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("redirect=/demo/cases/"+demo.DemoCaseID))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST %s status=%d body=%s", path, w.Code, w.Body.String())
	}
	if got := w.Header().Get("Location"); got != "/demo/cases/"+demo.DemoCaseID {
		t.Fatalf("redirect = %q", got)
	}

	follow := httptest.NewRequest(http.MethodGet, "/demo/cases/"+demo.DemoCaseID, nil)
	followW := httptest.NewRecorder()
	r.ServeHTTP(followW, follow)
	if followW.Code != http.StatusOK {
		t.Fatalf("GET case detail status=%d body=%s", followW.Code, followW.Body.String())
	}
	body := followW.Body.String()
	for _, want := range []string{"approval_granted", "approved"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}
