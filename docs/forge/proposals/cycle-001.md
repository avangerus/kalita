# Cycle-001 PROPOSE — «Входящие счета как executable model»

> Фаза C цикла Kalita Forge. Диагноз → предложение. Смотр владельца до реализации.

## Проблема

Сценарий-тест владельца («входящие счета из системы X → агент проверяет → пороговые утверждения
→ система Y») исполним сегодня примерно на 30%: есть вся механика (governed workflow, HITL-подписи,
guard-переходы, агент-проверка договора в contracts, ABAC), но отсутствует сам объект — модель
входящих счетов с пороговой лестницей утверждений. Ни один пак не показывает «лестницу сумма→роль».
Это самый наглядный продуктовый пробел: Kalita обязана уметь выражать именно такие процессы.

## Почему это важно (DIAGNOSE)

1. Это главный архитектурный тест продукта (12 шагов идеальной Kalita) — цикл двигает шаги 4–7 и 9–11.
2. Выявляет реальные kernel-границы (H1/H2) дешевле, чем абстрактное проектирование.
3. Даёт владельцу видимый результат: сценарий, который можно показать и пощупать.
4. Классификация: **APPLICATION/PACK** (packs/ap) — ядро не меняется.

## Варианты

- **A. Новый пак `packs/ap`** (рекомендован): Vendor, VendorInvoice, workflow с лестницей, роль APChecker-агента, events наружу. Отдельный домен — по правилу «один пак на домен»; finance остаётся дебиторкой.
- **B. Расширить `packs/finance`** входящими счетами: меньше файлов, но смешивает AR и AP в одном паке — против дисциплины.
- **C. Сначала достроить ядро** (орг-иерархия, first-class пороги): дольше, рискованнее, без продуктового подтверждения, что приём правильный.

Выбран **A**, с фиксацией kernel-гипотез (если DSL чего-то не выразит — записываем gap и возвращаемся к C точечно).

## Ожидаемое изменение архитектуры

- Ядро: **без изменений** (гипотеза H1/H2 проверяются; если guard-лестница или кросс-пак ref не
  выразимы — это находка цикла, фиксируется в DECISIONS, работаем workaround'ом).
- Новый пак: `packs/ap/` (pack.dsl, entities.dsl, workflows.dsl, security.dsl, ops.dsl при необходимости).
- Тесты: dogfood-тест полного цикла в internal/mcp (по образцу TestDogfoodFinancePack).

## Модель (эскиз, уточняется Kernel Architect + kalita-author)

- **Vendor** (поставщик): name, inn, kpp, bank_bic, bank_account — «реквизиты».
- **VendorInvoice**: number, vendor: ref[Vendor], contract: external_id (или ref, если H2), amount: money,
  due_date, status: enum[Received, Checking, ApprovalPending, Approved, Rejected, SentToPayment].
- **Workflow**: Received→Checking (агентом) → ApprovalPending; ApprovalPending→Approved тремя
  guarded-переходами: `approve_under_100k requires approval(LeadManager) when amount <= 100000`,
  `approve_under_1m requires approval(FinanceDirector) when amount <= 1000000`, `approve_above_1m
  requires approval(GeneralDirector) when amount > 1000000` (имена/формы — по грамматике после проверки);
  Approved→SentToPayment (webhook out / task для «системы Y»); Rejected — с комментарием.
- **Роли**: LeadManager, FinanceDirector, GeneralDirector (люди); **APChecker agent** — проверяет
  договор/сумму/реквизиты (комментарии + поля проверки), deny: delete*, act approve*/reject*,
  update status/amount/vendor — подписать не может.
- **Automation**: stuck-эскалация Checking/ApprovalPending; событие `invoice_approved` (для интеграции Y).

## Acceptance criteria

См. ROADMAP.md → N1. Сводно: пак компилируется; лестница работает (каждый уровень паркуется в HITL
соответствующей роли); агент не может подписать; после approve — событие/задача наружу; dogfood-тест;
303 существующих теста зелёные; Skeptic ACCEPT; EXPLAIN + обновление docs/forge.

## Риски и границы

- DSL-грамматика: несколько guarded-переходов между одной парой состояний — проверить поддержку
  (Kernel Architect; при невыразимости — явный gap в DECISIONS, workaround: цепочка промежуточных статусов).
- Cross-pack ref к contracts — вторично; первая версия: external_id + поле «договор проверен агентом».
- «Система Y» — заглушка честная: событие/задача/`webhook out`, не реальный коннектор (он в Next).
- Human gates не затрагиваются (ядра, DSL-философии, security model не трогаем; push не делаем).

## Решение владельца

Нужен только смотр: **принять модель A (рекомендую) / править эскиз / отложить**.
После смотра — IMPLEMENT → VERIFY → REVIEW (Skeptic) → EXPLAIN.

## Итог цикла (заполнено после завершения)

- **Статус: ЗАВЕРШЁН.** IMPLEMENT ✅ VERIFY ✅ REVIEW ✅ (Skeptic REJECT с 2 P1 → исправления → ACCEPT) EXPLAIN ✅.
- Реализовано: `packs/ap/*.dsl` (5 файлов) + `internal/mcp/ap_dogfood_test.go` + `tmp/ap-live-acceptance.ps1`.
- Доказательства: `go vet` чисто; `go test ./... -count=1` — 304 PASS / 0 FAIL; live acceptance 19/19 PASS на реальной ноде (REST+MCP, `serve --demo`).
- Гипотезы: H1 подтверждена (лестница — чистые guarded-переходы, ядро не менялось); H2 отрицательна (кросс-пак ref нет → contract_no + сверка агентом).
- Skeptic P1: (1) park→decide окно закрыто deny-строками на уровне пака, kernel-gap D-010; (2) мёртвый webhook-шов заменён на событие `InvoiceSentToPayment` + реальную TaskWebhook-задачу, доставка — kernel-gap D-011.
- Решения: D-009 (асимметрия reject), D-010, D-011 в DECISIONS.md.
