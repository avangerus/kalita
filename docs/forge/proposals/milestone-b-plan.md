# Milestone B — REAL INTEGRATION BOUNDARY: план (готовится к старту)

> После Milestone A exit-review. Факты из SYSTEM_TRUTH (Flow E/F/G, D-011) + чтения
> workers/lib/subscribe.py и workers/connectors/inbound.py.

## Что уже есть (доказано, не переписывать)

| Слой | Механизм | Reliability | Evidence |
|---|---|---|---|
| Ingress (connectors) | inbound.py: constant-time shared secret + field-whitelist + create_record через MCP под least-privilege ролью | at-least-once + idem-ключ на POST + retry под тем же ключом | inbound.py:81-99,144-164 |
| Ingress (MCP) | emit_event (грант emit, валидация payload по схеме) | idem-ключ обязателен | tools.go |
| Ingress (bootstrap) | POST /api/bootstrap (shared secret, роли только из allowlist) | — | main.go:526-533 |
| Подписки (egress→node) | subscribe.py: durable курсор save-after-confirm, transient/governed классы, dead-letter subscription.failed | **exactly-once** (at-least-once + детерминированный idem `<sub>|<seq>` + idempotent effect) | subscribe.py:16-41 |
| Федерация | bridge: tail A → emit B, ключи `bridge|<node>|<seq>` | exactly-once | bridge.py |
| Egress-шов (семантика) | transition → emits → event.emitted → outbox read_events | — | engine/events.go |
| **Egress-доставка** | **ОТСУТСТВУЕТ**: webhook/notify = TaskWebhook-намерение, раннера ноды нет | **не определена** | D-011, automation.go:290-294 |

## Работы Milestone B

1. **B1 · G2 webhook-раннер** (HUMAN GATE владельца — см. ниже): runner ноды доставляет
   TaskWebhook: retries c backoff, таймауты, **egress-whitelist доменов**, статус-событие,
   egress-report. Reliability = at-least-once + idempotent effect (паттерн subscribe.py):
   детерминированный idem-ключ доставки + dead-letter + operator recovery.
2. **B2 · reference-сценарий X → Kalita → approval → Y**: System X (inbound) → invoice →
   agent check → approval ladder → TaskWebhook → System Y; переживает: временную ошибку Y,
   retry, дубликат доставки, рестарт Kalita, рестарт агента, таймаут Y — без двойного
   business-эффекта. (Exit criteria Milestone B.)
3. **B3 · Secrets reference model**: подтвердить internal/secrets (Loader) — секретов нет в
   DSL/git/журнале/доках; reference-based (env/файл), не в паках.
4. **B4 · Egress-DLP (P2)**: маскирование бизнес-данных перед облачными LLM-вызовами
   (agent_runner) — шлюз/хук до llm().

## HUMAN GATE для B1 (пакет для владельца)

Проблема: runner ноды впервые открывает исходящий сетевой путь из Kalita. Решения:

- **G2-a · egress-whitelist семантика**: какие домены разрешены и кто их задаёт?
  Варианты: (1) node-level конфиг оператора (KALITA_EGRESS_DOMAINS) — recommended;
  (2) pack-декларация `egress domain:` в DSL (семантика в модели — требует DSL-изменения);
  (3) actor EgressDomains самодекларация (уже есть LLD-09 §3) + egress-report — согласование,
  не enforcement. Рекомендация Forge: (1) enforcement + (3) как audit-слой.
- **G2-b · delivery guarantee**: at-least-once + idempotent effect (рекомендуется, паттерн
  subscribe.py) vs at-most-once (проще, но возможна потеря эффекта — неприемлемо для денег).
- **G2-c · домены первого пилота**: какие реальные системы Y подключаем (для reference-сценария).

Без этих ответов B1 не начинается (trust boundary).
