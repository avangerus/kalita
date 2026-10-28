# PRODUCT ARCHITECTURE — Kalita self-building enterprise product

> Как «владелец сказал, как должен работать бизнес» превращается в работающую
> систему БЕЗ ручного DSL. Архитектура миссии: Business Expert → Conversational
> Builder → executable model → governed application.
> Каждый механизм ниже либо уже существует в kernel (file:line), либо явно
> помечен NEW. Документ — источник истины для milestone A–D; обновляется каждый
> цикл. Рецензия: Architect Pro (см. proposals/product-mission/arch-review.md).

## 1. Продуктовый принцип и четыре слоя знания

Kalita — **исполняемая модель предприятия с бизнес-экспертизой, являющаяся
общим языком для людей, агентов и систем**. LLM — reasoning interface, НЕ
источник истины. Четыре слоя, которые запрещено незаметно смешивать:

| Слой | Что это | Где живёт | Как попадает в модель |
|---|---|---|---|
| 1. Base model knowledge | Общие знания LLM | Внутри LLM | Никогда напрямую. LLM эмитит только структурированный intent/JSON, который проходит формальную валидацию. |
| 2. Kalita Business Frameworks | Curated бизнес-экспертиза | NEW `frameworks/*.json` (репо, integrity-верификация при загрузке) | Только как SYSTEM-контекст Expert-фазы; каждая рекомендация несёт provenance `framework@version`. |
| 3. Company Knowledge | Факты/policies/терминология организации | NEW pack `packs/builder` (SessionMessage kind=fact/preference) + существующие записи (`query`, `search_perimeter`) | Только факты, подтверждённые пользователем (DesignDecision accepted) или прочитанные из журнала. Предположение LLM ≠ policy. |
| 4. Executable Enterprise Model | Формальная истина | DSL-паки + event journal; `def_version` | Единственный исполняющий слой. Меняется ТОЛЬКО через proposal → человеческая подпись → atomic apply. |

**Инвариант границ**: слой N+1 может читать слой N, но содержимое слоя 3
создаётся только пользователем/журналом, а содержимое слоя 4 — только
движком. LLM-текст никогда не записывается в слой 4 без (а) структурной
валидации и (б) человеческой подписи. LLM-текст не записывается в слой 3 как
факт без пользовательского подтверждения.

## 2. Сквозной product loop (16 шагов → существующие механизмы)

```
Владелец: «У нас счета приходят из X. Сверять с договорами. ≤100к руководитель,
≤1М финдир, выше — гендир. После утверждения — в Y.»
        │  разговор (web #/ask → workers/builder HTTP → MCP под bearer человека)
        ▼
[1-2 UNDERSTAND]  сессия DesignSession (pack builder), framework=ap@1.0.0
[3-4 DISCOVER]    Expert задаёт blocking/important вопросы; факты/предпочтения
                  → SessionMessage (журнал, ABAC where owner=$me)
[5 RECOMMEND]     варианты с provenance framework@version, risks помечены
[6 CONFIRM]       DesignDecision accepted/rejected — только это кормит MODEL
[7 MODEL]         решения → структурированный JSON (compose_pack-схема) +
                  DSL-фрагменты (get_grammar); типы — field_types
[8 VALIDATE]      validate_dsl — ошибки {code,file:line,fix_hint} как данные
[9 EXPLAIN]       детерминированный рендер из структуры: сущности/роли/процесс/
                  права/агенты/гейты/интеграции/открытые вопросы
[10 SIMULATE]     прогон сценариев по живым данным (см. §6) + impact analysis
[11 APPROVE]      propose_change (basis, base_def_version) → parked; человеческая
                  подпись DecideProposal (только human + роль Owner + Ed25519/WebAuthn)
[12 APPLY]        движок применяет атомарно: definition.applied, def_version++
[13-16]           UI/meta уже рендерит приложение; роли/права/агенты/интеграции —
                  из той же модели; изменение позже = новый proposal
```

