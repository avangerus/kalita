# Cycle-004 → Cycle-A1 · PROPOSE — Milestone A: Trusted Kernel audit

> Миссия (§7, MILESTONE A): «Ядро Kalita достаточно корректно, чтобы на его semantics
> можно было опираться». Exit criteria: нет unresolved P0; нет P1 correctness/security
> в kernel без явного release exception; все core invariants имеют regression tests;
> Skeptic Pro и Security Pro дают **KERNEL READY FOR PILOT**.
> Cycle-004 (G5 pytest CI) отложен: его Deps = G4 (коммит) — HUMAN GATE владельца
> подготовлен (G4_COMMIT_PLAN.md), решение ожидается. Аудит ядра — не гейтованный.

## Проблема

Milestone A требует ДОКАЗАТЕЛЬНОЙ готовности ядра, но последний полный аудит ядра
проходил до Cycle-003; известные P1 (SEC-1 setPassword, SEC-2 prompt-injection) не
проверены на предмет новых фактов, а шесть областей milestone не имеют единого
свежего evidence-отчёта.

## Области аудита (6)

1. **Workflow**: transitions/guards/auto/approvals/reject/pending/mutation-during-approval/replay/definition-changes/determinism.
2. **Permissions/ABAC**: read/write/act/fields/dashboards/aggregates/agents/humans/portal/fail-closed.
3. **Identity**: human/agent identity, roles, attrs, system actors, persistence, replay.
4. **Eventstore**: append-only, hash chain, verify, checkpoints, replay, corrupted journal, crash boundaries.
5. **HITL**: authority, signature semantics, stale approval, TOCTOU, requester/approver, approve/reject asymmetry, definition changes, replay.
6. **Automation**: schedule/event/stuck, retries/leases, duplicates, restart.

## Метод

- OBSERVE: 6 параллельных cartographer-исследований (по одной области) — каждый:
  код+тесты+доки → отчёт «инварианты / тесты / пробелы» с file:line.
- DIAGNOSE: ведущий сводит пробелы к списку кандидатов (P0/P1/P2).
- PROPOSE: следующий цикл — самый острый подтверждённый пробел (ожидаемо SEC-1
  setPassword impersonation — P1 security, малый blast radius, без гейта).
- Каждый пробел далее закрывается отдельным циклом с полным Forge-протоколом.
- Итог Milestone A — только после закрытия пробелов: Skeptic+Security вердикт
  **KERNEL READY FOR PILOT**.

## Acceptance (этого цикла)

1. 6 отчётов с file:line-доказательствами сохранены в docs/forge/proposals/cycle-a1-*.md.
2. Сводный список пробелов с классификацией P0/P1/P2 и кандидатом в следующий цикл.
3. Ни один факт не утверждается без кода/теста.

## Reviewers

- architect_pro — утверждение выбранного следующего fix-цикла.
- skeptic_pro + review_security_pro — на каждом fix-цикле; финальный milestone-вердикт.

## Human gates

Нет (аудит read-only; fix-циклы — по своим гейтам). G4 остаётся единственным
открытым гейтом владельца.
