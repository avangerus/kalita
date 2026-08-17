# RELEASE BLOCKERS — классификация проблем на пути к первому пилоту

> Ведётся по правилу миссии: каждая проблема — BLOCKER / MUST FIX / KNOWN LIMITATION /
> POST-RELEASE. BLOCKER запрещает релиз безусловно; MUST FIX — закрыть до pilot release,
> если нет явного exception владельца; KNOWN LIMITATION — можно выпустить с документацией;
> POST-RELEASE — не мешает release target.
> **Release target: v1.0** (финализация тега v1.0-rc1; in-code version = 0.1.0-dev →
> ldflags-инъекция — см. MUST FIX OPS-1).
> Источник фактов: SYSTEM_TRUTH.md (реестр техдолга), текущий код/тесты.

## BLOCKER (релиз запрещён)

_На момент Cycle-003 — пусто. Нет unresolved P0; каждый новый P0/P1-critical заносится
сюда немедленно._

## MUST FIX (до пилота, если нет явного exception)

| ID | Проблема | Класс | Evidence | Ответственный цикл |
|---|---|---|---|---|
| SEC-1 | ~~setPassword без self/Owner-гейта — impersonation-класс~~ → **FIXED Cycle-A2** (D-014: self-or-admin setPassword/revokeCredential, admin-only disable, admin-role invites reserved) | P1 SECURITY | TECHDEBT.md:258-271; internal/api/login.go:23-42; admin_boundary_test.go | ✅ закрыт |
| SEC-2 | ~~анти-prompt-injection на ingest — только трипвайр~~ → **ЗАКРЫТ Cycle-A5** (детектор на KnowVault ingest + core `flag_ingest` журналирование; верифицировано Go-тестами) | P1 SECURITY | TECHDEBT.md:155-171; tools.go:98; indexer.py:601-620 | ✅ закрыт (residual: block-vs-tripwire = KNOWN LIMITATION) |
| SEC-3 | ~~`upgrade --insecure-skip-tls-verify` default true; prometheus insecure_skip_verify~~ → **FIXED Cycle-A3** (default false + ca_file/server_name; D-015) | P2 SECURITY | upgrade.go:102; deploy/prometheus/prometheus.yml:15 | ✅ закрыт |
| SEC-4 | PII-AAD = только subjectID (поле можно перенести) — перед пилотом с чужими персданными | P2 SECURITY | pii.go:111 vs LLD-16 §1; G8 (gate владельца — миграция ключей) | G8 (HUMAN_GATE) |
| SEC-5 | consumer-auth отсутствует (portal для внешних пользователей) — **РЕШЕНИЕ ВЛАДЕЛЬЦА: staff-only pilot; consumer-auth = KNOWN LIMITATION** | P1 SECURITY | TECHDEBT.md:144 | ✅ accepted exception (owner, 2026-08) |
| INT-1 | webhook-доставка: задача есть, runner ноды нет — egress мёртв | P1 MISSING CAPABILITY | engine/automation.go:290-294; D-011; G2 (gate владельца — семантика whitelist) | G2 (HUMAN_GATE) |
| INT-2 | Reliability-семантика доставки не определена (at-least-once? idempotency? DLQ?) | P1 DESIGN | Milestone B миссии | G2-цикл |
| OPS-1 | ~~in-code `version = "0.1.0-dev"` не связан с тегом; upgrade-гейт сравнивает по нему~~ → **FIXED Cycle-F1** (Dockerfile ARG VERSION + Makefile build таргет; `-X main.version` инъекция; проверено `kalita version`) | P2 BUG | cmd/kalita/main.go:31; Dockerfile:6; Makefile | ✅ закрыт |
| OPS-2 | ~~`verify` читает только PG, а backup советует файл journal.jsonl~~ → **FIXED Cycle-A3** (verify --file через loadVerifiedBackup; D-015) | P2 BUG | backup.go:136-137 vs main.go:89-94 | ✅ закрыт |
| OPS-3 | `kalita upgrade` неработоспособен из коробки (нет publish/cosign) | P2 MISSING | upgrade.go:108-111; ci.yml без release | Milestone F |
| TST-1 | CI не гоняет Python-воркеры; inbound.py без теста | P2 MISSING | .github/workflows/ci.yml; workers/connectors/ | ✅ **готово в дереве (Cycle-F2)**: test_inbound.py + pytest-джоба (network-free: connectors/lib/perimeter); активация после G4-коммита/push |
| IDN-1 | ~~LLD-10 PR-1/PR-2: Register не атомарен, инвайты full-scan~~ → **FIXED Cycle-A4** (D-016: mu-сериализация + invites-проекция + атомарный Redeem) | P2 MISSING | registry.go:138-160; invites.go:101-129 | ✅ закрыт |
| DOC-1 | .env.example не описывает KALITA_PG_DSN/KALITA_SECRETS_FILE | P3 DOC | .env.example | docs-цикл |

## KNOWN LIMITATION (выпускаем с документацией)

| ID | Проблема | Evidence | Где задокументировано |
|---|---|---|---|
| LIM-1 | `on update`-триггеры не срабатывают после act-переходов | runEventTriggers только в records.go | SYSTEM_TRUTH Flow D |
| LIM-2 | UI без браузерных тестов (smoke — G10) | tests/webui_static_test.go:3 | G10 |
| LIM-3 | O(журнала) на супервизорные операции (lastMutation и т.п.) | revert.go:388/329 | TECHDEBT |
| LIM-4 | PG-путь прогнан локально только в CI; локальные прогоны — mem-стор | skip-тесты | SYSTEM_TRUTH «Что НЕ проверено» |
| LIM-5 | i18n-рендер локалей не проверен | label_test | SYSTEM_TRUTH DSL matrix |
| LIM-6 | federation — уровень воркеров (bridge), не ядра; демо не прогнано end-to-end | deploy/federation-demo | SYSTEM_TRUTH Flow G |
| LIM-7 | findTransition==nil в Decide — legacy fail-open synthetic-переход (логируется + метрика) | D-013 | DECISIONS D-013 |
| LIM-8 | prompt-injection периметр — трипвайр (детектор+flag_ingest), НЕ блокирует ingest; бэкстоп = ABAC | TECHDEBT SEC-2 | TECHDEBT.md |

## POST-RELEASE

| ID | Проблема | Evidence |
|---|---|---|
| PR-1 | мёртвые kind-константы (definition.validated/reverted, projection.rebuilt, actor.role_changed) | eventstore/event.go |
| PR-2 | LLD-18 §2 эпохи ключа узла | checkpoint.go:61 |
| PR-3 | LLD-18 §3 — VERIFIED (RotateKey) — оставить как есть | registry.go |
| PR-4 | двойной стек SDK/UI-kit (React + preact) | packages/ |
| PR-6 | RotateKey/Disable — check-then-append без mu (idempotent-обновления, не создают identity; низкая тяжесть) | registry.go:399-434 |

## Открытые вопросы владельца (gates)

1. **G4** — коммит day-2 слоя (объём/состав, push) — блокер Cycle-004.
2. **G2** — семантика egress-whitelist (какие домены, кто решает) — до webhook-раннера.
3. **G8** — PII-AAD миграция ключей (необратимая) — когда.
4. **G14** — EVENT-STORE §4 «агент подписывает все мутации»: понизить документ или реализовать.
5. **SEC-5** — scope пилота: staff-only portal или нужен consumer-auth.
