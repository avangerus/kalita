# TECHDEBT — живой реестр технического долга kalita

Формат записи: `[severity] область — что не так → что надо. (когда/контекст)`
severity: **P0** блокирует прод · **P1** мешает заявленному сценарию · **P2** ергономика/качество · **P3** мелочь.

Этот файл ведётся непрерывно: всякий раз, когда обходим проблему, оставляем явный долг здесь, а не молча.

---

## Ядро: automation / schedule

- **P1 schedule-парсинг минимальный.** `Tick` понимает только `every <dur>` (интервал) и fallback «раз в день». Форма `daily at 09:00` НЕ парсится в время суток — на каждом тике в новом дне-ведре сработает, время игнорируется. → реальный cron-парсер (минуты/часы/дни недели/время). (2026-06-12, при auto-reindex; engine/automation.go `scheduleInterval`)
- **P1 неограниченный рост задач от schedule.** `on schedule every Nm` плодит task каждую интервал-ведро ВЕЧНО (resync KnowVault, regen сайта), даже если ничего не изменилось. In-memory `e.tasks` растёт без GC. → политика ретенции/прунинга завершённых задач; либо change-detection до создания task. (2026-06-12)
- **P2 automation не умеет напрямую дёргать workflow-переход.** Closed-list действий: agent/notify/webhook/escalate/create/update. Перевод статуса делаем обходом через `agent Role: task` + воркер зовёт `act`. → рассмотреть действие `transition X` в automation. (2026-06-12, knowvault reindex)
- **P2 multi-line скобки в permissions не поддержаны.** `act [a, b,` с переносом строки парсится как новые verbs (E018). Длинные списки прав — только одной строкой. → парсер должен склеивать скобки через перенос. (2026-06-12, content-пак)

## Ядро: язык / компилятор (вскрыто фиксами паков 2026-07-02)

- **P1 permissions не валидируют имена действий.** `act [foo]` / `approve [bar]`
  с несуществующими именами компилируются молча — ложная защита (карточки
  BUGS предлагали `deny approve X where…`, и это тихо стало бы мусором).
  → семантическая проверка имён + E-код с fix_hint. Решение: LLD-01 §4c.
  (2026-07-02, фиксы hr/servicedesk)
- **P1 computed-агрегаты: `where` парсится одним `strings.Cut(cond, "=")`.**
  Принимается только `reffield = $self`; составное `and status = Approved`
  ломает разбор МОЛЧА (поле unresolved, check не ловит). Блокирует hr BUG-01
  (квота отпусков считает Rejected/Draft) И contracts BUG-01 (`Contract.
  open_findings` считает ВСЕ findings, включая Addressed/Waived — нужен
  `where contract = $self and status = Open`). → полноценный where-парсер в
  evalAggregate (переиспользовать evalWhere) + ошибка компиляции на
  нераспознанное условие. (2026-07-02, engine/computed.go:357-429;
  packs/contracts/entities.dsl `open_findings`, DEMO-0 hardening)
- **P2 automation-действия create/update заявлены доками, отсутствуют в
  грамматике.** Закрыт список agent|notify|webhook|escalate_to. Блокирует
  eshop-склад, crm-convert. → LLD-01 §4b. (2026-07-02)
- **P2 нет композиции полей (include/mixin) — блокер `core.fields`.** DSL
  переиспользует СУЩНОСТИ через ref, но не ГРУППЫ ПОЛЕЙ. Значит «горизонтальный
  сахар» стдлиба (`core.Auditable` created/updated·by, `core.Money` amount+
  currency, `core.Address` вложенно) невыразим — каждый пак копипастит поля.
  → L0-фича `entity X { include core.Auditable }` (компилятор инлайнит поля,
  аддитивно I-1); отдельный LLD при постройке §core.fields (триггер — 2-й
  потребитель сахара). См. DSL-CORE-LIB §4. (2026-07-02, устав core.*)

## Ядро: journal read / outbox (LLD-08)

- **P2 `Store.Since` не принимает limit — read_events грузит весь хвост в память,
  затем режет до limit.** Для стабильного bridge (курсор близко к голове) хвост
  мал; но холодный старт с since=0 на большом журнале = разовый прогон по всему
  журналу в память. → добавить limit-параметр в `Store.Since` (memstore/pgstore),
  чтобы read_events читал O(limit), а не O(хвоста). (2026-07-02, LLD-08 §1)

## Ядро: identity / HITL

