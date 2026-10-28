# kalita — a handful of entry points for things CI and docs reference by name.
# Not a build system: `go build`/`go test` remain the source of truth: see
# .github/workflows/ci.yml.

.PHONY: test
test:
	go test ./... -count=1 -cover

# build compiles the node with its release version injected (OPS-1, Cycle-F1):
#   make build VERSION=v1.0.0        # explicit
#   make build                       # falls back to git describe (tag or dev)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0-dev)
.PHONY: build
build:
	go build -ldflags="-X main.version=$(VERSION)" -o bin/kalita ./cmd/kalita

.PHONY: golden-test
golden-test:
	go test ./tests/... -run TestGoldenReplay -v

# golden regenerates one testdata/golden/<ver>/ fixture (docs/lld/
# LLD-03-upgrade-eras.md §3/§7, PR-0). Golden directories are frozen history
# once committed: regenerating an EXISTING version rewrites what "that
# version's behavior" means, defeating the point of the gate. This target is
# for a NEW version at release time (VER=v0.2 ...); goldengen itself refuses
# to overwrite an existing directory unless FORCE=1.
#
#   make golden VER=v0.2
#   make golden VER=v0.1 FORCE=1   # intentional re-cut of an existing fixture
VER  ?= v0.1
FORCE ?=
.PHONY: golden
golden:
	go run ./cmd/goldengen -ver $(VER) $(if $(FORCE),-force,)

# passport runs the node performance passport benchmarks (docs/HLD-V1.md §H10,
# RELEASE-1.0 (git 5447fc5) N6 "не переделывать, а измерить" / N9 "измеримые пороги
# вместо веры"): P1 pilot mix, P2 bulk create, P3 journal replay, P4 auth
# storm. The numbers are the v0 baseline this release ships in release notes;
# regressing them >20% is the signal N6 asks for, even though nothing enforces
# that threshold yet (see internal/engine/passport_bench_test.go).
#
# Gated behind -tags=passport on purpose: `go test -bench=Bench ./internal/engine`
# is already the PR-gate bench smoke step in .github/workflows/ci.yml, a cheap
# order-of-magnitude check over the existing benchmarks. These profiles are
# heavier and informational — the build tag keeps them out of that sweep
# without touching its regex (internal/webui/embed_*.go already uses this
# pattern for opt-in-only code). They run here and in the nightly `passport`
# CI job instead.
#
# -benchtime is a fixed iteration count (Nx), not a duration, for every
# profile except P3: a fixed count makes the workload SIZE reproducible
# release over release — only wall-clock time should vary with the machine.
# P3 seeds its journal once per scale (100k / 1M events) outside the timed
# loop, so a small fixed count there re-times replay without re-seeding.
.PHONY: passport
passport:
	@echo "=== node performance passport ($$(go version)) ==="
	@echo "=== P1 pilot mix: create/transition/list/dashboard/tick over a 1000-ticket node ==="
	go test -tags=passport -run='^$$' -bench=BenchmarkPassportP1 -benchtime=300x -benchmem -timeout=10m ./internal/engine
	@echo "=== P2 bulk create: 10k records of one entity ==="
	go test -tags=passport -run='^$$' -bench=BenchmarkPassportP2 -benchtime=20x -benchmem -timeout=10m ./internal/engine
	@echo "=== P3 journal replay: engine.New() over a 100k- and 1M-event synthetic journal ==="
	go test -tags=passport -run='^$$' -bench=BenchmarkPassportP3 -benchtime=3x -benchmem -timeout=15m ./internal/eventstore
	@echo "=== P4 auth storm: identity.Authenticate under b.RunParallel, 100 and 1000 actors ==="
	go test -tags=passport -run='^$$' -bench=BenchmarkPassportP4 -benchtime=5000x -benchmem -timeout=10m ./internal/engine
	@echo "=== passport complete: the ns/op / allocs-per-op / derived *-per-s lines above are the v0 baseline for release notes ==="
