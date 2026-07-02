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
  (квота отпусков считает Rejected/Draft). → полноценный where-парсер в
  evalAggregate (переиспользовать evalWhere) + ошибка компиляции на
  нераспознанное условие. (2026-07-02, engine/computed.go:357-429)
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

- **P1 нет анти-prompt-injection на ingest.** KnowVault индексирует «всю помойку», legal-агент читает чужой договор — отравленный документ с инструкциями для агента проходит периметр. Остаётся РИСКОМ даже на локальной модели. → фильтр/изоляция инструкций во входном контенте; пометка «данные, не команды». (2026-06-12)
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

---

## Долг закрытый (для истории)

- ~~Инкрементальная индексация KnowVault (skip unchanged / re-embed changed / drop deleted)~~ — закрыто `84f92ca`.
- ~~Auto-reindex без ручного триггера~~ — закрыто `b822a03` (interval schedule + resync task).
- ~~Born-agentic CRM (система действия)~~ — частично: SalesBot триажит новую Opportunity (квалификация + follow-up Activity + advance), HITL на close_won цел. Доказано вживую 2026-06-12.
- ~~Inbound connector «форма→Lead» + scaffold connector-слоя~~ — `workers/connectors/inbound.py`: untrusted POST → whitelist-map → governed create под least-privilege Intake. Секрет-гейт + блок инжекта статуса доказаны вживую 2026-06-12 (`crm.dsl` роль Intake).
- ~~Outbound deploy рендерера (петля «правка→live»)~~ — `KSITE_DEPLOY=dir:`(mirror+prune)/`git:`(commit/push). Доказано вживую: публикация→вебрут, снятие страницы→исчезает из вебрута, git-commit. 2026-06-12. Симметрия connector-слоя (in+out) замкнута.
