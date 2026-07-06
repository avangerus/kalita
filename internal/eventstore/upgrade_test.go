package eventstore

import (
	"context"
	"encoding/json"
	"testing"
)

// TestDowngradeRefusal is acceptance test 2 of LLD-03-upgrade-eras.md §6: a
// journal with node.upgraded(to=X+1) makes binary X refuse to start;
// --force-downgrade starts it, unknown kinds are counted in a warning, and a
// node.downgraded event is recorded.
func TestDowngradeRefusal(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore(testClock())

	upgraded, _ := json.Marshal(UpgradedPayload{From: "0.1.0", To: "0.2.0", BinarySHA256: "deadbeef"})
	if _, err := s.Append(ctx, AppendInput{
		Actor: Actor{Type: ActorSystem, ID: "node"}, Kind: NodeUpgraded,
		Payload: upgraded, SchemaV: CurrentSchemaV,
	}); err != nil {
		t.Fatal(err)
	}
	// An event of a kind this (older) binary does not recognize — simulates
	// something a future 0.2.0 release introduced.
	if _, err := s.Append(ctx, AppendInput{
		Actor: Actor{Type: ActorSystem, ID: "node"}, Kind: Kind("future.thing"),
		SchemaV: CurrentSchemaV,
	}); err != nil {
		t.Fatal(err)
	}

	events, err := s.All(ctx)
	if err != nil {
		t.Fatal(err)
	}

	// Older binary (0.1.5), no force: refused.
	res := GuardStart(events, "0.1.5", false)
	if !res.Blocked {
		t.Fatalf("binary 0.1.5 must be blocked by a journal last run at 0.2.0, got %+v", res)
	}
	if res.JournalVersion != "0.2.0" {
		t.Fatalf("want journal version 0.2.0, got %q", res.JournalVersion)
	}
	if msg := res.RefusalMessage("0.1.5"); msg == "" {
		t.Fatal("RefusalMessage must be non-empty for an operator to act on")
	}

	// Same binary, forced: starts, counts the unknown kind, does not block.
	res = GuardStart(events, "0.1.5", true)
	if res.Blocked {
		t.Fatal("--force-downgrade must not block")
	}
	if !res.Downgrade {
		t.Fatal("--force-downgrade on an older binary must still report Downgrade=true so the caller journals node.downgraded")
	}
	if res.UnknownKinds[Kind("future.thing")] != 1 {
		t.Fatalf("want 1 unknown 'future.thing' event counted, got %v", res.UnknownKinds)
	}

	// The caller (kalita serve --force-downgrade) journals node.downgraded.
	downgraded, _ := json.Marshal(DowngradedPayload{From: res.JournalVersion, To: "0.1.5", Forced: true})
	ev, err := s.Append(ctx, AppendInput{
		Actor: Actor{Type: ActorSystem, ID: "node"}, Kind: NodeDowngraded,
		Payload: downgraded, SchemaV: CurrentSchemaV,
	})
	if err != nil {
		t.Fatal(err)
	}
	var p DowngradedPayload
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if p.From != "0.2.0" || p.To != "0.1.5" || !p.Forced {
		t.Fatalf("node.downgraded payload wrong: %+v", p)
	}

	// A binary equal to or newer than the journal's high-water mark is never blocked.
	if res := GuardStart(events, "0.2.0", false); res.Blocked || res.Downgrade {
		t.Fatalf("binary at the journal's own version must not be treated as a downgrade: %+v", res)
	}
	if res := GuardStart(events, "0.3.0", false); res.Blocked || res.Downgrade {
		t.Fatalf("a newer binary must not be treated as a downgrade: %+v", res)
	}

	// A journal that never saw an upgrade never blocks anyone.
	fresh := NewMemStore(testClock())
	freshEvents, _ := fresh.All(ctx)
	if res := GuardStart(freshEvents, "0.0.1", false); res.Blocked || res.Downgrade {
		t.Fatalf("a fresh journal (no node.upgraded) must never block: %+v", res)
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "1.0.1", -1},
		{"1.2.0", "1.10.0", -1}, // numeric, not lexicographic
		{"2.0.0", "1.9.9", 1},
		{"1.0.0", "1.0.0-dev", 1}, // a release beats its own pre-release
		{"1.0.0-dev", "1.0.0", -1},
		{"v1.2.3", "1.2.3", 0}, // an optional "v" prefix does not matter
	}
	for _, c := range cases {
		if got := compareSemver(c.a, c.b); got != c.want {
			t.Errorf("compareSemver(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestVerifyTailFromLastCheckpoint(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore(testClock())
	for i := 0; i < 3; i++ {
		if _, err := s.Append(ctx, AppendInput{Actor: Actor{Type: ActorHuman, ID: "a"}, Kind: RecordCreated}); err != nil {
			t.Fatal(err)
		}
	}
	seq, hash, err := s.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(CheckpointPayload{SealedSeq: seq, SealedHash: hash})
	if _, err := s.Append(ctx, AppendInput{Actor: Actor{Type: ActorSystem, ID: "node"}, Kind: CheckpointSealed, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := s.Append(ctx, AppendInput{Actor: Actor{Type: ActorHuman, ID: "a"}, Kind: RecordCreated}); err != nil {
			t.Fatal(err)
		}
	}
	events, err := s.All(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyTailFromLastCheckpoint(events); err != nil {
		t.Fatalf("unaltered tail must verify: %v", err)
	}
	tampered := make([]*Event, len(events))
	copy(tampered, events)
	last := *tampered[len(tampered)-1]
	last.Hash = append([]byte(nil), last.Hash...)
	last.Hash[0] ^= 0xFF
	tampered[len(tampered)-1] = &last
	if err := VerifyTailFromLastCheckpoint(tampered); err == nil {
		t.Fatal("a tampered event after the checkpoint must fail tail verify")
	}

	// no checkpoint at all: falls back to a full verify, which still passes
	// on a clean chain.
	fresh := NewMemStore(testClock())
	for i := 0; i < 2; i++ {
		if _, err := fresh.Append(ctx, AppendInput{Actor: Actor{Type: ActorHuman, ID: "a"}, Kind: RecordCreated}); err != nil {
			t.Fatal(err)
		}
	}
	freshEvents, _ := fresh.All(ctx)
	if err := VerifyTailFromLastCheckpoint(freshEvents); err != nil {
		t.Fatalf("no-checkpoint fallback to full verify must pass on a clean chain: %v", err)
	}
}

func TestCurrentRunningVersion(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore(testClock())
	if v := CurrentRunningVersion(mustAll(t, s)); v != "unknown" {
		t.Fatalf("empty journal must report unknown, got %q", v)
	}
	started, _ := json.Marshal(StartedPayload{Version: "0.1.0"})
	if _, err := s.Append(ctx, AppendInput{Actor: Actor{Type: ActorSystem, ID: "node"}, Kind: NodeStarted, Payload: started}); err != nil {
		t.Fatal(err)
	}
	if v := CurrentRunningVersion(mustAll(t, s)); v != "0.1.0" {
		t.Fatalf("want 0.1.0 from node.started, got %q", v)
	}
	upgraded, _ := json.Marshal(UpgradedPayload{From: "0.1.0", To: "0.2.0"})
	if _, err := s.Append(ctx, AppendInput{Actor: Actor{Type: ActorSystem, ID: "node"}, Kind: NodeUpgraded, Payload: upgraded}); err != nil {
		t.Fatal(err)
	}
	if v := CurrentRunningVersion(mustAll(t, s)); v != "0.2.0" {
		t.Fatalf("want 0.2.0 from node.upgraded once it exists, got %q", v)
	}
}
