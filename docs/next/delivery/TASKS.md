# Каталог задач N1

**Источник:** [TASKS.json](TASKS.json). Все 80 tasks — `planned`. 16 итераций, 8 gates; никаких календарных обязательств и завершённых implementation claims.

Effort: S — один ограниченный workstream; M — несколько согласованных изменений/проверок; L — subsystem или fault/pilot series. Это относительная сложность, не число дней. Root интегрирует; Luna/high исполняет bounded engineering work; Astra/high независимо разбирает существенные решения и безопасность.

Task закрывается только после acceptance evidence, проверки original business scenario и исправления material review findings. Будущие deliverables не считаются существующими артефактами. Условия R6/R7 проверяются owner; `deferred` не равняется `done`.

## I00 — Baseline и выбор первого пути (R0)

<a id="kal-001"></a>

### KAL-001 — Inventory checkout, journaling и compatibility

P0 · S · Tech lead · `planned`

Зависимости: нет. Контракты: [HLD.md](../HLD.md), [DECISIONS.md](../DECISIONS.md).

Результаты: docs/next/AS_IS.md; baseline report.

Приёмка:

- HEAD/diff и existing egress delivery отделены от release evidence
- Task/runner failure paths и поддерживаемые API записаны с воспроизведением

Проверка: Снять targeted baseline tests; reproduce Append failure и stale completion без отката пользовательских edits.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-002"></a>

### KAL-002 — Первый buyer, процесс, baseline и pilot thresholds

P0 · M · Product owner + process owner · `planned`

Зависимости: KAL-001. Контракты: [PRODUCT_CONTRACT.md](../PRODUCT_CONTRACT.md), [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md).

Результаты: buyer/process selector; pilot measurement protocol.

Приёмка:

- Есть полномочный owner и повторяемый дорогой результат
- До пилота зафиксированы время, качество, полная стоимость и stop thresholds

Проверка: Сопоставить текущую работу и доступную штатную автоматизацию на одинаковой выборке.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-003"></a>

### KAL-003 — PG против Temporal: bounded fault/ops prototype

P0 · L · Engineer · `planned`

Зависимости: KAL-001. Контракты: [lld/EXECUTION.md](../lld/EXECUTION.md), [lld/API_CONTRACTS.md](../lld/API_CONTRACTS.md).

Результаты: N-ADR-05 evidence; durability comparison report.

Приёмка:

- Crash/lease/fence/budget/unknown restore сценарии оценены по заранее объявленным критериям
- Выбран ровно один adapter либо реализация blocked; ownership ADR обновлён

Проверка: Убивать prototype на каждом commit boundary; parallel reserve; оценить install и operator recovery cost.

Review: Astra/high (independent adversarial review).

<a id="kal-004"></a>

### KAL-004 — Два источника и один deployment profile

P0 · M · Operations engineer · `planned`

Зависимости: KAL-002. Контракты: [lld/OPERATIONS.md](../lld/OPERATIONS.md).

Результаты: SourceBinding selection; installation profile decision.

Приёмка:

- Выбраны конкретные две системы, read scopes, owner и доступ
- Выбран self-host либо managed путь с полной dependency/data-flow картой

Проверка: Проверить source ACL и token revoke; пройти setup с будущим оператором без изменения сети PC.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-005"></a>

### KAL-005 — Утвердить N1, инварианты и критерии gates

P0 · M · Tech lead · `planned`

Зависимости: KAL-002, KAL-003, KAL-004. Контракты: [HLD.md](../HLD.md), [DECISIONS.md](../DECISIONS.md).

Результаты: docs/next/DECISIONS.md; release acceptance register.

Приёмка:

- Новые правила явно supersede conflicts старых target docs
- Все provisional решения имеют evidence/owner/stop; риск reviewed Astra/high

Проверка: Независимо попытаться опровергнуть HLD на recovery, authority и first value сценариях.

Review: Astra/high (independent adversarial review).

## I01 — Контракты работы и атомарная база (R1)

<a id="kal-006"></a>

### KAL-006 — Минимальный WorkCase и стабильная идентичность

P0 · M · Engineer · `planned`

Зависимости: KAL-005. Контракты: [lld/WORK_MODEL.md](../lld/WORK_MODEL.md), [lld/AGENT_PROTOCOL.md](../lld/AGENT_PROTOCOL.md).

Результаты: proposed WorkCase contract; pack domain schema.

Приёмка:

- Case имеет owner, результат, refs/revision и не зависит от chat ID
- Простой случай не требует всех сущностей платформы

Проверка: Закрыть чат, сменить actor/UI и открыть тот же case без потери условий.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-007"></a>

### KAL-007 — Source authority и mapping contracts

P0 · M · Engineer · `planned`

Зависимости: KAL-004, KAL-006. Контракты: [lld/INTEGRATIONS.md](../lld/INTEGRATIONS.md), [lld/EXECUTION.md](../lld/EXECUTION.md).

Результаты: SourceBinding schema; authority map.

Приёмка:

- Каждое поле/область имеет primary writer и freshness
- Binding не превращает source credential в права всех пользователей

Проверка: Read через broad credential скрывает чужие object/fields и отмечает stale source.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-008"></a>

### KAL-008 — Canonical contracts, versioning и transport parity

P0 · M · Engineer · `planned`

Зависимости: KAL-005, KAL-006. Контракты: [lld/API_CONTRACTS.md](../lld/API_CONTRACTS.md).

Результаты: canonical contract schema; REST/MCP compatibility matrix.

Приёмка:

- DSL adapter даёт version/digest и typed action contract
- REST/MCP используют одинаковые hooks/errors/If-Match/idempotency

Проверка: Одинаковые forbidden/conflict/duplicate запросы обоих transports дают эквивалентный исход.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-009"></a>

### KAL-009 — ResponsibilityContract и рискованные переходы

P0 · M · Engineer · `planned`

Зависимости: KAL-006, KAL-008. Контракты: [lld/WORK_MODEL.md](../lld/WORK_MODEL.md), [lld/AGENT_PROTOCOL.md](../lld/AGENT_PROTOCOL.md).

Результаты: responsibility/amendment schemas; risk policy contract.

Приёмка:

- Цель/критерии/grant отделены от execution plan revision
- Handoff и изменение условий требуют authorised decision

Проверка: Model plan patch не меняет цель, сумму, права или подтверждённое обязательство.

Review: Astra/high (independent adversarial review).

<a id="kal-010"></a>

### KAL-010 — Composable PG Tx API: ledger + journal + outbox

P0 · L · Engineer · `planned`

Зависимости: KAL-003, KAL-008. Контракты: [lld/EXECUTION.md](../lld/EXECUTION.md), [lld/API_CONTRACTS.md](../lld/API_CONTRACTS.md).

Результаты: internal/eventstore Tx extension (proposed); transaction/lock-order tests.

Приёмка:

- Canonical event и operational state коммитятся/откатываются вместе
- Все append paths соблюдают lock order, без nested pool Append; projections после commit
- Durable aggregate/authority heads и applied-head barrier запрещают stale owner/criteria/ACL admission

