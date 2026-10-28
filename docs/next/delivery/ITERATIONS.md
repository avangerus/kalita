# Итерации N1

**Статус:** I00–I15 planned. Итерации — ограниченные work packages, не calendar sprints. [TASKS.json](TASKS.json) задаёт реальную dependency DAG; [RELEASES](RELEASES.md) определяет gates.

Общий вход: подтверждены dependencies выбранного task, доступны тестовые данные/среда, ownership файлов согласован. Общий выход: deliverables воспроизводимы, acceptance и original scenario проверены, material independent review исправлен. Для pilot/market tasks нужен actual owner/data; synthetic demo не заменяет evidence.

## I00 — Baseline и выбор первого пути

Release: R0. Зависимые итерации: нет.

Цель: Снять truth, проверить покупателя и долговечное исполнение.

Работа и проверяемый выход:

| Task | Результат | Проверка выхода |
|---|---|---|
| [KAL-001](TASKS.md#kal-001) | Inventory checkout, journaling и compatibility | HEAD/diff и existing egress delivery отделены от release evidence; Task/runner failure paths и поддерживаемые API записаны с воспроизведением |
| [KAL-002](TASKS.md#kal-002) | Первый buyer, процесс, baseline и pilot thresholds | Есть полномочный owner и повторяемый дорогой результат; До пилота зафиксированы время, качество, полная стоимость и stop thresholds |
| [KAL-003](TASKS.md#kal-003) | PG против Temporal: bounded fault/ops prototype | Crash/lease/fence/budget/unknown restore сценарии оценены по заранее объявленным критериям; Выбран ровно один adapter либо реализация blocked; ownership ADR обновлён |
| [KAL-004](TASKS.md#kal-004) | Два источника и один deployment profile | Выбраны конкретные две системы, read scopes, owner и доступ; Выбран self-host либо managed путь с полной dependency/data-flow картой |
| [KAL-005](TASKS.md#kal-005) | Утвердить N1, инварианты и критерии gates | Новые правила явно supersede conflicts старых target docs; Все provisional решения имеют evidence/owner/stop; риск reviewed Astra/high |

Evidence: docs/next/AS_IS.md; baseline report; buyer/process selector; pilot measurement protocol; N-ADR-05 evidence; durability comparison report; SourceBinding selection; installation profile decision; docs/next/DECISIONS.md; release acceptance register.

Проверки: Снять targeted baseline tests; reproduce Append failure и stale completion без отката пользовательских edits; Сопоставить текущую работу и доступную штатную автоматизацию на одинаковой выборке; Убивать prototype на каждом commit boundary; parallel reserve; оценить install и operator recovery cost; Проверить source ACL и token revoke; пройти setup с будущим оператором без изменения сети PC; Независимо попытаться опровергнуть HLD на recovery, authority и first value сценариях.

При выходе root обновляет N-ADR и release thresholds; unresolved buyer/source/runtime решение остаётся blocked. Сам набор документов не закрывает эту итерацию.

## I01 — Контракты работы и атомарная база

Release: R1. Зависимые итерации: I00.

Цель: Закрепить work/authority/API contracts и Tx boundary.

Работа и проверяемый выход:

| Task | Результат | Проверка выхода |
|---|---|---|
| [KAL-006](TASKS.md#kal-006) | Минимальный WorkCase и стабильная идентичность | Case имеет owner, результат, refs/revision и не зависит от chat ID; Простой случай не требует всех сущностей платформы |
| [KAL-007](TASKS.md#kal-007) | Source authority и mapping contracts | Каждое поле/область имеет primary writer и freshness; Binding не превращает source credential в права всех пользователей |
| [KAL-008](TASKS.md#kal-008) | Canonical contracts, versioning и transport parity | DSL adapter даёт version/digest и typed action contract; REST/MCP используют одинаковые hooks/errors/If-Match/idempotency |
| [KAL-009](TASKS.md#kal-009) | ResponsibilityContract и рискованные переходы | Цель/критерии/grant отделены от execution plan revision; Handoff и изменение условий требуют authorised decision |
| [KAL-010](TASKS.md#kal-010) | Composable PG Tx API: ledger + journal + outbox | Canonical event и operational state коммитятся/откатываются вместе; Все append paths соблюдают lock order, без nested pool Append; projections после commit; Durable aggregate/authority heads и applied-head barrier запрещают stale owner/criteria/ACL admission |

Evidence: proposed WorkCase contract; pack domain schema; SourceBinding schema; authority map; canonical contract schema; REST/MCP compatibility matrix; responsibility/amendment schemas; risk policy contract; internal/eventstore Tx extension (proposed); transaction/lock-order tests.

Проверки: Закрыть чат, сменить actor/UI и открыть тот же case без потери условий; Read через broad credential скрывает чужие object/fields и отмечает stale source; Одинаковые forbidden/conflict/duplicate запросы обоих transports дают эквивалентный исход; Model plan patch не меняет цель, сумму, права или подтверждённое обязательство; Fault injection до/после commit, конкурирующий append/reserve и deadlock retry не расходят ledger/journal.

Решение выхода: owner принимает evidence либо возвращает конкретные tasks на repair; успешная сборка без исходного сценария не закрывает итерацию.

## I02 — Долговечный execution ledger

Release: R1. Зависимые итерации: I01.

Цель: Run/Attempt, budgets, effects, безопасная миграция.

Работа и проверяемый выход:

| Task | Результат | Проверка выхода |
|---|---|---|
| [KAL-011](TASKS.md#kal-011) | Run/Attempt lease, heartbeat и fencing | Параллельный claim не выдаёт одну Attempt дважды; Expired heartbeat/complete и прежний fence отклоняются |
| [KAL-012](TASKS.md#kal-012) | Retry, cancel, DLQ и committed expiration | Expiration меняет state только после commit; Cancel останавливает будущие шаги; DLQ retry сохраняет историю и authority |
| [KAL-013](TASKS.md#kal-013) | Общий reservation/settlement ledger | Children/calls/retry учитываются общим root без double count; Stable reservation key и duplicate receipt дают один settlement; unknown держит reserve |
| [KAL-014](TASKS.md#kal-014) | Effect intent, dispatch permit и unknown | Intent и reserve durable до сети; revoke сериализуется с permit; Unknown не переводится в failed/успех по отчёту worker и не повторяется вслепую |
| [KAL-015](TASKS.md#kal-015) | Миграция legacy tasks и write protocol | Legacy reads сохраняются; writes требуют Attempt ID/token/fence; Old writers drained; ambiguity blocked; cutover с одним writer |

Evidence: Run/Attempt schema; claim/heartbeat service; execution state machine; operator DLQ commands; budget schema; reserve/settle service; effect ledger schema; dispatch/reconcile protocol; migration preflight/dry-run; versioned worker handshake.

Проверки: A истёк; новая Attempt того же actor; старый A complete не меняет запись; Append/Tx failure, restart waiting_retry и отмена до/после dispatch дают оговорённые состояния; Параллельные child/retry не превышают hard cap; повтор receipt не удваивает списание; Crash после remote acceptance до receipt удерживает reserve и блокирует unsafe retry; Legacy complete от того же actor после re-claim rejected; interrupted import идемпотентен.

Решение выхода: owner принимает evidence либо возвращает конкретные tasks на repair; успешная сборка без исходного сценария не закрывает итерацию.

## I03 — Broker и ограниченный агент

Release: R1. Зависимые итерации: I02.

Цель: Enforced isolation, grants, model caps, durable loop.

Работа и проверяемый выход:

| Task | Результат | Проверка выхода |
|---|---|---|
| [KAL-016](TASKS.md#kal-016) | Grant lineage, issuer edges и revocation | Child проверяет всю активную parent lineage до каждого protected call; Дополнительное specialist право создаёт самостоятельный issuer decision; parent PII не расширяется |
| [KAL-017](TASKS.md#kal-017) | Enforced executor boundary и broker credentials | Executor не имеет DB, long-lived secrets и direct outbound; Trusted adapter/dev явно маркированы; indirect redirects/DNS/telemetry gated |
| [KAL-018](TASKS.md#kal-018) | Context и model broker: hard bounds и provenance | Input/output/duration/call caps и conservative cost reserve enforceable; Provider без bound доступен только soft mode; unknown billing держит максимум |
| [KAL-019](TASKS.md#kal-019) | Bounded durable agent harness | Accepted proposal/next checkpoint атомарны; replay не повторяет committed tool; Round/graph/time/attempt limits останавливают loop; worker не пишет done до server confirmation |
| [KAL-020](TASKS.md#kal-020) | R1 adversarial execution acceptance | Пройдены Tx/fence/parallel budget/revoke/effect unknown матрицы; Один adapter, no direct bypass; material review defects устранены |

Evidence: grant/policy service; lineage revocation tests; isolated executor profile; trusted tool/model broker; context/model call schemas; pricing revision registry; agent intent loop/checkpoints; updated worker protocol; R1 test evidence; Astra/high review dispositions.

Проверки: Revoke root при живом child/cached context блокирует новые reads/model/tool/effects; Попробовать прямой socket/secret/DB, rebinding, attachment redirect и prompt injection; Lost response/provider cap failure/actual>bound выявлены, новые calls остановлены, ACL скрывает private refs; Crash между model response/tool commit/next decision; repeated proposal и invalid tool завершаются безопасно; Review против исходных N-02–N-06; попытка обхода через MCP/automation/webhook.

Решение выхода: owner принимает evidence либо возвращает конкретные tasks на repair; успешная сборка без исходного сценария не закрывает итерацию.

## I04 — Два источника и intake

Release: R2. Зависимые итерации: I03.

Цель: Подключить выбранные источники, freshness/ACL и очередь.

Работа и проверяемый выход:

| Task | Результат | Проверка выхода |
|---|---|---|
| [KAL-021](TASKS.md#kal-021) | Первый intake adapter по выбранному binding | Поддержаны реальные IDs/версии/ACL и read-only scope; Cursor/checkpoint сохраняется; повтор входа не создаёт новый Claim |
| [KAL-022](TASKS.md#kal-022) | Второй intake adapter по выбранному binding | Есть отдельный source owner, mapping и error taxonomy; Данные связаны с case только по проверяемому mapping, без глобального импорта |
| [KAL-023](TASKS.md#kal-023) | Freshness, completeness и source reconciliation | Observation содержит source time/receive time/version и доступный период; Gap/partial source явно виден; stale не изображается complete/current |
| [KAL-024](TASKS.md#kal-024) | ACL на intake, search, context и derived outputs | Одна граница company/client действует на snippets/counts/context/caches; Удаление/отзыв источника блокирует новые derivations и отображается честно |
| [KAL-025](TASKS.md#kal-025) | Очередь кейсов и кандидатов обязательств | Любое обещание сначала candidate со source quotation/ref; Пользователь видит owner, deadline, blocker и boundary наблюдения |

Evidence: source A adapter; source contract fixtures; source B adapter; source contract fixtures; observation/cursor service; intake health view; source ACL mapping; privacy regression fixtures; first queue/card UI; candidate extraction method.

Проверки: Duplicate page, token revoke, schema mismatch и source downtime на fixture + разрешённом pilot sample; Конфликтующие IDs разных источников, private attachment и истёкшие permissions не смешиваются; Pagination gap, late update, missing page и replay checkpoint не теряются и не дублируются; Чужой клиент через поиск, ID, count, report и parent child summary не раскрывается; Вопрос/предположение/цитата чужой стороны не подтверждаются автоматически; закрытие чата сохраняет case.

Решение выхода: owner принимает evidence либо возвращает конкретные tasks на repair; успешная сборка без исходного сценария не закрывает итерацию.

## I05 — Подтверждённые обязательства

Release: R2. Зависимые итерации: I04.

Цель: Confirm/amend, handoff, acceptance и pilot onboarding.

Работа и проверяемый выход:

| Task | Результат | Проверка выхода |
|---|---|---|
| [KAL-026](TASKS.md#kal-026) | Confirm/amend/withdraw обязательств | Confirm пишет exact terms/actor/basis; amendment сохраняет прежние условия; Ожидаемая revision обязательна; изменение стороны/срока пересогласуется |
| [KAL-027](TASKS.md#kal-027) | Передача ответственности и следующий владелец | Новое владение начинается при явном принятии; отказ сохраняет прежнего owner; Pending effects/reserves и ограничения видны в допустимом объёме |
| [KAL-028](TASKS.md#kal-028) | Результат, Evidence, Acceptance и спор | Report модели отделён от результата/решения получателя; Acceptance привязан к criteria revision; новое свидетельство не переписывает прошлое |
| [KAL-029](TASKS.md#kal-029) | Revocation и client isolation end-to-end | Отзыв применяется в открытом UI, context/search и background processing; Closed/foreign evidence не раскрывается через summaries/errors/counts |
| [KAL-030](TASKS.md#kal-030) | Onboarding value pilot и operator fallback | Owner выбирает источники и пределы; first useful case проходится без developer editing; Есть понятный разбор ложного Claim/stale source/blocked run и прекращение пилота |

Evidence: obligation commands/workflow; versioned criteria contract; handoff offer/accept contract; owner/wait UI; result/accept/dispute commands; criteria review UI; pilot security evidence; cache/subscription invalidation; pilot guide; manual correction/escalation path.

Проверки: Два подтверждения/устаревшая revision/новая цитата не молча перезаписывают обещание; Handoff отклонён/owner потерял права/ожидание без ответа не оставляют бесхозную работу; Исполнитель пытается сам принять существенный результат; спор/conditional acceptance сохраняют авторство; Отозвать доступ между query, confirm и report; другой клиент пробует shared refs; Новый координатор проходит setup→confirm→handoff→accept и исправляет неверный candidate.

Решение выхода: owner принимает evidence либо возвращает конкретные tasks на repair; успешная сборка без исходного сценария не закрывает итерацию.

## I06 — Серия проверки ценности

Release: R2. Зависимые итерации: I05.

Цель: Сравнить наблюдаемую пользу и полную стоимость.

Работа и проверяемый выход:

| Task | Результат | Проверка выхода |
|---|---|---|
| [KAL-031](TASKS.md#kal-031) | Measurement protocol и comparison выборка | Окно, критерии выборки и thresholds утверждены до series; Текущий метод и native alternative имеют те же данные/условия |
| [KAL-032](TASKS.md#kal-032) | Серия Client Delivery value pilot | Повторяемые cases измерены по времени/пропускам/исправлениям; Качество наблюдаемых источников отделено от общей полноты компании |
| [KAL-033](TASKS.md#kal-033) | Failure/revoke/downtime audit пилота | Материальные утечки/дубли/неконтролируемый расход разбираются до continuation; Downtime/injection/late evidence включены в наблюдаемый исход |
| [KAL-034](TASKS.md#kal-034) | Полная экономика и operational burden | Учитываются модели, настройка, ручной review, инфраструктура и поддержка; ROI выводится из согласованного метода; uncertain estimates обозначены |
| [KAL-035](TASKS.md#kal-035) | R2 decision: continue, narrow или stop | Продолжение разрешено только при согласованных quality/value/cost thresholds; Один workflow не объявляется доказанной delegated organization |

Evidence: baseline/comparison dataset; measurement definitions; pilot outcome dataset; user feedback; pilot fault evidence; incident dispositions; full-cost comparison report; support time log; R2 gate decision; updated pack scope.

Проверки: Проверить inclusion отрицательных случаев и missing-channel denominator без post-hoc выбора успехов; Провести серию по согласованной выборке; вручную проверить ложные обещания и исключённые случаи; Повторить worst pilot failure и подтвердить stop/escalation на исходном бизнес-сценарии; Сравнить результат со штатной альтернативой с учётом всех ручных действий; Process owner рассматривает полную серию; failed gate даёт исправление/новый измеряемый probe.

Решение выхода: owner принимает evidence либо возвращает конкретные tasks на repair; успешная сборка без исходного сценария не закрывает итерацию.

## I07 — Генеративная композиция UI

Release: R3. Зависимые итерации: I06.

Цель: ViewSpec/catalog/генератор/trusted actions.

Работа и проверяемый выход:

| Task | Результат | Проверка выхода |
|---|---|---|
| [KAL-036](TASKS.md#kal-036) | ViewSpec/revision/binding/action schemas | Композиция expressive, но декларативная: нет arbitrary HTML/JS/SQL/network; Audience/bindings/version refs и action intents имеют schemas |
| [KAL-037](TASKS.md#kal-037) | Versioned component catalog и trusted renderer | Компоненты имеют typed inputs, accessibility и безопасные slots; ACL авторитетен на backend; renderer не принимает данные чужого audience |
| [KAL-038](TASKS.md#kal-038) | UI-agent composition и human repair | Генератор выбирает подходящий layout/data bindings для нескольких tasks; Invalid proposal имеет repair/fallback; генерация не публикует автоматически |
| [KAL-039](TASKS.md#kal-039) | Trusted consequential action confirmation | Confirmation строится из canonical connector args доверенным кодом; Recipient/amount/object/limits/version видны и связаны с nonce/digest |
| [KAL-040](TASKS.md#kal-040) | View safety, ACL и useful interaction verification | Agent composition полезна для нескольких ролей без единого fixed screen; Renderer/read/action paths сохраняют current rights и safe catalog boundary; publication/snapshot проверяются KAL-041/042 |

Evidence: ViewSpec contract; view version registry; catalog contracts; renderer components; view generator role; generation evaluation fixtures; action preview/challenge UI; digest binding service; view regression evidence; usability review.

Проверки: Попытки unsupported node/version/injection/oversized graph отклонены валидатором; Keyboard/mobile/empty/redacted/error states и unsafe literal/URL проверены; Сравнить queue/card/comparison views и попытку private context reuse вне audience; Generated label ложно обещает действие; изменённый payload/expired approval отвергнуты до dispatch; Revoke while view open, mislabel, source injection и sensitive aggregate regression; user completes original task.

Решение выхода: owner принимает evidence либо возвращает конкретные tasks на repair; успешная сборка без исходного сценария не закрывает итерацию.

## I08 — Жизненный цикл UI и эффекты

Release: R3. Зависимые итерации: I07.

Цель: Pin/share/snapshot и ограниченный effect/reconcile.

Работа и проверяемый выход:

| Task | Результат | Проверка выхода |
|---|---|---|
| [KAL-041](TASKS.md#kal-041) | Ephemeral/pinned/shared live lifecycle | Pin сохраняет запрос/композицию, не snapshot; expiry чистит ephemeral по retention; Share проверяет bindings и generated literals/provenance private context |
| [KAL-042](TASKS.md#kal-042) | Snapshot/export и отдельные права копии | Snapshot фиксирует values/versions и отдельную аудиторию/retention; Пользователь видит ограничения отзыва уже скачанного экспорта |
| [KAL-043](TASKS.md#kal-043) | Объединить existing egress delivery с Effect broker | DeliveryID/DLQ/receipts не дублируются второй независимой delivery системой; Transitive automation использует тот же grant/intent/approval contract |
| [KAL-044](TASKS.md#kal-044) | Reconciliation и bounded external action pilot | Unknown/reserve видны owner и разбираются по endpoint contract; Retry не происходит без proven receiver idempotency или confirmed-not-applied |
| [KAL-045](TASKS.md#kal-045) | R3 adaptive views/effect alpha gate | View lifecycle, literals/ACL и trusted confirmations проверены end-to-end; Limited effect crash/reconcile proof есть; коммерческий pack ещё не объявлен |

Evidence: view lifecycle service; pin/share/audience UI; snapshot/export contract; snapshot retention UI; existing webhook integration mapping; one limited outbound connector; receipt lookup/manual reconcile UI; limited action evidence; R3 acceptance evidence; Astra/high review dispositions.

Проверки: Смена аудитории, revoke автора/читателя и удалённая source link не раскрывают старые private literals; Создать snapshot, поменять live данные, revoke/source erasure и проверить объявленную copy policy; Webhook retry, revoked grant и handler-triggered outgoing action проходят один authority gate; Lost response, duplicate callback, cancel-after-dispatch и stale executor receipt не создают второе действие; Review пытается утечь через shared snapshot и повторить неизвестный external effect.

Решение выхода: owner принимает evidence либо возвращает конкретные tasks на repair; успешная сборка без исходного сценария не закрывает итерацию.

## I09 — Методы и делегированная область

Release: R4. Зависимые итерации: I08.

Цель: Проверить experience, adaptive plan, goal change/handoff/stop.

Работа и проверяемый выход:

| Task | Результат | Проверка выхода |
|---|---|---|
| [KAL-046](TASKS.md#kal-046) | MethodRevision registry, owner и applicability | Метод содержит owner, scope, basis, version, expiry и revoke; Чужой client material не переносится без отдельного разрешения |
| [KAL-047](TASKS.md#kal-047) | Positive/negative/new-case method evaluation | Acceptance thresholds определены до method rollout; Есть новые/отрицательные cases, drift и невидимые источники; оценка не из success count |
| [KAL-048](TASKS.md#kal-048) | Model/method rollout и обратимое переключение | Runs сохраняют pinned revisions; новый метод не меняет старые obligations; Откат/revoke блокирует future selection, не переписывает receipts/acceptance |
| [KAL-049](TASKS.md#kal-049) | Adaptive plan patch и specialist delegation | Graph changes в ResponsibilityContract ограничены types/nodes/cost/time; Company issuer может выдать specialist edge; parent не наследует closed PII; Plan patch не меняет начатые step IDs/digests и не скрывает outstanding unknown/reserves |
| [KAL-050](TASKS.md#kal-050) | Эксперимент области: goal change, handoff, stop | Агент выбирает метод и реагирует на новые разрешённые observations; Изменение цели/ответственного явно принято; stop не теряет pending effects/резервы |

Evidence: method revision schema/service; method review UI; method test corpus; evaluation report; model/method compatibility policy; staged rollout controls; plan revision validator; delegation edges/child protocol; delegated area experiment report; R4 behavioral evidence.

Проверки: Попробовать применить чужой/expired/revoked метод и сохранить сырой prompt как approved experience; Новый метод ошибается на adversarial case; gate отклоняет до публикации; Смена provider/pricing/model ухудшает corpus; policy блокирует и переключает selection безопасно; Unauthorized graph/cycle/child scope, parent revoke и repeated decisions blocked; accepted checkpoint переживает crash; Сменить goal/source condition, отказать grant, передать человеку и остановить во время unknown effect.

Решение выхода: owner принимает evidence либо возвращает конкретные tasks на repair; успешная сборка без исходного сценария не закрывает итерацию.

## I10 — Поддерживаемая эксплуатация

Release: R4. Зависимые итерации: I03, I05.

Цель: Install, source auth, quarantine restore, upgrade, support.

Работа и проверяемый выход:

| Task | Результат | Проверка выхода |
|---|---|---|
| [KAL-051](TASKS.md#kal-051) | Один installer/reference profile и first-use wizard | Control plane/PG/executor/broker dependency versions и checks закреплены; Setup idempotent; не меняет VPN/routes/DNS; выводит actual trust profile |
| [KAL-052](TASKS.md#kal-052) | Source authentication, rotation и disconnect | Credentials вне executor/prompt/logs; scopes и дата expiry видны owner; Disconnect закрывает новые calls/refresh/cache, историю отображает с ограничениями |
| [KAL-053](TASKS.md#kal-053) | Backup, lost tail, restore quarantine и epoch | Consistent backup включает journal/ledger/idempotency/definitions и keys plan; Restore не dispatch до lost-tail/effect reconciliation и актуальных grants |
| [KAL-054](TASKS.md#kal-054) | Upgrade/migration/replay и safe rollback | Active runs/views/pack versions и schema preflight проверены; Rollback не unsend и не включает второй writer; incompatible objects blocked |
| [KAL-055](TASKS.md#kal-055) | Observability, cost/unknown panel, support и exit | Operator видит stale source, approvals, unknown, spend/reserve и backup status; Export выборочный; no secrets/private transcripts по умолчанию; данные переносимы |

Evidence: packaged deployment manifest; installation/preflight guide; source credential onboarding; rotation/disconnect runbook; backup/restore runbook; recovery epoch gate; version compatibility matrix; checkpointed migration tooling; health/cost dashboards; redacted support/export/exit runbooks.

Проверки: Чистая площадка, повтор setup и прерывание каждого шага; новый оператор создаёт первый полезный case; Rotate/revoke токен при queued и active calls; support export не содержит secret; Backup→effect+revoke→потеря базы→restore: отсутствующий effect не повторён, старое право не ожило; измерить RPO/RTO; Прерывать миграцию; replay без effects; pinned view stale schema; app rollback читает новую историю безопасно; Новый оператор разбирает source outage/unknown cost и отключает pack без потери accepted history.

Можно вести параллельно UI/experience после task prerequisites. Перед R4 обязательно пройти lost-tail restore и lifecycle с новым оператором.

## I11 — Полный pack и candidate gate

Release: R4. Зависимые итерации: I09, I10.

Цель: Manifest, compatibility, clean-room и independent review.

Работа и проверяемый выход:

| Task | Результат | Проверка выхода |
|---|---|---|
| [KAL-056](TASKS.md#kal-056) | Client Delivery pack manifest и supported scope | Manifest связывает process/version/adapters/methods/views/roles и requirements; Нет unsupported ERP/provider promises; одна documented supported profile |
| [KAL-057](TASKS.md#kal-057) | Demo fixtures, real onboarding и documentation | Demo не содержит user data/secrets; показывает claims/acceptance/unknown честно; Guide объясняет first value, права, correction/stop и частичную полноту |
| [KAL-058](TASKS.md#kal-058) | Compatibility и replay candidate audit | Existing packs/events остаются читаемы; новые contract routes отдельно versioned; Replay/upgrade не повторяет effects; old unsafe writes explicitly rejected |
| [KAL-059](TASKS.md#kal-059) | Clean-room candidate и Astra/high adversarial review | Candidate воспроизводим из закреплённого source/diff, supported dependencies/pack versions; Независимый критик пытается опровергнуть исходные AC; material findings repaired |
| [KAL-060](TASKS.md#kal-060) | R4 release candidate decision и support ownership | Есть owner поддержки, severity/escalation и known limits; RC не объявлен коммерчески повторяемым до второй компании; blockers закрыты |

Evidence: pack manifest/version; contracts/views/policy defaults; synthetic fixture suite; pack user/operator guides; compatibility/replay evidence; migration acceptance report; candidate artifact inventory; independent review + repair evidence; R4 gate decision; support/incident ownership.

Проверки: Install manifest на чистой reference среде и проверить version mismatch/invalid policy rejected; Новый координатор проходит несколько original business cases без DSL editing; Golden history, legacy read clients и interrupted import/candidate downgrade scenarios; Чистая install→first case→effect unknown→revoke→restore→upgrade→exit матрица с record outcome; Process/security/operations owners принимают evidence по AC-01–AC-09 и remaining limits.

Решение выхода: owner принимает evidence либо возвращает конкретные tasks на repair; успешная сборка без исходного сценария не закрывает итерацию.

## I12 — Вторая компания и повторяемость

Release: R5. Зависимые итерации: I11.

Цель: Доказать reuse/configuration и экономику сопровождения.

Работа и проверяемый выход:

| Task | Результат | Проверка выхода |
|---|---|---|
| [KAL-061](TASKS.md#kal-061) | Вторая компания: независимый selector и setup | Другой owner/data environment проходит тот же selector; Результаты первой компании не выдаются второй как её опыт/данные |
| [KAL-062](TASKS.md#kal-062) | Reuse через configuration вместо code fork | Различия полей/policies/views оформлены versioned configuration/overlay; Не возникает отдельный runtime fork для обычной работы компании |
| [KAL-063](TASKS.md#kal-063) | Support burden и lifecycle economics | Измерены install/upgrade/recovery/support часы, model/infra costs; Фиксируются ручные вмешательства, unknown/incidents и hidden work |
| [KAL-064](TASKS.md#kal-064) | Повторная ценность против текущей/native работы | Benefit/quality измерены по новым заранее согласованным thresholds; Перенос метода не предполагается без applicability/evaluation |
| [KAL-065](TASKS.md#kal-065) | R5 gate: коммерческая повторяемость или исправление | Подтверждены useful repeatability и предсказуемый lifecycle без fork; Не прошедший gate возвращает к narrow/repair с причиной, без объявления готовой платформы |

Evidence: second-company eligibility; independent setup evidence; configuration portability report; pack extension decisions; support workload report; unit-cost model; second-company comparison series; transferability evidence; R5 readiness decision; supported offer boundaries.

Проверки: Setup и first useful case оператором второй компании по supported guide без developer rescue; Upgrade общего pack сохраняет локальные настройки и isolation обоих клиентов; Повторить отказ/restore со вторым оператором; cost reconciliation сравнить с реальными usage receipts; Новая серия включает отрицательные cases/ограниченные channels и полную цену review; Buyer/operator рассматривают два независимых evidence наборa и agreed acceptance thresholds.

Решение выхода: owner принимает evidence либо возвращает конкретные tasks на repair; успешная сборка без исходного сценария не закрывает итерацию.

## I13 — Выбор второго процесса

Release: R6. Зависимые итерации: I12.

Цель: Выбрать одну вертикаль и подготовить её контракты.

Условие входа: Удовлетворено условие release и выбран конкретный scope.

Работа и проверяемый выход:

| Task | Результат | Проверка выхода |
|---|---|---|
| [KAL-066](TASKS.md#kal-066) | Выбрать одну вторую вертикаль по evidence | Есть реальный buyer/owner/данные/частые cases выбранной вертикали; Не разрабатываются одновременно оба packs; сравнение native alternative учтено |
| [KAL-067](TASKS.md#kal-067) | Контракты второго процесса и границы ontology | Shared abstractions переиспользуются только при semantic compatibility; Новые business terms/acceptance/risk не насильно превращаются в Client Delivery |
| [KAL-068](TASKS.md#kal-068) | Source/ACL/risk для второй вертикали | Выбраны actual systems/scopes и primary writer каждого limited scope; Financial/doc-sensitive effects получают отдельные bounds/review |
| [KAL-069](TASKS.md#kal-069) | Проверить перенос опыта и стоимость адаптации | Метод переносится только при authorised applicability и новой evaluation; Издержки адаптации/поддержки учитываются, client data не мигрируют как method |
| [KAL-070](TASKS.md#kal-070) | R6 entry: scope, buyer и stop criteria | Owner принимает ограниченный scope/value hypothesis, source access и support cost; Handover отложен без доказанного writer/reconcile/fallback плана |

Evidence: Order Exceptions vs Document Close decision; second process selector; second pack domain/action schemas; mapping to common contracts; second pack source bindings; risk policy/source authority map; cross-process method applicability report; new negative cases; R6 scope gate; second pack acceptance thresholds.

Проверки: Selector + full-cost baseline на одинаковых cases выбранного процесса; Различные meanings похожих полей и спорный outcome не смешиваются автоматически; Broad source credential/duplicate transaction/document version и revoke не обходят local authority; Применить старый метод к отличающемуся процессу; corpus должен поймать неверные assumptions; Независимый review выбранного процесса пытается показать более дешёвую штатную альтернативу.

Решение выхода: owner принимает evidence либо возвращает конкретные tasks на repair; успешная сборка без исходного сценария не закрывает итерацию.

## I14 — Второй pack и handover

Release: R6. Зависимые итерации: I13.

Цель: Проверить selective ownership и пользу второго процесса.

Условие входа: Удовлетворено условие release и выбран конкретный scope.

Работа и проверяемый выход:

| Task | Результат | Проверка выхода |
|---|---|---|
| [KAL-071](TASKS.md#kal-071) | Поддерживаемые adapters второго pack | Поддержаны exact source contracts/versions и ACL по declared profile; Нет общей готовности ERP suite; unknown/retry/idempotency endpoint-specific |
| [KAL-072](TASKS.md#kal-072) | Views, methods и process flow второго pack | UI под роли использует тот же trusted catalog, новый process acceptance сохранён; Install/config/support guide дают first useful result выбранному owner |
| [KAL-073](TASKS.md#kal-073) | Selective AuthorityTransition одного scope | Scope/cutover owner/old/new writer и boundaries явно приняты; Shadow comparison не пишет; cutover/rollback исключают simultaneous authoritative writers |
| [KAL-074](TASKS.md#kal-074) | Вторая серия value/fault/ops verification | Value измерен с полной ценой новой вертикали; Handover/restore/revoke/unknown defects исправлены до readiness decision |
| [KAL-075](TASKS.md#kal-075) | R6 release decision и предел поддержки | Есть поддерживаемые process/source/version limits и owner; Новая отрасль не объявляется универсальным поглощением legacy данных |

Evidence: chosen source adapter versions; connector fault fixtures; second pack manifest/views/methods; onboarding cases; writer cutover/reconciliation protocol; manual fallback runbook; second pack pilot/restore evidence; independent review dispositions; R6 gate decision; second pack support matrix.

Проверки: Remote schema drift, partial page, lost receipt и permission downgrade scenarios; Новый пользователь проходит ordinary, disputed и missing-source cases без чужого контекста; Network partition/late legacy write/failed cutover: одна область blocked или один writer, данные reconciled; Crash на cutover, потерянный хвост после backup и ошибочное acceptance по новым criteria; Process/operations/security owner принимают evidence; failures возвращают scope в read-only/manual.

Решение выхода: owner принимает evidence либо возвращает конкретные tasks на repair; успешная сборка без исходного сценария не закрывает итерацию.

## I15 — Новые границы: эксперимент

Release: R7. Зависимые итерации: I14.

Цель: Third-party/federation/machine-first опыты по запросу.

Условие входа: Удовлетворено условие release и выбран конкретный scope.

Работа и проверяемый выход:

| Task | Результат | Проверка выхода |
|---|---|---|
| [KAL-076](TASKS.md#kal-076) | Third-party tools: TCB и bounded extension experiment | Каждое extension имеет typed contract, issuer scopes, egress limits и revoke; Plugin не получает общие secrets/DB/proxy bypass |
| [KAL-077](TASKS.md#kal-077) | Federation: ответственность и receipt semantics | Company authority, audience и protocol versions независимы; At-least-once/dedup/unknown явно описаны без global exactly-once |
| [KAL-078](TASKS.md#kal-078) | Machine-first business creation в ограниченном sandbox | Сценарий создаёт bounded область работы с issuer/owner/stop, не собственные права; Проверяются отличие нового бизнеса от автоматизации workflow и его экономика |
| [KAL-079](TASKS.md#kal-079) | Interop опыта: provenance, portability, licensing | Передаваемый метод отделён от confidential cases/evidence; Получатель заново подтверждает applicability, owner/expiry/revoke и разрешение переноса |
| [KAL-080](TASKS.md#kal-080) | R7 decision: продолжить, сузить или отказаться | Отобранные востребованные experiments проверены; остальные явно deferred и не требуются для gate; Неподтверждённые future universes остаются исследованием, не release promises |

Evidence: extension trust/permission proposal; isolated adapter prototype; federation contract experiment; cross-company dispute/reconcile protocol; business creation scenarios; human responsibility constraints; method exchange proposal; permission/applicability evaluation; experimental boundary review; new ADR/roadmap revision.

Проверки: Malicious tool/redirect/exfiltration/recursive delegation blocked в prototype; Partition/duplicate/counterparty revoke/conflicting acceptance оставляют explainable responsibility; Agent пытается сам открыть scope/потратить чужой бюджет/назначить human acceptance — blocked; Отозванный/заражённый/чужой метод и hidden client literal обнаружены до selection; Astra/high пытается опровергнуть authority/recovery/economics; owner принимает scoped next decision.

Выбираются только востребованные KAL-076–KAL-079. KAL-080 требует evidence всех выбранных ветвей, а не всех возможных. Невыбранные tasks остаются deferred.
