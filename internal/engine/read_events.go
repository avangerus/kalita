package engine

import (
	"context"
	"fmt"

	"github.com/avangerus/kalita/internal/eventstore"
)

// ReadEvents backs the MCP tool `read_events` (LLD-08 §1): a node-wide,
// since-cursor tail over the journal — NOT bound to a subject, unlike
// Journal/read_journal (journal.go, which stays per-record and untouched by
// this LLD). It reads Store.Since directly and builds no parallel projection.
//
// Permission (§2, HLD I-2 choke point): the actor must hold `tail [kind]` for
// every kind it ends up reading, checked BEFORE the store is touched — an
// actor with no matching grant learns nothing about the journal, not even its
// size.
//
// Content ABAC (§3): v1 ever serves only tailServableKinds regardless of
// grant — record.created/updated (and everything else) need the stronger
// tail_records grant that does not exist yet, so the outbox primitive cannot
// become a side door around per-record ABAC.
//
// since/kinds/limit follow §1 exactly: kinds omitted (nil) defaults to
// [event.emitted]; kinds present but empty means "every kind this role may
// tail, capped to what v1 can serve"; limit defaults to 500, capped at 5000.
// The cursor advances over RAW events examined (next_since = last examined
// seq + 1), not just the ones returned after kind-filtering — so pagination
// never stalls behind a run of kinds the actor cannot see.
func (e *Engine) ReadEvents(ctx context.Context, actor eventstore.Actor, since uint64, kinds []string, limit int) ([]*eventstore.Event, uint64, error) {
	e.mu.RLock()
	effective, terr := e.tailEffectiveKinds(actor.Role, kinds)
	e.mu.RUnlock()
	if terr != nil {
		return nil, 0, terr
	}

	switch {
	case limit <= 0:
		limit = 500
	case limit > 5000:
		limit = 5000
	}

	// `since` is an INCLUSIVE cursor (§1: next_since = seq of the last event
	// examined + 1, and the client passes next_since straight back as the next
	// call's since) — but eventstore.Store.Since is EXCLUSIVE (seq > afterSeq).
	// Translate: afterSeq = since-1 so Since returns seq >= since. since=0 (the
	// initial call, "from the beginning") stays afterSeq=0 — Store.Since(0)
	// already returns every event because real seqs start at 1, so the uint64
	// underflow that a literal since-1 would cause never triggers here.
	afterSeq := since
	if since > 0 {
		afterSeq = since - 1
	}
	all, err := e.store.Since(ctx, afterSeq)
	if err != nil {
		return nil, 0, err
	}
	if len(all) > limit {
		all = all[:limit]
	}
	nextSince := since
	if len(all) > 0 {
		nextSince = all[len(all)-1].Seq + 1
	}
	out := make([]*eventstore.Event, 0, len(all))
	for _, ev := range all {
		if effective[ev.Kind] {
			out = append(out, ev)
		}
	}
	return out, nextSince, nil
}

// tailServableKinds is the v1 allow-list of kinds read_events may ever return,
// regardless of grant (LLD-08 §3): event.emitted plus system kinds with no
// sensitive payload. Everything else — most pointedly record.created/updated,
// the raw record payload — needs the stronger tail_records grant this LLD
// explicitly does not add ("не в этом LLD"), so even a role holding, say,
// `tail [record.created]` gets nothing back for it from this tool.
var tailServableKinds = map[eventstore.Kind]bool{
	eventstore.EventEmitted:     true,
	eventstore.OpBegin:          true,
	eventstore.OpEnd:            true,
	eventstore.OpAborted:        true,
	eventstore.CheckpointSealed: true,
}

// tailEffectiveKinds resolves the §1/§2/§3 kind-selection rules into the set
// of kinds this call may return, or a PermissionDenied naming the first kind
// that blocked it. Caller holds at least e.mu.RLock (reads e.model.Perms via
// canTail).
func (e *Engine) tailEffectiveKinds(role string, requested []string) (map[eventstore.Kind]bool, *Err) {
	if requested == nil {
		requested = []string{string(eventstore.EventEmitted)}
	}
	if len(requested) == 0 {
		// explicit empty list (§1): "every kind this role may tail", capped to
		// what v1 can ever serve (§3).
		effective := map[eventstore.Kind]bool{}
		for k := range tailServableKinds {
			if e.canTail(role, string(k)) {
				effective[k] = true
			}
		}
		if len(effective) == 0 {
			return nil, &Err{Code: CodePermissionDenied,
				Message: fmt.Sprintf("role %s has no tail grant for any kind read_events can serve", role),
				Rule:    "tail permissions list the kinds a role may read_events",
				FixHint: "grant `tail [event.emitted]` (or another servable kind) on the role"}
		}
		return effective, nil
	}

	effective := map[eventstore.Kind]bool{}
	for _, ks := range requested {
		k := eventstore.Kind(ks)
		if !e.canTail(role, ks) {
			return nil, &Err{Code: CodePermissionDenied,
				Message: fmt.Sprintf("role %s may not tail %s", role, ks),
				Rule:    "tail permissions list the kinds a role may read_events",
				FixHint: "grant `tail [" + ks + "]` on the role, or drop it from kinds"}
		}
		if !tailServableKinds[k] {
			return nil, &Err{Code: CodePermissionDenied,
				Message: fmt.Sprintf("read_events does not serve kind %s in v1", ks),
				Rule:    "read_events v1 serves only event.emitted and non-sensitive system kinds (LLD-08 §3)",
				FixHint: "record kinds need a future tail_records grant, not read_events"}
		}
		effective[k] = true
	}
	return effective, nil
}