- **P1 consumer-auth отсутствует.** identity заточена под governed-акторов и агентов (токены/роли/подписи), НЕ под self-signup тысяч клиентов: регистрация, верификация почты, сброс пароля, OAuth-соцвход, rate-limit, GDPR-согласие. Блокирует личный кабинет на публичном сайте. → либо строить consumer-слой, либо интеграция внешнего IdP (Keycloak/Auth0) с маппингом в actors. (2026-06-12, вопрос про личный кабинет)
- **P2 подписи HITL не криптографически обязательны в dev-пути.** `requireSig` по умолчанию выкл → `decide grant=true` проходит без верифицированной подписи. Для прода нужен `WithRequireSignatures()` + реальная подпись approver. (2026-06-12, демо content)

## Безопасность LLM-периметра (гэп, вскрытый сравнением со StarGuard AI, 2026-06-12)

kalita держит границу ДЕЙСТВИЯ агента (ABAC/HITL/журнал/revert), но НЕ границу
промпта между агентом и моделью. Для ICP (финансы/юр/комплаенс) это вопрос №1.
StarGuard AI (Orion soft) — смежный слой, не конкурент: его можно поставить ПЕРЕД
LLM-вызовами agent_runner. Но пока его нет, дыры наши:

- **P1 нет анти-prompt-injection на ingest.** ЧАСТИЧНО закрыто для legal:
  `workers/agent_runner/runner.py`'s `handle_legal` теперь зовёт
  `workers/perimeter/detector.score()` на `Contract.terms` ДО промпта и, при
  флаге, пишет `comment(internal=true)` на договор — видимый в аудите сигнал
  (H4 layer 3), проверено на honeypot-корпусе (`testdata/honeypot/atk`,
  `workers/agent_runner/test_runner.py::TestHandleLegalPerimeter`). Это
  ТРИПВАЙР, не фильтр — ничего не блокирует; реальный бэкстоп остаётся
  ABAC (доказано `TestDogfoodContractsPoisonedTermsCannotSelfApprove`). НЕ
  закрыто: KnowVault по-прежнему индексирует «всю помойку» без детектора на
  ingest, и нигде не журналируется core-событие `ingest.flagged`
  (`eventstore.IngestFlagged` существует как Kind, но ни один MCP-тул его не
  умеет Append — только `emit_event` для ПАКОВЫХ событий; `ingest.flagged` —
  зарезервированный core kind, генерик-эмит не подходит). → (a) тот же паттерн
  на KnowVault upload/ingest; (b) core MCP-тул или engine-хук, который реально
  умеет журналировать `ingest.flagged` с source/reasons/score, чтобы сигнал
  жил в самом journal, а не только в comment-треде одной записи. (2026-06-12,
  расширено 2026-07-02 при DEMO-0 hardening)
- **P2 нет egress-DLP на LLM-вызовах.** Локальная LM Studio частично снимает (данные не покидают контур), но облачная модель = утечка бизнес-данных в провайдера без маскирования. → DLP-хук перед llm(), либо обязательный шлюз. (2026-06-12)
- **P3 нет low-friction режима вставки.** Урок StarGuard: «reverse-proxy без изменений инфраструктуры» = нулевое трение. kalita = новый рантайм. → продумать «governed-слой поверх существующего» как режим входа (коннекторы — начало). (2026-06-12)

## Федерация узлов (топология «узел на систему»)

Выбранная топология: один узел kalita на систему (сайт/CRM/…), узлы общаются по MCP. Сегодня поддержано ПО ФОРМЕ (узел = и MCP-сервер, и может держать воркеров-клиентов чужих узлов с governed-ролью; журнал пишет cross-node basis). НЕ поддержано:

- **P1 cross-node refs.** `ref[X]` — локальный лукап. Сослаться из CRM-узла на запись сайт-узла = удалённый id + синхронизация, нет механизма. (2026-06-12)
- **P1 надёжный cross-node handoff.** Поток «лид с сайт-узла → сделка в CRM-узле» требует outbox/ретраи/идемпотентность доставки. Нет. (2026-06-12)
- **P2 федеративный cockpit-query.** Сводка owner’а через несколько узлов = federated-query или rollup-реплика. Нет. (2026-06-12)
- **P2 федерация identity.** Claude как один оператор флота узлов — нужен общий якорь доверия / shared identity-authority. Сейчас идентичность на узел. (2026-06-12)

## Content / CMS пак + рендерер

