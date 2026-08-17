# Cycle-B1 · PROPOSE — webhook delivery runner (G2/D-011, Milestone B)

> Для ARCHITECT REVIEW. Решения владельца зафиксированы: node-level
> KALITA_EGRESS_DOMAINS (enforcement) + egress-report (audit); доставка
> at-least-once, business effect обязан быть idempotent; Y = reference/mock
> payment endpoint первого пилота.

## Проблема

`webhook out "https://..."` (DSL) журналирует `TaskWebhook{Entity,RecordID,Args:[url]}`
(automation.go:290-294, dsl/parser_blocks.go:366-373), но нода его НЕ доставляет —
раннера нет (D-011). Egress-шов мёртв: сценарий «счёт утверждён → система Y» доходит
только до намерения. Milestone B exit (reference-сценарий X→approve→Y) невозможен.

## Существующая механика (факт)

- Задачи: engine.Task{Kind: TaskWebhook, Entity, RecordID, Args:[url]} — без Role
  (automation.go:292); `Tasks(role,status)` фильтрует по role → для webhook-задач
  нужен новый доступ.
- Egress-audit: `egress-report` фолдит `event.emitted` с именем `EgressCalled`
  (domain/purpose/ref) против деклараций акторов (egress.go:21-123) — раннер должен
  сам журналировать EgressCalled о каждой реальной доставке.
- Tick ноды — раз в минуту (main.go:620-633); раннеру нужен свой, более частый тик.
- TakeTask требует role-совпадения (tasks.go:199) → для безрольной webhook-задачи
  не подходит; раннер — системный актор с собственным переходом.

## Дизайн (вариант A — in-process runner в serve, рекомендуется)

1. **Конфиг**: `KALITA_EGRESS_DOMAINS` (comma-separated allowlist; пусто = запрещены
   ВСЕ исходящие webhook — fail-closed).
2. **Раннер** — goroutine в `serve` рядом с Tick (тик ~10s):
   a. взять открытую `TaskWebhook` (новая `engine.OpenWebhookTasks()` — read-only);
   b. распарсить URL из Args[0], извлечь host; **whitelist enforcement ДО сети**:
      host не в allowlist → `FailTask` + эскалация defApprover (как automation abandon),
      доставки НЕТ, egress.called НЕ пишется (реального вызова не было), лог;
   c. собрать payload = запись (entity+record, из engine.Get) + `idempotency_key:
      "webhook:<taskID>"` (детерминированный — Y дедуплицирует at-least-once
      redelivery после краша);
   d. доставить POST (timeout, retries с backoff cap 5 → по exhausted: abandon-семантика);
   e. успех → `CompleteTask` + журналировать `EgressCalled` (domain/purpose/ref) —
      видно в `egress-report`;
   f. постоянная ошибка → `FailTask` + эскалация (как automation abandoned).
3. **Доставка** — функция `deliver(url, payload) error`, инжектируется для тестов
   (без сети); production = net/http.
4. **At-least-once + idempotent effect**: повторная доставка после краша даёт тот же
   `idempotency_key` → Y (mock payment) обязан идемпотентно обработать. Kalita не
   обещает exactly-once доставки — только exactly-once ЭФФЕКТА на стороне Y.

## Что НЕ входит (осознанно)

- Вебхуки со стороны воркеров (worker egress-DLP) — P2, Milestone G.
- Exactly-once доставка — не обещается; идемпотентность на приёмнике (patтern subscribe.py).
- Подписанные webhook-запросы (HMAC/secret на payload) — кандидат в Next, не блокер пилота
  (Y = mock, доверенный).

## Acceptance criteria

1. Задача TaskWebhook с URL вне KALITA_EGRESS_DOMAINS → доставки нет, task failed +
   эскалация, journal без egress.called (тест).
2. Разрешённый домен → deliver вызван ровно с idempotency_key "webhook:<taskID>" и
   payload-записью; успех → task completed + EgressCalled в журнале (тест, inject deliver).
3. Временная ошибка deliver → retries с backoff (тест: N-й вызов успешен).
4. Постоянная ошибка → после cap: FailTask + эскалация defApprover (тест).
5. Рестарт/повтор не даёт двойного ЭФФЕКТА: idempotency_key детерминирован (тест: два
   прогона одного task → одинаковый ключ).
6. egress-report видит доставку как вызов с purpose/ref (тест).
7. `go build/vet/test ./...` зелёные; 16 паков + 3 примера.
8. Reviewers: skeptic_pro + review_security_pro (egress trust boundary).

## Риски / blast radius

- Blast radius: internal/engine (OpenWebhookTasks + settle-хелперы) + cmd/kalita
  (webhooks.go: клиент/ретраи/whitelist) + main.go (тик) + тесты. Новые event-виды НЕ
  нужны (EgressCalled едет на event.emitted; settle — task.completed/failed).
- Риск: SSRF/whitelist-обход — митигация: fail-closed allowlist + host-парсинг (не только
  префикс-матч) + security-review.
- Журнал: без новых схем — replay не затронут.

## Human gates

Нет (семантика утверждена владельцем).
