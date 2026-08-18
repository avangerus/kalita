# Архитектурная рецензия (Architect Pro) — Kalita self-building enterprise product

Рецензируемые документы: `PRODUCT_ARCHITECTURE.md`, `BUSINESS_FRAMEWORKS.md`,
`CONVERSATIONAL_BUILDER.md`, `PRODUCT_UX.md`, `DEVELOPMENT_GRAPH.md`.

---

## 1. Вердикт: ACCEPT-WITH-NOTES

Архитектура связная, честно признаёт собственные границы и верно фиксирует
главный факт: весь change path уже существует в ядре, и builder действительно
едет по `ProposeChange → DecideProposal`, а не создаёт вторую admin-поверхность.
Все шесть grounding-утверждений подтверждены по коду — с одним существенным
исключением: дизайн **завышает** гарантию того, что definition-изменения уже
подписываются (см. Finding 1). Ещё два несущих утверждения (детерминированный
синтез, эквивалентность walker'а) слабее заявленного. Блокеров, отвергающих
подход, нет; правки точечные и принадлежат P2/P3/P4.

Итог проверки: (1) `proposals.go` — подтверждено; (2) 32 инструмента —
подтверждено ровно (`tools.go:40-100`), включая `compose_pack`, `validate_dsl`,
`field_types`, `propose_change`, `get_proposal`, `query`, `dashboard`,
`read_events` (плюс `get_grammar`, который упомянут в CONVERSATIONAL_BUILDER,
но пропущен в списке инструментов PRODUCT_ARCHITECTURE); (3) pass-through
chat_runner/facade — подтверждено (`chat_runner.py:1-49`, `facade.py:14-31`);
(4) `shadow.go:14-28` — role overlay над record-мутациями, не симуляция
definition — подтверждено; (5) `engine.go:206` `defApprover:"Owner"` и
`server.go:158-159` `/api/proposals` — подтверждено; (6) `web/app.js:1087-1101`
маршруты + `356-395` Inbox — подтверждено.

---

## 2. Findings

### Finding 1 — MAJOR — «Подписанное изменение» сегодня фактически не enforced; definition-решения бес-подписные по умолчанию и *не могли* быть сделаны обязательными в текущем CLI

**Доказательства:** `proposals.go:140-150` проверяет подпись только если она
передана, и отклоняет только при `e.requireSig`. `requireSig` выставляется
только `engine.WithRequireSignatures()` (`engine.go:140`), который
`cmd/kalita/main.go` никогда не вызывает (`main.go:487-493` подключает
`WithVerifier` + `WithDefinitionApprover` + опционально
`WithRequireHumanSignature`; последний выставляет `requireHumanSig`,
`engine.go:148`, который `DecideProposal` не читает). REST-обработчик
`decideProposal` (`handlers_ui.go:405-422`) читает только
`req.Grant/req.Signature/req.Basis` и молча игнорирует поле `HumanSignature`
(присутствует в `decideRequest`, `handlers_ui.go:157`); нет эндпоинта
`/api/proposals/{id}/challenge` (`server.go:158-159`), в отличие от
`/api/approvals/{id}/challenge` (`server.go:151`). UI шлёт только `{grant, basis}`
(`web/app.js:370-374`).

**Почему это проблема:** человек с ролью `Owner` применяет definition-изменение
по одному bearer-токену без подписи при любой комбинации флагов. Заявление доков
«DecideProposal требует … валидную подпись (Ed25519/WebAuthn)»
(`PRODUCT_ARCHITECTURE.md:46-47,107-110`; `CONVERSATIONAL_BUILDER.md:105`)
фактически неверно. Гарантия «LLM не может применить» сохраняется через
безусловный role gate (`proposals.go:124`), так что это **не** дыра
эскалации ИИ — но главный promise миссии («после человеческой подписи —
подписанное изменение») не имеет non-repudiation.

**Фикс:** прокинуть WebAuthn challenge+verify поток для proposals (зеркало
`handlers_ui.go:160-211`), заставить `DecideProposal` учитывать
`requireHumanSig`/`requireSig`, включить его в деплое builder'а по умолчанию,
поправить доки.

### Finding 2 — MAJOR — «Детерминированный Synthesizer» противоречив, а structured-JSON путь не покрывает семантическую поверхность модели; LLM всё равно пишет raw DSL-выражения

**Доказательства:** `compose_pack`/`PackSpec` покрывает только
pack/entities/workflows/roles/permissions/links (`compose.go:15-24`). Его
`when`/`computed`/permission-правила — raw DSL-строки, передаваемые дословно
(`compose.go:45-59,66-71,165`). События, automation, dashboards, integrations,
subscriptions, picker и agent tasks парсятся из DSL (`parser.go:67-74`;
`parser_blocks.go:223-509`) и отсутствуют в `compose_pack`.
`PRODUCT_ARCHITECTURE.md:188` говорит, что LLM выдаёт «первый вариант JSON»;
`CONVERSATIONAL_BUILDER.md:16,101` заявляет «детерминированный» Synthesizer.

**Почему это проблема:** guard-выражения (пороги сумм), computed-агрегаты,
automation-правила и permission-правила — ровно те места, где всплывают
галлюцинации бизнес-логики и инъекции, и их нельзя вывести из перечисленных
решений — значит LLM обязан писать их как raw DSL-текст. Инвариант «LLM не
пишет модель / синтез детерминирован» ложен для тех частей, которые важны.
Собственная оговорка дизайна «computed/dashboards/events через DSL-фрагменты»
(`CONVERSATIONAL_BUILDER.md:100`) признаёт это, не разрешая.

**Фикс:** выбрать одну истину — (а) версионированный детерминированный шаблон
поверх decisions+framework, а LLM-авторские фрагменты объявить first-class
поверхностью «требует ревью человека» и рендерить их в EXPLAIN-карточке; или
(б) расширить `compose_pack` структурным покрытием events/automations/
dashboards/integrations. Версию synthesizer'а записывать в proposal.

### Finding 3 — MAJOR — Walker воркера пере-реализует семантику выражений движка; режим отказа — тихое расхождение

**Доказательства:** guard'ы исполняются через `evalWhere`
(`workflow.go:157,353,524`; `expr.go:42`) на замкнутой, но богатой грамматике —
ref-пути, агрегаты (`computed.go:361`), `since`/`until`/`businessSince`
(`computed.go:276,294,314`), `$me/$self/$now`, семантика пропущенных значений
(«missing → condition false, not an error», `expr.go:343`). Walker дизайна
(`PRODUCT_ARCHITECTURE.md:143-153`; `CONVERSATIONAL_BUILDER.md:113-120`) — это
Python-ре-имплементация подмножества, а kernel dry-run для definition
отсутствует (`shadow.go:14-28`).

**Почему это проблема:** walker — ключевой дифференциатор продукта («показать
владельцу, что произойдёт»). Он может вернуть уверенно неверную трассу для
edge-case, который номинально «поддерживает» (сравнение money/decimal,
нормализация aggregate `where`, бизнес-календарь), без маркера «unknown» —
расхождение тихое.

**Фикс:** выставить read-only kernel-поверхность оценки guard'ов (оценить
`when` перехода над переданной записью), чтобы simulate делегировал движку; или,
если вне скоупа, принудительный маркер «аппроксимация walker'а; движок
перепроверит при исполнении» на каждом результате + дифференциальные
golden-тесты walker == `evalWhere` на общем корпусе.

