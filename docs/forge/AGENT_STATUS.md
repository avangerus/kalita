# Kalita Forge — статус автономной команды

> Живая карта: какая цель активна, какие роли работают и **на каких моделях**, над чем,
> какие файлы затронуты, какие гипотезы проверяются, что ждёт решения владельца.
> Обновляется в начале и конце каждого цикла.

## Активная цель

`goal-b638ebc5…` — **довести Kalita до первого пригодного для пилота релиза** (target v1.0).
**Milestone A (Trusted Kernel) ПРОЙДЕН** (Skeptic+Security «KERNEL READY FOR PILOT», staff-only).
**Milestone C core-exit ПРОЙДЕН** (4+ домена dogfood без Go-кода). OPS-1 (version identity) —
Cycle-F1. **Portfolio Review #1** — continue as-is, приоритет G2→egress.
**Три HUMAN GATE открыты**: G4 (коммит), G2 (egress-семантика), SEC-5 (scope пилота) —
критический путь Milestone B блокирован без решений владельца.

## Роли и над чем работают

| Роль | Модель | Статус |
|---|---|---|
| Ведущий (эта сессия) | deepseek-v4-pro | оркестрация Milestone A; ждёт exit-review |
| Chief Architect (architect_pro) | deepseek-v4-pro | A2/A3/A4 планы: ACCEPT(-WITH-CORRECTIONS) → директивы выполнены |
| Skeptic (skeptic_pro) | deepseek-v4-pro | A2/A3/A4: ACCEPT(-WITH-NOTES); сейчас — Milestone A exit-review |
| Security reviewer (review_security_pro) | deepseek-v4-pro | A2/A3/A4: ACCEPT(-WITH-NOTES); сейчас — Milestone A exit-review |

## Файлы/подсистемы, затронутые Cycles A2–A5

- `internal/api/login.go`, `handlers_ui.go`, `credentials.go` — identity admin boundary (D-014).
- `internal/engine/engine.go` — `DefinitionApprover()` геттер.
- `cmd/kalita/main.go`, `upgrade.go`, `backup.go` — verify --file + TLS default (D-015).
- `internal/identity/registry.go`, `invites.go` — atomic register/EnsureActor/Redeem + invite projection (D-016).
- `deploy/prometheus/prometheus.yml`, `docs/runbooks/README.md` — ca_file + cert export (D-015).
- `docs/TECHDEBT.md` — SEC-1/SEC-2 → FIXED; `RELEASE_BLOCKERS.md`/`RELEASE_SCORECARD.md`/`DECISIONS.md` — D-013..D-016.

\* В этой сессии role-инструменты пресета forge не смонтированы; роли исполнялись
независимыми subagent-делегированиями с persona по протоколу; «думающие» роли — на
deepseek-v4-pro. Cartographer выполнен на Pro вместо Flash — отклонение зафиксировано.

## Модели ролей (dual-model routing, строки role-* пресета forge)

| Роль (инструмент) | Модель | Назначение |
|---|---|---|
| Chief Architect (`architect_pro`) | **deepseek-v4-pro** | выбор проблемы, boundary, DSL/kernel, invariants, trade-offs, утверждение плана |
| Skeptic (`skeptic_pro`) | **deepseek-v4-pro** | независимый обзор; ACCEPT/REJECT/HUMAN_GATE |
| Security reviewer (`review_security_pro`) | **deepseek-v4-pro** | permissions/ABAC/HITL/signatures/identity/journal/trust/federation/migrations |
| Cartographer (`cartographer_flash`) | **deepseek-v4-flash** | чтение репо, карты, docs↔code, docs/forge |
| Implementer (`implement_flash`) | **deepseek-v4-flash** | scoped реализация/тесты/фиксы/паки |
| Test Engineer (`test_flash`) | **deepseek-v4-flash** | тесты, suites, воспроизведение багов |
| Documentation (`docs_flash`) | **deepseek-v4-flash** | синхронизация документации |
| Ведущий (эта сессия) | deepseek-v4-pro | оркестрация, gates, EXPLAIN |

**Доказательство routing (2026-07, workflow forge-routing-proof, живые модели):**
override flash → ребёнок на `deepseek-v4-flash` ✅; override pro → ребёнок на
`deepseek-v4-pro` ✅ (persona `{{model}}` отражает фактический маршрут).
Полное исполнение role-инструментов требует форж-сессии: текущий процесс держит
standing-генерацию старого пресета (до рестарта сервера).

## Файлы/подсистемы, затронутые Cycle-003 (факт)

- `internal/engine/workflow.go` — Decide: ре-оценка guard на grant-пути до журналирования
  решения (GUARD_FAILED, аппрувал остаётся pending); лог + метрика DecideFallbackSynthetic
  на findTransition==nil (fail-open edge).
- `internal/engine/decide_guard_test.go` (новый) — 8 регрессий: собственное поле / reject-путь /
  ref-путь / агрегат / часы / `$me`-контекст requester / replay / fallback при удалении перехода.
  Все PASS; полный прогон `go test ./... -count=1` зелёный (build/vet тоже).
- `packs/ap/workflows.dsl`, `packs/ap/security.dsl`, `internal/mcp/ap_dogfood_test.go` —
  комментарии: deny-строки = defense-in-depth (код пака не менялся).
- `docs/forge/` — proposals/cycle-003.md, cycle-003-trace.md, cycle-003-architect-verdict.md.

## Проверяемые гипотезы

1. **H-C3-1 (подтверждена)**: ре-оценка закрывает TOCTOU для всех поверхностей выражения —
   тесты own-field/ref-path/aggregate/clock/$me зелёные.
2. **H-C3-2 (подтверждена)**: `$me` = requester (журналирован как ev.Actor с attrs,
   engine.go:320) — replay-консистентно; TestDecideGuardUsesRequesterContext.
3. **H-C3-3 (подтверждена)**: reject не блокируется устаревшим guard — TestDecideRejectNotBlockedByStaleGuard.
4. **H-C3-4 (подтверждена)**: вариант A не трогает replay — TestDecideGuardFailureReplays,
   TestWorkflowReplay, TestGoldenReplay зелёные без изменения фикстур.

## Ждёт решения владельца

1. Рестарт сервера DSH для активации новой версии пресета forge (role-инструменты).
2. Коммит day-2 слоя (G4) — после него возможен Cycle-004 «pytest в CI» (G5).
3. EVENT-STORE §4 (подписи агентов) — понизить документ или реализовать (gate).
4. G2 webhook-раннер / G8 PII-AAD / G13 кросс-пак ref — решения владельца (gates).