- **P2 deploy рендерера — только `dir:`/`git:`.** Доставка вывода есть (`KSITE_DEPLOY=dir:<вебрут>` mirror+prune, `git:<repo>` commit/push — Pages/Netlify). Нет SFTP/rsync-remote/S3, нет атомарного swap (mirror правит вебрут на месте), запуск воркера всё ещё ручной (токен/env). (2026-06-12)
- **P2 страницы статей не рендерятся.** `articles_section` даёт ссылки `blog/<slug>`, но сами страницы статей не генерируются → битые ссылки. (2026-06-12)
- **P2 рендерер регенит вслепую.** regen каждые 2м даже без изменений; нет change-detection на контент (в отличие от индексатора KnowVault с fingerprint). (2026-06-12)
- **P2 markdown-рендер примитивный.** Без изображений, таблиц, вложенных списков; ссылки/escape грубые. (2026-06-12, render.py `md()`)
- **P2 нет визуального/блочного редактора страниц** для не-технаря (table stakes у Wix/Webflow/HubSpot). (2026-06-12, бенчмарк)
- **P3 нет SEO-обвязки** (sitemap.xml, og-метатеги, canonical, robots) и адаптива/тем. (2026-06-12)

## Legal пак / DEMO-0 hardening (2026-07-02, Fable review zone)

- ~~**P1 (КЛАСС) field-level update ALLOW-LIST не реализуемо в одиночку —
  ядровой гэп в движке прав.**~~ RESOLVED by LLD-20
  (docs/lld/LLD-20-field-scoped-update-perms.md, PR-1+PR-2, в рабочем дереве,
  не закоммичено). Гейт (1) в `engine.Update` (internal/engine/records.go)
  был нескоуп-проверкой `can(actor,"update",entity,"",rec)` на весь рекорд;
  field-scoped-only грант (`update [X.f]`) никогда не матчил `field=""`
  (`itemMatches`, internal/engine/permissions.go), значит гейт (1) отказывал
  ВСЕГДА и до гейта (2) `checkFieldWrites` (уже корректного per-field
  allow+deny) не доходил. Заменён на `canUpdateEntity` (permissions.go):
  проверка СУЩЕСТВОВАНИЯ — есть ли у роли хоть одно update-правило на entity
  любого скоупа, не перекрытое безусловным whole-entity/`*` deny — гейт (2)
  остаётся единственным источником per-field решения. Доказательство
  не-ослабления: `TestUpdateNotLoosened`
  (internal/engine/lld20_update_perms_test.go) — перебор роль×поле×запись,
  new⊇old, единственная новая способность — scoped-роль пишет ровно свои
  поля. `packs/contracts/security.dsl`'s роль `Legal` переведена с
  нескоуп-`update [Contract]` + исчерпывающий deny НА настоящий allow-list
  `update [Contract.assessment, Contract.risk]` — новое Contract-поле теперь
  безопасно по умолчанию, не по полноте deny-списка. (2026-07-02)

- **P2 `product.mcp`'s `compose:` — сырой, неинтерпретируемый компилятором
  текст.** Секция `product.mcp` (LLD-06 §1, `internal/dsl` — новый
  top-level блок, парсер/AST/семантика в parser.go/parser_blocks.go/ast.go/
  semantic.go, код ошибки `E051_BAD_PRODUCT_MCP`) парсит `tool <name>
  "desc":` с типизированными `in:` (переиспользует грамматику полей сущности)
  и `out: <binding>`, но КАЖДАЯ строка `compose:` — просто `Line.Raw`,
  СТРОКА, не выражение. Компилятор НЕ проверяет: что `create Contract(...)`
  ссылается на реальную entity/поля, что `$title` действительно объявлен в
  `in:`, что `-> $contract` действительно используется в `out:`. Осознанно
  — «декларация, не код» (задача явно исключила воркер-façade, LLD-06 PR-2,
  из этой волны), но значит: ошибка в `compose:` сегодня НЕ ловится
  компиляцией пака, проявится только когда façade-воркер (не построен)
  попытается её исполнить. → когда PR-2 строит façade, либо (a) façade сам
  валидирует compose при старте (fail fast, не ждать первого вызова), либо
  (b) добавить семантическую проверку в компилятор (ref-целостность
  `in`/`out`/entity-имён в compose) — второе предпочтительнее (ошибка видна
  на `kalita build`, не в проде), но это отдельный кусок работы сравнимый по
  объёму с текущим PR. (2026-07-02, LLD-06 §1 PR-1-adjacent grammar work)