### Finding 4 — MAJOR — Классификация высказываний выполняется LLM, а слой-3 «fact» пишется до confirm-гейта

**Доказательства:** `SessionMessage kind=fact` пишется воркером в DISCOVER
(`CONVERSATIONAL_BUILDER.md:96-97`, таблица §3) и классифицируется Expert'ом
(LLM). Слой 3 определён как «только факты, подтверждённые пользователем
(DesignDecision accepted) или прочитанные из журнала» (`PRODUCT_ARCHITECTURE.md:20`),
но `kind=fact` — не `DesignDecision` и не имеет явного accept-шага — при этом
тот же док говорит «фиксируется как факт … но НЕ policy»
(`CONVERSATIONAL_BUILDER.md:67`).

**Почему это проблема:** неверно классифицированная рекомендация (или случайная
оговорка пользователя) журналируется как слой-3 «факт» и читается обратно в
будущие промпты как знание компании — самоподкрепляющийся цикл. Финальная модель
всё ещё защищена CONFIRM+подписью, но факты слоя 3, направляющие будущую
discovery, — нет.

**Фикс:** требовать явный ход подтверждения пользователя перед любой меткой
`kind=fact`; неподтверждённые высказывания по умолчанию в `preference`/`answer`;
добавить не-LLM проверку, что любой факт, питающий MODEL, сводим к принятому
решению или подтверждённому факту.