Проверка: Fault injection до/после commit, конкурирующий append/reserve и deadlock retry не расходят ledger/journal; Commit owner rev11→projection failure→read/If-Match rev10 fail closed до catch-up.

Review: Astra/high (independent adversarial review).

## I02 — Долговечный execution ledger (R1)

<a id="kal-011"></a>

### KAL-011 — Run/Attempt lease, heartbeat и fencing

P0 · L · Engineer · `planned`

Зависимости: KAL-009, KAL-010. Контракты: [lld/EXECUTION.md](../lld/EXECUTION.md), [lld/API_CONTRACTS.md](../lld/API_CONTRACTS.md).

Результаты: Run/Attempt schema; claim/heartbeat service.

Приёмка:

- Параллельный claim не выдаёт одну Attempt дважды
- Expired heartbeat/complete и прежний fence отклоняются

Проверка: A истёк; новая Attempt того же actor; старый A complete не меняет запись.

Review: Astra/high (independent adversarial review).

<a id="kal-012"></a>

### KAL-012 — Retry, cancel, DLQ и committed expiration

P0 · M · Engineer · `planned`

Зависимости: KAL-010, KAL-011. Контракты: [lld/EXECUTION.md](../lld/EXECUTION.md), [lld/API_CONTRACTS.md](../lld/API_CONTRACTS.md).

Результаты: execution state machine; operator DLQ commands.

Приёмка:

- Expiration меняет state только после commit
- Cancel останавливает будущие шаги; DLQ retry сохраняет историю и authority

Проверка: Append/Tx failure, restart waiting_retry и отмена до/после dispatch дают оговорённые состояния.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-013"></a>

### KAL-013 — Общий reservation/settlement ledger

P0 · L · Engineer · `planned`

Зависимости: KAL-010, KAL-011. Контракты: [lld/EXECUTION.md](../lld/EXECUTION.md), [lld/API_CONTRACTS.md](../lld/API_CONTRACTS.md).

Результаты: budget schema; reserve/settle service.

Приёмка:

- Children/calls/retry учитываются общим root без double count
- Stable reservation key и duplicate receipt дают один settlement; unknown держит reserve

Проверка: Параллельные child/retry не превышают hard cap; повтор receipt не удваивает списание.

Review: Astra/high (independent adversarial review).

<a id="kal-014"></a>

### KAL-014 — Effect intent, dispatch permit и unknown

P0 · L · Engineer · `planned`

Зависимости: KAL-010, KAL-012, KAL-013. Контракты: [lld/INTEGRATIONS.md](../lld/INTEGRATIONS.md), [lld/EXECUTION.md](../lld/EXECUTION.md).

Результаты: effect ledger schema; dispatch/reconcile protocol.

Приёмка:

- Intent и reserve durable до сети; revoke сериализуется с permit
- Unknown не переводится в failed/успех по отчёту worker и не повторяется вслепую

Проверка: Crash после remote acceptance до receipt удерживает reserve и блокирует unsafe retry.

Review: Astra/high (independent adversarial review).

<a id="kal-015"></a>

### KAL-015 — Миграция legacy tasks и write protocol

P0 · M · Engineer · `planned`

Зависимости: KAL-011, KAL-012, KAL-014. Контракты: [lld/API_CONTRACTS.md](../lld/API_CONTRACTS.md).

Результаты: migration preflight/dry-run; versioned worker handshake.

Приёмка:

- Legacy reads сохраняются; writes требуют Attempt ID/token/fence
- Old writers drained; ambiguity blocked; cutover с одним writer

Проверка: Legacy complete от того же actor после re-claim rejected; interrupted import идемпотентен.

Review: Astra/high (independent adversarial review).

## I03 — Broker и ограниченный агент (R1)

<a id="kal-016"></a>

### KAL-016 — Grant lineage, issuer edges и revocation

P0 · L · Engineer · `planned`

Зависимости: KAL-009, KAL-011, KAL-013. Контракты: [lld/AUTHORITY_SECURITY.md](../lld/AUTHORITY_SECURITY.md), [lld/EXECUTION.md](../lld/EXECUTION.md).

Результаты: grant/policy service; lineage revocation tests.

Приёмка:

- Child проверяет всю активную parent lineage до каждого protected call
- Дополнительное specialist право создаёт самостоятельный issuer decision; parent PII не расширяется

Проверка: Revoke root при живом child/cached context блокирует новые reads/model/tool/effects.

Review: Astra/high (independent adversarial review).

<a id="kal-017"></a>

### KAL-017 — Enforced executor boundary и broker credentials

P0 · L · Engineer · `planned`

Зависимости: KAL-004, KAL-016. Контракты: [lld/AUTHORITY_SECURITY.md](../lld/AUTHORITY_SECURITY.md), [lld/EXECUTION.md](../lld/EXECUTION.md).

Результаты: isolated executor profile; trusted tool/model broker.

Приёмка:

- Executor не имеет DB, long-lived secrets и direct outbound
- Trusted adapter/dev явно маркированы; indirect redirects/DNS/telemetry gated

Проверка: Попробовать прямой socket/secret/DB, rebinding, attachment redirect и prompt injection.

Review: Astra/high (independent adversarial review).

<a id="kal-018"></a>

### KAL-018 — Context и model broker: hard bounds и provenance

P0 · L · Engineer · `planned`

Зависимости: KAL-007, KAL-013, KAL-016, KAL-017. Контракты: [lld/AUTHORITY_SECURITY.md](../lld/AUTHORITY_SECURITY.md), [lld/EXECUTION.md](../lld/EXECUTION.md).

Результаты: context/model call schemas; pricing revision registry.

Приёмка:

- Input/output/duration/call caps и conservative cost reserve enforceable
- Provider без bound доступен только soft mode; unknown billing держит максимум

Проверка: Lost response/provider cap failure/actual>bound выявлены, новые calls остановлены, ACL скрывает private refs.

Review: Astra/high (independent adversarial review).

<a id="kal-019"></a>

### KAL-019 — Bounded durable agent harness

P0 · L · Engineer · `planned`

Зависимости: KAL-011, KAL-014, KAL-018. Контракты: [lld/AGENT_PROTOCOL.md](../lld/AGENT_PROTOCOL.md), [lld/EXPERIENCE.md](../lld/EXPERIENCE.md).

Результаты: agent intent loop/checkpoints; updated worker protocol.

Приёмка:

- Accepted proposal/next checkpoint атомарны; replay не повторяет committed tool
- Round/graph/time/attempt limits останавливают loop; worker не пишет done до server confirmation

Проверка: Crash между model response/tool commit/next decision; repeated proposal и invalid tool завершаются безопасно.

Review: Astra/high (independent adversarial review).

<a id="kal-020"></a>

### KAL-020 — R1 adversarial execution acceptance

P0 · M · Tester + independent critic · `planned`

Зависимости: KAL-015, KAL-016, KAL-017, KAL-018, KAL-019. Контракты: [HLD.md](../HLD.md), [TRACEABILITY.md](../TRACEABILITY.md).

