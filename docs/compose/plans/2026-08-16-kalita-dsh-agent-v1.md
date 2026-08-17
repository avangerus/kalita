# Kalita v1.0 — DeepSeek Harness как агентный рантайм (подкапотный слой агентов)

> **For agentic workers:** REQUIRED SUB-SKILL: Use compose:subagent (recommended) or compose:execute to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Сделать DeepSeek Harness (DSH) штатным агентным рантаймом калиты: DSH-агент становится «сотрудником», который через MCP-шлюз калиты берёт задачу из пула, делает работу и сдаёт её человеку за подписью (HITL), при этом каждый шаг журналируется.

**Architecture:** Калита остаётся единственной «дверью» (MCP-шлюз + права + журнал + HITL). DSH — «голова»: агентный цикл (план, тулы, субагенты), подключённый к калите через `@deepseek-ai/dsh-mcp-client` (streamable-http, Bearer). Контракт работника зафиксирован в скилле `kalita-worker`; тонкий супервизор-цикл будит DSH headless по одной задаче за раз. Никакого изменения ядра калиты.

**Tech Stack:** Kalita (Go 1.25, MCP `2025-06-18` streamable HTTP) · DeepSeek Harness (TS, `dsh-mcp-client`, headless profile) · PowerShell/bash супервизор.

## Global Constraints

- Ядро калиты (`internal/`, `packs/`) НЕ меняется в v1 — интеграция живёт в `integrations/dsh/` + `.dsh/skills/` + один тест в `internal/mcp/`.
- Все мутирующие MCP-тулы требуют `basis` и `idempotency_key`; state-поле пишется только через `act`.
- HITL: `act` может вернуть `pending_approval` — агент обязан остановиться, человек подписывает.
- Имена MCP-тулов калиты валидны для DSH: `[A-Za-z0-9_]+` ≤ 64 символов.
- Без `DEEPSEEK_API_KEY` DSH-агент не запустится — машинно-проверяемая гарантия совместимости лежит в Go-тесте (wire-уровень), а не в прогоне LLM.

---

## Task 1.1: DSH интеграция — cordis.yml + kalita-worker skill

**Files:**
- Create: `integrations/dsh/cordis.yml`
- Create: `.dsh/skills/kalita-worker/SKILL.md`

**Interfaces:**
- Consumes: MCP-шлюз калиты `http://<KALITA_URL>/mcp` (Bearer, streamable HTTP, `initialize`/`tools/list`/`tools/call`).
- Produces: DSH patch-оверлей `integrations/dsh/cordis.yml`, применяемый как `dsh ... --patch integrations/dsh/cordis.yml`; скилл `kalita-worker` для модели.

**Шаги:** написать оба файла (содержимое ниже), затем T1.5 верифицирует форму через `dsh --dump-config` (best-effort).

`cordis.yml`:

```yaml
# Kalita agent overlay: mount the kalita node's MCP gateway as DSH tools.
# Apply:  dsh web --patch integrations/dsh/cordis.yml
#     or  dsh --profile headless --patch integrations/dsh/cordis.yml "<task>"
- insert:
    - id: mcp-kalita
      name: '@deepseek-ai/dsh-mcp-client'
      config:
        serverName: kalita
        transport: streamable-http
        url: !!js process.env.KALITA_URL || 'http://127.0.0.1:8080/mcp'
        headers:
          Authorization: !!js '`Bearer ${process.env.KALITA_TOKEN}`'
        failOnStartupError: false
```

`.dsh/skills/kalita-worker/SKILL.md`:

