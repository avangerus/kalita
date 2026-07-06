package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/eventstore"
)

// ENH-01 acceptance check: "/api/meta под ролью Steward возвращает все 5
// сущностей" — Steward holds an UNSCOPED `read` on Source, MasterRecord,
// MatchGroup, StewardshipTask and QualityMetric (packs/datapulse/security.dsl,
// ENH-01 open question #3: row-scoping deferred to M2, no `where owner=$me`
// here) so MetaFor (internal/engine/meta.go) must surface all five — an
// entity with no read rule at all is filtered out of /api/meta entirely
// (meta.go: "invisible entities do not exist for this actor").
func datapulsePackFiles(t *testing.T) map[string]string {
	t.Helper()
	dir := "../../packs/datapulse"
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read pack dir: %v", err)
	}
	files := map[string]string{}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".dsl") {
			src, err := os.ReadFile(dir + "/" + e.Name())
			if err != nil {
				t.Fatal(err)
			}
			files[e.Name()] = string(src)
		}
	}
	return files
}

func TestDataPulseMetaStewardSeesAllFiveEntities(t *testing.T) {
	model, errs := dsl.Compile(datapulsePackFiles(t))
	if len(errs) > 0 {
		t.Fatalf("datapulse must compile: %v", errs[0])
	}
	eng, err := engine.New(context.Background(), model, eventstore.NewMemStore(nil))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Secure(New(eng, nil, WithDevHeaders())))
	defer srv.Close()

	steward := &apiClient{t, srv.URL, "steward-1", "Steward"}
	resp, body := steward.do("GET", "/api/meta", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/meta for Steward: %d %v", resp.StatusCode, body)
	}
	ents, _ := body["entities"].([]any)
	got := map[string]bool{}
	for _, e := range ents {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		if name, _ := m["name"].(string); name != "" {
			got[name] = true
		}
	}
	want := []string{"Source", "MasterRecord", "MatchGroup", "StewardshipTask", "QualityMetric"}
	for _, w := range want {
		if !got[w] {
			t.Errorf("/api/meta for Steward missing entity %q, got %v", w, got)
		}
	}

	// a role with NO read grant on datapulse entities at all (ConnectorAgent
	// only reads Source, security.dsl) must not see MasterRecord/MatchGroup/
	// StewardshipTask/QualityMetric in its own meta — proves the filter is
	// actually role-scoped, not a blanket dump of the whole model.
	connector := &apiClient{t, srv.URL, "conn-1", "ConnectorAgent"}
	_, cbody := connector.do("GET", "/api/meta", nil)
	cents, _ := cbody["entities"].([]any)
	cgot := map[string]bool{}
	for _, e := range cents {
		if m, ok := e.(map[string]any); ok {
			if name, _ := m["name"].(string); name != "" {
				cgot[name] = true
			}
		}
	}
	if !cgot["Source"] {
		t.Errorf("ConnectorAgent must see Source in its own /api/meta, got %v", cgot)
	}
	for _, hidden := range []string{"MatchGroup", "QualityMetric"} {
		if cgot[hidden] {
			t.Errorf("ConnectorAgent has no grant on %s and must not see it in /api/meta, got %v", hidden, cgot)
		}
	}
}
