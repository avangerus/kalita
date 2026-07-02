package goldenfmt

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/avangerus/kalita/internal/eventstore"
)

// uuidPattern matches the canonical 8-4-4-4-12 hex form (google/uuid's
// .String() output; record/task/approval/proposal/actor ids in this codebase
// are all uuid.NewV7()).
var uuidPattern = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)

// BuildOrder scans events in seq order and assigns every distinct uuid-shaped
// string it finds a 1-based "first appearance" index — LLD-03 §3: "map uuid →
// порядковый id#N в порядке первого появления". It looks at each event's own
// id, every Subject id field, and the raw payload bytes (which is where a ref
// field pointing at another record's id shows up, e.g. `"case":"<uuid>"`
// inside a Memo's record.created payload).
//
// Both the generator (writing expected/*.json) and the replay gate (comparing
// against it) call this over the SAME frozen journal.jsonl, so the resulting
// labels — and the array order derived from them, see Dump — line up without
// ever injecting a deterministic uuid generator into the kernel (the
// alternative LLD-03 explicitly rejects).
func BuildOrder(events []*eventstore.Event) map[string]int {
	order := map[string]int{}
	next := 1
	see := func(s string) {
		for _, m := range uuidPattern.FindAllString(s, -1) {
			key := strings.ToLower(m)
			if _, ok := order[key]; !ok {
				order[key] = next
				next++
			}
		}
	}
	for _, ev := range events {
		see(ev.EventID)
		see(ev.Subject.RecordID)
		see(ev.Subject.ProposalID)
		see(ev.Subject.TaskID)
		see(ev.Subject.ApprovalID)
		see(ev.Subject.ActorID)
		see(string(ev.Payload))
	}
	return order
}

// Label returns the "id#N" alias for a known uuid (case-insensitive exact
// match), or ok=false if raw is not a uuid BuildOrder ever saw.
func Label(order map[string]int, raw string) (label string, ok bool) {
	n, ok := order[strings.ToLower(raw)]
	if !ok {
		return "", false
	}
	return fmt.Sprintf("id#%d", n), true
}

// normalizeTree walks a decoded-JSON value (map[string]any / []any / string /
// float64 / bool / nil — the shape Roundtrip produces) and replaces every
// string that is EXACTLY a known uuid with its "id#N" label. Only full-string
// matches are rewritten (never substrings inside larger text), so free-form
// fields (comment bodies, notes) are never corrupted even if they happened to
// quote an id.
func normalizeTree(v any, order map[string]int) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = normalizeTree(vv, order)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = normalizeTree(vv, order)
		}
		return out
	case string:
		if lbl, ok := Label(order, t); ok {
			return lbl
		}
		return t
	default:
		return v
	}
}

// Roundtrip marshals v and decodes it back into the generic any-tree
// encoding/json produces (numbers as float64, objects as map[string]any) —
// the shape normalizeTree and CanonicalJSON operate on, regardless of v's
// original Go type (engine.Record, engine.Task, a map, ...).
func Roundtrip(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CanonicalJSON marshals an any-tree with sorted object keys (encoding/json's
// standing behavior for map[string]any) and stable indentation, ending in a
// single trailing newline. This is the byte form committed to expected/*.json
// and the form both sides of the replay gate compare.
func CanonicalJSON(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// NormalizedCanonicalJSON is the full pipeline shared by the generator (when
// writing expected/*.json) and the replay gate (when computing the actual
// snapshot to compare against it): round-trip through JSON, normalize uuids
// against order, marshal canonically.
func NormalizedCanonicalJSON(v any, order map[string]int) ([]byte, error) {
	rt, err := Roundtrip(v)
	if err != nil {
		return nil, err
	}
	return CanonicalJSON(normalizeTree(rt, order))
}