### Finding 5 — MAJOR — Роль Builder (агент) не-scoped → кросс-тенантное чтение дизайн-сессий в фоновом режиме

**Доказательства:** `CONVERSATIONAL_BUILDER.md:53-57` скоупит человека-владельца
`where session.created_by = $me`, но даёт роли Builder «read сессий + create
SessionMessage» без `where`, при этом явно допуская режим «фоновый агент».
Прецедент доказывает доступность скоупинга (`packs/assistant/security.dsl:35-37`:
`read Session where owner = $me`, `read Message where session.owner = $me`).

**Почему это проблема:** в фоновом режиме роль Builder читает все
DesignSession/Message/Decision каждого пользователя (цели, обоснования, решения) —
ABAC-дыра ровно в том многопользовательском пилоте, который обещает D10.

**Фикс:** скоупить роль Builder идентично, либо удалить её и сделать воркер
строго pass-through под bearer человека.

### Finding 6 — MINOR — ProposeChange не-гейтится — реальная, ограниченная поверхность злоупотребления (митигация в доке недооценивает её)

**Доказательства:** dispatch `propose_change` (`tools.go:498-519`) и
`ProposeChange` (`proposals.go:59-105`) не имеют role-проверки; только basis +
base_def_version + compile + additive. Rate-limit — per-actor 240/мин
(`server.go:70,185-196`). Паркованные proposal копятся в памяти + журнале
(`proposals.go:103`) и видны в Inbox (`web/app.js:383-389`).

**Почему это проблема:** любой аутентифицированный пользователь паркует
произвольные proposal (шум в Inbox, рост журнала) и становится de-facto
инициатором правки модели. Утечка информации через compile-ошибки — *не* новый
канал (см. Finding 7), но неограниченный рост pending-предложений — да.

**Фикс:** role-грант на `propose_change`, либо cap pending-предложений на
актора + retention устаревших.

### Finding 7 — MINOR — `describe_system`/`describe_entity` отдают полную схему любому аутентифицированному актору; «три уровня / Developer DSL скрыт» — только UI

**Доказательства:** `describeSystem` (`tools.go:581-603`) и `describeEntity`
(`tools.go:606-643`) возвращают списки сущностей, типы полей, workflow и
approval-роли без role-проверки. `PRODUCT_UX.md:88-97` подаёт Developer-уровень
как «скрытый».

**Почему это проблема:** разделение «четырёх слоёв» и Business/Developer — это
презентация, не enforced на границе API/MCP; решительный пользователь читает всю
схему независимо от уровня. Для v1 допустимо, но должно быть заявлено, а не
подразумеваться.

### Finding 8 — MINOR — «validate_dsl зелёный» ≠ семантически корректно, и loop недооценивает остаточный риск

**Доказательства:** `validate_dsl` вызывает `dsl.Compile` (`tools.go:492`),
порождающий E-коды (синтаксис/тип/неизвестная ссылка); он не ловит «≤100k
Manager» переписанное как «>1M CFO». Семантический гейт на самом деле —
EXPLAIN + SIMULATE + человеческая подпись (шаги 9-11), но шаг 8 подаётся как
«до зелёного».

**Фикс:** явно заявить, что VALIDATE — compile/type-уровень; семантическая
корректность — человеческий гейт на EXPLAIN+CONFIRM.

### Finding 9 — MINOR — Счётчики Simulate/impact пермишн-скоуплены и могут молча занижать

**Доказательства:** `query`/`dashboard` соблюдают row-level ABAC (`tools.go:43,96`;
`records.go:594`; `dashboard.go:105`). ProcessOwner, предлагающий изменение,
может не иметь read на каждую запись затрагиваемой сущности — «из 23 открытых
счетов» на деле «23, которые ты можешь видеть».

