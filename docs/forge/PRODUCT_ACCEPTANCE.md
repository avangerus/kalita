# PRODUCT ACCEPTANCE — clean-room evidence (Milestone E)

> Полное чистое испытание продукта НЕЗАВИСИМЫМ Product Operator'ом, который
> не знает DSL и не читал документацию. Среда: свежая БД
> `kalita_cleanroom` (отдельный postgres-контейнер kalita-cleanroom-pg,
> 127.0.0.1:5445), свежий data-dir `tmp/cleanroom/data`, нода
> 127.0.0.1:8470 (packs/builder + web UI), Ask Kalita worker
> 127.0.0.1:8471 (детерминированный режим). Оператору дано ТОЛЬКО:
> бизнес-цель, URL продукта, токен входа, HTTP-читер поверхностей и
> документированные команды backup/restart. Запрещено: DSL-доки, код, прямое
> изменение БД, developer-workaround, незаметные исправления системы.

## Среда (чистота)

| Факт | Значение |
|---|---|
| БД | `kalita_cleanroom` — создана пустой 2026-08-18, отдельный контейнер postgres:16 (порт 5445) |
| Нода | `kalita.exe serve --pack packs/builder --listen 127.0.0.1:8470 --ui-dir web --approver ProcessOwner` |
| Builder worker | `workers/builder/builder.py`, 127.0.0.1:8471, KALITA_LLM_URL не задан (детерминированный режим) |
| Оператор | агент-субпроцесс без доступа к докам/DSL; токен выдан при регистрации `kalita user add --id operator-1 --role ProcessOwner` |
| Репозиторий | НЕ использовался оператором (запрет + проверка отчёта) |

## Чек-лист 20 шагов (заполняется по отчёту оператора)

| # | Шаг | Статус | Evidence |
|---|---|---|---|
| 1 | открыть Kalita | ✅ PASS | GET / → 200, страница с «Ask Kalita»; /api/meta: def 1, pack builder, role ProcessOwner |
| 2 | поговорить с Business Expert | ✅ PASS | POST /build/turn «Создай систему согласования счетов» → сессия, framework ap |
| 3 | ответить на discovery questions | ✅ PASS | 6 ответов (что-счёт/кто-утверждает/пороги/договора/PO/роли) |
| 4 | получить proposal | ✅ PASS | «да, построй» → человеческое резюме: сущности/роли/процесс/гейты/агент/X-Y/D-011 раскрыт |
| 5 | понять proposal | ✅ PASS | оператор пересказал структуру резюме в отчёте |
| 6 | approve model creation | ✅ PASS | propose → parked (pending) → decide → applied; def_version 2 |
| 7 | увидеть generated application | ✅ PASS (с caveat F1) | как APClerk: Vendor/VendorInvoice/PaymentHandoff с формами и workflow; ProcessOwner их не видел (F1) |
| 8 | создать/получить invoice | ✅ PASS | оператор 403 (SoD!) → onboarding APClerk через invites → Vendor «ООО Вектор» + Invoice INV-1001 (2.5M) |
| 9 | увидеть agent work | ✅ PASS | agent-board: start_checking completed (taken_by ap-agent-1); журнал: record.action (агент) Received→Checking |
| 10 | выполнить human approval | ✅ PASS (после фикса N2) | act approve_tier_4 → pending_approval + approval_id; GET /api/approvals — объект существует; decide grant → Approved |
| 11 | увидеть egress | ✅ PASS (намерение) | send_to_payment агентом → SentToPayment; полная цепочка в журнале; доставка в Y — disclosed gap D-011 |
| 12 | изменить approval rule conversationally | ✅ PASS | «Теперь счета выше 2М…» → вопрос → thresholds → change-сессия |
| 13 | увидеть impact analysis | ✅ PASS (честный лимит F5) | diff лестницы + «влияние по живым счетам не посчитано: нет прав» — честно |
| 14 | approve change | ✅ PASS | decide → applied; def_version 3; роли + Owner |
| 15 | проверить новый маршрут | ✅ PASS | meta: approve_tier_4 (requires_approval) — 1М–2М CEO, >2М Owner |
| 16 | restart | ✅ PASS | оператор сам остановил/поднял ноду; healthz 200 |
| 17 | проверить persistence | ✅ PASS | def_version 3, INV-1001 цел, токен валиден; + повторная проверка после platform-refresh (def 4) |
| 18 | посмотреть audit | ✅ PASS (F2) | 3 applied proposal, инициатор operator-1/ProcessOwner; журнал счёта: record.created → task.created (workflow-engine) |
| 19 | backup | ✅ PASS | backup → 72 события + sha256 + anchor; verify → «chain ok: 72 events, head seq 72» |
| 20 | restore | ✅ PASS | fresh DB → backup restore → «72 events loaded» → verify «chain ok» → нода переигрывает (данные целы, верна снапшоту) |

