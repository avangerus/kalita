# PRODUCT UX — audit существующего web/ и целевой product shell

> Milestone D. Принцип: НЕ переписывать notation-driven UI, а надстроить
> product shell для бизнес-пользователя. Три уровня: Business / Operator /
> Developer. Ключевое: ясность → доверие → управляемость.

## 1. Аудит: что реально умеет текущий web/ (факты)

Один файл `web/app.js` (~1109 строк, preact+htm, без сборки), чистый projection
`/api/meta` — бизнес-логики в UI нет (app.js:1-2). Маршруты (hash):

| Маршрут | Что есть | Оценка |
|---|---|---|
| `/inbox` | Signatures requested + Definition changes (Sign & apply / Reject) + My role's tasks (app.js:356-395, REST /api/approvals, /api/proposals, /api/tasks) | 🟡 ядро правильное; нет бизнес-diff/impact/session-контекста у proposal; нет «почему» |
| `/e/{entity}/{id}` (RecordView) | форма записи, workflow-кнопки, история (Timeline), связанные записи | 🟢 генеративный UI достаточен |
| `/e/{entity}` (EntityList) | список, фильтры, поиск, создание | 🟢 достаточен |
| `/dashboards` | тайлы из meta, ABAC | 🟢 |
| `/agent-board` | доска задач ролей | 🟢 операционный |
| `/agents` | акторы, invite/revoke (app.js:643) | 🟡 admin-утилита, не «governed workers»-взгляд |
| `/search`, `/upload` | периметр-RAG и загрузка файлов | 🟢 нишевые |
| каталог `/catalog.html` | storybook-каталог компонентов | developer |

**Чего нет совсем**: Home («что требует моего внимания»), разговорный
интерфейс Ask Kalita, человеческое представление модели компании
(Company Model), лента изменений/аудита, операционный взгляд на интеграции,
разделение уровней сложности, человеческие тексты ошибок.

**Вывод**: Work-слой (списки/формы/канбан/дашборды/инбокс) — 🟢, оставить и
не трогать семантику. Product shell вокруг него — строить.

## 2. Целевой shell (8 разделов)

### 2.1 Home (`#/home`)
«Что требует моего внимания»: pending approvals (счётчик+переход в Approvals),
definition proposals ждут подписи, agent exceptions (tasks failed/abandoned),
broken integrations (NEW: из worker-метрик/событий), SLA/risk alerts (из
dashboards), ключевые показатели (top-тайлы). Источники: /api/approvals,
/api/proposals, /api/tasks, /api/meta (dashboards). Агрегация — на клиенте из
существующих REST; новый бэкенд не нужен (при необходимости — один сводный
`GET /api/home` в api-слое, без семантики в UI).

### 2.2 Work (`#/work`)
Существующие EntityList/RecordView/AgentBoard — как есть, сгруппированы по
сущностям/доскам. Это notation-UI остаётся сердцем рабочего слоя.

### 2.3 Ask Kalita (`#/ask`) — ✅ реализовано Round-4
Главный conversational interface → workers/builder (NEW) и workers/chat_runner
(существующий). Режимы: построить процесс / изменить систему / спросить о
компании / объяснить процесс / анализ. Слева — чат, справа — живой DesignSession
(факты, вопросы, решения, статус фаз; из pack builder). Показывает provenance
рекомендаций («по фреймворку ap@1.0.0») и явные метки «рекомендация»/«риск»/
«требуется решение». Выход фазы MODEL → EXPLAIN-карточка → кнопка «Предложить
изменение» (propose) — НЕ «применить».

**Реализация (Round-4)**: web/app.js AskView — чат к /build/turn под bearer
пользователя, панель «Мои сессии проектирования» (goal/framework/mode/статус/
фаза из journal через POST /api/query), kind-бейджи и provenance, вывод модели
рендерится как ДАННЫЕ (никогда HTML), ссылка на Inbox при parked proposal,
builder-URL в localStorage (default 127.0.0.1:8400). Worker: loopback-only CORS
(прод — same-origin за прокси). Тесты: pytest CORS/HTTP + live-тест UI-пути
данных.

### 2.4 Approvals (`#/approvals`)
Развитие существующего Inbox-блока. Для каждого approval/proposal: что хотят
сделать, кто инициировал, агент или человек, почему (basis/description), данные
(record view), последствия (для proposals — business diff + impact analysis из
builder-сессии), approve/reject. Proposal-карточка дополнительно: session_id →
переход в сессию, технический diff в collapsed «Advanced». REST уже есть —
расширяем payload proposal (description с session_id, plan) и UI.

