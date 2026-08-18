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

## PR-2 · продуктовая миссия, после 5 циклов (Rounds 1–5, 2026-08)

**Вердикт: continue as-is; один универсальный loop доказан live на двух доменах;
оставшийся риск — не kernel, а невыполненный clean-room (идёт в Round-6).**

1. **Где мы**: миссия A→D реализована и live-доказана по механике: разговор →
   discovery → EXPLAIN/SIMULATE → parked proposal → подпись → applied; изменение
   разговором с diff/impact; второй домен (ITSM) тем же циклом; product shell
   (Home/Ask/Approvals/Agents/Integrations/Company Model/Changes) + error UX +
   уровни B/O/D. Clean-room acceptance оператора — Round-6 (в работе).
2. **P0**: нет. P1: egress-доставка (D-011) — единственный, кто влияет на шаг
   «увидеть egress»; остальное — честные статусы.
3. **Формальные BLOCKER**: 0. Условный: egress D-011 (kernel-gap, задокументирован;
   для продукт-миссии — честный статус в UI и в acceptance).
4. **«Существует, но не использовать»**: walker — только подмножество guard'ов
   (differential-корпус против движка, честные «не знаю»); developer-вид DSL —
   за уровнем Developer.
5. **Удалить/отложить**: CRM-фреймворк без spec-builder'а (явный отказ вместо
   угадывания — до спроса); федерация-демо, SDK-React — вне миссии.
6. **Ценность за 5 циклов**: P0 (подписи definition), P0.2 (CompileMulti +
   get_definition), P1–P8 (фреймворки→live create/change/ITSM/UI), walker↔engine
   differential-контракт; компилятор поймал 2 реальных бага спеки ДО подписи.
7. **Следующие 5**: P9 clean-room acceptance (Round-6, в работе) → F-ревью
   (Product/Architecture Skeptics + Security Pro) → фиксы findings → Industrial RC
   → OWNER APPROVAL (gate).
8. **Рост ядра**: +2 осознанных kernel-capabilities за миссию (подписи
   definition; CompileMulti/get_definition) — обе additive, все старые suites
   зелёные; домены (ap/itsm) — через фреймворки+assembler, НЕ Go-код. Граница
   слой-2/слой-4 держится (ревью Architect Pro, 6/10 findings закрыты).