**Ключевой факт**: весь change path (валидация → парковка → подпись → атомарное
применение → additive-валидация) УЖЕ существует в kernel —
`internal/engine/proposals.go` (ProposeChange:59, DecideProposal:110, проверка
human+роль+подпись:124-150, additive-ограничения validateAdditive:215,
diffPlan:249). Builder НЕ создаёт второй путь изменения системы — он лишь
готовит содержимое proposal. Это и есть AI SAFETY BOUNDARY.

## 3. Новые компоненты (NEW) и их место

### 3.1 `frameworks/` — curated business knowledge (слой 2)
- Каталог JSON-файлов в репо + `frameworks/manifest.json` (identity, version,
  origin, sha256, compatibility). Подробно: BUSINESS_FRAMEWORKS.md.
- Читается ТОЛЬКО worker'ом builder при старте; sha256 сверяется с manifest.
- Не Go-код, не DSL, не runtime-мутация. Обновление = релиз репо; на работающую
  компанию влияет только через будущие сессии (миграция = обычный proposal).

### 3.2 `packs/builder` — DesignSession как governed data (слой 3)
- NEW pack: сущности DesignSession, SessionMessage, DesignDecision.
- Пишется worker'ом через ОБЫЧНЫЙ create_record под bearer вызывающего
  человека (паттерн `packs/assistant` + workers/chat_runner — сессия чата уже
  так устроена: chat_runner.py:15-23). Значит: ABAC, журнал, undo, retention —
  бесплатно, без chat-specific кода в kernel.
- Transient-состояние (сырой JSON модели, список ошибок валидации, черновики
  diff) живёт в памяти/файлах worker'а и в журнал НЕ пишется. В журнале —
  только подтверждённые артефакты: сообщения, решения, ссылки на proposal.
- Схема и протокол: CONVERSATIONAL_BUILDER.md.

### 3.3 `workers/builder` — Business Expert + Builder (Python, stdlib-only)
- HTTP-эндпоинт для web UI + MCP-loop к ноде. **Pass-through**: без
  собственного service credential; каждый вызов ноды идёт с bearer
  вызывающего человека (правило chat_runner.py:5-11 и facade.py — LLD-06 §1).
- LLM через env-границу `LLM_URL/LLM_MODEL/LLM_KEY` (model independence, §8).
- Prompt-сборка через существующий `workers/agent_runner/promptbox.py`
  (сообщения пользователя — ДАННЫЕ, не команды; H4).
- Композиция: framework-загрузчик + expert (discovery/классификация) +
  synthesizer (структурированный JSON + DSL-фрагменты; LLM-assisted —
  честная граница см. ниже) + validator (validate_dsl-цикл) + explainer
  (детерминированный рендер) + simulator (§6) + proposer (propose_change).
- Worker не решает, не подписывает, не «помогает» pending approval
  (chat_runner.py:33-37, HITL не обходится).

### 3.4 `web/` — product shell поверх существующего notation-UI
- Текущее: один preact app.js без сборки; маршруты /inbox, /search, /upload,
  /dashboards, /agent-board, /agents (app.js:1087-1099); списки/формы из
  /api/meta. Это «Work»-слой — остаётся.
- NEW маршруты: #/ask (разговор с builder), #/proposals (human approval inbox —
  REST `/api/proposals` уже есть, server.go:158-159), #/company-model, #/changes,
  #/integrations, #/home. Подробно: UX.md.
- Три уровня: Business / Operator / Developer (существующие developer-виды —
  journal/DSL — прячутся за Developer-переключателем).

## 4. AI Safety Boundary (нерушимо)

1. **Нет второго AI admin API.** Worker builder'а — обычный actor: те же MCP-
   инструменты, тот же Permission Engine, тот же журнал. Его HTTP-эндпоинт
   ничего не умеет сверх MCP под bearer вызывающего.