## Коннекторы по краю (connector SDK)

«Интегрируем, не строим» пока декларация — интерфейса нет.

- **P2 connector scaffold есть, обобщения нет.** `workers/connectors/inbound.py` — первый коннектор (форма→Lead) и образец паттерна (untrusted POST → whitelist-map → MCP create под least-privilege identity). Но это только inbound/create: нет upsert/update, нет дедупа, нет rate-limit, секрет один на форму. → выделить общую базу когда появится 2-й коннектор. (2026-06-12)
- **P1 нет payment-коннектора** (Stripe/YooKassa) + webhook → инвойс в `Paid`. Агент к рельсам не прикасается — но системе нужна интеграция. Скопировать паттерн inbound.py. (2026-06-12)
- **P2 нет email/calendar sync** (Nylas/Graph) — table stakes CRM. (2026-06-12)
- **P2 нет Jitsi/appointment-коннектора** (комната + ссылка в кабинет). (2026-06-12)
- **P2 нет Bitrix24 REST/webhook-коннектора** (мост к его CRM: сделки/контакты/счета). (2026-06-12)

## CRM table-stakes (ориентир Bitrix24/Salesforce/Pipedrive)

- **P2 born-agentic SalesBot только на `on create Opportunity`.** Триаж лида (New→Working) не реализован: Activity нельзя привязать к Lead (нет ref Activity→Lead, только →Opportunity). → добавить Activity↔Lead или Lead-триаж без активности. (2026-06-12)
- **P2 SalesBot не ставит `due` у заведённой Activity** (нет дедлайна follow-up). → попросить у LLM срок или дефолт +2 дня. (2026-06-12)
- **P2 авто-капчур активностей, дедуп контактов, мобайл, форкаст по территориям** — отсутствуют (различители из бенчмарка). (2026-06-12)
- **P3 deal→invoice handoff между паками** (CRM Opportunity Won → finance Invoice) не связан — finance.dsl это анонсирует, моста нет. (2026-06-12)

## KnowVault

- **P2 legacy-форматы doc/xls/ppt** не извлекаются (нужен LibreOffice/конвертер; у фаундера не стоит). (ранее)
- **P2 mail/git как источники** — заявлены в видении, не реализованы (ingest понимает только Files/Upload). (ранее)

## Product Kit (LLD-05, PK-4 — password-credential, реализация 2026-07-02)

- **P1 `POST /api/actors/{id}/password` не ограничен self/Owner.** Гейт —
  «любой человек» (тот же паттерн, что уже у `disableActor`/`createInvite` в
  v0-модели), но в сочетании с password-login это значит: любой
  аутентифицированный человек любой роли может выставить пароль ЛЮБОМУ
  другому актору и залогиниться под ним (impersonation). Для v0 одноузловой
  admin-модели терпимо (принято по образцу существующих ручек), но при
  первом продукте с несколькими человеческими ролями needs role-gate
  (например, только Owner меняет чужой пароль; актор меняет только свой).
  → добавить проверку `actor.ID == id || actor.Role == approver` в
  `setPassword`. **Это КЛАСС** (setPassword/disableActor/createInvite — все
  «любой человек»); закрывать системно в **LLD-10** (identity hardening), не
  патчить одну ручку вразнобой. Ревью Fable: принято как долг, обязателен к
  закрытию ДО Legal-пилота с несколькими человеческими ролями.
  (2026-07-02, LLD-05 PR-4)
- **P2 нет self-service «забыли пароль».** `SetPassword`/CLI `user add
  --password` — только админский/CLI путь выставить начальный пароль; сброса
  по email/каналу нет (массовый self-service сознательно вне LLD-05 — внешний
  IdP, граница N7). Если продукту понадобится "забыли пароль" внутри текущей
  identity-модели — нужен отдельный LLD (канал доставки кода сброса — не
  тривиален для governed-модели без email-инфраструктуры в ядре). (2026-07-02)
- **P2 login не троттлится.** `/api/login` — публичный эндпойнт без
  rate-limit/backoff на неверные попытки; bcrypt сам по себе медленный
  (защита от офлайн-перебора хэша), но онлайн brute-force по каналу не
  ограничен отдельно. → rate-limit по login или по IP на уровне API/прокси —
  входит в **LLD-10 §4** (rate-limit + auth-fail-агрегация). (2026-07-02)

## Product Kit (LLD-05, PK-1 — atoms/molecules/organisms/faces, реализация 2026-07-02)

