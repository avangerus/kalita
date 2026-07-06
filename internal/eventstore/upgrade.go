package eventstore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// This file implements LLD-03-upgrade-eras.md §1 ("Версия узла и защита от
// downgrade"): the journal remembers the highest binary version that has ever
// run against it (node.upgraded.to), and an older binary refuses to fold a
// journal a newer one has already touched — unless the operator explicitly
// forces it. cmd/kalita wires GuardStart into `kalita serve` startup and
// appends NodeUpgraded from `kalita upgrade` step 7.

// UpgradedPayload is the payload of a node.upgraded event: written after a
// successful `kalita upgrade` (LLD-03 §2 step 7).
type UpgradedPayload struct {
	From         string `json:"from"`
	To           string `json:"to"`
	BinarySHA256 string `json:"binary_sha256"`
}

// DowngradedPayload is the payload of a node.downgraded event: written ONLY
// through an explicit --force-downgrade (LLD-03 §1) — never silently.
type DowngradedPayload struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Forced bool   `json:"forced"`
}

// GuardResult is what GuardStart found, and what the caller (kalita serve)
// must act on: refuse to start, or proceed and journal a forced downgrade.
type GuardResult struct {
	// JournalVersion is the highest node.upgraded.to ever recorded in the
	// journal (empty if the journal has never recorded an upgrade).
	JournalVersion string
	// Downgrade is true when JournalVersion is a strictly newer semver than
	// the running binary.
	Downgrade bool
	// Blocked is true when Downgrade is true and force was NOT given: the
	// caller must refuse to start (LLD-03 §1: "отказ стартовать").
	Blocked bool
	// UnknownKinds counts, by kind, events this binary's AllKinds does not
	// recognize. Populated only for a forced downgrade (Downgrade && !Blocked)
	// — LLD-03 §1: "неизвестные kind при fold — пропускаются С ПОДСЧЁТОМ и
	// итоговым warning (не молча — I-3)".
	UnknownKinds map[Kind]int
}

// GuardStart inspects a folded/foldable journal against the running binary's
// version (LLD-03 §1). It is pure (no I/O, no store dependency) so it can be
// unit-tested directly and reused by both `kalita serve` and any future
// preflight tooling.
func GuardStart(events []*Event, binaryVersion string, forceDowngrade bool) *GuardResult {
	res := &GuardResult{}
	journalVersion, found := latestUpgradedTo(events)
	if !found {
		return res
	}
	res.JournalVersion = journalVersion
	if compareSemver(journalVersion, binaryVersion) <= 0 {
		return res // binary is the same version or newer: no downgrade
	}
	res.Downgrade = true
	if !forceDowngrade {
		res.Blocked = true
		return res
	}
	res.UnknownKinds = unknownKindCounts(events)
	return res
}

// RefusalMessage renders the operator-facing explanation for a Blocked
// result: what happened, and the two ways to proceed (LLD-03 §1).
func (r *GuardResult) RefusalMessage(binaryVersion string) string {
	return fmt.Sprintf(
		"refusing to start: this journal was last run by node.upgraded version %s, "+
			"this binary is %s (older).\n"+
			"  either run the %s binary, or restart with --force-downgrade to proceed anyway.\n"+
			"  --force-downgrade will skip any event kinds this binary does not understand "+
			"(counted and printed as a warning, never silently) and record a node.downgraded event.",
		r.JournalVersion, binaryVersion, r.JournalVersion)
}

// VerifyTailFromLastCheckpoint is the "быстрый" preflight check of LLD-03 §2
// step 1: rather than re-hashing the entire history on every upgrade, it
// verifies only the events AFTER the last checkpoint.sealed, anchored to that
// checkpoint's own recorded (seq, hash). With no checkpoint yet, it falls
// back to a full VerifyChain (nothing cheaper is possible).
func VerifyTailFromLastCheckpoint(events []*Event) error {
	lastCkpt := -1
	for i, e := range events {
		if e.Kind == CheckpointSealed {
			lastCkpt = i
		}
	}
	if lastCkpt == -1 {
		return VerifyChain(events)
	}
	var p CheckpointPayload
	if err := json.Unmarshal(events[lastCkpt].Payload, &p); err != nil {
		return fmt.Errorf("checkpoint at seq %d: bad payload: %w", events[lastCkpt].Seq, err)
	}
	anchorIdx := -1
	for i, e := range events {
		if e.Seq == p.SealedSeq {
			anchorIdx = i
			break
		}
	}
	if anchorIdx == -1 {
		return fmt.Errorf("checkpoint at seq %d seals missing seq %d", events[lastCkpt].Seq, p.SealedSeq)
	}
	if !bytes.Equal(events[anchorIdx].Hash, p.SealedHash) {
		return fmt.Errorf("checkpoint at seq %d: sealed hash differs from journal at seq %d", events[lastCkpt].Seq, p.SealedSeq)
	}
	tail := events[anchorIdx:]
	prev, prevSeq := tail[0].Hash, tail[0].Seq
	for _, e := range tail[1:] {
		if e.Seq != prevSeq+1 {
			return &ChainBreakError{Seq: e.Seq, Reason: fmt.Sprintf("sequence gap: expected %d", prevSeq+1)}
		}
		if !bytes.Equal(e.PrevHash, prev) {
			return &ChainBreakError{Seq: e.Seq, Reason: "prev_hash mismatch"}
		}
		want, err := computeHash(prev, e)
		if err != nil {
			return &ChainBreakError{Seq: e.Seq, Reason: err.Error()}
		}
		if !bytes.Equal(e.Hash, want) {
			return &ChainBreakError{Seq: e.Seq, Reason: "hash mismatch (event content altered)"}
		}
		prev, prevSeq = e.Hash, e.Seq
	}
	return nil
}

