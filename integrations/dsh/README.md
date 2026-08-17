# Kalita × DeepSeek Harness

Делает DeepSeek Harness (`dsh`) штатным агентным рантаймом калиты: DSH-агент
становится «сотрудником» — берёт задачу из пула своей роли через MCP-шлюз,
делает работу и сдаёт её человеку за подписью (HITL). Каждый шаг журналируется.

Архитектура — **«голова / тело»**: DSH свободно *думает* (план, субагенты,
скиллы), но *действует* на предприятии только через `mcp__kalita__*`-тулы —
права, журнал и подписи остаются в калите. Мысль неограничена, руки — только
аудируемые кирпичи.

## Что здесь

| Путь | Что это |
|---|---|
| `cordis.yml` | DSH patch-оверлей: монтирует MCP-шлюз калиты (`@deepseek-ai/dsh-mcp-client`) как тулы `mcp__kalita__*` |
| `../.dsh/skills/kalita-worker/SKILL.md` | Контракт работника: цикл, HITL, idempotency/basis, честный отказ |
| `../.dsh/skills/kalita-author/SKILL.md` | Контракт автора: описание бизнес-результата → пак (сущности/воркфлоу/роли/подписи) → `propose_change` |
| `bin/kalita-dsh-worker.ps1` / `.sh` | Супервизор-цикл: одна задача = один headless-прогон DSH |
| `internal/mcp/dsh_compat_test.go` | Машинная гарантия совместимости MCP↔DSH (без API-ключа) |
| `internal/mcp/author_compose_test.go` | Доказывает цикл автора: JSON-описание → `compose_pack` → подпись → живой пак + HITL |

## Предусловия

- Node ≥ 22.19 (или 24+), `dsh` на PATH: `npm i -g @deepseek-ai/dsh` (или `npx`).
- `DEEPSEEK_API_KEY` — нужен только чтобы модель реально работала; composition
  и подключение к калите работают без ключа.
- Собранная калита: `go build ./cmd/kalita`.

## Быстрый старт

```bash
# 1. нода (in-memory, dev): печатает токен на каждую роль
./kalita serve --pack packs/servicedesk --ui-dir web --demo
#   UI+REST: http://127.0.0.1:8080   MCP: http://127.0.0.1:8080/mcp

# 2. зарегистрировать агента (роль берётся из пака, напр. Engineer/Collector)
./kalita agent add --id eng-1 --role Engineer     # печатает токен

# 3. env
export KALITA_URL=http://127.0.0.1:8080/mcp
export KALITA_TOKEN=<token-из-шага-2>
export DEEPSEEK_API_KEY=...

# 4. интерактив (агент в DSH-сеансе, с калитой на кончиках)
dsh web --patch integrations/dsh/cordis.yml

# 5. автономный работник (берёт задачи из пула сам)
integrations/dsh/bin/kalita-dsh-worker.sh        # или .ps1 на Windows
```

## Как работает worker-цикл

```
wait_for_task (long poll) → take_task (lease) → describe_entity/get_record
  → update_record/comment/act (basis + idempotency_key)
  → report_progress → complete_task | fail_task
```

HITL встроен в модель прав: `act` на переходе `requires approval(Role)` вернёт
`status: pending_approval` — агент обязан остановиться и сдать задачу; подписывает
только человек (в UI калиты). Агент не может ни ускорить подпись, ни выйти за
свои `deny`-правила — это гарантирует ядро, а не промпт.

## Две роли агентов

- **`kalita-author`** — «опиши бизнес-результат → модель строит процесс». Агент
  читает `describe_system`, заземляется на эталонные паки и знания компании,
  собирает пак через `compose_pack` (или `get_grammar`+`validate_dsl`), доводит до
  зелёного и `propose_change` — человек подписывает. Гейты подписей и `deny`-границы
  модель добавляет сама: это «правильный процесс, а не тот что попросили».
- **`kalita-worker`** — сотрудник: берёт задачу из пула, делает работу, сдаёт за подписью.

Обе роли бьют в один MCP-шлюз; ядро (`compose_pack` → `validate` → `propose` → подпись)
уже покрыто тестом `TestAcceptanceAgentBuildsItsWorkplace` + `TestAuthorComposePackLoop`.

## Верификация

- **Машинная (без ключа):** `go test ./internal/mcp/ -run TestDshClientCompat` —
  доказывает, что рукопожатие (`initialize`), список тулов (`tools/list`, имена в
  `[A-Za-z0-9_]{1,64}` + MCP-camelCase `inputSchema`) и полный worker-цикл работают
  поверх wire-протокола, который использует `dsh-mcp-client`.
- **Живое подключение (без ключа) — проверено:** `dsh --profile headless --patch
  integrations/dsh/cordis.yml "<task>"` против живой ноды. При живом `/mcp`
  активация `dsh-mcp-client` (штатный MCP SDK, `streamable-http`) проходит
  `initialize` + `tools/list` и регистрирует 31 тул — единственная ошибка в выводе
  это `MISSING_CREDENTIAL` (нет ключа модели). При мёртвом порту с
  `failOnStartupError: true` активация падает громко с
  `mcp-client(kalita): initial connection or tool synchronization failed` — тот же
  дифференциал доказывает, что подключение реально состоялось.
- **С реальным агентом (нужен `DEEPSEEK_API_KEY`):** запустить п.4/п.5 и убедиться,
  что агент берёт задачу, делает работу и сдаёт её на подпись.

## Границы v1 и следующие шаги

- v1 ограничивает **агентные руки калитой** контрактом (скилл + промпт), а не
  отключением fs/bash-тулов DSH. Важно: данные предприятия недостижимы иначе как
  через `mcp__kalita__*` (права + журнал), поэтому гарантия уже структурная;
  жёсткое сокрытие fs/bash — defense-in-depth. Механизм DSH: `ctx.tools.restrict()`
  в agent preset (`agent.cordis.yml`).
- DSH в developer preview: возможны ломающие изменения формата `cordis.yml`/скиллов.
