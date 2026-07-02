# LLD-08 — journal outbox read: MCP-тул `read_events` (since-курсор, node-wide)

Дата: 2026-07-02. Автор решений: Fable. Реализация: Sonnet 5 по документу.
Родитель: HLD.md §8a («журнал — готовый outbox»), T10 (generic journal-sink),
LLD-07 §2 (bridge). Закрывает ядровой гэп, найденный при реализации LLD-07.

**Мотив.** LLD-07-bridge наткнулся: `read_journal` — пер-записный (ABAC по
subject), без `since`, не даёт node-wide инкрементальный tail `event.emitted`.
Bridge обошёл через `query`+пер-записный журнал, но: (а) O(записей) на цикл;
(б) **свободностоящие `event.emitted` от MCP `emit_event` (PR-5) невидимы** —
значит cross-node события, инициированные коннектором/агентом через MCP, не
пролезают в федерацию. Нужен настоящий outbox-примитив.

**Границы для исполнителя.** Sonnet НЕ решает сам: capability-модель §2 (это
новая поверхность прав над журналом — необратимо, ревью Fable ОБЯЗАТЕЛЬНО);
то, что тул читает `Store.Since` и НЕ строит параллельную проекцию; отказ
отдавать payload-поля, запрещённые ABAC (§3). НЕ трогать семантику
`read_journal` (остаётся пер-записным аудит-view для UI/человека).

---

## 1. Тул `read_events`

MCP-тул: `read_events {since: uint64, kinds: []string?, limit: int?}` →
`{events: [{seq, kind, subject, payload, ts, actor}], next_since: uint64}`.
- Читает `Store.Since(since)` (уже есть в eventstore — ADR-002), НЕ сканирует
  проекции, НЕ строит своей структуры. Курсор — это `seq`, отдаётся `next_since`
  = seq последнего + 1 (клиент передаёт его в следующий вызов).
- `kinds` фильтрует по kind (дефолт — без фильтра? НЕТ: дефолт = `event.emitted`,
  чтобы неявно не отдать весь журнал; пустой явный список = все разрешённые).
- `limit` (дефолт 500, cap 5000) — страничный tail; клиент листает по next_since.
- Node-wide: НЕ привязан к subject (в отличие от read_journal). Это его смысл —
  тянуть поток фактов по всему узлу.

## 2. Capability (новая поверхность прав — необратимо)

Node-wide чтение журнала мощнее пер-записного — оно ОБЯЗАНО быть явно
разрешено, не выводиться из обычных read-грантов.
- Новый verb в permissions: `tail [kind, ...]` — роль объявляет, какие KIND
  событий она вправе тянуть через read_events (`tail [event.emitted]` для
  bridge/sink). Пустой/несуществующий → read_events отклоняется PermissionDenied.
- Валидация имён kind — против закрытого списка eventstore-kind (как §4c для
  действий), did-you-mean.
- Проверка в Permission Engine ДО чтения (I-2): актор без `tail`-гранта не
  узнаёт даже размер журнала.
- Это least-privilege для интеграционных акторов (bridge, sink, egress): им
  дают `tail [event.emitted]` и НИЧЕГО из обычного домена — они видят поток
  фактов, но не записи. Ровно топология LLD-07 §4.

## 3. ABAC на содержимом (не «tail = видишь всё сырьё»)

`tail`-грант разрешает ВИДЕТЬ факт события заданного kind, но payload
фильтруется:
- `event.emitted` — доменное событие, его payload объявлен схемой `event` и
  предназначен для передачи (bridge его и переносит) → отдаётся целиком.
- Прочие kind (если роль их затейлила, напр. для egress-отчёта) — payload
  проходит field-level ABAC актора; поля, которые актор не вправе читать,
  маскируются (переиспользовать существующий maskFields).
- **Решение:** в v1 `read_events` отдаёт ТОЛЬКО `event.emitted` и системные
  kind без чувствительного payload (op.*, checkpoint.sealed). Тейл
  record.created/updated (сырьё записей) — за отдельным более сильным грантом
  `tail_records`, НЕ в этом LLD (egress-отчёт спроектируем, когда понадобится).
  Так outbox-примитив не превращается в обход ABAC на записи.

## 4. Интеграция с bridge (LLD-07)

Bridge переключается с `query`+пер-записного обхода на `read_events
{since: cursor, kinds: [event.emitted]}` — O(новых событий), не O(записей); и
свободностоящие emit_event становятся видимы (гэп закрыт). Идемпотентный ключ
доставки §2 LLD-07 (`bridge|node|seq`) берёт `seq` прямо из ответа. Bridge-роль
на источнике: `tail [event.emitted]` + ничего больше.

## 5. Приёмочные тесты (фиксированы)

1. `TestReadEventsSince` — 3 emit подряд; read_events since=0 отдаёт все 3 +
   корректный next_since; since=next_since отдаёт пусто; новый emit → отдаётся.
2. `TestReadEventsSeesFreestandingEmit` — событие от MCP `emit_event` (без
   subject) видно в read_events (закрытие гэпа LLD-07).
3. `TestReadEventsRequiresTailGrant` — актор без `tail [event.emitted]` →
   PermissionDenied; актор с грантом → успех.
4. `TestReadEventsKindScoped` — `tail [event.emitted]` НЕ даёт вытянуть
   record.created (kind вне гранта отфильтрован/запрещён).
5. `TestReadEventsPagination` — limit-страницы через next_since покрывают весь
   поток без пропусков и дублей.
6. Golden/I-1: read_journal-семантика не изменилась; go test ./... + golden зелёные.
7. bridge (LLD-07) на read_events: демо federation-demo проходит; переигрывание
   с since=0 не дублирует (exactly-once сохранён).

## 6. PR-нарезка

PR-1: verb `tail` в грамматике+семантике (валидация kind-имён) + проверка в
      Permission Engine. ← ревью Fable (новая поверхность прав).
PR-2: MCP-тул read_events над Store.Since (since/kinds/limit/next_since) +
      §3 фильтр kind/payload. Тесты 1–5.
PR-3: переключить workers/bridge на read_events; убрать query-обход и его
      документированные ограничения из LLD-07. Тесты 2, 7.

**Порядок:** делать СРАЗУ после приёмки LLD-07-bridge (он работает на обходе;
LLD-08 ставит его на правильный примитив). До LLD-08 федерация переносит только
transition-emit — зафиксировать это ограничение в README federation-demo.
