# Kalita сегодня — текущее состояние

> Первая страница для владельца. Обновляется каждый цикл. Каноническая карта
> системы: SYSTEM_TRUTH.md (проверена Skeptic Pro). Кандидаты работ: DEVELOPMENT_GRAPH.md.

## 1. Что такое Kalita сейчас?

Один Go-бинарник, исполняющий описание бизнес-системы: пак (`packs/*.dsl`) описывает
сущности, workflow, роли/права, HITL-подписи, автоматику, дашборды и UI; движок
исполняет это напрямую; каждое действие — событие в append-only журнале с SHA-256
цепочкой; люди и агенты бьют в один движок. Граница: ядро (DSL+движок+журнал+права)
+ паки + Python-воркеры + notation-UI; агентские harnesses — заменяемый внешний слой.

## 2. Что реально работает?

- **Ядро**: DSL (16 паков + 3 примера компилируются), CRUD, workflow+HITL (Ed25519/WebAuthn),
  ABAC (row/field/dashboard), automation (schedule/stuck/event, retry cap 5), computed,
  undo/rewind, task pool; журнал с реплеем (1M событий < 5с, golden-гейт).
- **Поверхности**: 33 MCP-инструмента (контракт-гейт), REST+meta-UI (preact, без сборки), портал.
- **Сценарий владельца**: packs/ap — входящие счета, лестница ≤100к/≤1М/>1М, агент-проверка,
  hand-off «системе Y» (событие+webhook-задача; доставка — kernel-gap D-011).
- **Проверено**: `go test ./... -count=1` — **348 PASS / 12 SKIP / 0 FAIL** (skip: PG локально,
  live-model opt-in); все паки компилируются; live acceptance 19/19 (Cycle-001);
  clean-room PASS на свежем PG (RC_CLEANROOM_LOG.md).

## 3. Что изменилось с прошлого цикла?

- **Round-9/10**: **Milestone E PASS** — независимый clean-room оператор прошёл
  20/20 шагов + второй домен (вердикт: «да, для основного цикла — без
  разработчика»). Оператор нашёл 2 реальных дефекта — оба исправлены и
  закреплены live-регрессиями: N2 (approval-гранты: act+approve; полный HITL
  live) и N3 (union-база изменений: второе изменение сохраняет опции). F-ревью:
  Architecture Skeptic ACCEPT (6 findings закрыты), Security Pro ACCEPT (нет
  обхода авторизации; контент-хэш в подписи, честный diffPlan, лимит парковок),
  Product Skeptic REJECT→fixes (compose+builder, парольный вход, UX-фиксы) →
  повторный вердикт — сейчас. Rate-limit экономия воркера (TTL-кэши: вызовов
  243→<240 на полный поток). Evidence: PRODUCT_ACCEPTANCE.md.
- **Round-5**: milestone D (shell + error UX + уровни B/O/D).
- **Round-4**: #/ask UI + ITSM generality (live).
- **Round-3**: change разговором live (diff/impact, walker↔engine corpus).
- **Round-2/1**: слайс create live; kernel CompileMulti/get_definition; архитектура.
- **РЕЛИЗ v1.0.0-rc.2** — baseline продукт-миссии.

## 4. Над чем работают агенты?

| Роль | Модель | Статус |
|---|---|---|
| Ведущий (эта сессия) | deepseek-v4-pro | Round-1 продуктовая миссия: архитектура + P0/P1/P2/P3, тесты зелёные |
| Architect Pro | deepseek-v4-pro | Round-1: ACCEPT-WITH-NOTES; findings Round-1 закрываются, финальный вердикт — в arch-review.md |
| Skeptic | deepseek-v4-pro | ранее Cycle-003 ACCEPT-WITH-NOTES; продуктовые ревью — по плану P1–P9 |
| Security reviewer | deepseek-v4-pro | ранее Cycle-003 ACCEPT; P0 (сигнатуры definition) — план ревью security_pro |
| Cartographer | deepseek-v4-pro* | ранее trace park→decide; для продуктовой миссии — по необходимости |

## 5. Какая модель у каждого агента?

См. таблицу выше + AGENT_STATUS.md. Правило: **Pro думает и судит (архитектура, скепсис,
security), Flash исследует и исполняет (чтение, реализация, тесты, документация).**

## 6. Что сломано / не доказано?

- **Главные P1**: webhook-доставка отсутствует (D-011); анти-prompt-injection — только
  трипвайр (SEC-2); consumer-auth нет (SEC-5, gate). (park→decide окно — закрыто Cycle-003;
  setPassword impersonation-класс — закрыт Cycle-A2.)
- **Не доказано тестами**: Python-воркеры вне CI (inbound.py без теста); UI без браузерных
  тестов; PG локально; live-model опт-ин.
- **Доки**: синхронизированы Cycle-002; осталось решение владельца по EVENT-STORE §4
  (подписи агентов — OPEN QUESTION/G14).
- **Риск потери**: 91 dirty-файл в рабочем дереве (весь day-2 слой; ждёт G4).

## 7. Какой цикл следующий?

**Активная миссия (продуктовая)**: self-building enterprise product.
**Достигнут Definition of Done**: A–D ✅, E ✅ (20/20 + ITSM, независимый
оператор), F ✅ (Architecture Skeptic ACCEPT, Security Pro ACCEPT, Product
Skeptic ACCEPT-WITH-NOTES с закрытыми блокерами), 0 BLOCKER. Создан
Industrial Release Candidate: **docs/forge/INDUSTRIAL_RELEASE.md** (walkthrough,
architecture, scorecard, tests, clean-room evidence, limitations, artifacts).
**ОСТАНОВКА: OWNER INDUSTRIAL RELEASE APPROVAL** — решение владельца
(утвердить / доработки / отложить); автопубликации нет. Незакоммиченное
дерево (~40 файлов продукт-миссии) — коммит по решению владельца.

## 8. Нужен ли от вас decision?

1. ~~G4 — коммит day-2 слоя~~ → **ВЫПОЛНЕН (5 локальных коммитов: 6385409..43f90c7; push — ваш)**.
2. ~~G2 egress-семантика~~ → **РЕШЕНО владельцем**: node-level KALITA_EGRESS_DOMAINS + audit egress-report; at-least-once + idempotent business effect; Y = reference/mock payment endpoint.
3. ~~SEC-5 scope пилота~~ → **РЕШЕНО**: staff-only; consumer-auth = KNOWN LIMITATION.
4. EVENT-STORE §4 (подписи агентов) — понизить документ (рекомендую) или реализовать.
5. G8 (PII-AAD), G13 (кросс-пак ref) — когда брать (после пилота?).