2. **Definition меняет только движок.** propose_change паркует; DecideProposal
   требует `ActorHuman` + роль `defApprover` (default "Owner", engine.go:206) +
   валидную подпись над DefinitionMessage (proposals.go:124-150). LLM не может
   ни подписать, ни ускорить. Паркованный proposal безвреден (in-memory,
   не применяется без решения).
3. **Additive-валидация.** Существующие сущности/поля нельзя удалить или
   сменить тип (validateAdditive, proposals.go:215-246). «LLM придумал
   destructive change» — отвергается движком, не промптом.
4. **Prompt injection.** Пользовательский текст — данные через promptbox;
   содержимое фреймворков — curated + sha256; ингest-контент — через
   существующий perimeter-детектор (flag_ingest). Agent-текст никогда не
   рендерится как HTML (chat_runner.py:39-43).
5. **Staleness.** base_def_version должен совпадать с живой нодой
   (proposals.go:67-71, 129-133) — LLM не может «вспомнить» устаревшую модель.
6. **Атрибуция.** Каждая мутация несёт basis — цепочка от запроса пользователя
   через session_id до definition.applied (§7).

## 5. Design decisions (D1–D11)

| # | Решение | Обоснование |
|---|---|---|
| D1 | Builder = worker вне kernel, pass-through bearer | chat_runner/facade-прецедент; ноль новых прав; kernel не трогаем |
| D2 | DesignSession = записи в pack builder | журнал/ABAC/undo бесплатно; сессия ≠ stateless prompt |
| D3 | Frameworks = версионированные JSON в репо, sha256-верификация | inspectable, trusted, вне kernel; апдейт не трогает живую компанию |
| D4 | Модель генерируется только из confirmed decisions | LLM-память ≠ company policy; слой 3 → слой 4 только через подтверждение |
| D5 | Explain-рендер детерминированный, не LLM-проза | доверие: описание выводится из формальной модели |
| D6 | Simulate = сценарный прогон по живым данным (worker) + структурный diff | практично, честно ограничено; engine shadow — для record-мутаций, не для definition (shadow.go:14-28) |
| D7 | Impact analysis = структурный diff моделей + query/dashboard по живым записям | affected records/approvals считаются от журнала, не от LLM |
| D8 | UI: notation-UI остаётся Work-слоем; новый shell — надстройка buildless | не переписывать доказанное; meta уже рендерит приложение |
| D9 | LLM-граница = env (LLM_URL/LLM_MODEL), как в chat_runner | model independence; enterprise-семантика — в DSL/движке |
| D10 | Один пак builder на ноду, сессии scoped `where owner=$me` | многопользовательский пилот без новых механизмов |
| D11 | Второй домен (ITSM) доказывается на существующем packs/servicedesk | generality без нового kernel; goldens: packs/ap, packs/servicedesk |

**Честная граница синтеза (после ревью Architect Pro)**: «детерминированный
Synthesizer» — неверное имя. compose_pack покрывает СТРУКТУРНУЮ часть
(entities/typed fields/roles/permissions/links/базовые workflow-переходы —
tools.go:61-90); выражения (`when`, computed), события, automation, dashboards,
include-миксины — raw DSL-фрагменты, которые ТОЖЕ пишет LLM (по грамматике из
get_grammar), и ВСЁ это проходит validate_dsl + additive-валидацию движка.
То есть: LLM авторизует формулировки, движок — истину. Рендер EXPLAIN и diff —
детерминированные, но входом служит валидированная модель, а не LLM-проза.

## 6. Simulation и impact analysis (что именно показываем)

**Simulate (до apply)** — worker прогоняет сценарии по живым данным:
- вытащить текущие записи затрагиваемых сущностей через `query` (read-only,
  ABAC честен);
- для каждой записи вычислить, по какому маршруту она пошла бы в ПРЕДЛАГАЕМОЙ
  модели. Walker покрывает ТОЛЬКО консервативное подмножество guard-выражений
  (числовые пороги, and/or, enum-равенства); всё остальное помечается
  «движок проверит при исполнении», а HITL-гейты показываются как точки
  подписи, никогда как «пройдено»;
