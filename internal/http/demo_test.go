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
	result, err := demo.RunAISOtkhodyDemoScenario(t.Context())
	if err != nil {
		t.Fatalf("RunAISOtkhodyDemoScenario error = %v", err)
	}
	r := gin.New()
	api := r.Group("/api")
	registerOperatorRoutes(api, result.ControlPlane)
	registerDemoRoutes(r)
	return r
}

func TestDemoDashboardRendersDomainWidgets(t *testing.T) {
	t.Parallel()
	r := demoRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/demo", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /demo status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"AIS Otkhody demo slice", "Unresolved route incidents", "Pending supervisor reviews", "Deferred reconciliation tasks", ">1<"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestDemoCaseListAndDetailRenderDomainLabels(t *testing.T) {
	t.Parallel()
	r := demoRouter(t)

	listReq := httptest.NewRequest(http.MethodGet, "/demo/cases", nil)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("GET /demo/cases status=%d body=%s", listW.Code, listW.Body.String())
	}
	listBody := listW.Body.String()
	for _, want := range []string{"Missed Pickup", "Route R-2048", "Photo/GPS mismatch", "SITE-881"} {
		if !strings.Contains(listBody, want) {
			t.Fatalf("case list missing %q: %s", want, listBody)
		}
	}

	path := fmt.Sprintf("/demo/cases/%s", demo.AISDemoCaseID)
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"Missed Container Pickup Review", "Incident summary", "Fact Reconciliation", "Supervisor review required", "Incident detected", "Reconciliation task created", "Follow-up coordination performed"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestDemoApprovalActionUsesExistingApprovalFlow(t *testing.T) {
	t.Parallel()
	r := demoRouter(t)

	path := fmt.Sprintf("/demo/approvals/%s/approve", demo.AISDemoApprovalRequestID)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("redirect=/demo/cases/"+demo.AISDemoCaseID))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST %s status=%d body=%s", path, w.Code, w.Body.String())
	}
	if got := w.Header().Get("Location"); got != "/demo/cases/"+demo.AISDemoCaseID {
		t.Fatalf("redirect = %q", got)
	}

	follow := httptest.NewRequest(http.MethodGet, "/demo/cases/"+demo.AISDemoCaseID, nil)
	followW := httptest.NewRecorder()
	r.ServeHTTP(followW, follow)
	if followW.Code != http.StatusOK {
		t.Fatalf("GET case detail status=%d body=%s", followW.Code, followW.Body.String())
	}
	body := followW.Body.String()
	for _, want := range []string{"Approval granted", "approved", "Follow-up coordination performed"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}
