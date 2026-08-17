# BUSINESS_TO_RUNTIME — путь от слов бизнеса до исполнения

> Как «человек сказал, как должен работать бизнес» превращается в работающую систему.
> Сверено с кодом на 2026-07.

## Путь (10 шагов)

1. **Человек описывает бизнес** — на естественном языке (владельцу помогают бизнес-вопросы Forge; см. сценарий-тест ниже).
2. **Формализация в DSL** — entities, поля/типы, workflow, роли (люди и агенты), permissions/ABAC, approval-гейты, automation, computed, dashboards, события, integration-блоки. Один пак = каталог `.dsl`-файлов.
3. **Валидация** — `kalita check --pack …` / `validate_dsl`: компилятор возвращает ошибки `{code, file:line, message, fix_hint}` — агент исправляет сам, до зелёного. Компилятор гарантирует compile-time инварианты: агентская роль без `deny` не компилируется; workflow-поле не пишется вне переходов; расписания/агрегаты — один парсер.
4. **Подписанное изменение** — `propose_change` (basis + idempotency_key) паркуется: applied только после человеческой подписи (Ed25519-ключ ноды; для людей — WebAuthn). Агент не может ускорить/подменить.
5. **Runtime** — definition-события попадают в журнал; движок (in-memory проекции, реплей) поднимает модель: записи, переходы, права — живы.
6. **Entities/workflows живут** — люди и агенты работают через REST/UI и MCP с одними и теми же проверками ABAC и одними и теми же событиями.
7. **Агенты/люди действуют** — переходы workflow; критические — `requires approval(Role)`: паркуются (✍) до подписи. Агенты — роли с deny-границами и field-scoped правами.
8. **Интеграции** — события наружу: `webhook out`, emit/tail (федерация), воркеры-коннекторы (least-privilege роли, idempotency), product.mcp-фасады. Семантика интеграции декларируется в DSL; транспорт — вне ядра.
9. **Журнал** — каждое действие — событие в append-only журнале с SHA-256 цепочкой; всё аудируемо, undo/rewind — тоже события.
10. **Проекции/UI** — `/api/meta` отдаёт UI описание модели: браузер строит списки/формы/канбан/дашборды/inbox без build-шага; дашборды считают агрегаты с row-level ABAC.

## Живые end-to-end сценарии

### 1. Согласование входящего счёта (архитектурный тест владельца)

Сегодня реализовано ≈30% (готово: механика; нет: AP-объекта, порогов, иерархии, интеграций X/Y).

| Шаг сценария | Есть сегодня | Пробел |
|---|---|---|
| Счёт приходит из «системы X» | паттерн приёма: Intake-роль (crm), `workers/connectors/inbound.py` (форма→Lead, secret+whitelist) | нет сущности входящего счёта/поставщика; нет коннектора 1С/банка |
| Агент проверяет договор, сумму, реквизиты | Legal-агент (contracts): Playbook, clause-level Findings, field-scope (assessment/risk), `flag`-трипвайр, honeypot-тесты | нет сверки счёт↔договор (кросс-пак ref отсутствуют); нет проверки сумм/реквизитов |
| ≤100к — руководитель, ≤1М — финдир, >1М — гендир | механика: `requires approval(Role)` + guard-условия (`when amount…` — hr.dsl); 2-подписной цикл (contracts) | нет лестницы «сумма→роль»; нет орг-иерархии (Department нет); статичный утверждающий |
| После утверждения → «система Y» | `webhook out` (pangram), emit/tail, escalation_to | в finance/contracts ничего не выходит наружу после approve |
| Сотрудники с правами | row-level ABAC, field-scope, deny>allow, портал-инвайты | create-time unscoped (задокументированный гэп TECHDEBT) |

**Цель Cycle-001**: поднять сценарий до исполняемого (пак `packs/ap`) — см. proposals/cycle-001.md.

### 2. Service Desk incident (живой, полный)

1. Пользователь создаёт Incident через портал (`source=Portal`) или агент через MCP.
2. Workflow: `New → Investigating (assignee=OperatorL2) → Identified → Resolved → Closed`; `auto_close` для Tivoli; `sla_left` — computed от SLAPolicy (бизнес-календарь).
3. HITL: у Change — CAB-гейт (`requires approval(ChangeManager)`); у ServiceRequest — approve/reject (Supervisor).
4. Дашборд OperatorBoard: открытые, SLA-бріч, по приоритету — с row-level ABAC.
5. Проверено: `TestDogfoodServiceDeskPack` (полный цикл через MCP) + демо-нода на :8080.

### 3. DevTrack: агент выполняет задачу и сдаёт результат человеку (живой, полный)

1. Lead создаёт Issue (Backlog); `pick_up` — переход с `assignee = Engineer agent`.
2. Воркер agent_runner: long-poll `wait_for_task` → `take_task` (lease, идемпотентный ключ) → работа (LLM или детерминированный drive) → `report_progress` → submit.
3. `submit` паркуется: `accept`/`reject` — за подписью Lead (HITL). Агент не может сам принять.
4. Проверено: `TestDogfoodDevTrackAgentLoop`; deny-границы Engineer (не трогает Issue.status/reporter, delete*).

## Принцип, который это скрепляет

Люди и агенты бьют в один движок: одни проверки прав, один журнал, одни подписи.
«Welding → bricks»: свободный артефакт (код, документ) дрейфует; в Kalita дрейф не компилируется,
критические переходы требуют подписи человека, и ничего не происходит молча.