// StartedPayload is the payload of node.started (written by every `kalita
// serve` boot).
type StartedPayload struct {
	Version string `json:"version"`
}

// CurrentRunningVersion is the version an operator would reasonably call
// "what this journal is currently running": the latest node.upgraded.to if
// this journal has ever been upgraded, else the most recent node.started
// version (the original genesis binary). "unknown" if neither is present —
// used by `kalita upgrade` to name its pg_dump backup file (LLD-03 §2 step 2:
// "имя = ts+from-ver").
func CurrentRunningVersion(events []*Event) string {
	if v, ok := latestUpgradedTo(events); ok {
		return v
	}
	best := ""
	for _, ev := range events {
		if ev.Kind != NodeStarted {
			continue
		}
		var p StartedPayload
		if json.Unmarshal(ev.Payload, &p) == nil && p.Version != "" {
			best = p.Version
		}
	}
	if best == "" {
		return "unknown"
	}
	return best
}

func latestUpgradedTo(events []*Event) (string, bool) {
	best := ""
	found := false
	for _, ev := range events {
		if ev.Kind != NodeUpgraded {
			continue
		}
		var p UpgradedPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil || p.To == "" {
			continue
		}
		if !found || compareSemver(p.To, best) > 0 {
			best = p.To
			found = true
		}
	}
	return best, found
}

// unknownKindCounts counts events whose Kind is not in AllKinds — the set
// this binary's engine can fold. Used only on a forced downgrade: an older
// binary may not recognize kinds a newer one introduced.
func unknownKindCounts(events []*Event) map[Kind]int {
	known := make(map[Kind]bool, len(AllKinds))
	for _, k := range AllKinds {
		known[k] = true
	}
	counts := map[Kind]int{}
	for _, ev := range events {
		if !known[ev.Kind] {
			counts[ev.Kind]++
		}
	}
	return counts
}

// --- minimal semver comparison -------------------------------------------
//
// The binary's version string (ldflags, `kalita version`) is a plain semver,
// optionally with a "-suffix" (e.g. "0.1.0-dev"): major.minor.patch compare
// numerically; a build with NO suffix is considered newer than the same
// major.minor.patch WITH a suffix (release > pre-release), matching common
// semver precedence without pulling in a library (go.mod is out of scope for
// this change).

type semver struct {
	major, minor, patch int
	suffix              string
}

func parseSemver(s string) semver {
	s = strings.TrimPrefix(s, "v")
	core := s
	suffix := ""
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		core = s[:i]
		suffix = s[i+1:]
	}
	parts := strings.SplitN(core, ".", 3)
	get := func(i int) int {
		if i >= len(parts) {
			return 0
		}
		n, _ := strconv.Atoi(parts[i])
		return n
	}
	return semver{major: get(0), minor: get(1), patch: get(2), suffix: suffix}
}

// compareSemver returns -1, 0, 1 as a < b, a == b, a > b.
func compareSemver(a, b string) int {
	sa, sb := parseSemver(a), parseSemver(b)
	if sa.major != sb.major {
		return cmpInt(sa.major, sb.major)
	}
	if sa.minor != sb.minor {
		return cmpInt(sa.minor, sb.minor)
	}
	if sa.patch != sb.patch {
		return cmpInt(sa.patch, sb.patch)
	}
	switch {
	case sa.suffix == sb.suffix:
		return 0
	case sa.suffix == "": // release beats any pre-release suffix
		return 1
	case sb.suffix == "":
		return -1
	default:
		return strings.Compare(sa.suffix, sb.suffix)
	}
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
