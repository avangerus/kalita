# RELEASE CANDIDATE — v1.0.0-rc.2 (pilot, staff-only) — ОПУБЛИКОВАН

> **Release baseline: commit `359da54`, tag `v1.0.0-rc.2`** (main + tag pushed to
> origin; одобрено владельцем 2026-08). Артефакты: Docker-образ `kalita:v1.0.0-rc.2`
> (VERSION=v1.0.0-rc.2; внутри образа `kalita version` = v1.0.0-rc.2), бинарник
> bin/kalita.exe, sha256 `3FC5E08CE0D1788FBC747FD776C76AB7E0D49289829A32ECDFC0741DADC3A2D9`.
> Version identity проверена: CLI / образ / MCP serverInfo — `v1.0.0-rc.2`.
> Переход к v1.0.0 — по критериям POST_RELEASE.md.

## Что вошло в релиз (с v1.0-rc1)

| Цикл | Изменение | Decision |
|---|---|---|
| Cycle-003 | Decide перепроверяет guard HITL (окно park→decide закрыто в ядре) | D-013 |
| A2 | identity admin boundary: setPassword/revokeCredential self-or-admin, disable admin-only, admin-role invites reserved | D-014 |
| A3 | operator trust: verify --file, TLS-флаги default false, Prometheus ca_file | D-015 |
| A4 | registry atomicity: атомарные register/EnsureActor/Redeem + invite-проекция O(1) | D-016 |
| A5 | SEC-2 anti-prompt-injection верифицирован (детектор + flag_ingest) | — |
| F1 | version identity: ldflags-инъекция + Dockerfile ARG + compose build-arg + MCP serverInfo | OPS-1 |
| F2 | pytest CI (network-free воркеры) + test_inbound.py | TST-1 |
| B1 | webhook delivery runner: KALITA_EGRESS_DOMAINS fail-closed, at-least-once + idempotent effect | D-017 |
| B2 | reference-сценарий X→approve→Y с failure-injection | Milestone B |
| RC-fix | **PII-AAD = {subject, entity.field}** с legacy-fallback (SEC-4, блокер RC Skeptic #2) | G8 не нужна (additive) |

## Test evidence

- `go vet` чистый; `go test ./... -count=1` — **348 PASS / 12 SKIP / 0 FAIL**;
  golden replay / MCP-контракт / 16 паков + 3 примера зелёные.
- **Clean-room PASS** (RC_CLEANROOM_LOG.md): свежий PG-контейнер + свежая БД,
  16 шагов — build v1.0-rc2 → bootstrap 4 акторов → REST+MCP workflow → HITL →
  webhook в mock Y (детерминированный idempotency_key) → restart/replay →
  backup → DROP DATABASE → restore → re-verify. Среда уничтожена (артефакты
  удалены — блокер RC Skeptic #3 закрыт).
- Performance: Create 6.6µs, Replay-1M 1.30s (budget 5s), PermittedSet-index 317µs.

## RC-гейты

| Гейт | Статус |
|---|---|
| Clean-room (PG, изолированно) | ✅ PASS |
| Security (Milestone G, 24 поверхности) | ✅ ACCEPT |
| Independent RC Skeptic | REJECT → 3 блокера исправлены → повторный обзор (см. RC-фиксы ниже) |
| BLOCKER count | **0** |
| Test suite | ✅ 348 PASS / 12 SKIP / 0 FAIL |
| Working tree | ✅ clean (кроме .dsh/) |

## RC Skeptic REJECT → исправления (все закрыты)

1. ~~Версия не прибита (Dockerfile dev-default, MCP serverInfo "0.1.0")~~ → MCP
   serverInfo = реальная версия (mcp.WithVersion), compose build-arg
   `VERSION: ${KALITA_VERSION:-v1.0.0}`, Makefile `make build VERSION=…`.
2. ~~SEC-4 (PII-AAD field-transplant) — открытый MUST FIX без exception~~ → FIXED:
   AAD = {subject, entity.field}, legacy-fallback для старых данных; регрессии
   TestPIIFieldTransplantBlocked + TestLocationBindingBlocksTransplant +
   TestLegacySubjectOnlyAADFallback. G8 (миграция) не нужна — фикс additive.
3. ~~Clean-room ключи/токены остались в дереве~~ → tmp/rc-cleanroom удалён.
4. Doc-фиксы: счётчики 348, README «Status: v1.0 RC», SECURITY «v1.0 RC»,
   runbook §2 (backup restore --dir) и §5 (--image), milestone-таблица,
   RELEASE_BLOCKERS (SEC-4/DOC-1 закрыты, G2/G4/G8 сняты).

## Known limitations (выпускаем с документацией)

staff-only (consumer-auth — accepted exception), prompt-injection трипвайр
(бэкстоп ABAC), webhook IDN/DNS-rebinding (pilot-grade), federation-демо не
E2E, OPS-3 upgrade-publish (post-release), EVENT-STORE §4 подписи агентов
(open question владельца). Полный список: RELEASE_BLOCKERS.md + THREAT_MODEL.md.

## Milestones

A ✅ PASS · B ✅ PROVEN · C ✅ core-exit · F 🟢 · G ✅ ACCEPT · H 🟢 ·
I ✅ clean-room PASS · J 🟢 dogfood · D/E — осознанно после A–C.

## Release actions (только после OWNER RELEASE APPROVAL)

`git push` + `git tag v1.0` (владелец); сборка: `docker compose up --build`
(VERSION=v1.0.0 инжектится compose) или `make build VERSION=v1.0`.
