# Cycle-A1 — Milestone A synthesis: gaps list (DIAGNOSE)

> Примечание о методе: шесть параллельных area-исследователей были прерваны
> (long-run паттерн); синтез ниже — ведущим по доказательствам, уже собранным в
> этой сессии: Skeptic-проверенный SYSTEM_TRUTH.md (реестр техдолга с file:line),
> полные Cycle-003 reviews (architect/skeptic/security, все три Pro) и целевые
> чтения кода этого цикла (identity/eventstore/engine/api). Каждый fix-цикл ниже
> повторно проверяется reviewers на своей области — «понимание не отстаёт».

## Milestone A: состояние по шести областям

| Область | Вердикт | Ключевые инварианты и тесты | Пробелы |
|---|---|---|---|
| Workflow | ✅ после Cycle-003 | transitions/guards/auto/approvals/reject/pending/mutation-during-approval/replay — workflow_test + decide_guard_test (8) + shadow_test | W-1: definition-change при pending → synthetic fail-open (D-013, LIM-7, логируется+метрика); W-2: on-update триггеры не из act (LIM-1) |
| ABAC | ✅ (полная sweep — Milestone G) | deny>allow fail-closed, field-level LLD-20, row-level where, dashboard-агрегаты — abac_test/lld20_update_perms/dashboard_dogfood/ap agent-boundedness | A-1: consumer-auth отсутствует (SEC-5 — gate владельца) |
| Identity | 🔴 P1 найден | token-auth constant-time, attrs, WebAuthn, key rotation, lockdown — tested | **I-1 setPassword any-human→any-actor (login.go:23-42, password.go:42-74)**; **I-2 disableActor any-human (handlers_ui.go:266-279, registry.go:384-398)**; **I-3 invite-any-role + public register (handlers_ui.go:289-311, invites.go:37-63)**; I-4 LLD-10 PR-1/2 |
| Eventstore | ✅ | append-only/SHA-256/VerifyChain/checkpoint/golden replay/op-recovery — tested | E-1 verify PG-only vs backup jsonl (OPS-2) |
| HITL | ✅ после Cycle-003 | signatures/WebAuthn/head_seq/offline verify/TOCTOU/requester-ctx/reject-asymmetry — LLD-02 + Cycle-003 tests | — |
| Automation | 🟡 | schedule-dedup/stuck/retry cap 5/escалation/leases/fold-детерминизм — automation_test+retry_test | A-2: retry-payload не заморожен (automation.go:476-484); webhook=намерение (Milestone B) |

## Порядок fix-циклов (DIAGNOSE)

1. **Cycle-A2 — identity admin boundary** (I-1+I-2+I-3): P1 impersonation/takeover-класс;
   closes SEC-1; малый blast radius (internal/api + internal/identity + тесты).
2. **Cycle-A3 — OPS-2 (verify/backup consistency) + SEC-3 (TLS-флаги по умолчанию)**.
3. **Cycle-A4 — LLD-10 PR-1/PR-2** (G6: атомарный Register, инвайт-проекция).
4. **Cycle-A5 — SEC-2 prompt-injection hardening** (design: workers/perimeter).
5. Далее — Milestone A exit-review: skeptic_pro + review_security_pro →
   **KERNEL READY FOR PILOT** (или новый список пробелов).

## Acceptance Cycle-A1

- [x] Синтез с file:line сохранён (этот файл).
- [x] Кандидат следующего цикла выбран: Cycle-A2 (identity admin boundary).
- [ ] ARCHITECT REVIEW Cycle-A2 — ACCEPT (следующая фаза).
