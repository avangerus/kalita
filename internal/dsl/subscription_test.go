package dsl

import "testing"

// LLD-21 §5 (PR-2): the `subscription` top-level block — a durable, born-
// governed event subscription declared in the pack. These tests cover the
// compiler's contract only (test 5 of LLD-21 §8): the block parses into
// typed AST/model data, `on event` validates like automation's trigger,
// `sink worker <Role>` validates against declared (agent) roles, and a pack
// without `subscription` compiles exactly as before (I-1). The durable
// delivery mechanism (subscribe.py) and the egress whitelist for `sink
// endpoint` are out of scope here (PR-1 and PR-3 respectively).

const subscriptionFixture = `pack people
version 1

event OutOfOffice:
    employee: string

event EmployeeDeactivated:
    employee: string

entity Coverage:
    employee: string required

roles:
    Owner
    CoverageBot agent

permissions:
    Owner:
        full [Coverage]
    CoverageBot:
        read [Coverage]
        deny [delete Coverage.*]
`

// TestSubscriptionDeclParses: the happy path — `subscription` parses into
// typed model data, listing both events and the worker sink.
func TestSubscriptionDeclParses(t *testing.T) {
	src := subscriptionFixture + `
subscription CoverageSync:
    on event OutOfOffice, EmployeeDeactivated
    sink worker CoverageBot
`
	m, errs := Compile(map[string]string{"p.dsl": src})
	for _, e := range errs {
		t.Errorf("unexpected: %v", e)
	}
	if t.Failed() {
		t.FailNow()
	}
	if len(m.Subscriptions) != 1 {
		t.Fatalf("want 1 subscription, got %d: %+v", len(m.Subscriptions), m.Subscriptions)
	}
	sub := m.Subscriptions[0]
	if sub.Name != "CoverageSync" {
		t.Errorf("name = %q, want CoverageSync", sub.Name)
	}
	if len(sub.Events) != 2 || sub.Events[0] != "OutOfOffice" || sub.Events[1] != "EmployeeDeactivated" {
		t.Errorf("events = %+v, want [OutOfOffice EmployeeDeactivated]", sub.Events)
	}
	if sub.SinkKind != "worker" || sub.SinkWorker != "CoverageBot" {
		t.Errorf("sink = kind=%q worker=%q, want kind=worker worker=CoverageBot", sub.SinkKind, sub.SinkWorker)
	}
	if sub.External {
		t.Error("a worker sink must not be marked External")
	}
}

// TestSubscriptionUnknownEventFails: `on event` referencing an event the pack
// never declared is a compile error, exactly like automation's `on event`.
func TestSubscriptionUnknownEventFails(t *testing.T) {
	src := subscriptionFixture + `
subscription CoverageSync:
    on event NoSuchEvent
    sink worker CoverageBot
`
	errs := compileOne(src)
	if errWithCode(errs, EUnknownEvent) == nil {
		t.Fatalf("on event NoSuchEvent must be E040_UNKNOWN_EVENT, got: %v", errs)
	}
}

// TestSubscriptionBlockOptional: a pack without any `subscription` block
// compiles exactly as before — additive grammar (I-1).
func TestSubscriptionBlockOptional(t *testing.T) {
	m, errs := Compile(map[string]string{"p.dsl": subscriptionFixture})
	for _, e := range errs {
		t.Errorf("unexpected: %v", e)
	}
	if len(m.Subscriptions) != 0 {
		t.Fatalf("want no subscriptions, got %+v", m.Subscriptions)
	}
}

// TestSubscriptionSinkEndpointIsExternal: `sink endpoint "..."` parses, is
// captured verbatim, and is marked External — the egress whitelist gate
// itself is PR-3 (SECURITY rule 3), not enforced here.
func TestSubscriptionSinkEndpointIsExternal(t *testing.T) {
	src := subscriptionFixture + `
subscription NotifyExternal:
    on event OutOfOffice
    sink endpoint "https://example.com/hook"
`
	m, errs := Compile(map[string]string{"p.dsl": src})
	for _, e := range errs {
		t.Errorf("unexpected: %v", e)
	}
	if t.Failed() {
		t.FailNow()
	}
	sub := m.Subscriptions[0]
	if sub.SinkKind != "endpoint" || sub.SinkURL != "https://example.com/hook" {
		t.Errorf("sink = kind=%q url=%q", sub.SinkKind, sub.SinkURL)
	}
	if !sub.External {
		t.Error("sink endpoint must be marked External")
	}
}

// TestSubscriptionSinkWorkerUnknownRoleFails: `sink worker <Role>` must name
// a role declared in this pack.
func TestSubscriptionSinkWorkerUnknownRoleFails(t *testing.T) {
	src := subscriptionFixture + `
subscription CoverageSync:
    on event OutOfOffice
    sink worker NoSuchRole
`
	errs := compileOne(src)
	if errWithCode(errs, EUnknownRole) == nil {
		t.Fatalf("sink worker NoSuchRole must be E015_UNKNOWN_ROLE, got: %v", errs)
	}
}

// TestSubscriptionSinkWorkerNonAgentRoleFails: the sink worker's role is the
// real barrier (I-2, least-privilege) — it must be declared with the `agent`
// marker, not a plain human role.
func TestSubscriptionSinkWorkerNonAgentRoleFails(t *testing.T) {
	src := subscriptionFixture + `
subscription CoverageSync:
    on event OutOfOffice
    sink worker Owner
`
	errs := compileOne(src)
	if errWithCode(errs, ENotAgentRole) == nil {
		t.Fatalf("sink worker Owner (not an agent role) must be E031_ROLE_IS_NOT_AGENT, got: %v", errs)
	}
}

// TestSubscriptionMissingSinkFails: a subscription with no sink line at all
// is a structured error, not a silent no-op sink (I-3).
func TestSubscriptionMissingSinkFails(t *testing.T) {
	src := subscriptionFixture + `
subscription CoverageSync:
    on event OutOfOffice
`
	errs := compileOne(src)
	if errWithCode(errs, EBadSubscription) == nil {
		t.Fatalf("subscription without a sink must be E057_BAD_SUBSCRIPTION, got: %v", errs)
	}
}

// TestSubscriptionMissingOnEventFails: a subscription with no `on event` line
// is equally a structured error.
func TestSubscriptionMissingOnEventFails(t *testing.T) {
	src := subscriptionFixture + `
subscription CoverageSync:
    sink worker CoverageBot
`
	errs := compileOne(src)
	if errWithCode(errs, EBadSubscription) == nil {
		t.Fatalf("subscription without on event must be E057_BAD_SUBSCRIPTION, got: %v", errs)
	}
}

// TestSubscriptionDuplicateNameFails: two subscriptions sharing a name are
// rejected, not merged.
func TestSubscriptionDuplicateNameFails(t *testing.T) {
	src := subscriptionFixture + `
subscription CoverageSync:
    on event OutOfOffice
    sink worker CoverageBot

subscription CoverageSync:
    on event EmployeeDeactivated
    sink worker CoverageBot
`
	errs := compileOne(src)
	if errWithCode(errs, EBadSubscription) == nil {
		t.Fatalf("duplicate subscription name must be E057_BAD_SUBSCRIPTION, got: %v", errs)
	}
}
