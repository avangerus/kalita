package goldenfmt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/avangerus/kalita/internal/dsl"
	"github.com/avangerus/kalita/internal/engine"
	"github.com/avangerus/kalita/internal/identity"
)

// Snapshot is the golden-journal projection dump (LLD-03 §3): "records по
// сущностям, tasks, approvals, actors". Everything here comes from Engine's
// and Registry's existing public API — Export is the operator-only unmasked
// dump already used for era-migration (internal/engine/engine.go), so records
// are read without ABAC filtering/masking, matching ground truth rather than
// any one actor's view. Nothing new was added to internal/ to produce this.
type Snapshot struct {
	Records   map[string][]*engine.Record `json:"records"`
	Tasks     []*engine.Task              `json:"tasks"`
	Approvals []*engine.Approval          `json:"approvals"`
	Actors    []*identity.ActorInfo       `json:"actors"`
}

// expectedFiles lists the snapshot files under expected/, in the fixed order
// used for both writing and comparing.
var expectedFiles = []string{"records.json", "tasks.json", "approvals.json", "actors.json"}

func (s *Snapshot) fields() map[string]any {
	return map[string]any{
		"records.json":   s.Records,
		"tasks.json":     s.Tasks,
		"approvals.json": s.Approvals,
		"actors.json":    s.Actors,
	}
}

// Dump builds the snapshot for the current state of eng/reg, sorting every
// list by each item's position in order (see BuildOrder) so that dumping the
// same frozen journal twice — once at generation, once at replay — always
// produces the same array order.
func Dump(ctx context.Context, eng *engine.Engine, reg *identity.Registry, order map[string]int) (*Snapshot, error) {
	snap := &Snapshot{Records: map[string][]*engine.Record{}}

	model := eng.Model()
	for _, name := range model.Order {
		recs := eng.Export(name)
		if len(recs) == 0 {
			continue
		}
		sortByOrder(order, len(recs), func(i int) string { return recs[i].ID },
			func(i, j int) { recs[i], recs[j] = recs[j], recs[i] })
		snap.Records[name] = recs
	}

	roles := roleNames(model)
	taskSeen := map[string]*engine.Task{}
	approvalSeen := map[string]*engine.Approval{}
	for _, role := range roles {
		for _, t := range eng.Tasks(role, "") {
			taskSeen[t.ID] = t
		}
		for _, a := range eng.PendingApprovals(role) {
			approvalSeen[a.ID] = a
		}
	}
	for _, t := range taskSeen {
		snap.Tasks = append(snap.Tasks, t)
	}
	for _, a := range approvalSeen {
		snap.Approvals = append(snap.Approvals, a)
	}
	sortByOrder(order, len(snap.Tasks), func(i int) string { return snap.Tasks[i].ID },
		func(i, j int) { snap.Tasks[i], snap.Tasks[j] = snap.Tasks[j], snap.Tasks[i] })
	sortByOrder(order, len(snap.Approvals), func(i int) string { return snap.Approvals[i].ID },
		func(i, j int) { snap.Approvals[i], snap.Approvals[j] = snap.Approvals[j], snap.Approvals[i] })

	actors, err := reg.List(ctx)
	if err != nil {
		return nil, err
	}
	snap.Actors = actors // Registry.List already sorts by (human-readable, non-uuid) id

	return snap, nil
}

// roleNames returns every declared role plus "" — automation actions with no
// role (notify, webhook) queue tasks under the empty role, so it must be
// scanned too for a complete task dump.
func roleNames(m *dsl.Model) []string {
	out := []string{""}
	names := make([]string, 0, len(m.Roles))
	for name := range m.Roles {
		names = append(names, name)
	}
	sort.Strings(names)
	return append(out, names...)
}

// sortByOrder sorts an n-element sequence (via swap) by order[id(i)],
// ascending. Ids the order map does not know about (should not happen for
// well-formed golden data) sort after everything known, by raw id string, so
// output stays fully deterministic regardless.
func sortByOrder(order map[string]int, n int, id func(int) string, swap func(i, j int)) {
	rank := func(i int) int {
		if r, ok := order[strings.ToLower(id(i))]; ok {
			return r
		}
		return len(order) + 1
	}
	// Simple insertion sort driven by the caller's swap/id closures, so this
	// stays generic without reflection: n is small (test-fixture sized), so
	// O(n^2) is irrelevant here.
	for i := 1; i < n; i++ {
		for j := i; j > 0 && (rank(j) < rank(j-1) || (rank(j) == rank(j-1) && id(j) < id(j-1))); j-- {
			swap(j, j-1)
		}
	}
}

// WriteExpected writes the four normalized, canonical snapshot files under
// dir (testdata/golden/<ver>/expected/).
func WriteExpected(dir string, snap *Snapshot, order map[string]int) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	fields := snap.fields()
	for _, name := range expectedFiles {
		b, err := NormalizedCanonicalJSON(fields[name], order)
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// FileDiff is a non-nil result from CompareExpected: the canonical form found
// on disk vs. the canonical form computed from replay.
type FileDiff struct {
	File     string
	Expected string
	Got      string
}

// CompareExpected reads dir's four snapshot files and compares each against
// the corresponding field of snap (already normalized against order),
// semantically (parsed-JSON equality via canonical re-marshal) rather than by
// raw bytes, so incidental formatting/line-ending differences never cause a
// false mismatch. Returns one FileDiff per file that does not match.
func CompareExpected(dir string, snap *Snapshot, order map[string]int) ([]FileDiff, error) {
	fields := snap.fields()
	var diffs []FileDiff
	for _, name := range expectedFiles {
		path := filepath.Join(dir, name)
		wantRaw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		var wantAny any
		if err := json.Unmarshal(wantRaw, &wantAny); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		wantCanon, err := CanonicalJSON(wantAny)
		if err != nil {
			return nil, fmt.Errorf("canonicalize %s: %w", path, err)
		}
		gotCanon, err := NormalizedCanonicalJSON(fields[name], order)
		if err != nil {
			return nil, fmt.Errorf("compute actual %s: %w", name, err)
		}
		if !bytes.Equal(wantCanon, gotCanon) {
			diffs = append(diffs, FileDiff{File: name, Expected: string(wantCanon), Got: string(gotCanon)})
		}
	}
	return diffs, nil
}
