package http

import (
	"encoding/json"
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
	result, err := demo.RunAISOtkhodyMultiScenario(t.Context())
	if err != nil {
		t.Fatalf("RunAISOtkhodyMultiScenario error = %v", err)
	}
	r := gin.New()
	api := r.Group("/api")
	registerOperatorRoutes(api, result.ControlPlane)
	registerDemoRoutes(r)
	return r
}

func findDemoCaseIDByRoute(t *testing.T, r *gin.Engine, route string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/operator/cases", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/operator/cases status=%d body=%s", w.Code, w.Body.String())
	}
	var cases []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &cases); err != nil {
		t.Fatalf("unmarshal cases error = %v", err)
	}
	for _, item := range cases {
		if strings.Contains(fmt.Sprint(item["subject_ref"]), route) {
			return fmt.Sprint(item["case_id"])
		}
	}
	t.Fatalf("route %q not found in %#v", route, cases)
	return ""
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
	for _, want := range []string{"AIS Otkhody demo workload", "Unresolved route incidents", "Pending supervisor reviews", "Deferred reconciliation tasks", ">5<", ">1<"} {
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
	for _, want := range []string{"Missed Pickup", "Route R-1001", "Photo/GPS mismatch", "SITE-881", "Executing", "Waiting Approval"} {
		if !strings.Contains(listBody, want) {
			t.Fatalf("case list missing %q: %s", want, listBody)
		}
	}

	path := "/demo/cases/" + findDemoCaseIDByRoute(t, r, "R-1003")
	req := httptest.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s status=%d body=%s", path, w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"Missed Container Pickup Review", "Incident summary", "Fact Reconciliation", "Execution failed", "Trust updated", "Route", "R-1003"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestDemoApprovalActionUsesExistingApprovalFlow(t *testing.T) {
	t.Parallel()
	r := demoRouter(t)

	caseID := findDemoCaseIDByRoute(t, r, "R-1001")
	path := fmt.Sprintf("/demo/approvals/%s/approve", demo.DemoApprovalRequestID)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader("redirect=/demo/cases/"+caseID))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("POST %s status=%d body=%s", path, w.Code, w.Body.String())
	}
	if got := w.Header().Get("Location"); got != "/demo/cases/"+caseID {
		t.Fatalf("redirect = %q", got)
	}

	follow := httptest.NewRequest(http.MethodGet, "/demo/cases/"+caseID, nil)
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

func TestDemoCaseDetailOperatorWorkbenchUsesOperatorFlow(t *testing.T) {
	t.Parallel()
	r := demoRouter(t)

	caseID := findDemoCaseIDByRoute(t, r, "R-1003")
	for _, reqSpec := range []struct {
		path string
		body string
	}{
		{path: "/demo/cases/" + caseID + "/acknowledge"},
		{path: "/demo/cases/" + caseID + "/notes", body: "text=Carrier+confirmed+blocked+gate"},
		{path: "/demo/cases/" + caseID + "/external-input", body: "source=carrier_report&text=Access+restored"},
		{path: "/demo/cases/" + caseID + "/recoordinate"},
	} {
		req := httptest.NewRequest(http.MethodPost, reqSpec.path, strings.NewReader(reqSpec.body))
		if reqSpec.body != "" {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusSeeOther {
			t.Fatalf("POST %s status=%d body=%s", reqSpec.path, w.Code, w.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/demo/cases/"+caseID, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET detail status=%d body=%s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, want := range []string{"Operator workbench", "Carrier confirmed blocked gate", "carrier_report", "Access restored", "operator_case_acknowledged", "operator_note_added", "operator_recoordination_requested", "external_input_received"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}
