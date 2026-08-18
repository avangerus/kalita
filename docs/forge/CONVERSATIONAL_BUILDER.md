# CONVERSATIONAL BUILDER — DesignSession и протокол построения модели

> Как разговор становится формальной валидированной моделью. Builder — НЕ
> stateless prompt: состояние сессии — governed data (pack `builder`, слой 3),
> путь изменения — существующий proposal→signature→apply (слой 4). Протокол
> цикла: UNDERSTAND → DISCOVER → RECOMMEND → CONFIRM → MODEL → VALIDATE →
> EXPLAIN → SIMULATE → APPROVE → APPLY.

## 1. Роли и доверие

| Участник | Что делает | Что НЕ может |
|---|---|---|
| Человек (Process Owner) | формулирует цель, отвечает, подтверждает, подписывает | — (его подпись — authority) |
| Expert (workers/builder, LLM + фреймворки) | понимает, спрашивает, рекомендует, объясняет | записывать предположения как факты; применять что-либо |
| Synthesizer (workers/builder, детерминированный) | собирает JSON-модель из confirmed decisions | менять решения |
| Node engine | валидирует, паркует, применяет после подписи | применять без human+Owner+signature (proposals.go:124-150) |

LLM — reasoning interface: генерирует вопросы/варианты/текст; структура модели
собирается из ПОДТВЕРЖДЁННЫХ решений и проверяется компилятором.

## 2. DesignSession — схема pack `builder` (NEW)

```dsl
entity DesignSession:
  goal        text        # исходная цель владельца (слой 3, факт)
  framework   string      # framework id + version, напр. "ap@1.0.0"
  status      enum [open, modeling, validating, proposed, applied, rejected, abandoned]
  proposal_id string      # заполняется на фазе APPROVE
  def_before  int         # def_version до (из describe_system)
  def_after   int         # def_version после apply
  created_by  ref[core.User]
  updated_at  datetime

entity SessionMessage:    # каждый ход разговора — журналируемое сообщение
  session     ref[DesignSession]
  author      enum [user, expert]
  kind        enum [fact, preference, answer, goal, decision, question,
                    recommendation, risk, required_decision, explanation,
                    validation_error, proposal_summary, simulation, status]
  topic       string          # id вопроса/решения, к которому относится ход
  body        text            # человекочитаемый текст
  provenance  string          # напр. "framework ap@1.0.0" | "user statement" | "general knowledge"
  turn        int             # номер хода (question discipline budget)

entity DesignDecision:    # только confirmed/rejected решения кормят модель
  session     ref[DesignSession]
  topic       string      # вопрос/решение (id из discovery фреймворка или custom)
  decision    string      # выбранный вариант
  status      enum [accepted, rejected]
  rationale   text        # почему (слова пользователя)
  decided_by  ref[core.User]
```

Права (builder pack, реализовано в packs/builder/security.dsl): v1 — ТОЛЬКО
pass-through. Роли `ProcessOwner` (create + read/update своих сессий
`where owner = $me`, сообщения/решения `where session.owner = $me`) и `Admin`
(full, супервизорный). Builder-агентской роли НЕТ, пока worker работает под
bearer человека — отдельная agent-роль (для фонового builder'а с собственным
token) вводится позже отдельным additive proposal и ТОЛЬКО со scoped правами
на назначенные сессии; unscoped read всех сессий агенту запрещён. Это
исправление ревью Architect Pro (finding: «Builder agent role granted
unscoped session read» — роль не должна существовать, пока не нужна).
Definition-решения движок гейтит отдельно (human + defApprover + подпись,
proposals.go), builder-пак на это не влияет.

## 3. Классификация высказываний (Expert conversation)

Expert обязан различать и помечать (SessionMessage.kind):

| Пользователь сказал | kind | Правило |
|---|---|---|
| «Все счета утверждает директор» | `fact` | журналируется как ВЫСКАЗЫВАНИЕ пользователя (данные разговора, слой 3). В MODEL не попадает сам по себе: перед MODEL Expert обязан показать сводку «я понял так» и получить подтверждение — факт входит в модель только как DesignDecision(accepted) |
| «Мне не нравятся лишние согласования» | `preference` | влияет на рекомендации, не на модель напрямую |
| Ответ на вопрос | `answer` | связывается с question через topic; ответ на вопрос = подтверждение → DesignDecision(accepted) по этому topic |
| Kalita предлагает | `recommendation` | provenance: framework@version; явно «рекомендация, не правило» |
| Kalita видит риск | `risk` | risk id из фреймворка или custom; likelihood/impact если известны |
| Kalita требует решение | `required_decision` | blocking question — без него MODEL не начинается |
| Kalita не знает | (kind=`question`, explicit) | честно: «этого нет в фреймворке, нужен ваш ответ» |