**Фикс:** помечать счётчики как пермишн-скоупленные и маркировать «unknown» при
неполном покрытии чтения.

### Finding 10 — NIT — sha256 фреймворков — это integrity, а не trust; и контент фреймворка попадает в доверенный SYSTEM-блок

**Доказательства:** `promptbox.build_messages` помещает `system` как доверенный,
а data-блоки как untrusted (`promptbox.py:78-98`); фреймворки грузятся read-only
с sha256-верификацией и заявлены в SYSTEM-контексте (`BUSINESS_FRAMEWORKS.md:19-21,73,85`).
sha256 связывает файл↔manifest, но злонамеренный PR обновляет оба
(`BUSINESS_FRAMEWORKS.md:136-138`) — то есть он ловит tamper после релиза, а не
отравленный curated-изменение.

**Почему это проблема:** фреймворк — доверенный инструктивный текст для LLM; его
реальный якорь доверия — человеческое ревью + «валидация вне LLM» (это держится).
Стоит проговорить, чтобы sha256 не принимали за доверие контенту.

**Фикс:** задокументировать sha256 как integrity-only; считать контент фреймворка
review-grade; рассмотреть помещение текста фреймворка в маркированный data-блок,
а не в инструктивный блок.

---

## 3. Рulings D1–D11

- **D1 — holds.** Pass-through bearer соответствует `chat_runner.py:1-49`/
  `facade.py:14-31`; нет нового admin API. (Оговорка: едет на не-гейченном
  ProposeChange, Finding 6.)
- **D2 — holds.** Pack-data сессия обоснована `packs/assistant/security.dsl:35-37`.
  (Оговорка: скоупинг agent-роли, Finding 5.)
- **D3 — holds-with-notes.** Версионированный репо + отсутствие runtime-сети
  здраво; sha256 — только integrity (Finding 10).
- **D4 — не holds как заявлено.** «Модель только из подтверждённых решений»
  верно для DesignDecision→модель, но заявление о детерминированном Synthesizer
  противоречиво и решения не определяют expression-фрагменты (Finding 2).
- **D5 — holds.** Детерминированный explain-рендер поверх meta/workflow корректен
  и обоснован.
- **D6 — не полностью holds.** shadow-это-record-мутации корректно
  (`shadow.go:14-28`), но walker воркера пере-реализует семантику `evalWhere` с
  риском тихого расхождения (Finding 3).
- **D7 — holds.** Структурный diff + счётчики по журналу честны; оговорка
  пермишн-скоупленных счётчиков (Finding 9).
- **D8 — holds.** Buildless shell поверх notation UI; маршруты/Inbox подтверждены
  (`app.js:1087-1101,356-395`).
- **D9 — holds.** Env LLM-граница соответствует `chat_runner.py:76-78`.
- **D10 — holds.** Один pack + `where owner=$me` обосновано; оговорка Finding 5.
- **D11 — holds.** Golden `packs/servicedesk`; доказательство generality в
  принципе здраво.

---

## 4. MISSING

- **Позиция по подписи (шаг миссии 10).** Нет заявления, что продукт builder
  должен выйти с включённым enforcement подписи на definition — и, по Finding 1,
  путь enforcement надо сначала построить. Это главный promise и он сейчас не
  подкреплён.
- **«Кто становится ProcessOwner / может начать сессию».** Назначение ролей для
  builder-пака предполагается, но не специфицировано; вместе с не-гейченным
  `propose_change` каждый аутентифицированный пользователь может инициировать
  работу над изменением модели.
- **Retention/cleanup данных builder-пака.** Цели/сообщения/решения могут
  содержать PII; у брошенных сессий (`status=abandoned`,
  `CONVERSATIONAL_BUILDER.md:152`) нет заявленного жизненного цикла/erase.
- **Человекочитаемый рендер automation-правил и event payload перед подписью.**
  Детерминированный EXPLAIN (`CONVERSATIONAL_BUILDER.md:102`) перечисляет
  сущности/роли/процесс/права/агентов/гейты/интеграции, но *не* automation-правила
  («on stuck for 2d escalate to Owner») и не схемы payload событий — ровно те
  поверхности, где живёт тихое поведение, — оставляя их видимыми только как raw DSL.
