package eventstore

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"time"
)

// This file implements the data shapes and the pure verification logic for
// LLD-03-upgrade-eras.md §4 ("Эры: деструктивные миграции как процедура").
// The export/import/era CLI (filesystem, journal I/O) lives in cmd/kalita;
// record.imported's fold into the projection lives in internal/engine
// (mirrors record.created — see engine.go/ImportRecord).

// EraManifest is the trailing line of an era.jsonl export: it fixes the OLD
// journal's final, sealed state so the NEW journal's era.genesis can chain to
// it — "история доказуема через разрушение" (LLD-03 §4).
type EraManifest struct {
	EraID                     string    `json:"era_id"`
	SourceFinalSeq            uint64    `json:"source_final_seq"`
	SourceFinalCheckpointHash string    `json:"source_final_checkpoint_hash"` // hex-encoded
	DefVersion                uint64    `json:"def_version"`
	NodeID                    string    `json:"node_id"`
	ExportedAt                time.Time `json:"exported_at"`
}

// EraRow is one exported record line in era.jsonl, preceding the trailing
// EraManifest line.
type EraRow struct {
	Entity     string         `json:"entity"`
	ID         string         `json:"id"`
	Values     map[string]any `json:"values"`
	StateSince time.Time      `json:"state_since"`
}

// EraGenesisPayload is the payload of the era.genesis event: the FIRST event
// appended to a new era's (empty) journal, chaining back to the old era.
type EraGenesisPayload struct {
	PrevEra EraManifest `json:"prev_era"`
}

// RecordOrigin marks a record.imported event's provenance: which era and
// which original record id it was restored from (LLD-03 §4: "провенанс
// различим").
type RecordOrigin struct {
	EraID string `json:"era_id"`
	ID    string `json:"id"`
}

// RecordImportedPayload is the payload of record.imported — deliberately NOT
// record.created: on-create automation must not fire for a restored row (an
// import is not a creation), and origin is preserved for audit.
type RecordImportedPayload struct {
	Values map[string]any `json:"values"`
	Origin RecordOrigin   `json:"origin"`
}

// VerifyEraChain is `kalita era verify`'s core check (LLD-03 §4, acceptance
// test 4, "TestEraChain"): the OLD era's journal (already sealed and
// exported) must actually end where the NEW era's genesis says it does. It
// re-verifies the OLD chain from scratch (so a byte flipped anywhere in the
// old dump — not just at the tail — is caught) and compares its final
// (seq, hash) against prevEra.
func VerifyEraChain(oldJournal []*Event, prevEra EraManifest) error {
	if err := VerifyChain(oldJournal); err != nil {
		return fmt.Errorf("old era journal fails its own hash chain: %w", err)
	}
	if len(oldJournal) == 0 {
		return fmt.Errorf("old era journal is empty: nothing to chain from")
	}
	last := oldJournal[len(oldJournal)-1]
	if last.Seq != prevEra.SourceFinalSeq {
		return fmt.Errorf("old era journal's final seq is %d, era manifest says %d",
			last.Seq, prevEra.SourceFinalSeq)
	}
	wantHash, err := hex.DecodeString(prevEra.SourceFinalCheckpointHash)
	if err != nil {
		return fmt.Errorf("era manifest source_final_checkpoint_hash is not valid hex: %w", err)
	}
	if !bytes.Equal(last.Hash, wantHash) {
		return fmt.Errorf("old era journal's final hash does not match era manifest " +
			"(the old dump was altered, or this is not the era it claims to chain from)")
	}
	return nil
}

// EraRowCounts tallies EraRow entries by entity — used by `kalita era verify`
// to cross-check "счётчики записей совпадают с манифестом" (LLD-03 §4)
// against the new node's projected record counts.
func EraRowCounts(rows []EraRow) map[string]int {
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Entity]++
	}
	return counts
}