- расхождение walker↔engine закрывается golden-тестами против packs/ap
  (движок — истина; расходящийся walker падает тест);
- вывод: «из 23 открытых счетов: 12 → руководитель (было 18), 8 → финдир,
  3 → гендир; 0 застрянут; 2 потеряли бы маршрут — посмотри».
Ограничение документируется: полная engine-семантика недоступна без
definition-тени; walker — честная аппроксимация, не второй движок.

**Impact analysis (для изменения существующей модели)** — структурный diff:
- сущности/поля (additive diff), роли и права (кто получил/потерял какие
  grants — из propose-структуры vs describe_system),
- workflow-переходы и approval-гейты (business diff: «было >1M → CEO; станет
  1M–2M → CEO, >2M → CEO+Owner»),
- живые записи по состояниям (`query` counts), pending approvals (через
  list_my_tasks/структурный diff — что именно можно показать, зависит от
  данных; неопределимое помечается «неизвестно», не выдумывается),
- дашборды и интеграции (структурно из модели), агенты (gain/loss действий).
Всё считается из структуры и журнала, LLM только подбирает формулировки
бизнес-слоя поверх чисел (слой 3, помечается).

## 7. Observability и корреляция

Цепочка `user request → design session → model proposal → signed change`
коррелируется полями:
- `DesignSession.id` (session_id), `basis` на каждом событии;
- `propose_change.description` содержит `session_id`;
- proposal-события (DefinitionProposed/Approved/Applied) несут proposal_id и
  basis (proposals.go:89-104, 151-180);
- метрики worker'а (структурированный лог): turns/session, questions asked,
  validate-циклов до зелёного, propose→sign latency, simulation runs,
  framework hits. Ошибки — с correlation id, понятным текстом для пользователя
  и кодом для разработчика (см. UX.md §Error UX).

## 8. Model independence

- Worker говорит с LLM только через env-границу (LLM_URL/LLM_MODEL/LLM_KEY) —
  прецедент chat_runner.py:76-78. Никаких вызовов конкретного провайдера в
  коде продукта.
- Смена reasoning-модели не меняет enterprise-семантику: (а) валидация —
  компилятор DSL; (б) explain/diff/simulate — детерминированный код;
  (в) применение — движок. LLM влияет только на качество разговора и качество
  первого варианта JSON, который всё равно проходит validate_dsl.
- Тест на независимость: один и тот же DesignSession прогнать с двумя разными
  моделями → результирующий proposal должен компилироваться и проходить
  additive-валидацию в обоих случаях (семантика — в структуре, не в прозе).

## 9. Scope discipline (что НЕ строим)

Мультитенант-облако, marketplace фреймворков, arbitrary workflow designer,
mobile, billing, свой foundation model, ещё один agent harness, десятки
отраслевых фреймворков. Один универсальный loop на двух доменах (AP, ITSM).

## 10. Риски и открытые вопросы

| Риск | Митигация | Статус |
|---|---|---|
| LLM галлюцинирует поля/роли | validate_dsl + additive-валидация + confirm-гейт | engine-enforced |
| Builder-пак раздувает журнал (каждое сообщение — событие) | сообщения компактные; retention через существующий механизм | следить в E |
| Walker simulation расходится с engine guard-семантикой | помечать «аппроксимация»; покрыть golden-тестами против packs/ap | P-риск |
| ProposeChange не гейтится по роли (парковка) | безвредно: proposal не применяется без подписи; спам-защита — rate-limit MCP (240/мин) | задокументировать в SECURITY |
| Framework poisoning через репо | sha256 в manifest + code review + релиз-процесс; frameworks не грузятся из сети | trusted by release |
| «Эксперт» выдаёт рекомендацию за policy | классификация statement + provenance-метки + confirm-фаза | протокол, не код — тестировать в E |
