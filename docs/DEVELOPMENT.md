# Разработка kalita

Как собрать, запустить и проверить проект. Текущее состояние продукта — [STATUS.md](STATUS.md),
карта документации — [README.md](README.md).

## Окружение

| Что | Версия | Зачем |
|---|---|---|
| Go | `go 1.26.0` + `toolchain go1.26.10` в go.mod (тулчейн подтягивается сам при `GOTOOLCHAIN=auto`) | ядро, CLI, тесты |
| Python | 3.12+ (на Windows — лаунчер `py`), `pytest` | воркеры (`workers/`) |
| Docker | Postgres 16 | журнал на PG, PG-тесты, `docker compose` |
| Node.js | любой LTS | только валидатор плана N1 (`docs/next/delivery/validate.cjs`) |

Локальный `.env` (не коммитится) может задавать `KALITA_TEST_PG_DSN`. Секреты — только через
`KALITA_SECRETS_FILE` / `KALITA_EGRESS_SECRETS`, никогда в репозитории (репозиторий публичный).

## Структура

```
cmd/kalita/        бинарь: serve · check · user/agent · verify · backup · egress · mcp contract …
cmd/goldengen/     генератор golden-фикстур журнала (make golden)
internal/          ядро: dsl · engine · eventstore · identity · api · mcp · pii · secrets · transport · webui
web/               UI без сборки (preact + htm); packages/kalita-ui — атомы/молекулы UI, грузятся из web/
packages/kalita-sdk клиентский SDK для внешних фронтендов (ADR-003)
packs/             продуктовые модули (.dsl); examples/ — приёмочные паки (pangram = все конструкции)
frameworks/        бизнес-фреймворки для builder (ap, crm, itsm; sha256 в manifest.json)
workers/           Python-воркеры: agent_runner, builder (Ask Kalita), connectors, knowvault_*, bridge, …
deploy/            entrypoint, QUICKSTART, prometheus, knowvault, federation-demo
testdata/golden/   замороженные журналы версий — гейт совместимости реплея
tests/             сквозные Go-тесты (golden replay, transport parity, webui static)
docs/              документация (см. docs/README.md)
```

## Сборка и запуск

```bash
go build ./cmd/kalita                                   # dev-сборка (kalita version = 0.1.0-dev)
make build                                              # bin/kalita с версией из git describe
./kalita serve --pack packs/ap --ui-dir web --demo      # журнал в памяти; печатает токен на роль
#   UI + REST: http://127.0.0.1:8080   MCP: http://127.0.0.1:8080/mcp (Authorization: Bearer <token>)
KALITA_PG_DSN=postgres://… ./kalita serve --pack packs/ap --ui-dir web   # реальный журнал
./kalita check --pack packs/servicedesk                 # компиляция пака с агентскими диагностиками
docker compose up --build                               # PG + узел (HTTPS :8443) + builder (:8400) + prometheus
bash scripts/embed-ui.sh                                # один бинарь со встроенным UI (-tags embedui)
```

Эксплуатация узла (backup, verify, upgrade, ротация) — [OPERATIONS.md](OPERATIONS.md) и [runbooks/](runbooks/README.md).

## Тесты

Те же проверки, что в CI ([.github/workflows/ci.yml](../.github/workflows/ci.yml)):

| Проверка | Команда |
|---|---|
| vet + gofmt + build | `go vet ./... && test -z "$(gofmt -l cmd internal tests)" && go build ./...` |
| запрет «проглатывания» ошибок автоматики | `bash scripts/check-automation-error-swallow.sh` |
| все Go-тесты | `go test ./... -count=1 -cover` (= `make test`) |
| PG-журнал (иначе эти тесты skip) | `docker run -d --rm --name kalita-pg -e POSTGRES_PASSWORD=kalita -p 127.0.0.1:55432:5432 postgres:16-alpine`, затем `KALITA_TEST_PG_DSN='postgres://postgres:kalita@127.0.0.1:55432/postgres?sslmode=disable' go test ./internal/eventstore/ -count=1` |
| приёмка релиза (реальный бинарь + PG, ~2 мин) | `KALITA_ACCEPT_PG_DSN='postgres://postgres:kalita@127.0.0.1:55432/postgres?sslmode=disable' go test -tags acceptance ./tests/acceptance -v -count=1` (тот же контейнер PG; база создаётся и удаляется сама) |
| golden-реплей | `go test ./tests/... -run TestGoldenReplay -v` (= `make golden-test`) |
| bench smoke | `go test -bench=Bench -benchtime=50x -run='^$' ./internal/engine` |
| уязвимости | `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` |
| гонки | `go test -race -short -count=1 ./...` (нужен cgo; в CI — на Linux) |
| Python-воркеры | `py -m pytest workers -q` (так же в CI) |
| live-тест builder | `KALITA_LIVE_TEST=1 py -m pytest workers/builder/test_builder_live.py` |
| все паки компилируются | `for p in packs/* examples/*; do ./kalita check --pack $p; done` |
| паспорт производительности | `make passport` (информационно; в CI — ночью) |
| структура плана N1 | `node docs/next/delivery/validate.cjs` |

Ручная приёмка релиза «с нуля» на PG — [ACCEPTANCE.md](ACCEPTANCE.md).

## Генерируемые и замороженные артефакты

- **MCP-контракт**: `docs/MCP-CONTRACT-v1.md` генерируется `kalita mcp contract > docs/MCP-CONTRACT-v1.md`;
  `TestMcpContractMatchesCode` падает, если файл разошёлся с `internal/mcp/tools.go`. Руками не править.
- **Golden-журналы** `testdata/golden/<ver>/` — история; новая версия: `make golden VER=v0.2`.
  Перегенерация существующей (`FORCE=1`) меняет смысл «поведения той версии» — только осознанно.
- **Бизнес-фреймворки** `frameworks/*.json` проверяются по sha256 из `manifest.json` (fail-closed) —
  после правки обновить хеш.

## Правила работы

- Коммиты: `type(scope): что` — `feat`, `fix`, `docs`, `test`, `chore`; scope — область (`egress`, `builder`, `dsl`…).
- Работа идёт в `main`; перед push — зелёные тесты выше. CI должен быть зелёным на каждом push.
- Новое поведение ядра — с тестом; изменение формата журнала — только additive или с LLD-03 (эры, golden).
- Временные файлы, логи, бинарники, выгрузки исследований — вне репозитория (см. `.gitignore`).
- Коммерческие/стратегические документы в этот публичный репозиторий не попадают.
- Изменение документации: одна тема — один документ; устаревшее удаляется (история остаётся в git),
  а не копится рядом с актуальным.
