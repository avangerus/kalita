# Контракты релизных gates

**Статус:** proposed milestones N1. Ни один из этих gates не объявлен пройденным. Это не публикация продукта. [TASKS.json](TASKS.json) — полный task catalog; [ITERATIONS](ITERATIONS.md) — порядок работы.

## Общий protocol выпуска

Кандидат закрепляет source commit и локальный patch inventory, dependency/pack/schema versions, выбранный trust/deployment profile, миграции, supported scope и acceptance evidence. Build без этой привязки не является воспроизводимым candidate. Нельзя засчитать исторический тест как доказательство ещё не реализованного свойства.

У каждого gate есть product/process owner и tech/security/operations decision по своей области. Material implementation review выполняется независимо; дефекты исправляются и соответствующие проверки повторяются. Семантическое изменение контракта сначала оформляется N-ADR. Feature disable/rollback останавливает future dispatch; уже отправленные эффекты требуют reconcile/компенсации по отдельному контракту.

## R0 — архитектура и входные решения

**Вход:** доступен checkout и исходные гипотезы. **Состав:** I00, KAL-001–KAL-005. **Выход:** current truth + compatibility inventory; конкретный buyer/process/baseline; две системы и один installation profile; PG/Temporal fault+ops decision; архитектурные инварианты без противоречий и критерии пилота.

Документный артефакт N1 можно закончить раньше экспериментов. В таком случае architecture bundle готов, R0 implementation entry gate остаётся pending. Если нет компании/доступа — synthetic technical probe явно отделён от buyer proof. **Stop:** неизвестны authority/данные/owner либо runtime comparison не дал defensible choice.

## R1 — safe execution alpha

**Вход:** R0 решения. **Состав:** I01–I03, KAL-006–KAL-020. **Выход:** composable Tx атомарно journal+ledger+outbox; Attempt fencing; committed expiration; retry/cancel/DLQ; общие reserves; Effect unknown; grant lineage; broker и enforced executor; provider caps; bounded durable harness. Один active adapter, unsafe legacy writers drained; unbound legacy automation/webhook egress отключён до безопасного bridge или имеет отдельный bounded company service grant.

**Evidence:** crash boundary матрица, same-actor stale completion, parallel child/call budgets, root revoke before child read, direct network/secret/DB denial, transitive tool/webhook paths. Unknown costs/effects сохраняют reserve. **Stop/rollback:** новые calls выключены, старые unknown reconciled; legacy writer не возвращается автоматически. Это technical alpha на ограниченном наборе contracts, не commercial release.

## R2 — Client Delivery value pilot

**Вход:** R1 и конкретный first process. **Состав:** I04–I06, KAL-021–KAL-035. **Выход:** два поддержанных sources с freshness/ACL; candidate→confirm/amend; handoff/acceptance; одна полезная queue/card; end-to-end revoke/privacy; серия сравнения по заранее установленным quality/value/full-cost thresholds.

**Evidence:** наблюдаемая область материала и denominator полноты; ложные обещания; correction time; current/native alternative; hidden setup/review/support work. Для первого probe внешние записи можно выключить; наличие read-only probe не доказывает безопасную запись. **Stop:** не выиграны agreed criteria или нет наблюдаемых данных. **Rollback:** отключить ingestion/agent suggestions, сохранить принятые obligations и manual ownership.

## R3 — adaptive views и bounded effects alpha

**Вход:** полезный R2. **Состав:** I07–I08, KAL-036–KAL-045. **Выход:** выразительный ViewSpec/catalog/renderer; UI-agent repair/fallback; trusted action confirmation; ephemeral/pin/shared lifecycle; snapshot/export отдельно; существующий webhook delivery согласован с Effect broker; один limited outbound action и reconcile.

**Evidence:** несколько task/role layouts; generated mislabel, unsafe HTML/JS/URL, private literals при share, live revoke, snapshot policy, stale versions; crash после remote acceptance. **Stop:** нельзя гарантировать audience/provenance или endpoint retry-safety. **Rollback:** fallback view и запрет новых effects; snapshots/accepted history сохраняются по объявленной retention, already exported copies не объявляются отозванными.

## R4 — полный starter pack candidate

**Вход:** R3. **Состав:** I09–I11, KAL-046–KAL-060; I10 по task dependencies может начаться раньше. **Выход:** MethodRevision evaluation/expiry/revoke/rollout; adaptive plan и bounded specialist delegation; один area experiment с goal change/handoff/stop; installation/onboarding/rotation/backup/upgrade/support/exit; versioned pack manifest; compatibility/replay; clean-room candidate; independent Astra/high review.

**Evidence:** install новым оператором без developer rescue; first useful real case; lost tail restore из backup до effect+revoke без повторной отправки и воскресших grants; recovery epoch; measurable RPO/RTO agreed buyer; interrupted migration и safe reader rollback; поддержка с secret-free export. **Stop:** material defects или невоспроизводимое восстановление. **Rollback:** safe read/manual mode и quarantine affected areas; release candidate не означает R5 commercial repeatability.

## R5 — повторяемая коммерческая готовность

**Вход:** R4 и второй независимый eligible buyer. **Состав:** I12, KAL-061–KAL-065. **Выход:** тот же pack без fork, конфигурация/overlays переживают upgrade; полезность снова измерена; install/support/recovery/full-cost подтверждены; offer ограничен реально поддерживаемыми profiles/process/sources.

**Evidence:** обе компании и отрицательные cases, method applicability второй компании, ручные вмешательства, экономика сопровождения. **Stop:** ценность не переносится или команда превращает setup в bespoke внедрение. **Rollback:** сузить supported profile/process, исправить pack и повторить relevant series. Gate — готовность к повторяемой поставке, не финансовое обещание и не разрешение писать в чужие системы без scope.

## R6 — второй pack и selective handover (условный)

**Вход:** R5, выбран один buyer/process и новая экономика. **Состав:** I13–I14, KAL-066–KAL-075. **Выход:** Order Exceptions **или** Document Close либо обоснованный лучше подходящий процесс; новые domain/source/risk contracts; адаптация опыта с evaluation; pack и series; AuthorityTransition одного scope с cutover/fallback/one-writer.

**Evidence:** нет насильственной общей ontology; partition/late legacy write/failed cutover/restore; конфликт criteria и ownership. **Stop:** unsupported sources, no owner или цена нового bespoke решения выше альтернативы. **Rollback:** явный writer transfer после reconciliation; нельзя просто включить два writers. Утверждение handover не означает полное поглощение legacy ERP.

## R7 — новые организационные границы (условный эксперимент)

**Вход:** R6 и подтверждённый запрос на конкретную capability. **Состав:** I15; KAL-076–KAL-079 выбираются независимо, KAL-080 принимает выбранный scope. **Выход:** scoped evidence и новый ADR/план либо отказ: TCB extensions, federation responsibilities, machine-first area creation, interop опыта.

**Evidence:** отсутствие credential/authority bypass; partition/dedup/unknown и независимые companies; explicit responsible human; applicability/permissions без hidden client data. **Stop:** нет demand, экономической причины или доказуемой границы. Глобальное exactly-once и универсальная автономная корпорация не обещаются. Невыбранные ветви deferred, не обязаны считаться завершёнными.