### 2.5 Agents (`#/agents`)
Не chatbots — governed workers: identity, role, permissions (из meta), текущие
задачи (tasks), recent actions (journal по актору), failures, awaiting approval.
Основа — /api/tasks + /api/meta + journal-read; вкладка «недавние действия» =
read_journal по актору.

### 2.6 Company Model (`#/company-model`)
Человеческое представление executable model — НЕ редактор YAML. Секции:
Entities (карточки с полями), Roles (люди/агенты + права словами), Processes
(workflow-диаграммы из meta: состояния и переходы, approval-гейты помечены ✍),
Integrations (declared в модели + operational health), Agents, Policies
(approval ladders словами). Всё — из /api/meta (уже есть поля/actions/workflows)
+ dsl-события для истории версий. Developer-вид: сырой DSL (collapse).

### 2.7 Integrations (`#/integrations`)
Операционный взгляд: какие системы подключены (из модели), last successful
exchange, failures, DLQ, retry, health. Данные: events (integration.*/webhook
задачи из журнала) + worker-статусы. Честно: где доставки нет (D-011) —
показывать статус «декларировано, доставка недоступна».

### 2.8 Changes / Audit (`#/changes`)
Кто изменил компанию и что произошло: лента definition.* событий (proposed/
approved/rejected/applied, с авторами и basis), человеческие описания, ссылки
на сессии; фильтр по времени/актору. Плюс record-аудит существующим Timeline.

## 3. Три уровня сложности

| Уровень | Видит | Пример |
|---|---|---|
| Business (default) | Home, Work, Ask, Approvals, Company Model (человеческий), Changes | «Счёт больше не соответствует условиям согласования: сумма изменена с 80 000 до 140 000. Требуется согласование финансового директора.» |
| Operator | + Agents (полный), Integrations (health/DLQ/retry), технические детали ошибок | + «GUARD_FAILED (engine, переход confirm)» |
| Developer | + DSL-вид модели, raw journal, /catalog, диагностика | существующие developer-инструменты |

Переключатель уровня — один на shell; developer-вид никогда не является
default. Business-уровень не содержит кодов ошибок без человеческого текста.

## 4. Error UX (обязательно для D)

Маппинг существующих кодов ошибок в человеческие сообщения. Движок уже
возвращает `{code, message, fix_hint}` — добавляем business-нарратив по классам:

| Класс | Сегодня | Станет (Business) + Advanced |
|---|---|---|
| GUARD_FAILED / workflow guard | сырой код | «Счёт больше не соответствует условиям этого согласования: сумма была изменена с 80 000 до 140 000. Требуется согласование финансового директора.» + code |
| PERMISSION_DENIED | сырой код | «У вашей роли нет права <действие>. Запросите доступ у администратора или выберите другой объект.» + rule |
| VALIDATION (поле) | сырой код | «Поле <label> не заполнено/неверный формат: <hint>.» + code |
| STALE_BASE | сырой код | «Система изменилась, пока вы работали. Обновите страницу и повторите.» + code |
| INTEGRATION / доставка | нет статуса | «Отправка в Y не удалась (попытка 3/5). Повторится автоматически; если не уйдёт — ответственный получит задачу.» + task |
| pending_approval | есть карточка | остаётся + человеческое объяснение «почему требуется подпись» |

Реализация: одна функция `humanizeError(code, ctx)` в web (client-side, по
meta-контексту) + в builder-worker для его ошибок; Advanced details — collapsed.

## 5. Consistency pass (чек-лист)

Navigation (единый shell-набор), typography (существующие tokens/format.js —
не плодить), forms (FieldAtom уже один источник), empty states («Пока нет
счетов. Создайте первый или подключите систему X»), errors (§4), confirmations
(опасные действия — явный confirm), loading (skeleton вместо пустоты),
permissions feedback (кнопка без права — скрыта + подсказка), approval
experience (единая карточка Approvals), audit timelines (TimelineItem —
единый), mobile sanity (проверить, не переписывать). Декоративная анимация —
вне миссии.

## 6. План реализации (без переписывания)

1. Shell-навигация + Home + Approvals-улучшения (существующий REST).
2. `#/ask` чат-поверхность (к workers/builder HTTP) + DesignSession-панель.
3. Company Model + Changes (meta + journal REST).
4. Integrations (worker-статусы + события).
5. Error UX pass + уровни сложности.
6. Consistency pass по чек-листу §5.
Всё — buildless preact в web/ (существующий стек, один файл + kalita-ui
компоненты); новых JS-рантаймов/сборок не вводим.

## 7. Exit criteria D

Новый пользователь без сопровождения разработчика: заходит → Home понятен →
создаёт счёт/инцидент через Work → видит работу агента → выполняет approval →
меняет процесс в Ask → видит impact → подписывает → продолжает работать.
Разработчик нужен только на Developer-уровне.
