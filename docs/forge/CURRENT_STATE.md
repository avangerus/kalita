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
- **Поверхности**: 32 MCP-инструмента (контракт-гейт), REST+meta-UI (preact, без сборки), портал.
- **Сценарий владельца**: packs/ap — входящие счета, лестница ≤100к/≤1М/>1М, агент-проверка,
  hand-off «системе Y» (событие+webhook-задача; доставка — kernel-gap D-011).
- **Проверено**: `go test ./... -count=1` — **348 PASS / 12 SKIP / 0 FAIL** (skip: PG локально,
  live-model opt-in); все паки компилируются; live acceptance 19/19 (Cycle-001);
  clean-room PASS на свежем PG (RC_CLEANROOM_LOG.md).

## 3. Что изменилось с прошлого цикла?

- **РЕЛИЗ ОПУБЛИКОВАН: v1.0.0-rc.2** (staff-only pilot). Release baseline: commit
  `359da54`, tag `v1.0.0-rc.2` (main + tag в origin; push одобрен владельцем).
  Артефакты: Docker-образ `kalita:v1.0.0-rc.2` (VERSION=v1.0.0-rc.2) + бинарник
  (sha256 в RELEASE_CANDIDATE.md); version identity проверена (CLI/образ/MCP
  serverInfo). Все RC-гейты зелёные: clean-room PASS (RC_CLEANROOM_LOG.md),
  independent RC Skeptic ACCEPT, Security ACCEPT, 0 BLOCKER, 348 PASS / 12 SKIP /
  0 FAIL, чистое дерево. Пост-релиз: POST_RELEASE.md (known limitations,
  monitoring, rollback, критерии RC→v1.0.0).

## 4. Над чем работают агенты?

| Роль | Модель | Статус |
|---|---|---|
| Ведущий (эта сессия) | deepseek-v4-pro | Cycle-003 завершён: оба reviewers ACCEPT, доки обновлены |
| Chief Architect | deepseek-v4-pro | Cycle-003 план: ACCEPT (11/11); два прогона — расхождение решено ruling reviewers (D-013) |
| Skeptic | deepseek-v4-pro | Cycle-003: ACCEPT-WITH-NOTES → заметки учтены (метрика, документирование) |
| Security reviewer | deepseek-v4-pro | Cycle-003: ACCEPT (TOCTOU закрыт; edge fail-open — кандидат в Next) |
| Cartographer | deepseek-v4-pro* | trace park→decide: no contradictions (cycle-003-trace.md) |

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

**Активная миссия**: довести Kalita до первого пригодного для внешнего пилота релиза
(**release target: v1.0**). Трекинг: RELEASE_BLOCKERS.md, RELEASE_SCORECARD.md.
Milestones A→J по фактам; Portfolio Review каждые 5 циклов.

**Сейчас — HUMAN GATE G4** (ждёт вашего слова: «коммить по плану»). Параллельно идёт
**Milestone A** (kernel readiness): Cycle-A1 аудит → Cycle-A2 identity admin boundary ✅ →
**Cycle-A3 operator trust** (verify --file + TLS-флаги, PROPOSE готов) → A4 LLD-10 →
A5 SEC-2 → exit-review «KERNEL READY FOR PILOT» (skeptic+security).
Полный граф: DEVELOPMENT_GRAPH.md.

## 8. Нужен ли от вас decision?

1. ~~G4 — коммит day-2 слоя~~ → **ВЫПОЛНЕН (5 локальных коммитов: 6385409..43f90c7; push — ваш)**.
2. ~~G2 egress-семантика~~ → **РЕШЕНО владельцем**: node-level KALITA_EGRESS_DOMAINS + audit egress-report; at-least-once + idempotent business effect; Y = reference/mock payment endpoint.
3. ~~SEC-5 scope пилота~~ → **РЕШЕНО**: staff-only; consumer-auth = KNOWN LIMITATION.
4. EVENT-STORE §4 (подписи агентов) — понизить документ (рекомендую) или реализовать.
5. G8 (PII-AAD), G13 (кросс-пак ref) — когда брать (после пилота?).
