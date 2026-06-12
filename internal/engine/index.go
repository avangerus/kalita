package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/avangerus/kalita/internal/eventstore"
)

// Secondary indexes for the "give me only the permitted records" query — the one
// that kills naive ABAC at scale. The kernel never scans every row to find the
// few an actor may see: when the actor's read permission scopes by an indexed
// field (`read X where region = $me.region`), the query iterates only that
// field's index bucket. The per-row can() check still runs as the final
// authority, so the index can never leak — it only narrows the candidate set
// (worst case: it falls back to a full scan). Indexes are lazy per entity,
// rebuilt on demand and dropped on write — ideal for read-often directories.

// entityIndex maps an indexed field's stringified value to the set of record ids.
type entityIndex struct {
	byField map[string]map[string]map[string]struct{} // field -> value -> id set
	count   int                                        // record count at build, a staleness backstop
}

// reScopeTerm extracts `field = $me` / `field = $me.<attr>` from a where-clause,
// capturing the (un-dotted) field on the left — exactly the predicates we can
// resolve from the actor and serve from an index.
var reScopeTerm = regexp.MustCompile(`(?:^|[\s(])(\w+)\s*=\s*\$me(\.\w+)?(?:[\s)]|$)`)

// indexedScopeFields returns the fields of an entity that any role's read/full
// allow rule scopes by `= $me[.attr]` — the dimensions worth indexing.
func (e *Engine) indexedScopeFields(entity string) map[string]bool {
	out := map[string]bool{}
	for _, pb := range e.model.Perms {
		for _, rule := range pb.Rules {
			if rule.Verb != "read" && rule.Verb != "full" {
				continue
			}
			for _, item := range rule.Items {
				if item.Entity != entity || item.Where == "" {
					continue
				}
				for _, m := range reScopeTerm.FindAllStringSubmatch(reDotSpace.ReplaceAllString(item.Where, "."), -1) {
					out[m[1]] = true
				}
			}
		}
	}
	return out
}

// getIndex returns the entity's index, (re)building it if missing or stale. The
// caller holds e.mu (records are read-stable); idxMu serializes index builds.
func (e *Engine) getIndex(entity string) *entityIndex {
	e.idxMu.Lock()
	defer e.idxMu.Unlock()
	rows := e.records[entity]
	if idx := e.idxCache[entity]; idx != nil && idx.count == len(rows) {
		return idx
	}
	fields := e.indexedScopeFields(entity)
	idx := &entityIndex{byField: map[string]map[string]map[string]struct{}{}, count: len(rows)}
	for f := range fields {
		idx.byField[f] = map[string]map[string]struct{}{}
	}
	for id, rec := range rows {
		for f := range fields {
			v := idxKey(rec.Values[f])
			if idx.byField[f][v] == nil {
				idx.byField[f][v] = map[string]struct{}{}
			}
			idx.byField[f][v][id] = struct{}{}
		}
	}
	if e.idxCache == nil {
		e.idxCache = map[string]*entityIndex{}
	}
	e.idxCache[entity] = idx
	return idx
}

// dropIndex invalidates an entity's index after a write. Cheap; the next query
// rebuilds. Called under e.mu (write side).
func (e *Engine) dropIndex(entity string) {
	e.idxMu.Lock()
	delete(e.idxCache, entity)
	e.idxMu.Unlock()
}

func idxKey(v any) string { return fmt.Sprintf("%v", v) }

// candidateIDs derives the set of record ids the actor's read permission could
// allow, using indexes — or (nil, false) if it cannot be narrowed (an
// unconditional allow, an `or`, or a non-indexed scope), in which case the
// caller scans all rows. The set is always a SUPERSET of the permitted rows
// (each allow rule's index bucket ⊇ its satisfying set); can() does the rest.
func (e *Engine) candidateIDs(entity string, actor eventstore.Actor) ([]string, bool) {
	pb, ok := e.model.Perms[actor.Role]
	if !ok {
		return nil, false // no rules → default deny; a scan yields the same (nothing)
	}
	idx := e.getIndex(entity)
	cand := map[string]struct{}{}
	sawAllow := false
	for _, rule := range pb.Rules {
		if rule.Verb != "read" && rule.Verb != "full" {
			continue
		}
		for _, item := range rule.Items {
			if !(item.All || item.Entity == entity) {
				continue
			}
			sawAllow = true
			if item.Where == "" || item.All {
				return nil, false // unconditional → must scan
			}
			where := reDotSpace.ReplaceAllString(item.Where, ".")
			if strings.Contains(where, " or ") {
				return nil, false // disjunction → conservative: scan
			}
			f, val, ok := e.scopeValue(where, idx, actor)
			if !ok {
				return nil, false // scope field not indexed → scan
			}
			for id := range idx.byField[f][val] {
				cand[id] = struct{}{}
			}
		}
	}
	if !sawAllow {
		return nil, false
	}
	out := make([]string, 0, len(cand))
	for id := range cand {
		out = append(out, id)
	}
	return out, true
}

// scopeValue finds an indexed `field = $me[.attr]` term and resolves its value
// from the actor (id for $me, attribute for $me.attr).
func (e *Engine) scopeValue(where string, idx *entityIndex, actor eventstore.Actor) (string, string, bool) {
	for _, m := range reScopeTerm.FindAllStringSubmatch(where, -1) {
		f := m[1]
		if idx.byField[f] == nil {
			continue // field not indexed
		}
		if m[2] == "" {
			return f, idxKey(actor.ID), true // $me
		}
		attr := strings.TrimPrefix(m[2], ".")
		if actor.Attrs == nil {
			return f, idxKey(nil), true // no attrs → empty bucket → nothing (fail-closed)
		}
		return f, idxKey(actor.Attrs[attr]), true
	}
	return "", "", false
}
