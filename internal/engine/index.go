package engine

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/avangerus/kalita/internal/dsl"
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
	count   int                                       // record count at build, a staleness backstop
}

// reScopeTerm extracts a DIRECT `field = $me` / `field = $me.<attr>` term,
// capturing the (un-dotted) field on the left — a predicate resolvable from the
// actor and servable from an index on `entity` itself.
var reScopeTerm = regexp.MustCompile(`(?:^|[\s(])(\w+)\s*=\s*\$me(\.\w+)?(?:[\s)]|$)`)

// reRefScopeTerm extracts a REF-PATH `ref.tfield = $me[.attr]` term — indirect
// ownership ("tasks in projects I own"), the most common scope the direct index
// misses. m[1]=local ref field, m[2]=target field, m[3]=optional actor attr.
var reRefScopeTerm = regexp.MustCompile(`(?:^|[\s(])(\w+)\.(\w+)\s*=\s*\$me(\.\w+)?(?:[\s)]|$)`)

// refTarget returns the entity a ref field points at, or "" if it is not a ref.
func (e *Engine) refTarget(entity, field string) string {
	if fd := fieldDecl(e.model.Entities[entity], field); fd != nil && fd.Type.Kind == dsl.TyRef {
		return fd.Type.RefTarget
	}
	return ""
}

// indexedScopeFields returns the fields of `entity` worth indexing for the
// permitted-set query. Three sources, all derived from `= $me[.attr]` scopes in
// any role's read/full rule: a DIRECT scope on entity (`owner`), the LOCAL ref of
// a ref-path scope on entity (`project` in `project.owner = $me`), and the TARGET
// field of a ref-path scope that points AT entity (`owner`, because some other
// entity scopes by `<ref→entity>.owner = $me`).
func (e *Engine) indexedScopeFields(entity string) map[string]bool {
	out := map[string]bool{}
	for _, pb := range e.model.Perms {
		for _, rule := range pb.Rules {
			if rule.Verb != "read" && rule.Verb != "full" {
				continue
			}
			for _, item := range rule.Items {
				if item.Where == "" {
					continue
				}
				where := reDotSpace.ReplaceAllString(item.Where, ".")
				if item.Entity == entity {
					for _, m := range reScopeTerm.FindAllStringSubmatch(where, -1) {
						out[m[1]] = true
					}
				}
				for _, m := range reRefScopeTerm.FindAllStringSubmatch(where, -1) {
					ref, tfield := m[1], m[2]
					target := e.refTarget(item.Entity, ref)
					if target == "" || strings.HasPrefix(target, "core.") {
						continue // a complete index is required (see candidateIDs); core.* isn't in records
					}
					if item.Entity == entity {
						out[ref] = true // index the local ref so we can map target-id → local rows
					}
					if target == entity {
						out[tfield] = true // index the target field so we can find target-ids by = $me
					}
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
			ids, ok := e.ruleCandidates(entity, idx, where, actor)
			if !ok {
				return nil, false // scope not index-backed → scan
			}
			for id := range ids {
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

// ruleCandidates returns the record-id set one (non-or) allow rule's where
// permits, served from indexes — or ok=false if it cannot be narrowed (the
// caller then scans the whole entity). It tries a DIRECT scope first, then a
// REF-PATH scope. Each returned set is a SUPERSET of (in fact, exactly) that
// term's satisfying rows; can() remains the final authority.
func (e *Engine) ruleCandidates(entity string, idx *entityIndex, where string, actor eventstore.Actor) (map[string]struct{}, bool) {
	// direct: field = $me[.attr]  → that field's bucket on `entity`
	if f, val, ok := e.scopeValue(where, idx, actor); ok {
		return idx.byField[f][val], true
	}
	// ref-path: ref.tfield = $me[.attr]  → union the local-ref buckets of every
	// target record the actor owns. Both indexes must be present and COMPLETE
	// (target is a normal in-records entity, guaranteed by indexedScopeFields
	// skipping core.*), else the union could omit permitted rows.
	for _, m := range reRefScopeTerm.FindAllStringSubmatch(where, -1) {
		ref, tfield := m[1], m[2]
		target := e.refTarget(entity, ref)
		if target == "" || strings.HasPrefix(target, "core.") || idx.byField[ref] == nil {
			continue
		}
		tIdx := e.getIndex(target)
		if tIdx.byField[tfield] == nil {
			continue
		}
		av := actorScopeVal(actor, m[3])
		cand := map[string]struct{}{}
		for p := range tIdx.byField[tfield][av] {
			for id := range idx.byField[ref][p] {
				cand[id] = struct{}{}
			}
		}
		return cand, true
	}
	return nil, false
}

// scopeValue finds an indexed DIRECT `field = $me[.attr]` term and resolves its
// value from the actor (id for $me, attribute for $me.attr).
func (e *Engine) scopeValue(where string, idx *entityIndex, actor eventstore.Actor) (string, string, bool) {
	for _, m := range reScopeTerm.FindAllStringSubmatch(where, -1) {
		f := m[1]
		if idx.byField[f] == nil {
			continue // field not indexed
		}
		return f, actorScopeVal(actor, m[2]), true
	}
	return "", "", false
}

// actorScopeVal is the index key a `$me` / `$me.<attr>` term resolves to: the
// actor id, or the named attribute (nil → empty bucket, i.e. fail-closed).
func actorScopeVal(actor eventstore.Actor, attrGroup string) string {
	if attrGroup == "" {
		return idxKey(actor.ID)
	}
	if actor.Attrs == nil {
		return idxKey(nil)
	}
	return idxKey(actor.Attrs[strings.TrimPrefix(attrGroup, ".")])
}