```markdown
---
name: kalita-worker
description: Work as a kalita agent-employee: poll your role's task pool over MCP, take one task, do the work, and complete it. Use whenever you act as a worker inside a kalita node (tools are mcp__kalita__*).
whenToUse: "Acting as a kalita worker: taking tasks from the pool, mutating records, or advancing a workflow on a kalita node."
---

# kalita-worker

You are a kalita agent: an employee with an identity, permissions and an audit trail.
Every action you take goes through the kalita node's MCP tools (`mcp__kalita__*`) and is
journaled. You can never act outside your grants, and you can never sign a human approval.

## The loop

1. `mcp__kalita__wait_for_task` (long poll, up to 55s) or `mcp__kalita__list_my_tasks`.
2. `mcp__kalita__take_task` (exclusive lease with TTL; requires idempotency_key).
3. Understand the work: `mcp__kalita__describe_entity` + `mcp__kalita__get_record`.
4. Do the work with mutating tools only inside your grants:
   `mcp__kalita__update_record`, `mcp__kalita__create_record`, `mcp__kalita__comment`, `mcp__kalita__act`.
5. `mcp__kalita__report_progress`, then `mcp__kalita__complete_task` — or `mcp__kalita__fail_task` honestly on failure.

## Hard rules

- **idempotency_key** on every mutating tool: a stable string derived from the task id + call site (e.g. `uuid5`-like). Re-derive identically on retry; never random per call.
- **basis** on every mutation: `{"type": "task", "id": "<task id>"}`.
- **The workflow state field is written ONLY via `act`**, never via `update_record`.
- **HITL:** if `act` returns `status: "pending_approval"`, STOP. The human signs; you cannot rush it. Report progress and complete the task as "submitted for human approval".
- **Never fabricate.** Write only real values from the record; never placeholders like `[Name]` or invented units/currency.
- **Honest failure:** if you cannot do the work (parse error, no grant, ambiguous), `mcp__kalita__fail_task` with a clear reason — never silently hang the lease.
```

## Task 1.2: Супервизор-цикл

**Files:**
- Create: `integrations/dsh/bin/kalita-dsh-worker.ps1`
- Create: `integrations/dsh/bin/kalita-dsh-worker.sh`

**Interfaces:**
- Consumes: `dsh` на PATH, `KALITA_URL`, `KALITA_TOKEN` (или `KALITA_BOOTSTRAP_SECRET`/`KALITA_WORKER_ID`/`KALITA_WORKER_ROLE` для self-register), `integrations/dsh/cordis.yml`.
- Produces: бесконечный цикл: одна задача = один `dsh --profile headless --patch ... "<prompt>"`.

**Промпт на итерацию (инлайн-контракт, запасной к скиллу):**

```
You are a kalita worker (role from env). Load the kalita-worker skill and follow it.
Poll your kalita task pool ONCE with mcp__kalita__wait_for_task. If a task arrives,
take it, do the work, report and complete/fail it. If the poll times out with no task,
just reply "no task". Never touch anything outside mcp__kalita__* tools.
```

## Task 1.3: Go-тест совместимости MCP↔DSH

**Files:**
- Create: `internal/mcp/dsh_compat_test.go`

**Interfaces:**
- Consumes: существующий тест-харнес `internal/mcp/server_test.go` (in-memory engine/registry), `toolDefs`.
- Produces: `TestDshClientCompat` — доказывает, что последовательность DSH `mcp-client` (`initialize` → `tools/list` → `tools/call` full worker loop) работает поверх калиты.

**Проверяет:**
1. `initialize` возвращает `protocolVersion`, `capabilities.tools`, `serverInfo`, `instructions`.
2. `tools/list` возвращает 31 тул; каждое имя `[A-Za-z0-9_]+` ≤ 64 символов (без хэш-коллизий при DSH-нормализации).
3. Полный wire-цикл работника Bearer-токеном агента: `describe_system` → `wait_for_task`/`list_my_tasks` → `take_task` → `get_record` → `update_record` (basis+idempotency) → `act` → `complete_task`, каждый шаг с `isError=false`.

## Task 1.4: README + документация

**Files:**
- Create: `integrations/dsh/README.md`

**Содержит:** как зарегистрировать агента (`kalita agent add` / bootstrap), запустить ноду (`kalita serve --demo`), экспортировать `KALITA_URL`/`KALITA_TOKEN`, запустить `dsh web --patch ...` (интерактив) и `bin/kalita-dsh-worker` (автономный работник). Плюс границы: что проверено, что требует `DEEPSEEK_API_KEY`.

## Task 1.5: Верификация

- `go build ./...` — зелёный.
- `go test ./internal/mcp/ -run TestDshClientCompat -count=1 -v` — зелёный.
- Best-effort: `npx @deepseek-ai/dsh --version` + `dsh --profile headless --patch integrations/dsh/cordis.yml --dump-config` против живой ноды (без ключа — только composition/MCP-подключение). При неудаче (node < 22.19 / нет сети) — зафиксировать в README.
