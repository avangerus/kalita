# RELEASE SCORECARD — доказательная оценка готовности к пилоту

> RED / YELLOW / GREEN **только с evidence**. GREEN без доказательства запрещён.
> Baseline: после Cycle-003. Release target: v1.0. Обновляется каждый цикл и на
> каждом milestone-выходе (A–J).

| Категория | Статус | Evidence |
|---|---|---|
| Kernel correctness | 🟢 GREEN | **Milestone A exit: skeptic_pro + review_security_pro → KERNEL READY FOR PILOT** (2026-08). 325 PASS / 12 SKIP / 0 FAIL; golden replay; Cycles 003/A2/A3/A4/A5 закрыли P1 (Decide-guard, impersonation, prompt-injection) и P2 (OPS-2/SEC-3/LLD-10). |
| Security | 🟡 YELLOW | WebAuthn/Ed25519, secrets, shredding, lockdown — в коде+тестах. НО: MUST FIX SEC-1..SEC-5 открыты; Milestone G (adversarial threat model) не проводился. |
| Persistence/replay | 🟢 GREEN | TestGoldenReplay (замороженные журналы), TestWorkflowReplay, TestReplayBudget1M (<5с), SHA-256 цепочка VerifyChain; PG-триггер неизменяемости. |
| Permissions | 🟢 GREEN | abac_test, lld20_update_perms_test, deny>allow fail-closed, agent-boundedness dogfood (ap); permitted-set index (ADR-005). |
| HITL | 🟢 GREEN | Cycle-003: guard перепроверяется в Decide (8 регрессий); LLD-02 WebAuthn + offline-verify; D-009 асимметрия зафиксирована; TestDebtorLifecycle. |
| Integration | 🔴 RED | egress-доставка отсутствует (D-011, INT-1); inbound.py без теста; reference-сценарий X→approve→Y с retry/restart не доказан (Milestone B). |
| Packs | 🟢 GREEN | 16 паков + 3 примера компилируются (TestAllPacksCompile); ap live acceptance 19/19 (Cycle-001); finance/servicedesk/crm dogfood. |
| Agent governance | 🟡 YELLOW | Agents = identity-bound (LLD-20 field-scope, deny-блоки, no-approve) — dogfood-тесты. НО: воркеры вне CI (TST-1); EVENT-STORE §4 open question (G14). |
| UI | 🟡 YELLOW | buildless preact UI из meta; static-тесты. НО: без браузерных smoke (LIM-2). |
| Business modeling | 🟢 GREEN | workflow/guards/HITL/computed/агрегаты/UI-нотация выражают домены; H1 лестница; Milestone C «3 системы без Go-кода» — на проверке KERNEL_PRESSURE.md (создать). |
| Business expert | 🔴 RED | Milestone D не начат (осознанно: после A–C). |
| Installability | 🟡 YELLOW | docker-compose + deploy/QUICKSTART. НО: clean-room установка не прогонялась (Milestone I); OPS-3 upgrade. |
| Operations | 🟢 GREEN | backup/anchor/TSA/trace/metrics/egress-report в cmd; graceful shutdown + health/readyz + Prometheus — все с тестами; OPS-1 версия FIXED; OPS-2 verify --file FIXED. KNOWN LIMITATION: structured JSON-логи (сейчас log.Printf), tracing. |
| Documentation | 🟢 GREEN | Cycle-002 doc-truth sync; docs/forge живая карта; MCP-контракт гейтится TestMcpContractMatchesCode. |
| Tests | 🟡 YELLOW | Go-сьюта зелёная (325 PASS). НО: Python/UI вне CI (TST-1, LIM-2); PG-прогон только в CI. |
| Performance | 🟡 YELLOW | TestReplayBudget1M, benchmarks в engine. НО: нет зафиксированных throughput/limits первого релиза (Milestone H). |
| Recovery | 🟡 YELLOW | replay-восстановление доказано; recoverUnclosedOps. НО: backup→restore clean-room не прогонялся. |

## Milestones (A–J) — статус

| Milestone | Статус | Evidence |
|---|---|---|
| A · Trusted Kernel | ✅ **ПРОЙДЕН** | Skeptic + Security «KERNEL READY FOR PILOT» (2026-08); Cycles 003/A2–A5 |
| B · Real Integration Boundary | 🟡 в работе | План: milestone-b-plan.md; ingress-семантика доказана (subscribe/inbound); B3 secrets-модель ✅ верифицирована; B1 webhook-раннер — HUMAN GATE G2 |
| C · Enterprise Model Completeness | 🟢 core-exit PROVEN | 3+ домена end-to-end без Go-кода: ap/finance/servicedesk/crm dogfood-тесты зелёные (internal/mcp/*_dogfood_test.go); kernel-pressure-записи в KERNEL_PRESSURE.md |
| D · Business Expert Layer | 🔴 не начат | после A–C |
| E · Self-Building Experience | 🔴 не начат | — |
| F · Operability | 🟢 в основном готов | graceful shutdown (main.go:666-708 + shutdown_test), /healthz /readyz (healthz_test), /metrics Prometheus (metrics_test), backup/restore/verify (backup_test), version guard + schema_v (safe startup), crash-recovery op-скобки. Остаточные: structured JSON-логи (сейчас log.Printf), tracing (single-node), OPS-3 upgrade publish (gated) |
| G · Security Review (adversarial) | ⏸ перед RC | — |
| H · Performance & Resilience | 🟢 измерено (baseline v1.0) | Create 6.6µs, Get+computed 1.3µs, Query-10k 18.8ms, Auth-10k 44.8µs, PermittedSet-index 317µs (vs full-scan 116ms, ~365x), Replay-1M 1.30s (budget 5s). Limits: single-node, in-memory проекции, один мутекс — вертикальный масштаб, НЕ hyperscale; Query/агрегаты O(n) — ok до десятков тысяч записей (permitted-set index снимает ABAC-скан). |
| I · Release Experience (clean-room) | ⏸ перед RC | — |
| J · Pilot Acceptance (3 сценария) | ⏸ перед RC | — |

## Сводка

- 🟢 7 / 🟡 8 / 🔴 2 (Integration, Business expert).
- **Milestone A (Trusted Kernel) — ПРОЙДЕН**: Skeptic + Security «KERNEL READY FOR PILOT».
- Business expert — осознанно RED (Milestone D после A–C).
- Integration RED — **Milestone B** (G2 webhook-раннер + reference-сценарий X→Y; HUMAN GATE владельца на egress-семантику — см. milestone-b-plan.md).
- До RC обязательны: все MUST FIX (или явные exceptions), Milestones B, G, I, J.
