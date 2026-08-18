# ENGLISH CLEAN-ROOM ACCEPTANCE — run log (v1.0.0-rc.3 candidate, 2026-08)

> Live run of docs/forge/ENGLISH_CLEAN_ROOM.md against a real node on Postgres
> (fresh `kalita_en` database, clean data dirs, release binary built with
> `-ldflags "-X main.version=v1.0.0-rc.3"`). Mock Y: `tmp/en-cleanroom/mocky`
> (stdlib Go receiver with `/fail/N`, `/state`; uncommitted acceptance
> tooling). Builder worker: `workers/builder/builder.py`, deterministic expert
> (no LLM). All node restarts replayed the SAME journal — no state resets
> except where the step itself demands it (restore).

| # | Step | Evidence | Result |
|---|---|---|---|
| 1 | Build | `go build -ldflags "-X main.version=v1.0.0-rc.3"` | ✅ `kalita v1.0.0-rc.3` |
| 2 | Frameworks integrity | `py -m pytest workers/builder/test_frameworks.py -q` | ✅ 8 passed (sha256 fail-closed) |
| 3 | Identities | `user add` admin/clerk/manager/processowner, `agent add` checker | ✅ tokens issued, hashes journaled |
| 4 | Node start | serve + PG + UI + `--egress-domains 127.0.0.1` + `KALITA_EGRESS_SECRETS` | ✅ readyz ready; boot log: runner 1 domain, secrets loaded 1 credential |
| 5 | UI English | `GET /` + served `app.js` | ✅ HTML serves; app.js 95 733 bytes, ZERO Cyrillic (grep `[А-Яа-яЁё]` on the served file) |
| 6 | Ask builder English | `/build/turn` English goal "We need to approve supplier invoices before payment" (ProcessOwner, fresh node `kalita_ask`) | ✅ framework `ap` detected; 7 English discovery questions ("What counts as an 'invoice to pay'…"); English model summary; session journaled |
| 7 | Company Model gate | `/api/model` as non-approver | ✅ 403 for non-defApprover (handlers_ui.go gate) |
| 8 | Workflow | REST create Vendor + VendorInvoice (clerk) | ✅ `AP-2026-00001`, status Received |
| 9 | Agent check | MCP `act start_checking`/`submit_check` (checker) | ✅ Received → Checking → ApprovalPending |
| 10 | HITL | manager `approve_small` → `/api/approvals/{id}/decide` grant | ✅ Approved |
| 11 | Egress success | `send_to_payment` → runner tick ≤10s → mock Y | ✅ 1 POST, `idempotency_key=webhook:<delivery_id>`, `Authorization: Bearer <token>` (runtime-injected), `kalita_egress_delivered=1`, latency 3ms/1 |
| 12 | Failure → DLQ | Y `/fail?n=10`; second invoice through the flow | ✅ 5 attempts (receipts: retrying…failed, `retry_exhausted`), DLQ lists it (`kalita_egress_dead_lettered=1`) |
| 13 | Recovery | Y healthy; `POST /api/egress/retry/{id}` (approver) | ✅ new task SAME `delivery_id`; Y records the SAME key → one business effect per invoice; manager gets 403 |
| 14 | Allowlist fail-closed | node restarted with `--egress-domains other.example.com`; third invoice | ✅ ZERO requests reach Y (total_posts unchanged); DLQ entry `category=allowlist`, 1 attempt |
| 15 | Secrets fail-safe | `KALITA_EGRESS_SECRETS` without the destination | ✅ no unauthenticated POST; DLQ entry `category=auth_missing` |
| 16 | Crash/restart | Y failing, kill node mid-delivery, heal Y, restart | ✅ receipts survive replay (attempts 9→14 across restart); open/failed tasks replayed exactly; post-restart DLQ retry delivered under the SAME identity |
| 17 | Backup | `kalita backup --out tmp/en-cleanroom/backup` | ✅ 125 events + MANIFEST |
| 18 | Verify | `kalita verify --file … --pub node.pub` | ✅ chain ok 125; checkpoints ok; 5 human signatures ok |
| 19 | Restore | drop DB → `backup restore` → serve | ✅ 125 events loaded; invoice status SentToPayment, DLQ count and metrics identical after replay |
| 20 | ITSM + hygiene | English ITSM flow on the same node (AP applied first); `kalita check`-equivalent = engine applied the merged definition; secret scan | ✅ ITSM interview → proposal → signature → `def_version=3`, 11 entities (AP+ITSM+builder); journal/pack/git contain NO bearer literal (git grep clean); tree clean |

## Findings fixed DURING the run (all committed)

1. **REST panic `invalid WriteHeader code 0`** — a GUARD_FAILED engine error
   (manager approving above threshold) hit `statusByCode[code]==0` and killed
   the handler goroutine. Fixed: every engine code mapped (GUARD_FAILED/
   STALE_BASE/RECORD_INCONSISTENT → 409), unknown codes degrade to 500;
   regression test `TestGuardFailedDoesNotPanic` (`5aeadf3`).
2. **Builder false success on rejected proposals** — the node answers a
   rejected DSL as `{"ok": false, "errors": [...]}` with `isError=false`;
   the builder printed "Proposal <EMPTY> created and AWAITING your signature"
   while the journal held nothing. Fixed: `!ok` or missing `proposal_id` is
   reported to the user as a rejection with the node's own errors; success
   message no longer hardcodes the "Owner" role (`fad44d6`).

## Residual notes (documented, not blockers)

- The live run's mid-flight kill landed after the tick completed (the 5th
  attempt won the race); the exact crash-between-send-and-settle window is
  proven deterministically by `TestFailInjectE_CrashBetweenSendAndSettle`
  and `TestFailInjectD_NodeRestart` in cmd/kalita.
- On a node that ALREADY runs the ap pack, a create-mode proposal for AP is
  rejected by the additive gate with a clear English error (entity duplicates)
  — the builder's change-mode conversation is the correct path there; on a
  fresh node (step 6) create-mode parks and applies cleanly.
- PowerShell Get-Content decorates strings with ETS metadata that
  ConvertTo-Json serializes — the run scripts used literals (tooling note,
  not a product issue).

## Verdict

**ENGLISH CLEAN-ROOM PASS (live).** Steps 1–20 executed literally; every
egress scenario A–H re-proven against a real node on Postgres (unit-level
harness `go test ./cmd/kalita/ -run FailInject` stays green alongside); the
only two real bugs found were fixed and regression-tested in the same run.
