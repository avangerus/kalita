// Package transport holds the ONE implementation of the pieces every kalita
// transport (REST in internal/api, MCP in internal/mcp) must build the exact
// same way: the eventstore.Actor a resolved bearer token maps to, and the
// request DTOs for the canonical mutating operations (create/update/act).
//
// RELEASE-1.0 N8 ("два транспорта — не дать REST и MCP разъехаться",
// docs/lld/LLD-11-transport-parity.md): the business logic in internal/engine
// was already the single choke point (I-2) — engine.Create/Update/Act build
// the journal event, and both transports already call those same functions
// directly with no event-JSON of their own. What WAS duplicated, byte-for-byte,
// was the code that turns a resolved identity.ActorInfo into an
// eventstore.Actor, and the create/update/act request field sets
// (values/basis/idempotency_key, action/basis/idempotency_key) — each
// re-declared once per transport, free to drift silently the next time
// someone edits one copy and forgets the other. This package is where that
// code now lives once; internal/api and internal/mcp import it instead of
// re-declaring it. engine remains the only place that decides permissions or
// mutates state — this package only assembles the call and shapes the wire
// request, exactly as LLD-11 §1 specifies.
package transport

import (
	"github.com/avangerus/kalita/internal/eventstore"
	"github.com/avangerus/kalita/internal/identity"
)

// ActorFromInfo builds the eventstore.Actor the engine sees from a resolved
// identity.Registry.Authenticate result. This is the ONE mapping from
// registry state to actor identity — REST's bearer-token path
// (internal/api/server.go's resolveActor) and MCP's bearer-token path
// (internal/mcp/server.go's authenticate) both call this instead of each
// re-declaring the same three lines. Before N8 they had already drifted to
// the point of being copy-pasted rather than shared; a future field added to
// ActorInfo (or to how Attrs are carried) now only needs to be taught here
// once.
func ActorFromInfo(info *identity.ActorInfo) eventstore.Actor {
	a := eventstore.Actor{Type: info.Type, ID: info.ID, Role: info.Role}
	if info.Meta != nil {
		a.Attrs = info.Meta.Attrs
	}
	return a
}

// MutationRequest is the wire shape both transports parse for create_record
// and update_record: the values to write, the provenance basis, and an
// idempotency key. entity/id are NOT here — REST carries them in the URL
// path, MCP carries them as sibling JSON fields alongside this struct (an MCP
// request struct embeds MutationRequest to get Values/Basis/IdempotencyKey
// promoted into its own JSON unmarshal for free, entity/id declared once
// beside it). Kept minimal on purpose: this is a wire DTO, not a domain type;
// the engine's own Create/Update signatures remain the source of truth for
// what a mutation actually needs.
type MutationRequest struct {
	Values         map[string]any    `json:"values"`
	Basis          *eventstore.Basis `json:"basis"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
}

// ActionRequest is the wire shape both transports parse for the `act`
// operation: which transition to fire, its basis, and an idempotency key.
// Same embedding convention as MutationRequest — entity/id live beside it,
// not inside it.
type ActionRequest struct {
	Action         string            `json:"action"`
	Basis          *eventstore.Basis `json:"basis"`
	IdempotencyKey string            `json:"idempotency_key,omitempty"`
}
