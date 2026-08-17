# RC CLEAN-ROOM ACCEPTANCE — журнал прогона (v1.0-rc2, 2026-08)

> Полный clean-room в изолированной среде: свежий PostgreSQL-контейнер
> (`docker run -d --name kalita-rc-pg -e POSTGRES_PASSWORD=rcpass -p 55432:5432
> postgres:16`), свежая БД `kalita_rc`, свежая data-dir `tmp/rc-cleanroom/data`,
> релизный бинарник `tmp/rc-cleanroom/bin/kalita.exe` (VERSION=v1.0-rc2).
> Без существующего local state. По завершении контейнер и процессы удалены.

## Шаги (evidence)

| # | Шаг | Команда/факт | Результат |
|---|---|---|---|
| 1 | Install/build | `go build -ldflags="-X main.version=v1.0-rc2"` | ✅ `kalita v1.0-rc2` |
| 2 | Initialize | `docker run postgres:16` + `CREATE DATABASE kalita_rc` | ✅ CREATE DATABASE |
| 3 | Bootstrap admin | `kalita user add --id admin --role Owner` (+ clerk APClerk, mgr Manager, `agent add` checker APChecker) | ✅ 4 токена выданы, hash в журнале |
| 4 | Start | `serve --pack tmp/rc-cleanroom/pack/ap --listen 127.0.0.1:8100 --data-dir … --egress-domains 127.0.0.1` | ✅ /healthz ok, /readyz ready, «pack ap: 3 entities» |
| 5 | Workflow (X→…) | REST: Vendor → VendorInvoice (clerk) | ✅ created, serial AP-2026-00001, status Received |
| 6 | Agent check | MCP `act start_checking` → `submit_check` (APChecker) | ✅ Checking → ApprovalPending |
| 7 | Approval (HITL) | REST `act approve_small` → `pending_approval` + `approval_id`; `POST /api/approvals/{id}/decide {"grant":true}` | ✅ Approved |
| 8 | Handoff к Y | MCP `act send_to_payment` (APChecker) → emits InvoiceSentToPayment | ✅ SentToPayment |
| 9 | Reference integration | webhook-раннер (тик 10s, egress 127.0.0.1) → mock Y (Go, tmp/rc-cleanroom/mocky) | ✅ mock Y получил POST с `idempotency_key=webhook:<taskID>`, entity/record_id/values; «webhook runner: settled 1 task(s)» |
| 10 | Restart | kill serve → `serve` снова (тот же DSN) | ✅ GET invoice → status SentToPayment (реплей из PG) |
| 11 | Persistence/replay | journal: postgresql; 29 событий | ✅ состояние на месте, токены валидны |
| 12 | Backup | `kalita backup --out tmp/rc-cleanroom/backup` | ✅ 29 events + MANIFEST.json |
| 13 | Backup verify | `kalita verify --file tmp/rc-cleanroom/backup` | ✅ «chain ok: 29 events, head seq 29» |
| 14 | Destroy state | `DROP DATABASE kalita_rc WITH (FORCE)` | ✅ |
| 15 | Restore | `CREATE DATABASE` + `kalita backup restore --dir tmp/rc-cleanroom/backup` | ✅ «restore: 29 events loaded» |
| 16 | Re-verify | `serve` снова → GET invoice; `verify --file` | ✅ SentToPayment на месте; chain ok |

## PROBLEMS (всё найдено и зафиксировано)

1. **Окружение (не продукт)**: порты 8095/8096 заняты старыми нодами (existing
   local state) — clean-room перешёл на 8100/8098; порт 18096 попал в
   Windows-excluded port range. На продукт не влияет.
2. **STALE-комментарий packs/ap/ops.dsl** («node-side webhook delivery runner does
   not exist yet») — **ИСПРАВЛЕНО** в этом же прогоне (комментарий теперь описывает
   Cycle-B1/D-017). Пак-код не менялся.
3. WARNING «KALITA_PG_DSN is set from a plain environment variable» — ожидаемое,
   документированное поведение (prod: KALITA_SECRETS_FILE).

## Вердикт

**CLEAN-ROOM PASS.** Все 16 шагов пройдены буквально, без угадывания (единственная
недокументированность — stale-комментарий п.2, исправлен). Изолированная среда
(свежий PG-контейнер + свежая БД + свежая data-dir), по завершении — уничтожена.