- **P2 два fetch-клиента (web/app.js's `api()` и `packages/kalita-sdk/client.js`)
  не сходятся на одном источнике правды.** LLD-05 §1 явно фиксирует это как
  НЕ блокер PR-1/PR-2 ("два fetch-клиента... сходятся на client.js отдельным
  шагом"); зафиксировано здесь как отдельный шаг, а не молча оставлено. →
  перевести web/app.js's `api()` на `@kalita/sdk/client.js` целиком (auth
  token/records/act/approve/search/meta/invites), одним PR, вне LLD-05.
  (2026-07-02, LLD-05 PR-1/PR-2)
- **P2 `@kalita/sdk`'s React organisms (KList/KDetail/KBoard) не резолвят
  `ref`/`array_ref` в человеческий label** — показывают сырой id.
  packages/kalita-ui/tokens/format.js's `formatValue()` намеренно возвращает
  `{kind:'ref'|'array_ref', id/ids}` БЕЗ резолюции (async id→label — сетевой
  вызов, вне чистой data-in/data-out функции); preact-стек (kalita-ui's
  `RefLink`) резолвит через injectable `fetcher`, React-стек аналога пока не
  получил. → добавить `useRefLabel`-эквивалент в `packages/kalita-sdk/react.js`
  и звать его в `components.js`'s `renderSpec('ref')`. (2026-07-02, LLD-05 PR-1)
- **P2 web/app.js's `EntityList`/`Board` НЕ переведены на kalita-ui's новые
  `KList`/`KBoard` organisms.** LLD-05 PR-1 требовал только `FieldInput`/
  `FieldValue` на atoms; PR-2 добавил organisms как НОВЫЙ слой (используется
  `web/catalog.html`), но существующие списочные/канбан-экраны admin-клиента
  сохранены как есть (пагинация, инлайн-фильтр, инлайн-`CreateForm` — богаче,
  чем текущий `KList`/`KBoard`). `ApprovalCard`/`TimelineItem`/`DashboardTile`
  — единственные molecules, на которые app.js реально переключён (это и было
  предметом де-дупликации, которую фиксирует LLD-05 §1). → либо расширить
  `KList`/`KBoard` до паритета (pagination/filter/inline-create) и
  переключить app.js, либо явно принять, что admin-клиент и storybook-organisms
  — разные реализации одного паттерна. (2026-07-02, LLD-05 PR-1/PR-2)

## LLD-06 (product MCP façade / KChat, реализация 2026-07-02)

- **P1 (КЛАСС, унаследован, не новый) cross-pack composition — один узел
  компилирует ОДИН каталог `--pack`, не несколько.** Уже задокументировано
  под LLD-01 PR-4 (`agent/kalita/QUESTIONS-LLD.md` "Cross-pack — реальная
  граница загрузчика"): `cmd/kalita/main.go`'s `loadPack` читает `.dsl` из
  ОДНОГО каталога в ОДНУ `dsl.Model`; два `pack.dsl`-манифеста в одной
  компиляции молча схлопываются (`internal/dsl/parser.go:118` `m.Name =
  rest` — второй `pack` перезаписывает первый без ошибки). Следствие для
  LLD-06: пак `assistant` (Session/Message, PR-1) и пак `contracts` (Contract/
  Finding + `product.mcp`, PR-2) НЕ МОГУТ быть загружены в ОДИН работающий
  узел сегодня — значит полное демо «KChat → façade legal.submit_contract →
  Contract» на ОДНОМ узле требует ЛИБО дождаться cross-pack composition
  (ядровая фича, уже отдельно залогирована), ЛИБО деплой-время слияние (не
  правка `internal/`, а просто копирование/конкатенация `.dsl`-файлов обоих
  паков в один каталог с ОДНИМ `pack.dsl`) как временный обходной путь для
  демо-окружения. Ничего не хакалось в ядре под это — PR-1 (assistant) и
  PR-2 (façade, generic — доказано на `packs/contracts`, live-тест
  `workers/product_mcp/test_facade_live.py`) КАЖДЫЙ по отдельности приняты и
  протестированы против РЕАЛЬНОГО узла; только их СОВМЕСТНАЯ загрузка на
  одном узле упирается в этот унаследованный гэп. (2026-07-02, LLD-06 PR-1/PR-2)
- **P2 `workers/product_mcp/compose.py`'s интерпретатор closed на 3
  глаголах.** Реализованы ровно `create Entity(...) -> $x`, `act action on
  $x`, `query Entity [where ...] -> $x` — весь набор, который РЕАЛЬНО
  использует `packs/contracts/product_mcp.dsl` сегодня. `update`/`comment`/
  `emit_event` compose-шаги НЕ поддержаны — `parse_step` кидает
  `ManifestError` с явным списком поддерживаемых форм (fail fast при
  старте façade на любом паке, который попробует такой шаг), а не тихо
  игнорирует/падает на первом вызове. → расширить `compose.py`'s грамматику
  формой за формой, когда РЕАЛЬНЫЙ продуктовый пак объявит `update`/
  `comment`/`emit_event` в `compose:`. (2026-07-02, LLD-06 PR-2)
- **P2 (унаследованный класс, не новый) `create`-время cross-owner ref-запись
  не гейтится ABAC.** `internal/engine/permissions.go`'s `can()`: условный
  (`where`) allow-правило требует `record != nil` для матча (`if record !=
  nil && evalWhere(...)`), а на `create` записи ещё не существует (record ==
  nil) — значит `create [Session, Message]` (пак `assistant`, как и
  `create [Lead, Opportunity, Activity]` в `crm.dsl`, `create [Employee,
  LeaveRequest]` в `hr.dsl` и далее по всем owner-scoped пакам в этом репо)
  НЕ может быть сужен `where`-условием на запись: актор технически МОЖЕТ
  передать `values.session = <чужой session id>` при создании Message и
  привязать сообщение к чужой сессии — движок это не остановит на этапе
  create (только `read`/`update` потом скрывают чужие записи через `where
  session.owner = $me`). Это ТОТ ЖЕ структурный гэп, что и во ВСЕХ
  owner-scoped паках репозитория уже сегодня, не что-то новое, внесённое
  паком `assistant` — задокументировано здесь явно, потому что LLD-06 §2
  особо подчёркивает «диалог governed по построению» и микс с чужой сессией
  был бы прямым нарушением этого обещания. → core-фикс: `can()` для `create`
  должен уметь проверять `where`-условие против ПЕРЕДАВАЕМЫХ `values`, не
  только против существующей записи (`record` сейчас либо nil на create,
  либо реальная запись на read/update — нужен третий режим «черновые values
  без id»). Не хакалось (задача явно запрещает патчить `internal/`).
  (2026-07-02, LLD-06 PR-1, packs/assistant/security.dsl)
- **P3 `workers/chat_runner/chat_runner.py`'s `idempotency_key` для
  Message-постов не retry-safe через границу процесса.** `_idem(...)`
  подмешивает случайный `uuid4().hex` на каждый вызов (не чисто
  детерминированная функция входа, в отличие от `agent_runner.runner.
  idem_key`, которая ключуется от `task_id` — у чат-хода нет natural
  "attempt id" без запроса клиента на предмет retry-семантики). Значит
  ретрай HTTP-запроса к `/chat/turn` (сетевой сбой на стороне клиента)
  создаст ВТОРОЕ Message-событие, а не схлопнется в одно. Не критично для
  демо (retry чат-хода — редкий путь, в отличие от task-ретраев
  agent_runner), но зафиксировано как гэп надёжности. → пробрасывать
  request-id от KChat-клиента (ChatFace) и ключевать `_idem` от него.
  (2026-07-02, LLD-06 PR-3)

---

## Долг закрытый (для истории)

- ~~Инкрементальная индексация KnowVault (skip unchanged / re-embed changed / drop deleted)~~ — закрыто `84f92ca`.
- ~~Auto-reindex без ручного триггера~~ — закрыто `b822a03` (interval schedule + resync task).
- ~~Born-agentic CRM (система действия)~~ — частично: SalesBot триажит новую Opportunity (квалификация + follow-up Activity + advance), HITL на close_won цел. Доказано вживую 2026-06-12.
- ~~Inbound connector «форма→Lead» + scaffold connector-слоя~~ — `workers/connectors/inbound.py`: untrusted POST → whitelist-map → governed create под least-privilege Intake. Секрет-гейт + блок инжекта статуса доказаны вживую 2026-06-12 (`crm.dsl` роль Intake).
- ~~Outbound deploy рендерера (петля «правка→live»)~~ — `KSITE_DEPLOY=dir:`(mirror+prune)/`git:`(commit/push). Доказано вживую: публикация→вебрут, снятие страницы→исчезает из вебрута, git-commit. 2026-06-12. Симметрия connector-слоя (in+out) замкнута.