Результаты: R1 test evidence; Astra/high review dispositions.

Приёмка:

- Пройдены Tx/fence/parallel budget/revoke/effect unknown матрицы
- Один adapter, no direct bypass; material review defects устранены

Проверка: Review против исходных N-02–N-06; попытка обхода через MCP/automation/webhook.

Review: Astra/high (independent adversarial review).

## I04 — Два источника и intake (R2)

<a id="kal-021"></a>

### KAL-021 — Первый intake adapter по выбранному binding

P1 · M · Engineer · `planned`

Зависимости: KAL-007, KAL-018, KAL-020. Контракты: [lld/INTEGRATIONS.md](../lld/INTEGRATIONS.md), [lld/EXECUTION.md](../lld/EXECUTION.md).

Результаты: source A adapter; source contract fixtures.

Приёмка:

- Поддержаны реальные IDs/версии/ACL и read-only scope
- Cursor/checkpoint сохраняется; повтор входа не создаёт новый Claim

Проверка: Duplicate page, token revoke, schema mismatch и source downtime на fixture + разрешённом pilot sample.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-022"></a>

### KAL-022 — Второй intake adapter по выбранному binding

P1 · M · Engineer · `planned`

Зависимости: KAL-007, KAL-018, KAL-020. Контракты: [lld/INTEGRATIONS.md](../lld/INTEGRATIONS.md), [lld/EXECUTION.md](../lld/EXECUTION.md).

Результаты: source B adapter; source contract fixtures.

Приёмка:

- Есть отдельный source owner, mapping и error taxonomy
- Данные связаны с case только по проверяемому mapping, без глобального импорта

Проверка: Конфликтующие IDs разных источников, private attachment и истёкшие permissions не смешиваются.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-023"></a>

### KAL-023 — Freshness, completeness и source reconciliation

P1 · M · Engineer · `planned`

Зависимости: KAL-021, KAL-022. Контракты: [lld/INTEGRATIONS.md](../lld/INTEGRATIONS.md), [lld/EXECUTION.md](../lld/EXECUTION.md).

Результаты: observation/cursor service; intake health view.

Приёмка:

- Observation содержит source time/receive time/version и доступный период
- Gap/partial source явно виден; stale не изображается complete/current

Проверка: Pagination gap, late update, missing page и replay checkpoint не теряются и не дублируются.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-024"></a>

### KAL-024 — ACL на intake, search, context и derived outputs

P1 · M · Engineer · `planned`

Зависимости: KAL-016, KAL-021, KAL-022. Контракты: [lld/AUTHORITY_SECURITY.md](../lld/AUTHORITY_SECURITY.md), [lld/EXECUTION.md](../lld/EXECUTION.md).

Результаты: source ACL mapping; privacy regression fixtures.

Приёмка:

- Одна граница company/client действует на snippets/counts/context/caches
- Удаление/отзыв источника блокирует новые derivations и отображается честно

Проверка: Чужой клиент через поиск, ID, count, report и parent child summary не раскрывается.

Review: Astra/high (independent adversarial review).

<a id="kal-025"></a>

### KAL-025 — Очередь кейсов и кандидатов обязательств

P1 · M · Product owner + process owner · `planned`

Зависимости: KAL-006, KAL-019, KAL-023, KAL-024. Контракты: [PRODUCT_CONTRACT.md](../PRODUCT_CONTRACT.md), [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md).

Результаты: first queue/card UI; candidate extraction method.

Приёмка:

- Любое обещание сначала candidate со source quotation/ref
- Пользователь видит owner, deadline, blocker и boundary наблюдения

Проверка: Вопрос/предположение/цитата чужой стороны не подтверждаются автоматически; закрытие чата сохраняет case.

Review: Independent engineer; material changes reviewed by Astra/high.

## I05 — Подтверждённые обязательства (R2)

<a id="kal-026"></a>

### KAL-026 — Confirm/amend/withdraw обязательств

P1 · M · Engineer · `planned`

Зависимости: KAL-009, KAL-025. Контракты: [lld/WORK_MODEL.md](../lld/WORK_MODEL.md), [lld/AGENT_PROTOCOL.md](../lld/AGENT_PROTOCOL.md).

Результаты: obligation commands/workflow; versioned criteria contract.

Приёмка:

- Confirm пишет exact terms/actor/basis; amendment сохраняет прежние условия
- Ожидаемая revision обязательна; изменение стороны/срока пересогласуется

Проверка: Два подтверждения/устаревшая revision/новая цитата не молча перезаписывают обещание.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-027"></a>

### KAL-027 — Передача ответственности и следующий владелец

P1 · M · Engineer · `planned`

Зависимости: KAL-009, KAL-026. Контракты: [lld/WORK_MODEL.md](../lld/WORK_MODEL.md), [lld/AGENT_PROTOCOL.md](../lld/AGENT_PROTOCOL.md).

Результаты: handoff offer/accept contract; owner/wait UI.

Приёмка:

- Новое владение начинается при явном принятии; отказ сохраняет прежнего owner
- Pending effects/reserves и ограничения видны в допустимом объёме

Проверка: Handoff отклонён/owner потерял права/ожидание без ответа не оставляют бесхозную работу.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-028"></a>

### KAL-028 — Результат, Evidence, Acceptance и спор

P1 · M · Engineer · `planned`

Зависимости: KAL-026, KAL-027. Контракты: [lld/WORK_MODEL.md](../lld/WORK_MODEL.md), [lld/AGENT_PROTOCOL.md](../lld/AGENT_PROTOCOL.md).

Результаты: result/accept/dispute commands; criteria review UI.

Приёмка:

- Report модели отделён от результата/решения получателя
- Acceptance привязан к criteria revision; новое свидетельство не переписывает прошлое

Проверка: Исполнитель пытается сам принять существенный результат; спор/conditional acceptance сохраняют авторство.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-029"></a>

### KAL-029 — Revocation и client isolation end-to-end

P1 · M · Engineer · `planned`

Зависимости: KAL-024, KAL-026, KAL-028. Контракты: [lld/AUTHORITY_SECURITY.md](../lld/AUTHORITY_SECURITY.md), [lld/EXECUTION.md](../lld/EXECUTION.md).

Результаты: pilot security evidence; cache/subscription invalidation.

Приёмка:

- Отзыв применяется в открытом UI, context/search и background processing
- Closed/foreign evidence не раскрывается через summaries/errors/counts

Проверка: Отозвать доступ между query, confirm и report; другой клиент пробует shared refs.

Review: Astra/high (independent adversarial review).

<a id="kal-030"></a>

### KAL-030 — Onboarding value pilot и operator fallback

P1 · M · Operations engineer · `planned`

Зависимости: KAL-004, KAL-025, KAL-026, KAL-027, KAL-028, KAL-029. Контракты: [lld/OPERATIONS.md](../lld/OPERATIONS.md).

Результаты: pilot guide; manual correction/escalation path.

