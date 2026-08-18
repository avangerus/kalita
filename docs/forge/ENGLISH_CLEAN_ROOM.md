# ENGLISH CLEAN-ROOM ACCEPTANCE — Industrial Release (English-first)

> Run by an operator who did NOT build the system, LITERALLY step by step, in a
> clean environment (no existing local state, no prior knowledge assumed).
> Where a step requires guessing, the release is NOT accepted — open a doc gap.
> The node, its UI, the builder worker and the egress destination mock all run
> on this machine; the only external prerequisite is Go 1.25+ and Docker
> (Postgres 16).
>
> Companion live script: the failure-injection scenarios A–H of
> `docs/forge/ENGLISH_RELEASE_PLAN.md` are automated in
> `cmd/kalita/webhooks_failure_injection_test.go` (`go test ./cmd/kalita/
> -run FailInject -v`); steps 10–16 below re-prove the SAME contract against a
> real node on Postgres.

## 0. Prepare the clean environment

```powershell
docker run -d --name kalita-en-pg -e POSTGRES_PASSWORD=cleanroom -p 5445:5432 postgres:16
docker exec kalita-en-pg psql -U postgres -c "CREATE DATABASE kalita_en;"
go build -ldflags "-X main.version=v1.0.0-rc.3" -o tmp\en-cleanroom\bin\kalita.exe .\cmd\kalita
```

Mock destination Y (controlled failures; stdlib only, file
`tmp\en-cleanroom\mocky\main.go` per the log appendix below) and an egress
credentials file:

```powershell
New-Item -ItemType Directory -Force tmp\en-cleanroom\secrets | Out-Null
Set-Content tmp\en-cleanroom\secrets\egress.json '{"127.0.0.1":"Bearer sk_test_en"}'
# on POSIX: chmod 600; on Windows the loader only checks existence
```

## The 20 steps

| # | Step | Command / fact | Acceptance |
|---|---|---|---|
| 1 | **Build** | `go build -ldflags "-X main.version=v1.0.0-rc.3"` | binary builds; `kalita version` prints `kalita v1.0.0-rc.3`, not `0.1.0-dev` |
| 2 | **Frameworks integrity** | `py -m pytest workers/builder/test_frameworks.py -q` | 8 passed — every English framework file matches its manifest sha256 (fail-closed loader) |
| 3 | **Bootstrap identities** | `kalita user add --id admin --role Owner`, `--id clerk --role APClerk`, `--id manager --role Manager`; `kalita agent add --id checker --role APChecker` | 4 tokens issued, only hashes journaled |
| 4 | **Start the node** | `serve --pack packs/ap --ui-dir web --listen 127.0.0.1:8470 --data-dir tmp\en-cleanroom\data --egress-domains 127.0.0.1` with `KALITA_PG_DSN` and `KALITA_EGRESS_SECRETS` set | `/healthz` = ok; `/readyz` = ready; boot log: "webhook runner: 1 allowed egress domain(s)"; "egress secrets: loaded 1 destination credential(s)" |
| 5 | **UI is English** | open `http://127.0.0.1:8470/` in a browser, sign in as admin | login screen, shell nav, Work/Approvals/Agents/Integrations/Company Model/Changes/Ask — every visible string is English; empty states are English ("No approvals pending", not Cyrillic) |
| 6 | **Ask (builder, English)** | start the builder worker (`KALITA_URL=http://127.0.0.1:8470`, no LLM = deterministic expert), open the Ask view, write the goal "I need to approve supplier invoices before payment" | the expert detects the AP framework, interviews in English, records an English DesignSession; no Cyrillic in any expert message |
| 7 | **Company Model** | open Company Model view as admin | entities/roles/fields render with English labels; the approver-only gate holds (manager gets 403 on the full model) |
| 8 | **Execute the workflow** | REST as clerk: create Vendor, VendorInvoice | created, serial, status Received — no approval bypassed |
| 9 | **Agent check** | MCP `act start_checking`, `submit_check` as checker (APChecker) | Checking → ApprovalPending |
| 10 | **HITL approval (English)** | manager approves in the UI Approvals view | Approved; approval journaled with human basis |
| 11 | **Egress success** | mock Y running on 127.0.0.1; wait ≤10s (runner tick) | Y received ONE POST: body carries `idempotency_key=webhook:<delivery_id>`, entity, record_id, values; `Authorization: Bearer sk_test_en` present; node log "settled 1 task(s)"; `GET /metrics` shows `kalita_egress_delivered` = 1, latency sum/count > 0 |
| 12 | **Egress failure → DLQ** | set mock Y to answer 500 for every request; trigger a new webhook | after the retry cap the delivery dead-letters: `kalita egress dlq` lists it; Integrations view shows the DLQ card with attempts and category; `kalita_egress_dead_lettered` = 1 |
| 13 | **Egress recovery** | set mock Y healthy again; `kalita egress retry <task-id>` (or the UI Retry button) | the retry delivers under the SAME `idempotency_key`; Y reports ONE distinct business effect across the original and the retry; `kalita_egress_delivered` grows |
| 14 | **Allowlist fail-closed** | restart the node with `--egress-domains other.example.com`; trigger a webhook | ZERO requests reach Y (its counter does not move); the delivery dead-letters with receipt category `allowlist`; log line "outside KALITA_EGRESS_DOMAINS" |
| 15 | **Secrets fail-safe** | restart the node with the allowlist back but `KALITA_EGRESS_SECRETS` pointing at a file WITHOUT 127.0.0.1; trigger a webhook | NO unauthenticated POST leaves the node; the delivery dead-letters with category `auth_missing` |
| 16 | **Crash/restart safety** | kill `serve` while a delivery is still open (500s), start it again with healthy Y | the open delivery survives the replay and is delivered after restart under the SAME delivery identity; Y collapses duplicates to one business effect |
| 17 | **Backup** | `kalita backup --out tmp\en-cleanroom\backup` | journal.jsonl + MANIFEST.json written |
| 18 | **Backup verify** | `kalita verify --file tmp\en-cleanroom\backup --pub tmp\en-cleanroom\data\node.pub` | `chain ok: N events`; checkpoints ok; human signatures ok |
| 19 | **Restore** | drop the database, recreate it, restore from the backup, start the node again | records, approvals and egress receipts are back (replay); `/readyz` = ready |
| 20 | **ITSM sanity + hygiene** | builder Ask: "We need incidents handled under SLA with a change-approval board" (deterministic expert); then `kalita check --pack` on the generated pack; finally grep the journal and git status | the expert produces a coherent ITSM design in English (sla deadlines, CAB approval) and the pack compiles; the egress bearer token appears NOWHERE in the journal, the DSL, the backup, or `git status` (clean tree, no secrets) |

## Success criterion

All 20 steps pass literally, without guessing. Any step where the operator had
to guess opens a documentation gap and REJECTS the release.

## Appendix — mock Y (tmp\en-cleanroom\mocky\main.go)

A minimal Go HTTP receiver: `POST /hook` records `{body, authorization}` and
answers 200; `GET /fail/N` makes the next N POSTs answer 500; `GET /state`
returns `{distinct_keys, total_posts, keys}`. Distinct keys = business effects;
the whole scenario A–H contract is re-proven against a real node in steps 11–16.
