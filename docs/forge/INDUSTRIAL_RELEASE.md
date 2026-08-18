# INDUSTRIAL RELEASE CANDIDATE — Kalita self-building enterprise product

> Миссия: «Превратить Kalita из промышленного executable enterprise runtime в
> промышленный self-building enterprise product для обычного бизнес-пользователя».
> Статус: **Достигнут Definition of Done** — останавливаемся ТОЛЬКО перед
> **OWNER INDUSTRIAL RELEASE APPROVAL**.

## Что теперь умеет продукт (видеоподобный walkthrough полного сценария)

Владелец открывает Kalita (UI, парольный вход). Дальше — всё разговором:

1. **Home** — «что требует моего внимания»: подписи, изменения модели,
   задачи, провалы агентов, ключевые показатели.
2. **Ask Kalita**: «Создай систему согласования счетов» → эксперт задаёт
   блокирующие/важные вопросы (≤1 за ход, каждый с «зачем спрашиваю»).
3. «да, построй» → **человеческое предложение**: что создаётся, кто участвует,
   процесс, кто что может, что делает агент, где подпись, какие системы (X/Y),
   прогон порогов; provenance «framework ap@1.0.0».
4. «да, предложи» → proposal **parked**; Inbox → «Подписать и применить» →
   подпись (passkey при --require-human-signature) → applied, def_version+1.
5. **Сгенерированное приложение живое**: сущности/формы/дашборды (AP +
   tiles)/роли/гейты — из meta; агентская роль с deny-границами.
6. Работа: счёт 2.5М создан (через onboarding APClerk) → **агент** выполнил
   start_checking (agent-board: completed, журнал: актор агента) → клерк
   submit → approver act (гейт >2M — Owner) → **реальный approval-объект** →
   human decide → Approved → **агент передаёт в Y** (SentToPayment, событие в
   журнале; доставка — disclosed gap D-011).
7. **Изменение разговором**: «Теперь счета выше 2М утверждает собственник» →
   один вопрос → business diff («Было/Станет») + impact по живым записям
   (честно: «не вижу — нет прав») → подпись → applied. Второе изменение —
   работает, опции сохраняются (union-база).
8. **Второй домен**: «Нам нужен service desk с инцидентами и SLA» → тот же
   цикл → Incident/SLAPolicy/Change + SLA-дашборд + CAB-гейты — generality
   доказана, builder не захардкожен.
9. **Оперции**: restart → persistence; audit (Changes: кто/что/когда + сессия);
   backup → verify → fresh DB → restore → replay. Всё — документированными
   командами продукта.

## Architecture (кратко)

Четыре слоя знания (LLM-база / frameworks curated+sha256 / company knowledge
в журнале / executable model) не смешиваются; builder — pass-through актор
без собственных прав; изменения — только propose → human+defApprover+
подпись (каноник с хэшем контента) → additive-гейт → atomic apply; walker —
подмножество, дифференциально прибито к движку (общий корпус). Детали:
PRODUCT_ARCHITECTURE.md, BUSINESS_FRAMEWORKS.md, CONVERSATIONAL_BUILDER.md.

## Scorecard

Все 15 категорий 🟢, кроме Integrations 🟡 (egress-доставка — kernel-gap
D-011, честно раскрыт в продукте). Таблица: INDUSTRIAL_SCORECARD.md.

## Tests

- `go test ./... -count=1` — все пакеты ok (0 FAIL; +hardening: контент-хэш
  подписи, diffPlan-дельты, лимит парковок, multi-pack proposals, walker-корпус).
- pytest workers/builder — 50 + 1 live (live: create + 2×change + ITSM +
  полный HITL + дашборды; враждебная LLM не меняет семантику).
- Контракт-гейт MCP (33 тула) — зелёный; app.js — без innerHTML, syntax OK.

## Clean-room evidence

PRODUCT_ACCEPTANCE.md — 20/20 шагов + второй домен независимым оператором
(только бизнес-цель и URL продукта; без DSL/кода/DB). Вердикт оператора:
«Да — для основного цикла бизнес-пользователь может работать без
разработчика». Найденные дефекты (N2 approval-гранты, N3 union-база) —
исправлены с live-регрессиями.

## Skeptics (миссия F)

- Architecture Skeptic: ACCEPT (границы держатся; 6 findings закрыты).
- Security Pro: ACCEPT (LLM не обходит авторизацию; MAJOR'ы закрыты).
- Product Skeptic: ACCEPT-WITH-NOTES (BLOCKER'ы/MAJOR'ы/R1/R2/R10 закрыты;
  NIT'ы — лимиты пилота).

## Known limitations (честно)

1. Egress-доставка webhook (D-011): намерение журналируется, раннер — gap.
2. Impact-анализ ограничен правами вызывающего (SoD); честный текст.
3. RU-only UI; bulk data import — паттерн inbound-коннекторов, после пилота.
4. crm-фреймворк — knowledge-only (явный отказ вместо угадывания).
5. Один Reasoning-модель провайдер — env-граница, OpenAI-совместимая;
   смена модели не меняет enterprise-семантику (доказано тестом).
6. Браузерный click-through выполнен оператором через HTTP-поверхности
   продукта (те же, что вызывает UI).

## Release artifacts

- Бинарник `kalita.exe` (сборка текущего дерева); Docker: Dockerfile +
  Dockerfile.builder + docker-compose.yml (node + builder + postgres +
  prometheus).
- Пакеты: packs/builder (+ ap/itsm генерируются разговором); frameworks/.
- **Exact commit (Industrial Release Candidate, main, локально — push по
  решению владельца)**:
  - `05a0dc3` kernel: definition-change hardening (content-hash signatures,
    rich diffPlan, caps, CompileMulti, get_definition, ModelMeta)
  - `ca0be9c` product: frameworks + packs/builder + workers/builder (51 pytest
    + live vertical slice)
  - `2b38147` product: web shell + Ask Kalita + deploy (compose/Dockerfile.builder)
  - `1065df8` docs: product-mission artifacts (A–F, acceptance, reviews)
  - baseline: `12fb6f7` (v1.0.0-rc.2, `359da54`)
- Дерево чистое; финальный прогон на этих коммитах: go ./... — все пакеты
  ok (0 FAIL); pytest 50+1 live зелёные; app.js syntax OK.

---

> **OWNER INDUSTRIAL RELEASE APPROVAL** — миссия останавливается здесь до
> решения владельца: (1) утвердить как Industrial Release, (2) список
> доработок перед релизом, (3) отложить. Автопубликации нет.