Приёмка:

- Owner выбирает источники и пределы; first useful case проходится без developer editing
- Есть понятный разбор ложного Claim/stale source/blocked run и прекращение пилота

Проверка: Новый координатор проходит setup→confirm→handoff→accept и исправляет неверный candidate.

Review: Independent engineer; material changes reviewed by Astra/high.

## I06 — Серия проверки ценности (R2)

<a id="kal-031"></a>

### KAL-031 — Measurement protocol и comparison выборка

P1 · S · Product owner + process owner · `planned`

Зависимости: KAL-002, KAL-030. Контракты: [PRODUCT_CONTRACT.md](../PRODUCT_CONTRACT.md), [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md).

Результаты: baseline/comparison dataset; measurement definitions.

Приёмка:

- Окно, критерии выборки и thresholds утверждены до series
- Текущий метод и native alternative имеют те же данные/условия

Проверка: Проверить inclusion отрицательных случаев и missing-channel denominator без post-hoc выбора успехов.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-032"></a>

### KAL-032 — Серия Client Delivery value pilot

P1 · L · Product owner + process owner · `planned`

Зависимости: KAL-031. Контракты: [PRODUCT_CONTRACT.md](../PRODUCT_CONTRACT.md), [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md).

Результаты: pilot outcome dataset; user feedback.

Приёмка:

- Повторяемые cases измерены по времени/пропускам/исправлениям
- Качество наблюдаемых источников отделено от общей полноты компании

Проверка: Провести серию по согласованной выборке; вручную проверить ложные обещания и исключённые случаи.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-033"></a>

### KAL-033 — Failure/revoke/downtime audit пилота

P1 · M · Tester + independent critic · `planned`

Зависимости: KAL-029, KAL-032. Контракты: [HLD.md](../HLD.md), [TRACEABILITY.md](../TRACEABILITY.md).

Результаты: pilot fault evidence; incident dispositions.

Приёмка:

- Материальные утечки/дубли/неконтролируемый расход разбираются до continuation
- Downtime/injection/late evidence включены в наблюдаемый исход

Проверка: Повторить worst pilot failure и подтвердить stop/escalation на исходном бизнес-сценарии.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-034"></a>

### KAL-034 — Полная экономика и operational burden

P1 · M · Product owner + process owner · `planned`

Зависимости: KAL-030, KAL-032. Контракты: [PRODUCT_CONTRACT.md](../PRODUCT_CONTRACT.md), [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md).

Результаты: full-cost comparison report; support time log.

Приёмка:

- Учитываются модели, настройка, ручной review, инфраструктура и поддержка
- ROI выводится из согласованного метода; uncertain estimates обозначены

Проверка: Сравнить результат со штатной альтернативой с учётом всех ручных действий.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-035"></a>

### KAL-035 — R2 decision: continue, narrow или stop

P1 · S · Product owner + process owner · `planned`

Зависимости: KAL-032, KAL-033, KAL-034. Контракты: [PRODUCT_CONTRACT.md](../PRODUCT_CONTRACT.md), [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md).

Результаты: R2 gate decision; updated pack scope.

Приёмка:

- Продолжение разрешено только при согласованных quality/value/cost thresholds
- Один workflow не объявляется доказанной delegated organization

Проверка: Process owner рассматривает полную серию; failed gate даёт исправление/новый измеряемый probe.

Review: Independent engineer; material changes reviewed by Astra/high.

## I07 — Генеративная композиция UI (R3)

<a id="kal-036"></a>

### KAL-036 — ViewSpec/revision/binding/action schemas

P1 · M · Engineer · `planned`

Зависимости: KAL-008, KAL-035. Контракты: [lld/VIEWS.md](../lld/VIEWS.md).

Результаты: ViewSpec contract; view version registry.

Приёмка:

- Композиция expressive, но декларативная: нет arbitrary HTML/JS/SQL/network
- Audience/bindings/version refs и action intents имеют schemas

Проверка: Попытки unsupported node/version/injection/oversized graph отклонены валидатором.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-037"></a>

### KAL-037 — Versioned component catalog и trusted renderer

P1 · L · Engineer · `planned`

Зависимости: KAL-036. Контракты: [lld/VIEWS.md](../lld/VIEWS.md).

Результаты: catalog contracts; renderer components.

Приёмка:

- Компоненты имеют typed inputs, accessibility и безопасные slots
- ACL авторитетен на backend; renderer не принимает данные чужого audience

Проверка: Keyboard/mobile/empty/redacted/error states и unsafe literal/URL проверены.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-038"></a>

### KAL-038 — UI-agent composition и human repair

P1 · M · Engineer · `planned`

Зависимости: KAL-019, KAL-036, KAL-037. Контракты: [lld/VIEWS.md](../lld/VIEWS.md), [lld/AGENT_PROTOCOL.md](../lld/AGENT_PROTOCOL.md).

Результаты: view generator role; generation evaluation fixtures.

Приёмка:

- Генератор выбирает подходящий layout/data bindings для нескольких tasks
- Invalid proposal имеет repair/fallback; генерация не публикует автоматически

Проверка: Сравнить queue/card/comparison views и попытку private context reuse вне audience.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-039"></a>

### KAL-039 — Trusted consequential action confirmation

P1 · M · Engineer · `planned`

Зависимости: KAL-014, KAL-016, KAL-037. Контракты: [lld/VIEWS.md](../lld/VIEWS.md).

Результаты: action preview/challenge UI; digest binding service.

Приёмка:

- Confirmation строится из canonical connector args доверенным кодом
- Recipient/amount/object/limits/version видны и связаны с nonce/digest

Проверка: Generated label ложно обещает действие; изменённый payload/expired approval отвергнуты до dispatch.

Review: Astra/high (independent adversarial review).

<a id="kal-040"></a>

### KAL-040 — View safety, ACL и useful interaction verification

P1 · M · Tester + independent critic · `planned`

Зависимости: KAL-036, KAL-037, KAL-038, KAL-039. Контракты: [HLD.md](../HLD.md), [TRACEABILITY.md](../TRACEABILITY.md).

Результаты: view regression evidence; usability review.

Приёмка:

- Agent composition полезна для нескольких ролей без единого fixed screen
- Renderer/read/action paths сохраняют current rights и safe catalog boundary; publication/snapshot проверяются KAL-041/042

Проверка: Revoke while view open, mislabel, source injection и sensitive aggregate regression; user completes original render/read/action task.

Review: Astra/high (independent adversarial review).

## I08 — Жизненный цикл UI и эффекты (R3)

<a id="kal-041"></a>

### KAL-041 — Ephemeral/pinned/shared live lifecycle

P1 · M · Engineer · `planned`

Зависимости: KAL-038, KAL-040. Контракты: [lld/VIEWS.md](../lld/VIEWS.md).

Результаты: view lifecycle service; pin/share/audience UI.

Приёмка:

- Pin сохраняет запрос/композицию, не snapshot; expiry чистит ephemeral по retention
- Share проверяет bindings и generated literals/provenance private context