- **Concurrency UX.** `base_def_version` + `idempotency_key`
  (`CONVERSATIONAL_BUILDER.md:154`) — технический guard, но нет продуктового
  потока «ваш proposal устарел, rebase» при параллельном моделировании двумя
  владельцами.
- **End-to-end «run» (шаг миссии 14).** Доставка egress — известный gap
  (G2/D-011), честно помечен, но дизайн должен заявить, что «run» частично
  демонстрируем лишь после появления webhook runner'а.

---

## 5. Статус закрытия findings (Round-1)

Проверено по коду и докам (без правок кода). Итог: **6 закрыто, 4 открыто.**

- **Finding 1 — CLOSED (код).** Путь enforcement построен end-to-end:
  `WithProposalHumanSignature` + signature-лестница с учётом `requireHumanSig`
  (`proposals.go:134,185-202`), `BuildDefinitionCanonical`
  (`webauthn.go:509,516`), `GET /api/proposals/{id}/challenge`
  (`server.go:159`, `credentials.go:380`), проверка `human_signature` в
  `decideProposal` (`handlers_ui.go:416-451`), подпись в UI с fallback на
  token-only (`app.js:37-58,418-422`). Остаток: флаг
  `--require-human-signature` всё ещё opt-in (`main.go:491,508`) — включить в
  деплое builder'а, чтобы «signed change» выполнялось по умолчанию.
- **Finding 2 — CLOSED (док).** Явно отозвано имя «детерминированный
  Synthesizer» и заявлена LLM-assisted честная граница
  (`PRODUCT_ARCHITECTURE.md:88-89,142-149`; `CONVERSATIONAL_BUILDER.md:107`).
  Остаток: `compose_pack` не расширен; EXPLAIN-рендер LLM-авторских фрагментов
  (automation/event payload) всё ещё тонкий — отслеживать.
- **Finding 3 — CLOSED (док, с оговоркой).** Walker честно ограничен подмножеством
  + обязательный маркер «движок проверит» + дифференциальные golden-тесты против
  `evalWhere` (`CONVERSATIONAL_BUILDER.md:120-125`; `PRODUCT_ARCHITECTURE.md:156-166`).
  Остаток: golden-тесты ещё не написаны (P-риск); kernel dry-run не введён
  (принятый трейдофф).
- **Finding 4 — CLOSED (протокол).** Обязательная сводка «я понял так» + явное
  подтверждение перед MODEL; классификатор помечает только текст пользователя, а
  рекомендации Expert всегда `kind=recommendation` с provenance
  (`CONVERSATIONAL_BUILDER.md:71,82-84`). Остаток: это протокол, не код-инвариант —
  проверяется в acceptance E.
- **Finding 5 — CLOSED (код).** Роль Builder-агента удалена; в
  `packs/builder/security.dsl:19-35` только `ProcessOwner` (scoped
  `where owner = $me` / `where session.owner = $me`) + `Admin` (full);
  задокументировано (`CONVERSATIONAL_BUILDER.md:53-61`).
- **Finding 6 — OPEN.** Роль-гранта/cap на `propose_change` нет; риск-таблица всё
  ещё «задокументировать в SECURITY» (`PRODUCT_ARCHITECTURE.md:219`).
- **Finding 7 — OPEN.** `describe_system`/`describe_entity` по-прежнему отдают
  полную схему без role-проверки (`tools.go:581-643`), в доках не оговорено.
- **Finding 8 — CLOSED (док).** Семантический гейт теперь явный: подтверждение
  сводки + EXPLAIN + подпись, а `validate_dsl` подан как compile-loop
  (`PRODUCT_ARCHITECTURE.md:142-149`; `CONVERSATIONAL_BUILDER.md:71,82`).
- **Finding 9 — OPEN.** Метки пермишн-скоупленных счётчиков Simulate/impact не
  добавлено.
- **Finding 10 — OPEN.** Различие integrity-vs-trust в
  `BUSINESS_FRAMEWORKS.md:73,85` не проговорено; контент фреймворка остаётся в
  доверенном SYSTEM-блоке.

**Сводка D-рулингов после Round-1:** D4 и D6 теперь **hold** (неверное имя
Synthesizer отозвано, walker честно ограничен + golden-тесты) — см. закрытие
Findings 2 и 3.
