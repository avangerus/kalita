# kalita — a handful of entry points for things CI and docs reference by name.
# Not a build system: `go build`/`go test` remain the source of truth: see
# .github/workflows/ci.yml.

.PHONY: test
test:
	go test ./... -count=1 -cover

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