Проверка: Смена аудитории, revoke автора/читателя и удалённая source link не раскрывают старые private literals.

Review: Astra/high (independent adversarial review).

<a id="kal-042"></a>

### KAL-042 — Snapshot/export и отдельные права копии

P1 · M · Engineer · `planned`

Зависимости: KAL-040, KAL-041. Контракты: [lld/VIEWS.md](../lld/VIEWS.md).

Результаты: snapshot/export contract; snapshot retention UI.

Приёмка:

- Snapshot фиксирует values/versions и отдельную аудиторию/retention
- Пользователь видит ограничения отзыва уже скачанного экспорта

Проверка: Создать snapshot, поменять live данные, revoke/source erasure и проверить объявленную copy policy.

Review: Astra/high (independent adversarial review).

<a id="kal-043"></a>

### KAL-043 — Объединить existing egress delivery с Effect broker

P1 · L · Engineer · `planned`

Зависимости: KAL-001, KAL-014, KAL-016, KAL-039. Контракты: [lld/INTEGRATIONS.md](../lld/INTEGRATIONS.md), [lld/EXECUTION.md](../lld/EXECUTION.md).

Результаты: existing webhook integration mapping; one limited outbound connector.

Приёмка:

- DeliveryID/DLQ/receipts не дублируются второй независимой delivery системой
- Transitive automation использует тот же grant/intent/approval contract

Проверка: Webhook retry, revoked grant и handler-triggered outgoing action проходят один authority gate.

Review: Astra/high (independent adversarial review).

<a id="kal-044"></a>

### KAL-044 — Reconciliation и bounded external action pilot

P1 · L · Engineer · `planned`

Зависимости: KAL-043. Контракты: [lld/INTEGRATIONS.md](../lld/INTEGRATIONS.md), [lld/EXECUTION.md](../lld/EXECUTION.md).

Результаты: receipt lookup/manual reconcile UI; limited action evidence.

Приёмка:

- Unknown/reserve видны owner и разбираются по endpoint contract
- Retry не происходит без proven receiver idempotency или confirmed-not-applied

Проверка: Lost response, duplicate callback, cancel-after-dispatch и stale executor receipt не создают второе действие.

Review: Astra/high (independent adversarial review).

<a id="kal-045"></a>

### KAL-045 — R3 adaptive views/effect alpha gate

P1 · M · Tester + independent critic · `planned`

Зависимости: KAL-041, KAL-042, KAL-043, KAL-044. Контракты: [HLD.md](../HLD.md), [TRACEABILITY.md](../TRACEABILITY.md).

Результаты: R3 acceptance evidence; Astra/high review dispositions.

Приёмка:

- View lifecycle, literals/ACL и trusted confirmations проверены end-to-end
- Limited effect crash/reconcile proof есть; коммерческий pack ещё не объявлен

Проверка: Review пытается утечь через shared snapshot и повторить неизвестный external effect.

Review: Astra/high (independent adversarial review).

## I09 — Методы и делегированная область (R4)

<a id="kal-046"></a>

### KAL-046 — MethodRevision registry, owner и applicability

P1 · M · Engineer · `planned`

Зависимости: KAL-035, KAL-045. Контракты: [lld/EXPERIENCE.md](../lld/EXPERIENCE.md), [lld/AGENT_PROTOCOL.md](../lld/AGENT_PROTOCOL.md).

Результаты: method revision schema/service; method review UI.

Приёмка:

- Метод содержит owner, scope, basis, version, expiry и revoke
- Чужой client material не переносится без отдельного разрешения

Проверка: Попробовать применить чужой/expired/revoked метод и сохранить сырой prompt как approved experience.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-047"></a>

### KAL-047 — Positive/negative/new-case method evaluation

P1 · M · Tester + independent critic · `planned`

Зависимости: KAL-046. Контракты: [HLD.md](../HLD.md), [TRACEABILITY.md](../TRACEABILITY.md).

Результаты: method test corpus; evaluation report.

Приёмка:

- Acceptance thresholds определены до method rollout
- Есть новые/отрицательные cases, drift и невидимые источники; оценка не из success count

Проверка: Новый метод ошибается на adversarial case; gate отклоняет до публикации.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-048"></a>

### KAL-048 — Model/method rollout и обратимое переключение

P1 · M · Engineer · `planned`

Зависимости: KAL-018, KAL-046, KAL-047. Контракты: [lld/EXPERIENCE.md](../lld/EXPERIENCE.md), [lld/AGENT_PROTOCOL.md](../lld/AGENT_PROTOCOL.md).

Результаты: model/method compatibility policy; staged rollout controls.

Приёмка:

- Runs сохраняют pinned revisions; новый метод не меняет старые obligations
- Откат/revoke блокирует future selection, не переписывает receipts/acceptance

Проверка: Смена provider/pricing/model ухудшает corpus; policy блокирует и переключает selection безопасно.

Review: Astra/high (independent adversarial review).

<a id="kal-049"></a>

### KAL-049 — Adaptive plan patch и specialist delegation

P1 · L · Engineer · `planned`

Зависимости: KAL-009, KAL-016, KAL-019, KAL-046, KAL-048. Контракты: [lld/AGENT_PROTOCOL.md](../lld/AGENT_PROTOCOL.md), [lld/EXPERIENCE.md](../lld/EXPERIENCE.md).

Результаты: plan revision validator; delegation edges/child protocol.

Приёмка:

- Graph changes в ResponsibilityContract ограничены types/nodes/cost/time
- Company issuer может выдать specialist edge; parent не наследует closed PII
- Plan patch не меняет начатые step IDs/digests и не скрывает outstanding unknown/reserves

Проверка: Unauthorized graph/cycle/child scope, parent revoke и repeated decisions blocked; accepted checkpoint переживает crash; Удалить unknown effect из graph и повторить под новым ID запрещено; terminal success blocked.

Review: Astra/high (independent adversarial review).

<a id="kal-050"></a>

### KAL-050 — Эксперимент области: goal change, handoff, stop

P1 · L · Tester + independent critic · `planned`

Зависимости: KAL-027, KAL-028, KAL-044, KAL-049. Контракты: [HLD.md](../HLD.md), [TRACEABILITY.md](../TRACEABILITY.md).

Результаты: delegated area experiment report; R4 behavioral evidence.

Приёмка:

- Агент выбирает метод и реагирует на новые разрешённые observations
- Изменение цели/ответственного явно принято; stop не теряет pending effects/резервы

Проверка: Сменить goal/source condition, отказать grant, передать человеку и остановить во время unknown effect.

Review: Astra/high (independent adversarial review).

## I10 — Поддерживаемая эксплуатация (R4)

<a id="kal-051"></a>

### KAL-051 — Один installer/reference profile и first-use wizard

P1 · L · Operations engineer · `planned`

Зависимости: KAL-004, KAL-020, KAL-030. Контракты: [lld/OPERATIONS.md](../lld/OPERATIONS.md).

Результаты: packaged deployment manifest; installation/preflight guide.

Приёмка:

- Control plane/PG/executor/broker dependency versions и checks закреплены
- Setup idempotent; не меняет VPN/routes/DNS; выводит actual trust profile

Проверка: Чистая площадка, повтор setup и прерывание каждого шага; новый оператор создаёт первый полезный case.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-052"></a>

### KAL-052 — Source authentication, rotation и disconnect

P1 · M · Operations engineer · `planned`

Зависимости: KAL-021, KAL-022, KAL-024, KAL-051. Контракты: [lld/OPERATIONS.md](../lld/OPERATIONS.md).

Результаты: source credential onboarding; rotation/disconnect runbook.

Приёмка:

- Credentials вне executor/prompt/logs; scopes и дата expiry видны owner
- Disconnect закрывает новые calls/refresh/cache, историю отображает с ограничениями

Проверка: Rotate/revoke токен при queued и active calls; support export не содержит secret.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-053"></a>

### KAL-053 — Backup, lost tail, restore quarantine и epoch

P1 · L · Operations engineer · `planned`

Зависимости: KAL-010, KAL-013, KAL-014, KAL-016, KAL-051. Контракты: [lld/OPERATIONS.md](../lld/OPERATIONS.md).

Результаты: backup/restore runbook; recovery epoch gate.

Приёмка:

- Consistent backup включает journal/ledger/idempotency/definitions и keys plan
- Restore не dispatch до lost-tail/effect reconciliation и актуальных grants

Проверка: Backup→effect+revoke→потеря базы→restore: отсутствующий effect не повторён, старое право не ожило; измерить RPO/RTO.

Review: Astra/high (independent adversarial review).

<a id="kal-054"></a>

### KAL-054 — Upgrade/migration/replay и safe rollback

P1 · L · Operations engineer · `planned`

Зависимости: KAL-015, KAL-041, KAL-042, KAL-048, KAL-053. Контракты: [lld/OPERATIONS.md](../lld/OPERATIONS.md).

Результаты: version compatibility matrix; checkpointed migration tooling.

Приёмка:

- Active runs/views/pack versions и schema preflight проверены
- Rollback не unsend и не включает второй writer; incompatible objects blocked

Проверка: Прерывать миграцию; replay без effects; pinned view stale schema; app rollback читает новую историю безопасно.

Review: Astra/high (independent adversarial review).

<a id="kal-055"></a>

### KAL-055 — Observability, cost/unknown panel, support и exit

P1 · M · Operations engineer · `planned`

Зависимости: KAL-023, KAL-044, KAL-051, KAL-052, KAL-053. Контракты: [lld/OPERATIONS.md](../lld/OPERATIONS.md).

Результаты: health/cost dashboards; redacted support/export/exit runbooks.

Приёмка:

- Operator видит stale source, approvals, unknown, spend/reserve и backup status
- Export выборочный; no secrets/private transcripts по умолчанию; данные переносимы

Проверка: Новый оператор разбирает source outage/unknown cost и отключает pack без потери accepted history.

Review: Independent engineer; material changes reviewed by Astra/high.

## I11 — Полный pack и candidate gate (R4)

<a id="kal-056"></a>

### KAL-056 — Client Delivery pack manifest и supported scope

P1 · M · Engineer · `planned`

Зависимости: KAL-026, KAL-028, KAL-041, KAL-046, KAL-051. Контракты: [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md), [lld/WORK_MODEL.md](../lld/WORK_MODEL.md).

Результаты: pack manifest/version; contracts/views/policy defaults.

Приёмка:

- Manifest связывает process/version/adapters/methods/views/roles и requirements
- Нет unsupported ERP/provider promises; одна documented supported profile

Проверка: Install manifest на чистой reference среде и проверить version mismatch/invalid policy rejected.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-057"></a>

### KAL-057 — Demo fixtures, real onboarding и documentation

P1 · M · Engineer · `planned`

Зависимости: KAL-030, KAL-050, KAL-056. Контракты: [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md), [lld/WORK_MODEL.md](../lld/WORK_MODEL.md).

Результаты: synthetic fixture suite; pack user/operator guides.

Приёмка:

- Demo не содержит user data/secrets; показывает claims/acceptance/unknown честно
- Guide объясняет first value, права, correction/stop и частичную полноту

Проверка: Новый координатор проходит несколько original business cases без DSL editing.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-058"></a>

### KAL-058 — Compatibility и replay candidate audit

P1 · M · Tester + independent critic · `planned`

Зависимости: KAL-015, KAL-054, KAL-056. Контракты: [HLD.md](../HLD.md), [TRACEABILITY.md](../TRACEABILITY.md).

Результаты: compatibility/replay evidence; migration acceptance report.

Приёмка:

- Existing packs/events остаются читаемы; новые contract routes отдельно versioned
- Replay/upgrade не повторяет effects; old unsafe writes explicitly rejected

Проверка: Golden history, legacy read clients и interrupted import/candidate downgrade scenarios.

Review: Astra/high (independent adversarial review).

<a id="kal-059"></a>

### KAL-059 — Clean-room candidate и Astra/high adversarial review

P1 · L · Tester + independent critic · `planned`

Зависимости: KAL-050, KAL-053, KAL-055, KAL-057, KAL-058. Контракты: [HLD.md](../HLD.md), [TRACEABILITY.md](../TRACEABILITY.md).

Результаты: candidate artifact inventory; independent review + repair evidence.

Приёмка:

- Candidate воспроизводим из закреплённого source/diff, supported dependencies/pack versions
- Независимый критик пытается опровергнуть исходные AC; material findings repaired

Проверка: Чистая install→first case→effect unknown→revoke→restore→upgrade→exit матрица с record outcome.

Review: Astra/high (independent adversarial review).

<a id="kal-060"></a>

### KAL-060 — R4 release candidate decision и support ownership

P1 · S · Product owner + process owner · `planned`

Зависимости: KAL-059. Контракты: [PRODUCT_CONTRACT.md](../PRODUCT_CONTRACT.md), [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md).

Результаты: R4 gate decision; support/incident ownership.

Приёмка:

- Есть owner поддержки, severity/escalation и known limits
- RC не объявлен коммерчески повторяемым до второй компании; blockers закрыты

Проверка: Process/security/operations owners принимают evidence по AC-01–AC-09 и remaining limits.

Review: Astra/high (independent adversarial review).

## I12 — Вторая компания и повторяемость (R5)

<a id="kal-061"></a>

### KAL-061 — Вторая компания: независимый selector и setup

P1 · L · Product owner + process owner · `planned`

Зависимости: KAL-060. Контракты: [PRODUCT_CONTRACT.md](../PRODUCT_CONTRACT.md), [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md).

Результаты: second-company eligibility; independent setup evidence.

Приёмка:

- Другой owner/data environment проходит тот же selector
- Результаты первой компании не выдаются второй как её опыт/данные

Проверка: Setup и first useful case оператором второй компании по supported guide без developer rescue.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-062"></a>

### KAL-062 — Reuse через configuration вместо code fork