## Второй домен (ITSM)

| Шаг | Статус | Evidence |
|---|---|---|
| framework → conversation → model | ✅ PASS | «Нам нужен service desk с инцидентами и SLA» → itsm; 5 ответов; модель SLAPolicy/Incident/ServiceRequest/Change/Knowledge |
| UI | ✅ PASS | meta: Incident/SLAPolicy/Change с ролями и гейтами |
| incident | ✅ PASS | EndUser onboarded → Incident «не работает почта» (P3, Portal) создан |
| agent/human | ✅ PASS | задача «take» для ServiceDeskAgent в agent-board (workforce ITSM — platform, аналогично AP) |
| SLA | ✅ PASS | P1–P4 политики подтверждены в резюме |
| approval/change | ✅ PASS | гейты: close/approve_request → Supervisor; approve_change → ChangeManager (requires_approval в meta) |

## Product findings (оператор)

- **F1 — нет полного представления модели у подписанта** → **FIXED Round-9**: `GET /api/model` (approver-gated полная структура: сущности/роли/гейты без прав на записи); Company Model UI использует его.
- **F2 — у proposals нет времени** → **FIXED Round-9**: `created_at`/`decided_at` из journal TS; ChangesView показывает.
- **F3 — статусы сессий не синхронизируются** → **FIXED Round-9**: `_sync_sessions` на каждом ходу (applied/rejected отражаются в панели).
- **F4 — нет AI-атрибуции** → **FIXED Round-9**: description = «… (designed by Business Expert, framework ap@1.0.0)».
- **F5 — impact-анализ не видит живые счета (у designer нет read)** → **KNOWN LIMITATION (принятая честность)**: «если невозможно определить — говорим»; права остаются разделёнными (designer ≠ читатель записей).
- **F6 — egress-доставка gap (D-011)** → **KNOWN LIMITATION**: честно раскрыт в EXPLAIN/Integrations; доставка — kernel-gap.
- **F7 — заметки оператора (не баги продукта)**: Cyrillic UTF-8 транспорта (PowerShell-клиент), curl `--data-binary @file`.

## Итог оператора (независимый вердикт)

> **«Да — для основного цикла согласования счетов обычный бизнес-пользователь
> может запустить продукт end-to-end без разработчика»**: описал → подписал →
> ввёл данные → агент проверил → клерк подал → approver запросил+решил → агент
> передал → всё в журнале → backup/verify/restore работают только через
> поверхности продукта. F1–F4 исправлены и перепроверены оператором.

Дефекты, найденные оператором и исправленные (Round-9/10):
- **N2** (approval-гейт не создавал объекты — отсутствовал act-грант) → FIXED + live-регрессия полного HITL.
- **N3** (change-перегенерация теряла опциональные элементы: Contract) → FIXED (union всех applied-сессий вместо последней) + live: второе изменение применяется, Contract сохраняется (def 4).
- **N1** (auth после platform-refresh) — платформенная ошибка запуска без PG DSN, не дефект продукта.

Оставшиеся честные ограничения: egress-доставка (D-011, kernel-gap, открыто раскрыт), impact по правам вызывающего (SoD), RU-only.

## PERFORMANCE (миссия: измерь, не оптимизируй premature)

Прогон `tmp/measure_builder.py` (эпhemeral-нода, детерминированный режим,
один процесс; Round-7):

| Метрика | Значение |
|---|---|
| UNDERSTAND (первый ход) | 5.4 ms |
| DISCOVER (ход, 6 тем) | 5.4 ms/ход |
| MODEL+VALIDATE+EXPLAIN+SIMULATE | 9.6–28.3 ms |
| APPROVE (propose_change + merge) | 10.4–50.7 ms |
| CHANGE: understand/discover/model+diff+impact | 0.7 / 4.4 / 3.5 ms |
| Design-session storage (create-поток) | 29 governed-записей в журнале |

Латентность без LLM ~5–50 ms на ход (сеть loopback); с reasoning-моделью
добавится её inference — enterprise-семантика от этого не зависит (валидация
и применение — движок). Оптимизация не требуется.

## Известные ограничения (честно)

- Egress-доставка webhook (D-011): намерение журналируется, раннер — kernel-gap; статус открыто показан в продукте.
- Impact-анализ ограничен правами вызывающего (SoD по дизайну; честный текст в отчёте).
- Детерминированный режим Expert (без LLM): разговор корректен, формулировки шаблонные; с LLM-провайдером формулировки лучше, семантика не меняется (test_llm_adapter.py: враждебная модель не меняет модель).
- Browser click-through: оператор-агент работал через HTTP-поверхности продукта (те же, что вызывает UI); живой браузерный прогон — при желании владельца.
