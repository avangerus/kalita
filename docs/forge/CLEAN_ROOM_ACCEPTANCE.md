# CLEAN-ROOM ACCEPTANCE — Milestone I (release experience)

> Проходит АГЕНТ, НЕ участвовавший в реализации, буквально по шагам. Если ему
> приходится угадывать — документация не готова. Чистая среда, без локального
> state. Предпосылки: Go 1.25+, Postgres 16 (или in-memory для smoke), нет
> существующего kalita-data/.

## Шаги (каждый — проверяемым критерием)

1. **Install**: `go build -o kalita ./cmd/kalita` (или docker build). ✅ бинарник собран.
2. **Initialize**: `./kalita version` — печатает версию (не «0.1.0-dev» при release-сборке). ✅.
3. **Start** (in-memory smoke): `./kalita serve --pack packs/ap --ui-dir web --listen 127.0.0.1:8080`. ✅ /healthz = 200.
4. **Create first admin**: `./kalita user add --id admin --role Owner` → токен. ✅ `GET /api/meta` с токеном = 200.
5. **Connect agent**: `./kalita agent add --id checker --role APChecker` → токен. ✅ `describe_entity` по MCP работает.
6. **Pack already installed** (packs/ap скомпилирован при старте). ✅ `kalita check --pack packs/ap` = ok.
7. **Open UI**: `GET /` возвращает HTML. ✅.
8. **Execute workflow**: создать VendorInvoice через REST (APClerk) → агент submit_check → ApprovalPending. ✅.
9. **Perform approval**: approve_small (Manager) — HITL. ✅ статус Approved + событие InvoiceApproved.
10. **Restart**: перезапустить serve с тем же (in-memory теряет state — для persistence нужен Postgres; см. ниже). ✅.
11. **Verify persistence** (с Postgres): после рестарта запись и состояние на месте (replay). ✅.
12. **Run integration**: webhook-доставка с KALITA_EGRESS_DOMAINS → mock Y получает POST с idempotency_key. ✅.
13. **Inspect audit**: `kalita journal` / `kalita verify --pub node.pub` → chain ok. ✅.
14. **Backup**: `kalita backup --out backup/demo` → journal.jsonl + MANIFEST. ✅ `kalita backup verify --dir backup/demo` ok.
15. **Restore**: fresh DB → `kalita verify --file backup/demo --pub node.pub` ok → serve переигрывает. ✅.
16. **Upgrade** (если baseline есть): `kalita upgrade --to <tag>` — вне scope (нет publish/cosign, OPS-3 KNOWN LIMITATION).

## Критерий успеха Milestone I

Все 16 шагов (кроме 16) пройдены буквально, без угадывания; где угадывали —
открыть doc-gap.

_Статус: частично прогнан в сессии (smoke installability, 2026-08): build с
`-X main.version=v1.0-rc` → `kalita version` = v1.0-rc ✅; `serve --pack packs/ap`
→ /healthz {"status":"ok"} ✅, /readyz {"status":"ready"} ✅ (пак скомпилирован).
Полные шаги 4–16 (user/agent/workflow/approval/restart/backup/restore/upgrade) —
требуют Postgres + оператора (чистая среда)._