**Запрет**: выдавать предположение LLM за policy компании. Факт попадает в
модель только через DesignDecision(status=accepted), созданный из слов
пользователя (и подтверждённый им в сводке перед MODEL). LLM-текст без
подтверждения в модель не попадает никогда. Классификатор помечает kind
только ТЕКСТУ ПОЛЬЗОВАТЕЛЯ — рекомендации Expert всегда несут provenance и
kind=recommendation, их нельзя спутать с фактом компании.

## 4. Question discipline

- **Blocking** (без ответа нельзя безопасно строить): `required_decision`.
  Для AP: кто утверждает, что такое «счёт к оплате».
- **Important** (есть default, лучше спросить): пороги сумм, сверка с
  договорами, портфель ролей. Default явно показывается: «если не скажете —
  сделаю единое утверждение директором».
- **Optional** (можно отложить): валюты, PO-сверка, отчёты. Предлагаются
  «потом», не блокируют.
- Бюджет: максимум 2–3 вопроса за ход (turn); Expert выбирает минимальный набор
  для СЛЕДУЮЩЕГО meaningful proposal; «интервью из 50 вопросов» запрещено
  протоколом (проверяется в acceptance E).

## 5. Цикл по фазам и маппинг на существующие инструменты

| Фаза | Действия Expert/worker | Инструменты (все — существующие MCP/REST) |
|---|---|---|
| UNDERSTAND | прочитать цель, определить домен, выбрать фреймворк (правила + LLM-классификатор по списку из manifest), создать DesignSession | `create_record` (builder.DesignSession); manifest локально |
| DISCOVER | вопросы из framework.discovery; классификация ответов; фиксация fact/preference | `create_record` (SessionMessage) |
| RECOMMEND | варианты из framework.alternatives + риски + SoD; provenance-метки | `create_record` (SessionMessage kind=recommendation/risk) |
| CONFIRM | пользователь выбирает; решения фиксируются | `create_record` (DesignDecision accepted/rejected) |
| MODEL | Synthesizer: confirmed decisions → структурированный JSON (compose_pack-схема: entities/typed fields/roles/permissions/links/базовые переходы) + DSL-фрагменты для выражений (`when`), computed, событий, automation, dashboards, include-миксинов. Честная граница (ревью Architect Pro): LLM авторизует формулировки И структуры И фрагментов; истина — validate_dsl и additive-валидация движка, которые прогоняют ВСЁ | decisions из журнала (read), `field_types`, `get_grammar` |
| VALIDATE | цикл: `validate_dsl` → ошибки {code,file:line,fix_hint} как данные → исправить → повтор до зелёного; compile-ошибки ≠ провал, это loop | `validate_dsl`, `compose_pack` |
| EXPLAIN | детерминированный рендер из структуры модели: Что создаётся / Кто участвует / Как идёт процесс (переходы списком/диаграммой) / Кто что может / Что делает агент / Где нужен человек (approval gates) / Какие системы подключаются / Открытые вопросы. LLM может только подобрать формулировки поверх структуры | локальный рендер структуры |
| SIMULATE | сценарный прогон + impact analysis (§6) | `query`, `dashboard`, `describe_system`, локальный walker |
| APPROVE | `propose_change` (files, base_def_version=текущий, description с session_id, basis=человек, idempotency_key) → parked | `propose_change`, `get_proposal` |
| APPLY | человек решает через UI/REST; движок применяет | `GET /api/proposals`, `POST /api/proposals/{id}/decide` (server.go:158-159), WebAuthn-подпись |

**Правило фаз**: MODEL не начинается, пока есть unanswered blocking questions.
APPROVE не начинается, пока VALIDATE не зелёный и EXPLAIN не показан
пользователю. Ни одна фаза не перескакивается.