P1 · M · Engineer · `planned`

Зависимости: KAL-056, KAL-061. Контракты: [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md), [lld/WORK_MODEL.md](../lld/WORK_MODEL.md).

Результаты: configuration portability report; pack extension decisions.

Приёмка:

- Различия полей/policies/views оформлены versioned configuration/overlay
- Не возникает отдельный runtime fork для обычной работы компании

Проверка: Upgrade общего pack сохраняет локальные настройки и isolation обоих клиентов.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-063"></a>

### KAL-063 — Support burden и lifecycle economics

P1 · M · Operations engineer · `planned`

Зависимости: KAL-055, KAL-061, KAL-062. Контракты: [lld/OPERATIONS.md](../lld/OPERATIONS.md).

Результаты: support workload report; unit-cost model.

Приёмка:

- Измерены install/upgrade/recovery/support часы, model/infra costs
- Фиксируются ручные вмешательства, unknown/incidents и hidden work

Проверка: Повторить отказ/restore со вторым оператором; cost reconciliation сравнить с реальными usage receipts.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-064"></a>

### KAL-064 — Повторная ценность против текущей/native работы

P1 · L · Product owner + process owner · `planned`

Зависимости: KAL-031, KAL-061, KAL-063. Контракты: [PRODUCT_CONTRACT.md](../PRODUCT_CONTRACT.md), [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md).

Результаты: second-company comparison series; transferability evidence.

Приёмка:

- Benefit/quality измерены по новым заранее согласованным thresholds
- Перенос метода не предполагается без applicability/evaluation

Проверка: Новая серия включает отрицательные cases/ограниченные channels и полную цену review.

Review: Independent engineer; material changes reviewed by Astra/high.

<a id="kal-065"></a>

### KAL-065 — R5 gate: коммерческая повторяемость или исправление

P1 · S · Product owner + process owner · `planned`

Зависимости: KAL-062, KAL-063, KAL-064. Контракты: [PRODUCT_CONTRACT.md](../PRODUCT_CONTRACT.md), [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md).

Результаты: R5 readiness decision; supported offer boundaries.

Приёмка:

- Подтверждены useful repeatability и предсказуемый lifecycle без fork
- Не прошедший gate возвращает к narrow/repair с причиной, без объявления готовой платформы

Проверка: Buyer/operator рассматривают два независимых evidence наборa и agreed acceptance thresholds.

Review: Independent engineer; material changes reviewed by Astra/high.

## I13 — Выбор второго процесса (R6)

<a id="kal-066"></a>

### KAL-066 — Выбрать одну вторую вертикаль по evidence

P2 · M · Product owner + process owner · `planned`

Зависимости: KAL-065. Контракты: [PRODUCT_CONTRACT.md](../PRODUCT_CONTRACT.md), [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md).

Результаты: Order Exceptions vs Document Close decision; second process selector.

Приёмка:

- Есть реальный buyer/owner/данные/частые cases выбранной вертикали
- Не разрабатываются одновременно оба packs; сравнение native alternative учтено

Проверка: Selector + full-cost baseline на одинаковых cases выбранного процесса.

Review: Independent engineer; material changes reviewed by Astra/high. Условие: Удовлетворено условие release и выбран конкретный scope.

<a id="kal-067"></a>

### KAL-067 — Контракты второго процесса и границы ontology

P2 · M · Engineer · `planned`

Зависимости: KAL-066. Контракты: [lld/WORK_MODEL.md](../lld/WORK_MODEL.md), [lld/AGENT_PROTOCOL.md](../lld/AGENT_PROTOCOL.md).

Результаты: second pack domain/action schemas; mapping to common contracts.

Приёмка:

- Shared abstractions переиспользуются только при semantic compatibility
- Новые business terms/acceptance/risk не насильно превращаются в Client Delivery

Проверка: Различные meanings похожих полей и спорный outcome не смешиваются автоматически.

Review: Independent engineer; material changes reviewed by Astra/high. Условие: Удовлетворено условие release и выбран конкретный scope.

<a id="kal-068"></a>

### KAL-068 — Source/ACL/risk для второй вертикали

P2 · M · Engineer · `planned`

Зависимости: KAL-066, KAL-067. Контракты: [lld/INTEGRATIONS.md](../lld/INTEGRATIONS.md), [lld/EXECUTION.md](../lld/EXECUTION.md).

Результаты: second pack source bindings; risk policy/source authority map.

Приёмка:

- Выбраны actual systems/scopes и primary writer каждого limited scope
- Financial/doc-sensitive effects получают отдельные bounds/review

Проверка: Broad source credential/duplicate transaction/document version и revoke не обходят local authority.

Review: Astra/high (independent adversarial review). Условие: Удовлетворено условие release и выбран конкретный scope.

<a id="kal-069"></a>

### KAL-069 — Проверить перенос опыта и стоимость адаптации

P2 · M · Engineer · `planned`

Зависимости: KAL-047, KAL-066, KAL-067. Контракты: [lld/EXPERIENCE.md](../lld/EXPERIENCE.md), [lld/AGENT_PROTOCOL.md](../lld/AGENT_PROTOCOL.md).

Результаты: cross-process method applicability report; new negative cases.

Приёмка:

- Метод переносится только при authorised applicability и новой evaluation
- Издержки адаптации/поддержки учитываются, client data не мигрируют как method

Проверка: Применить старый метод к отличающемуся процессу; corpus должен поймать неверные assumptions.

Review: Independent engineer; material changes reviewed by Astra/high. Условие: Удовлетворено условие release и выбран конкретный scope.

<a id="kal-070"></a>

### KAL-070 — R6 entry: scope, buyer и stop criteria

P2 · S · Product owner + process owner · `planned`

Зависимости: KAL-067, KAL-068, KAL-069. Контракты: [PRODUCT_CONTRACT.md](../PRODUCT_CONTRACT.md), [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md).

Результаты: R6 scope gate; second pack acceptance thresholds.

Приёмка:

- Owner принимает ограниченный scope/value hypothesis, source access и support cost
- Handover отложен без доказанного writer/reconcile/fallback плана

Проверка: Независимый review выбранного процесса пытается показать более дешёвую штатную альтернативу.

Review: Astra/high (independent adversarial review). Условие: Удовлетворено условие release и выбран конкретный scope.

## I14 — Второй pack и handover (R6)

<a id="kal-071"></a>

### KAL-071 — Поддерживаемые adapters второго pack

P2 · L · Engineer · `planned`

Зависимости: KAL-068, KAL-070. Контракты: [lld/INTEGRATIONS.md](../lld/INTEGRATIONS.md), [lld/EXECUTION.md](../lld/EXECUTION.md).

Результаты: chosen source adapter versions; connector fault fixtures.

Приёмка:

- Поддержаны exact source contracts/versions и ACL по declared profile
- Нет общей готовности ERP suite; unknown/retry/idempotency endpoint-specific

Проверка: Remote schema drift, partial page, lost receipt и permission downgrade scenarios.

