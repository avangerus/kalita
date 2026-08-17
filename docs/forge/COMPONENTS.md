# COMPONENTS — реестр компонентов Kalita

> Назначение, расположение, интерфейсы, зависимости, инварианты, зрелость, долг.
> Сверено с кодом 2026-07. Легенда зрелости: 🟢 стабильно · 🟡 в развитии · 🔴 черновик.

| Компонент | Назначение | Публичный интерфейс | Invariants (enforced) | Зрелость · Долг |
|---|---|---|---|---|
| **DSL compiler** `internal/dsl` (13/4301 стр, 19 тест-файлов) | Компиляция packs → проверенные определения | Compile, Error{Code,File,Line,Message,FixHint} (E001..E062), mixins `core.*`, mdg-словари, ParseSchedule/ParseAggregate | agent без deny не компилируется; workflow-поле только через переходы; паника-безопасность; один парсер на два слоя | 🟢 frozen · «проверка достижимости состояний» из спеки НЕ реализована; product.mcp compose — данные без исполнителя |
| **Engine** `internal/engine` (30/7909, 39 тест) | CRUD, workflow+HITL, ABAC, automation, computed, dashboards, undo/rewind, task pool | Create/Update/Act, Revert/RewindTo/Diff, MetaFor, Tick, proposals | default deny, deny>allow, fail-closed; basis-гейт; идемпотентность до валидации; op-скобки+recovery; retry cap 5→abandoned; один мьютекс (single-writer) | 🟡 · PR-4b каскад одиночного create/update; дубль лимитов 8 vs 10; retry-контекст не персистится |
| **Eventstore** `internal/eventstore` (9/1278, 10 тест) | Append-only журнал, SHA-256, чекпоинты, эры | Append/All, VerifyChain, Seal/VerifyCheckpoints, EraGuard, Mem/PG-паритет | цепочка SHA-256; PG-триггер запрета UPDATE/DELETE; seq без пропусков; payload TEXT (не JSONB — сохраняет байты хэша) | 🟢 · partitioning «planned»; нет external anchoring |
| **Identity** `internal/identity` (7/1719, 6 тест) | Акторы: токены, пароли, WebAuthn, lockdown | Register/RotateKey/Disable, Authenticate, VerifyApprovalAssertion | tokenIndex O(1) constant-time; auth.throttled — 1 событие/порог/окно; lockdown — только emergency-admin | 🟡 · LLD-10 PR-1/PR-2 НЕ сделаны (атомарный Register, инвайт-проекция); invites full-scan; LLD-18 §2/§3 нет (эпохи ключа) |
| **PII** `internal/pii` (1/151, 1 тест) | Шифрование полей + шреддинг | Seal/Open/Destroy per-subject (XChaCha20-Poly1305) | шифруется ДО журнала; shred-first; хэш над шифртекстом | 🟢 · AAD = только subjectID (LLD-16 обещал {event,field}) — перенос поля между полями субъекта |
| **Secrets** `internal/secrets` (3/124, 1 тест) | Секреты: файл 0600, `_FILE`, env | Load/Resolve | отказ старта при слабых правах; значения не логируются | 🟢 |
| **MCP gateway** `internal/mcp` (4 src/946, 26 тест-файлов) | 32 инструмента агентам, JSON-RPC /mcp | tools/call, authenticate (Bearer→identity), `mcp contract` | контракт-гейт TestMcpContractMatchesCode; мутации требуют idempotency_key; паника-безопасный dispatch | 🟡 · README говорит «31» — устарел |
| **REST API** `internal/api` (14 src/1930, 12 тест) | REST для UI/людей, портал, meta | /api/*, /api/meta, /register, healthz, metrics | CORS/CSP, rate-limit, token/password auth, WebAuthn REST | 🟡 · покрыт внешний контур, внутренние UI-сценарии — нет |
| **Transport** `internal/transport` | Общая обвязка акторов/мутаций REST↔MCP | ActorFromInfo, MutationRequest | parity-тест: байт-идентичные события | 🟢 |
| **UI** `web/` + `packages/kalita-ui` | Notation UI без build-шага (preact) | списки/формы/канбан/дашборды/inbox/agent-board из meta | CSP, import-map, без inline-скриптов (тест) | 🟡 · браузерных тестов нет вовсе (ADR-004: нет JS-runner) |
| **Packs** `packs/` (15, ≈1926 строк DSL) | Продуктовые модули — ядро не знает доменов | см. BUSINESS_TO_RUNTIME и таблицу паков | агентские роли — с deny; SoD-гейты (hr, datapulse); HITL в 11 паках | 🟡 · кросс-пак ref нет; AP/входящие счета нет; орг-иерархии нет |
| **agent_runner** `workers/agent_runner` | Reference-цикл агента: pool→lease→работа→submit | env: KALITA_HANDLER, KALITA_ONCE, KALITA_LLM_URL | lease+идемпотентные ключи (uuid5); честный fail (parse-фейл = fail, не ложный Low); периметр до LLM | 🟡 · нет heartbeat/completion-retry, мульти-роли (ENH-02/04/05) |
| **bridge+subscribe** `workers/bridge`, `workers/lib/subscribe.py` | Федерация: tail→emit, durable-подписки | read_since, Act, DeadLetter | exactly-once ключи `bridge\|<node>\|<seq>`; GovernedError dead-letter'ится, курсор идёт дальше | 🟡 · Go-ядра федерации нет (сознательно не в 1.0) |
| **connectors** `workers/connectors` | Внешний приём: inbound (HTTP+secret+whitelist), excel, exact-match | MCP под least-privilege ролями | constant-time secret; whitelist field_map; create-only MatchGroup; deny-матрицы | 🔴 · inbound.py без теста; только level-1 |
| **perimeter** `workers/perimeter` | Анти-injection трипвайр (не фильтр) | score(text)→(0..1, reasons), THRESHOLD=0.5 | бэкстопы — права+promptbox; таблица правил прозрачна | 🟡 |
| **product_mcp / chat_runner** `workers/product_mcp`, `workers/chat_runner` | Product MCP-фасады; KChat | create/act/query; POST /chat/turn | pass-through под токеном вызывающего (своих credential нет) | 🟡 · фасад есть, product.mcp-блоки только у 2 паков |
| **CLI** `cmd/kalita` | serve, check, agent/user, verify, lockdown, backup/anchor, upgrade, mcp contract, … | подкоманда = файл | verify — офлайн-цепь; lockdown — экстренный тормоз | 🟡 · upgrade без теста; backup/anchor/TSA/metrics — untracked |
| **Deploy** `deploy/`, `docker-compose.yml`, `Dockerfile` | compose-стек: PG+kalita(+qdrant+воркеры) | entrypoint (самоподписанный TLS), bootstrap-secret self-registration | TLS с отказом от plaintext не-loopback | 🟡 · у contracts (beachhead) своего deploy нет |
| **CI** `.github/workflows/ci.yml` | vet→build→error-swallow→test→bench→golden→govulncheck; race | push/PR/nightly | govulncheck — гейт; golden-replay — гейт | 🟡 · Python-воркеры (15 pytest) НЕ гоняются |
| **Тесты** `internal/*_test.go`, `tests/` | 303 PASS/8 SKIP/0 FAIL | golden replay, transport parity, webui static, 11 dogfood | почти каждый тест ссылается на LLD/RELEASE-DoD | 🟢 · PG локально skip (в CI есть); live-model opt-in |
| **Kalita Forge** `${DSH_HOME}/.agent-presets/forge/` + `docs/forge/` | Режим непрерывной эволюции (этот документ) | пресет + скилл kalita-forge; цикл A–H | docs/forge актуальны каждый цикл; Skeptic-гейт; human gates | 🟡 · создан 2026-07, первый цикл — Cycle-001 |

## Кто кого вызывает (основные связи)

- `api` и `mcp` → `transport` → `engine` → `eventstore`; `engine` → `identity` (акторы), `pii` (шифрование).
- `engine` ← реплей ← `eventstore` (старт/рестарт, golden-реплей).
- `workers/*` → MCP-инструменты → тот же движок (никаких отдельных API).
- `cmd/kalita serve` собирает всё в один процесс; `--demo` сеет токены на роль.
- `DSH Forge` → `mcp__kalita__*` (если нода поднята и задан KALITA_TOKEN) или → репозиторий (код/DSL/тесты).