## 6. Simulation и impact analysis

**Simulate (для создания)**: прогон сценариев на предложенной модели —
walker worker'а берёт ТОЛЬКО подмножество guard-выражений, которое он умеет
оценивать консервативно (сравнения числовых полей с порогами, `and/or`,
enum-равенства). Всё остальное НЕ «считается» — помечается «движок проверит
при исполнении». Расхождение walker↔evalWhere закрывается golden-тестами
против packs/ap (движок — истина; walker, расходящийся с ним, падает тест).
HITL-гейты никогда не «считаются пройденными» — они показываются как точки
подписи. Для изменений — те же сценарии по ЖИВЫМ записям (`query`):
«если бы новая модель была активна сейчас: из N открытых счетов ...».

**Impact analysis (для изменения)**: структурный diff old model
(`describe_system`) → proposed:
- affected records: counts по состояниям (`query`);
- active workflows/pending approvals: counts; для approvals — структурно
  (какие гейты появились/изменились) + где возможно — живые pending;
- users/agents gaining/losing: diff grants (gain/loss списки);
- dashboards changed / integrations affected: diff структуры;
- business diff: человеческие строки («было: >1M → CEO. станет: 1M–2M → CEO,
  >2M → CEO+Owner») + технический diff (DSL) в Developer-виде.
Неопределимое помечается «неизвестно» — не выдумывается.

**Реализованный change-протокол (Round-3, live-доказан)**: change-сессия
(DesignSession.mode=change) задаёт ТОЛЬКО один вопрос — новое правило
(topic=amount_thresholds); diff base = journaled decisions последней APPLIED
build-сессии (статусы синхронизируются с движком через get_proposal — журнал
единственная истина, модель не реконструируется LLM из «памяти»); влияние по
живым записям считается прогоном generated guard'ов через walker, причём:
- читаемых записей нет ИЛИ прав нет (engine fail-closed: пустой результат без
  ошибки) → честная строка «счетов не видно (0): либо их нет, либо у роли нет
  права чтения»;
- guard вне подмножества walker'а → «маршрут не определим», не угадывается;
- differential-контракт: общий корпус walker_golden.json гоняется И движком
  (evalWhere, internal/engine/walker_golden_test.go), И walker'ом
  (test_walker_golden.py) — расхождение валит один из suite'ов.

## 7. Корреляция и аудит (требование OBSERVABILITY)

- `basis` на каждом событии: create сессии (basis=пользователь), сообщения
  (basis=сессия), proposal (basis=сессия + человек).
- `propose_change.description` = `"[builder session <id>] <человеческое описание>"`.
- В журнале выстраивается: user request → session.* → messages/decisions →
  DefinitionProposed → DefinitionApproved/Applied → работа системы.
- Метрики worker'а: turns, questions, validate_loops, propose→sign latency,
  simulation_runs, framework_id/version. Все логи — correlation id = session_id.

## 8. Failure modes и защиты

| Failure | Защита |
|---|---|
| LLM галлюцинирует сущности/поля | validate_dsl (compile) + additive-валидация движка |
| LLM выдаёт рекомендацию за факт | kind-классификация + provenance + CONFIRM |
| Пользовательский текст = prompt injection | promptbox (данные ≠ команды); периметр для ingest |
| LLM «вспоминает» устаревшую модель | base_def_version гейт (proposals.go:67-71) |
| LLM хочет применить без человека | невозможно: DecideProposal engine-enforced |
| Сессия брошена | status=abandoned через automation (stuck) — можно; не критично |
| Валидация зациклилась | лимит loop'ов (напр. 5) → status + вопрос человеку/ручной фикс |
| Два процесса меняют модель параллельно | base_def_version + idempotency_key (уже в движке) |

## 9. Acceptance-критерии протокола (milestone B)

1. Новый пользователь (без DSL-доков) доводит AP-модель до working pack:
   сессия → вопросы → proposal → подпись → живая модель (проверяется в E).
2. Каждый шаг сессии виден в журнале с корректными kind/provenance.
3. Ни один LLM-текст не появляется в DSL без подтверждённого DesignDecision.
4. Изменение approval-правила разговором проходит impact analysis и business
   diff до подписи (milestone C).
5. Второй домен (ITSM) проходит тот же путь без правок протокола
   (generality-proof).
