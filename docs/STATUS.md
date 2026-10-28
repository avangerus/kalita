# Состояние kalita

Обновлено: 2026-10-28. Единственная страница текущего состояния. При изменении состояния её правят,
а не создают рядом новую.

## Версия

| | |
|---|---|
| Последний тег | `v1.0.0-rc.2` (коммит `12fb6f7`, 2026-08-18) — staff-only пилот |
| Текущая версия | ветка `main`. `make build` / `kalita version` берут версию из `git describe`; сборка без версии — `0.1.0-dev` |
| Что добавлено после rc.2 | self-building продукт (Ask Kalita: builder-воркер, `packs/builder`, бизнес-фреймворки, web-оболочка Home/Ask/Company Model/Changes/Integrations), English-first UI/фреймворки/отчёты, долговечная доставка webhook (D-011: DeliveryID, retry, DLQ, receipts, секреты получателей), hardening egress, фиксы по ревью |
| Последняя приёмка | English clean-room на PG, 20 шагов + egress A–H — PASS на `67f3587` (сборка-кандидат `v1.0.0-rc.3`); процедура — [ACCEPTANCE.md](ACCEPTANCE.md), журнал прогона — `git show 67f3587:docs/forge/ENGLISH_CLEANROOM_LOG.md` |
| Следующий тег | `v1.0.0-rc.3` — после зелёного CI и повторного прогона [ACCEPTANCE.md](ACCEPTANCE.md) на финальном коммите |

## Что работает

- **Ядро**: DSL-компилятор (сущности, богатые типы, workflow, ABAC row/field/dashboard, автоматика
  schedule/event/stuck, computed, дашборды, i18n-метки, миксины `include`) с ошибками
  `{code, file:line, fix_hint}`; CRUD, guarded-переходы, HITL-подписи (Ed25519/WebAuthn), undo/rewind,
  task pool с lease, PII-шифрование и crypto-shredding.
- **Журнал**: append-only PostgreSQL, SHA-256 цепочка, иммутабельность на уровне БД, чекпойнты ключом узла,
  replay (1M событий < 5 с — гейт в тестах), golden-гейт совместимости.
- **Поверхности**: REST + 33 MCP-инструмента с паритетом транспортов; UI без сборки; инвайт-портал.
- **Эксплуатация**: `verify`, `backup`/`restore`, `anchor`, `lockdown`, `report`, `egress-report`, shadow-режим,
  `/healthz` `/readyz` `/metrics`, docker compose (PG + узел HTTPS + builder + Prometheus).
- **Интеграции**: webhook-доставка at-least-once с идемпотентным ключом, allowlist fail-closed, DLQ и
  операторский retry; inbound-коннектор, Excel/CSV, federation bridge, KnowVault (RAG).
- **Паки**: 17 продуктовых + 3 примера, все компилируются (`kalita check`).

## Проверки (прогон 2026-10-28, Windows, Go 1.26.10, Python 3.14, PG 16 в docker)

| Проверка | Результат |
|---|---|
| `go vet`, `gofmt`, `go build`, гейт ошибок автоматики | ok |
| `go test ./... -count=1` (с PG) | 759 тестов и подтестов pass, 2 skip, 0 fail |
| `govulncheck` | уязвимостей в вызываемом коде нет |
| `py -m pytest workers` | 248 passed, 1 skipped |
| `kalita check` всех паков | 20 из 20 ok |
| структура плана N1 (`validate.cjs`) | 80 задач `planned`, 0 ошибок |
| не прогонялось локально | `-race` (нужен cgo; гоняется в CI на Linux), passport-бенчи, браузерные тесты UI (их нет) |

CI (`.github/workflows/ci.yml`): test (vet, gofmt, build, гейт, тесты на PG, bench smoke, golden, govulncheck),
race, python workers, ночной passport. Включён 2026-10-28; прогон на `3285908` — все 4 джоба зелёные.

## Известные ограничения (выпускается с ними)

| ID | Ограничение |
|---|---|
| SEC-5 | Нет consumer-auth: портал только для сотрудников (решение владельца, staff-only пилот) |
| LIM-1 | `on update`-триггеры не срабатывают после act-переходов |
| LIM-2 | UI без браузерных тестов |
| LIM-3 | Супервизорные операции O(журнала) (lastMutation и т.п.) |
| LIM-6 | Федерация — уровень воркеров (bridge), демо end-to-end не прогнано |
| LIM-7 | `findTransition == nil` в Decide — legacy fail-open синтетический переход (логируется + метрика, D-013) |
| LIM-8 | Анти-prompt-injection — трипвайр (детектор + `flag_ingest`), не блокирует; бэкстоп — ABAC |
| LIM-9 | Webhook allowlist: IDN/punycode-гомографы и DNS-rebinding не закрыты — держать ASCII-домены |
| OPS-3 | `kalita upgrade` без публикации образа/cosign; апгрейд = пересборка `docker compose up --build` |
| — | Один узел, проекции в памяти, один мьютекс: вертикальный масштаб |

Отложено после релиза: мёртвые kind-константы журнала; эпохи ключа узла (LLD-18 §2); двойной UI-стек
(`packages/kalita-sdk` не используется `web/`); `RotateKey`/`Disable` без общего мьютекса.
Полный реестр долга — [TECHDEBT.md](TECHDEBT.md).

## Дефекты рантайма, известные по коду (вход в N1)

Из [next/AS_IS.md](next/AS_IS.md); исправляются задачами N1, а не точечными правками:

1. `finishTask` проверяет `TakenBy == actor.ID`, но не fence конкретной попытки — поздний ответ того же
   actor после перевыдачи неотличим от нового (KAL-011/013/015).
2. `expireIfDue` игнорирует ошибку `Append` журнала и меняет задачу в памяти (KAL-010/012).
3. `countActorEvents` считает историю actor, а не действия текущей попытки (KAL-015/021).
4. `workers/agent_runner` `run_once` игнорирует ошибки report/complete/fail (KAL-019/020).

## Открытые решения владельца

1. **G14** — EVENT-STORE §4 «агент подписывает каждую мутацию»: понизить документ или реализовать
   (рекомендация: понизить сейчас; подписи сейчас — у approvals, definitions, checkpoints).
2. **Переход RC → 1.0**: пилот ≥ 2 недель на реальном процессе без BLOCKER; egress с реальной системой Y;
   финальный suite зелёный; явное решение о теге `v1.0.0`.
3. **Вход в N1 (gate R0, итерация I00)**: KAL-002 первый покупатель/процесс и baseline, KAL-003 PG против
   Temporal, KAL-004 два источника и профиль развёртывания, KAL-005 утверждение N1 — см.
   [next/delivery/ITERATIONS.md](next/delivery/ITERATIONS.md).

## Где доказательства прошлых релизов

- `v1.0.0-rc.2`: `git show v1.0.0-rc.2:docs/forge/RELEASE_CANDIDATE.md`, `…/RC_CLEANROOM_LOG.md`,
  `…/RELEASE_BLOCKERS.md`, `…/THREAT_MODEL.md`.
- Self-building продукт и English-first: `git show 5447fc5:docs/forge/<файл>` — PRODUCT_ACCEPTANCE,
  INDUSTRIAL_RELEASE, ENGLISH_CLEANROOM_LOG, proposals/.