Review: Independent engineer; material changes reviewed by Astra/high. Условие: Удовлетворено условие release и выбран конкретный scope.

<a id="kal-072"></a>

### KAL-072 — Views, methods и process flow второго pack

P2 · L · Engineer · `planned`

Зависимости: KAL-067, KAL-069, KAL-071. Контракты: [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md), [lld/WORK_MODEL.md](../lld/WORK_MODEL.md).

Результаты: second pack manifest/views/methods; onboarding cases.

Приёмка:

- UI под роли использует тот же trusted catalog, новый process acceptance сохранён
- Install/config/support guide дают first useful result выбранному owner

Проверка: Новый пользователь проходит ordinary, disputed и missing-source cases без чужого контекста.

Review: Independent engineer; material changes reviewed by Astra/high. Условие: Удовлетворено условие release и выбран конкретный scope.

<a id="kal-073"></a>

### KAL-073 — Selective AuthorityTransition одного scope

P2 · L · Engineer · `planned`

Зависимости: KAL-012, KAL-014, KAL-053, KAL-068, KAL-071. Контракты: [lld/INTEGRATIONS.md](../lld/INTEGRATIONS.md), [lld/EXECUTION.md](../lld/EXECUTION.md).

Результаты: writer cutover/reconciliation protocol; manual fallback runbook.

Приёмка:

- Scope/cutover owner/old/new writer и boundaries явно приняты
- Shadow comparison не пишет; cutover/rollback исключают simultaneous authoritative writers

Проверка: Network partition/late legacy write/failed cutover: одна область blocked или один writer, данные reconciled.

Review: Astra/high (independent adversarial review). Условие: Удовлетворено условие release и выбран конкретный scope.

<a id="kal-074"></a>

### KAL-074 — Вторая серия value/fault/ops verification

P2 · L · Tester + independent critic · `planned`

Зависимости: KAL-072, KAL-073. Контракты: [HLD.md](../HLD.md), [TRACEABILITY.md](../TRACEABILITY.md).

Результаты: second pack pilot/restore evidence; independent review dispositions.

Приёмка:

- Value измерен с полной ценой новой вертикали
- Handover/restore/revoke/unknown defects исправлены до readiness decision

Проверка: Crash на cutover, потерянный хвост после backup и ошибочное acceptance по новым criteria.

Review: Astra/high (independent adversarial review). Условие: Удовлетворено условие release и выбран конкретный scope.

<a id="kal-075"></a>

### KAL-075 — R6 release decision и предел поддержки

P2 · S · Product owner + process owner · `planned`

Зависимости: KAL-074. Контракты: [PRODUCT_CONTRACT.md](../PRODUCT_CONTRACT.md), [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md).

Результаты: R6 gate decision; second pack support matrix.

Приёмка:

- Есть поддерживаемые process/source/version limits и owner
- Новая отрасль не объявляется универсальным поглощением legacy данных

Проверка: Process/operations/security owner принимают evidence; failures возвращают scope в read-only/manual.

Review: Independent engineer; material changes reviewed by Astra/high. Условие: Удовлетворено условие release и выбран конкретный scope.

## I15 — Новые границы: эксперимент (R7)

<a id="kal-076"></a>

### KAL-076 — Third-party tools: TCB и bounded extension experiment

P2 · L · Engineer · `planned`

Зависимости: KAL-075. Контракты: [lld/AUTHORITY_SECURITY.md](../lld/AUTHORITY_SECURITY.md), [lld/EXECUTION.md](../lld/EXECUTION.md).

Результаты: extension trust/permission proposal; isolated adapter prototype.

Приёмка:

- Каждое extension имеет typed contract, issuer scopes, egress limits и revoke
- Plugin не получает общие secrets/DB/proxy bypass

Проверка: Malicious tool/redirect/exfiltration/recursive delegation blocked в prototype.

Review: Astra/high (independent adversarial review). Условие: Только если выбран именно этот R7 эксперимент и есть owner/request.

<a id="kal-077"></a>

### KAL-077 — Federation: ответственность и receipt semantics

P2 · L · Tech lead · `planned`

Зависимости: KAL-073, KAL-075. Контракты: [HLD.md](../HLD.md), [DECISIONS.md](../DECISIONS.md).

Результаты: federation contract experiment; cross-company dispute/reconcile protocol.

Приёмка:

- Company authority, audience и protocol versions независимы
- At-least-once/dedup/unknown явно описаны без global exactly-once

Проверка: Partition/duplicate/counterparty revoke/conflicting acceptance оставляют explainable responsibility.

Review: Astra/high (independent adversarial review). Условие: Только если выбран именно этот R7 эксперимент и есть owner/request.

<a id="kal-078"></a>

### KAL-078 — Machine-first business creation в ограниченном sandbox

P2 · L · Product owner + process owner · `planned`

Зависимости: KAL-049, KAL-050, KAL-075. Контракты: [PRODUCT_CONTRACT.md](../PRODUCT_CONTRACT.md), [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md).

Результаты: business creation scenarios; human responsibility constraints.

Приёмка:

- Сценарий создаёт bounded область работы с issuer/owner/stop, не собственные права
- Проверяются отличие нового бизнеса от автоматизации workflow и его экономика

Проверка: Agent пытается сам открыть scope/потратить чужой бюджет/назначить human acceptance — blocked.

Review: Independent engineer; material changes reviewed by Astra/high. Условие: Только если выбран именно этот R7 эксперимент и есть owner/request.

<a id="kal-079"></a>

### KAL-079 — Interop опыта: provenance, portability, licensing

P2 · M · Engineer · `planned`

Зависимости: KAL-069, KAL-075. Контракты: [lld/EXPERIENCE.md](../lld/EXPERIENCE.md), [lld/AGENT_PROTOCOL.md](../lld/AGENT_PROTOCOL.md).

Результаты: method exchange proposal; permission/applicability evaluation.

Приёмка:

- Передаваемый метод отделён от confidential cases/evidence
- Получатель заново подтверждает applicability, owner/expiry/revoke и разрешение переноса

Проверка: Отозванный/заражённый/чужой метод и hidden client literal обнаружены до selection.

Review: Astra/high (independent adversarial review). Условие: Только если выбран именно этот R7 эксперимент и есть owner/request.

<a id="kal-080"></a>

### KAL-080 — R7 decision: продолжить, сузить или отказаться

P2 · S · Product owner + process owner · `planned`

Зависимости: KAL-075. Контракты: [PRODUCT_CONTRACT.md](../PRODUCT_CONTRACT.md), [packs/CLIENT_DELIVERY.md](../packs/CLIENT_DELIVERY.md).

Результаты: experimental boundary review; new ADR/roadmap revision.

Приёмка:

- Отобранные востребованные experiments проверены; остальные явно deferred и не требуются для gate
- Неподтверждённые future universes остаются исследованием, не release promises

Проверка: Astra/high пытается опровергнуть authority/recovery/economics; owner принимает scoped next decision.

Review: Astra/high (independent adversarial review). Условие: Удовлетворено условие release и выбран конкретный scope.
