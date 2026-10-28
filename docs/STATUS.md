# Состояние kalita

Обновлено: 2026-10-28. Единственная страница текущего состояния. При изменении состояния её правят,
а не создают рядом новую.

## Версия

| | |
|---|---|
| Последний тег | `v1.0.0` — первая релизная версия (D-018): [GitHub Releases](https://github.com/avangerus/kalita/releases), образ `ghcr.io/avangerus/kalita:v1.0.0`, изменения — [CHANGELOG.md](../CHANGELOG.md) |
| Текущая версия | ветка `main`. `make build` / `kalita version` берут версию из `git describe`; сборка без версии — `0.1.0-dev` |
| Что вошло в 1.0 после rc.2 | см. [CHANGELOG.md](../CHANGELOG.md) |
| Последняя приёмка | автоматическая `tests/acceptance` (реальный узел + PG) — PASS на каждом rc-теге, в CI на каждом push; ручная часть (браузер, passkey) — [ACCEPTANCE.md](ACCEPTANCE.md) |
| Следующий тег | `v1.0.x` — исправления 1.0 (D-018: нарушение критериев эксплуатации = патч-релиз); новая разработка — N1 (`docs/next`) |

## Путь к 1.0 (пройден)

Скоуп и критерии — [D-018](DECISIONS.md). Каждый этап заканчивается зелёными тестами, CI и rc-тегом.

| Этап | Результат | Состояние |
|---|---|---|
| S1 | Автоматическая приёмка `tests/acceptance` (реальный бинарь + PG) (сборка → роли → workflow → MCP → egress → backup/verify/restore) | готово, `v1.0.0-rc.3` |
| S2 | Корректность рантайма: ошибки журнала не теряются (expire, runner), мьютекс в RotateKey/Disable, DLQ без восстановленных доставок (D-019), LIM-1 и LIM-7 (D-020) | готово, `v1.0.0-rc.4` |
| S3 | Egress: IDN/punycode, IPv6-канонизация и DNS-rebinding (LIM-9, D-021), плоский payload dead-letter подписок | готово, `v1.0.0-rc.5` |
| S4 | События записи через индекс, а не проход по журналу (LIM-3: история, rewind/diff, revert, проверка фактов задачи); неписуемые kind-ы помечены как зарезервированные | готово, `v1.0.0-rc.6` |
| S5 | Release-пайплайн `release.yml`: архивы linux/windows/darwin со встроенным UI, SHA256SUMS, образ GHCR amd64+arm64, подпись cosign keyless; `kalita upgrade` проверяет её (D-022) | готово, `v1.0.0-rc.7` |
| S6 | G14 закрыт в документах (D-023), CHANGELOG, тег `v1.0.0` | готово, `v1.0.0` |

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
| `go test ./... -count=1` (с PG) | pass, 0 fail |
| `go test -tags acceptance ./tests/acceptance` (реальный узел + PG) | PASS, ~100 с |
| `govulncheck` | уязвимостей в вызываемом коде нет |
| `py -m pytest workers` | 251 passed, 1 skipped |
| `kalita check` всех паков | 20 из 20 ok |
| структура плана N1 (`validate.cjs`) | 80 задач `planned`, 0 ошибок |
| не прогонялось локально | `-race` (нужен cgo; гоняется в CI на Linux), passport-бенчи, браузерные тесты UI (их нет) |

CI (`.github/workflows/ci.yml`): test (vet, gofmt, build, гейт, тесты на PG, bench smoke, golden, govulncheck),
race, python workers, ночной passport. Включён 2026-10-28; прогон на `3285908` — все 4 джоба зелёные.

## Известные ограничения (выпускается с ними)

| ID | Ограничение |
|---|---|
| SEC-5 | Нет consumer-auth: портал только для сотрудников (решение владельца, staff-only пилот) |
| LIM-2 | UI без браузерных тестов |
| LIM-6 | Федерация — уровень воркеров (bridge), демо end-to-end не прогнано |
| LIM-8 | Анти-prompt-injection — трипвайр (детектор + `flag_ingest`), не блокирует; бэкстоп — ABAC |
| — | Один узел, проекции в памяти, один мьютекс: вертикальный масштаб |

Отложено после релиза: эпохи ключа узла (LLD-18 §2); двойной UI-стек
(`packages/kalita-sdk` не используется `web/`).
Полный реестр долга — [TECHDEBT.md](TECHDEBT.md).

## Дефекты рантайма, известные по коду (вход в N1)

Из [next/AS_IS.md](next/AS_IS.md). Пункты 2 и 4 исправлены в 1.0 (S2); 1 и 3 — задачи N1:

1. `finishTask` проверяет `TakenBy == actor.ID`, но не fence конкретной попытки — поздний ответ того же
   actor после перевыдачи неотличим от нового (KAL-011/013/015).
2. *Исправлено:* `expireIfDue` игнорировал ошибку `Append` журнала и менял задачу в памяти (KAL-010/012).
3. `countActorEvents` считает историю actor, а не действия текущей попытки (KAL-015/021).
4. *Исправлено:* `workers/agent_runner` `run_once` игнорировал ошибки report/complete/fail (KAL-019/020).

## Открытые решения владельца

Решены: G14 — документ понижен (D-023); переход RC → 1.0 без пилота (D-018).

1. **Вход в N1 (gate R0, итерация I00)**: KAL-002 первый покупатель/процесс и baseline, KAL-003 PG против
   Temporal, KAL-004 два источника и профиль развёртывания, KAL-005 утверждение N1 — см.
   [next/delivery/ITERATIONS.md](next/delivery/ITERATIONS.md).

## Где доказательства прошлых релизов

- `v1.0.0-rc.2`: `git show v1.0.0-rc.2:docs/forge/RELEASE_CANDIDATE.md`, `…/RC_CLEANROOM_LOG.md`,
  `…/RELEASE_BLOCKERS.md`, `…/THREAT_MODEL.md`.
- Self-building продукт и English-first: `git show 5447fc5:docs/forge/<файл>` — PRODUCT_ACCEPTANCE,
  INDUSTRIAL_RELEASE, ENGLISH_CLEANROOM_LOG, proposals/.
