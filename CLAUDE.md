# kalita — instructions for coding agents

Read first: [docs/STATUS.md](docs/STATUS.md) (current state) and
[docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) (build, run, test). The doc map is
[docs/README.md](docs/README.md).

## Which document wins

- New development follows the N1 target architecture: `docs/next/` (product contract → HLD →
  N-ADR → LLD → delivery tasks). A task never silently widens the HLD.
- Existing behavior (DSL grammar, REST/MCP API, journal format, released versions) is described by
  the reference docs in `docs/` and is preserved unless an N-ADR says otherwise.

## Before you push

All of these must pass (they mirror `.github/workflows/ci.yml`):

```bash
go vet ./... && test -z "$(gofmt -l cmd internal tests)" && go build ./...
bash scripts/check-automation-error-swallow.sh
go test ./... -count=1
py -m pytest workers -q            # `python3` on Linux/macOS
```

PG-backed journal tests run only with `KALITA_TEST_PG_DSN` set (see DEVELOPMENT.md).

## Rules

- `docs/MCP-CONTRACT-v1.md` is generated (`kalita mcp contract`); `testdata/golden/` is frozen
  history; `frameworks/*.json` are sha256-pinned in `frameworks/manifest.json`.
- Journal format changes are additive or go through LLD-03 (eras + golden fixtures).
- No secrets, scratch files, logs, binaries or research downloads in the repo — it is public.
- One topic, one document: update the existing doc and delete what it supersedes; git keeps history.
- Commit style: `type(scope): summary` (`feat`, `fix`, `docs`, `test`, `chore`).
