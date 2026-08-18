# ENGLISH-FIRST INDUSTRIAL RELEASE — план (scope-frozen)

> Миссия: довести существующий Industrial RC до публикуемого English-first
> industrial release. No new product scope. Три работы: (1) D-011 egress до
> production-shaped состояния, (2) English-first pass, (3) финальная
> acceptance/review. Всё остальное — freeze.

## Фактическое состояние (проверено по коду, Round-1)

- **Git**: clean; head `9b2c08b` (mission-complete RC поверх v1.0.0-rc.2).
- **D-011**: runner СУЩЕСТВУЕТ (Cycle-B1): node-side tick (10s),
  `runWebhookDeliveries` (cmd/kalita/webhooks.go), at-least-once +
  `idempotency_key = "webhook:"+taskID`, 5 попыток in-tick, backoff 2s×attempt,
  timeout 30s, no-redirect, allowlist `KALITA_EGRESS_DOMAINS` fail-closed,
  settle completed/failed, egress-report reconciliation, restart-safe open
  tasks. **Разрывы против требуемой семантики**:
  1. idempotency-identity привязана к TASK id → requeue/DLQ-recovery даст
     новый ключ → риск дубля business effect;
  2. нет per-attempt receipts (attempt started/N/category/retry-scheduled);
  3. нет DLQ-списка и ручного recovery (settled-failed = мёртвый конец);
  4. нет секретов/авторизации к destinations (и нет их защиты от утечек);
  5. нет классификации retryable/non-retryable (4xx ≠ 408/429 ретраится 5×);
  6. нет метрик (queued/success/failure/retry/DLQ/latency/oldest pending);
  7. нет acceptance сценариев E (crash-окно) и F (DLQ-recovery) на уровне
     automated harness (reference-сценарий Cycle-B2 частично покрывает).
- **Метрики**: текстовый Prometheus в internal/api/metrics.go (gauge-набор) —
  egress-метрики добавятся туда.
- **Язык**: UI/фреймворки/builder/egress-report — русско-центричные; целевой
  язык релиза — English (i18n-платформу НЕ строим; English canonical).

## План (только release blockers)

### P-1 Egress hardening (kernel+runner, без смешивания уровней)
> ИМПЛЕМЕНТИРОВАНО (commit `1a6c126`), с поправкой Architect Pro: identity —
> НЕ business-coordinate hash (`ruleKey` нестабилен file:line), а per-firing
> `DeliveryID` (uuid v7) в taskPayload, переносится вперёд через
> requeue/DLQ-retry. Остальное — по плану ниже.
- **Engine**: TaskWebhook получает per-firing `DeliveryID` (uuid v7) в
  taskPayload — ключ стабилен через requeue/recovery; новый eventstore kind
  `WebhookAttempt` (additive; fold — receipts-проекция, с event-id dedup
  guard) с {attempt, outcome, category, destination, latency_ms}; листинг
  failed webhook tasks + `RetryWebhookTask` (новый open-таск с ТОЙ ЖЕ
  identity, role-gated defApprover); egress-метрики из проекций.
- **Runner**: классификация failure (3xx и 4xx кроме 408/425/429 →
  non-retryable → сразу DLQ с категорией; network/timeout/5xx retry, 5
  попыток, backoff 2s×attempt); per-attempt receipts в журнал; секреты через
  runtime injection: `KALITA_EGRESS_SECRETS` = путь к 0600-JSON
  {"domain": "Bearer …"} (валидация формата+0600 на boot, читается один раз;
  НИКОГДА в DSL/prompt/journal/git); отсутствующий секрет при
  configured=true → fail safely (категория auth_missing, без утечки); payload
  без секретов; метрики.
- **Ops surface**: `kalita egress dlq` (список) + `kalita egress retry <id>`
  + REST GET /api/egress/dlq и POST /api/egress/retry/{id} (defApprover-gated)
  + UI (Integrations: DLQ-список + Retry). Бизнес-семантика в DSL не меняется
  (intent остаётся TaskWebhook из automation `webhook out`).
- **Semantics-контракт**: at-least-once delivery + idempotent business effect
  (per-firing DeliveryID как idempotency_key) — exactly-once НЕ обещаем;
  документируем.

### P-2 Failure-injection acceptance (сценарии A–H)
Автоматизированный harness: mock Y (контролируемые 500/timeout/dup/crash/
persistent-fail/allowlist-off/secret-missing) + real node на PG.
A 500→retry→success; B timeout; C duplicate collapse (один business effect);
D node restart продолжает; E crash между send и settle → тот же ключ,
receiver коллапсирует; F persistent → DLQ → операторский recovery → один
эффект; G allowlist deny → fail-closed без network request; H secret
unavailable → fail safely.

### P-3 English-first pass
web/app.js (все user-facing строки), frameworks/*.json (content EN),
workers/builder (вопросы/ответы/объяснения EN), egress-report EN,
ошибки EN-first с machine-code в Advanced. Тесты синхронизируются.
Локализация-платформу не строим; семантика не привязана к тексту.

### P-4 English clean-room acceptance + ITSM sanity
Независимый оператор, English goal (AP), 20 шагов + egress success/failure/
recovery; ITSM минимальный сценарий English.

### P-5 Reviews + regression
Security Pro (egress SSRF/DNS/allowlist/redirect/IDN/secrets/retry/DLQ +
conversational + UI) → INDUSTRIAL SECURITY ACCEPT/REJECT;
Architecture Skeptic (runner не лезет в бизнес-семантику; нет второго
runtime; restart-safe; idempotency честная) → ARCHITECTURE ACCEPT/REJECT;
Product Skeptic (English operator без developer). Полная регрессия
(go vet/test, pytest, live, replay/golden, packs, clean-room, failure
injection, backup/restore). Scorecard, 0 BLOCKER, чистое дерево, secret scan,
версия (v1.0.0-rc.3 candidate → final v1.0.0 после гейтов), artifacts.

## Freeze (НЕ строим)
consumer auth, marketplace, billing, mobile, десятки фреймворков, visual
workflow designer, полная мультиязычность, bulk-import UI, multi-tenancy,
новый agent framework, model training, декоративный frontend.

## Гейты
Автономно до финала; HUMAN GATE только: destructive migration / major
incompatible change / weakening security / production credentials / push-tag-
publish / material P1 risk / **OWNER FINAL INDUSTRIAL RELEASE APPROVAL**.
