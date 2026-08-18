# testdata/golden — fold-compatibility fixtures

Each `<ver>/` directory is a frozen historical journal plus the projections it
must fold into: `pack/*.dsl` (the genesis pack — self-contained, does not
depend on `packs/*`), `journal.jsonl` (one `eventstore.Event` per line, fixed
byte-for-byte once committed — never hand-edited, never reformatted),
`expected/{records,tasks,approvals,actors}.json` (canonical, sorted snapshots
of the projections that journal must reproduce; uuids inside are normalized to
`id#N` in order of first appearance, per `docs/lld/LLD-03-upgrade-eras.md` §3,
so the comparison never depends on injecting a deterministic uuid generator
into the kernel).

`TestGoldenReplay` (`tests/golden_replay_test.go`) is the gate: it replays
every `<ver>/journal.jsonl` with the *current* binary's fold logic and asserts
the result still matches `<ver>/expected/`. Run it directly with
`make golden-test`, or as part of the full suite (`go test ./...` / `make test`
already include `tests/...`).

`v0.1` is the pre-LLD-01 baseline (PR-0 of the release track): it fixes v0's
fold behavior before any upgrade/era machinery exists, so every later change
to fold logic is provably additive against it.

When a fold change is ADDITIVE (a new projection field appears, an old one
never disappears or changes meaning — e.g. `created_at` on tasks, folded from
the event's own TS), the frozen `journal.jsonl` stays byte-for-byte untouched
and only `expected/` is re-derived from it with the current binary, via
`WriteExpected` in `cmd/goldengen/goldenfmt` (a one-off test run, never
committed). A change that alters or drops an existing field is NOT additive
and must be treated as a version cut or a bug instead.

## Regenerating

Golden directories are frozen history — do not regenerate an existing `<ver>`
casually; that redefines what "that version's behavior" means and defeats the
point of the gate. A new version is cut at release time, from the (possibly
extended) scenario in `cmd/goldengen`:

```
make golden VER=v0.2          # go run ./cmd/goldengen -ver v0.2
make golden VER=v0.1 FORCE=1  # intentional re-cut of an existing fixture
```

`cmd/goldengen` refuses to overwrite an existing `<ver>/journal.jsonl` unless
`-force` (`FORCE=1`) is passed. Determinism of *generation* is not required
(uuids differ run to run — see LLD-03 §3); once written, a golden directory is
committed as-is and only ever replayed.
