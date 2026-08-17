# PORTFOLIO REVIEW — реестр (каждые 5 dev-циклов, V4 Pro)

## PR-1 · после Milestone A + C-core-exit (2026-08)

**Вердикт: continue as-is, приоритизация G2 → egress.**

Кратко по 8 вопросам:
1. Приближаемся к пилоту, НО не на пороге: Milestone B (integration) — последний
   критический барьер; egress-доставка мёртва (INT-1).
2. P0 нет. P1 остаются: SEC-5 consumer-auth (scope-решение), INT-1 webhook-доставка,
   INT-2 семантика доставки, SEC-4 PII-AAD (P2, gate). Два из четырёх — HUMAN GATE.
3. Формальных BLOCKER нет; по сути блокируют Milestone B: G2 (egress-семантика, gate),
   SEC-5 (scope), TST-1 (Python в CI, после G4).
4. «Существует, но не использовать»: webhook-доставка (D-011), on-update-триггеры (LIM-1),
   кросс-пак ref (G13), kalita upgrade (OPS-3).
5. Можно удалить/отложить: federation-демо (LIM-6), React-слой SDK (PR-4), мёртвые kinds.
6. Ценность дали: A2 (SEC-1 impersonation), A4 (registry atomicity), A5 (SEC-2),
   Cycle-003 (Decide-guard), F1 (version identity).
7. Следующие 5 (порядок): G2 egress-семантика → webhook-раннер → SEC-5 scope →
   TST-1 pytest CI (после G4) → G4 коммит.
8. Kernel НЕ растёт быстрее packs: 4 домена чистым DSL без Go-кода (Milestone C);
   KERNEL_PRESSURE — 3 записи, все решаются в packs. Продукт ≠ framework.

**Приоритет после PR-1**: G2 (egress, HUMAN GATE) → webhook-раннер → SEC-5 scope →
TST-1 → G4. Не строить Milestone D/E до пилота.
