# THREAT MODEL — adversarial security review (Milestone G)

> Метод миссии: для каждой угрозы — threat / surface / prerequisite / mitigation /
> test / residual risk. Exit: Security Pro → «SECURITY REVIEW: RELEASE CANDIDATE
> ACCEPT / REJECT». Заполняется обзором Security Pro (см. вердикт в конце).

## Поверхности (из миссии Milestone G)

1. Authentication (token/WebAuthn/password/dev-headers)
2. Authorization / ABAC fail-closed
3. Agent privilege boundaries (LLD-20 field-scope, deny, no-approve)
4. Human/agent impersonation
5. MCP exposure (32 tools, bearer→identity, rate-limit)
6. Prompt/tool abuse (perimeter tripwire + ABAC backstop)
7. DSL mutation authority (definition approval = defApprover)
8. Signed diff (definition change = proposal → signature)
9. Approval authority (HITL signatures, TOCTOU, requester-ctx)
10. Journal tampering (SHA-256 chain, checkpoints, PG trigger)
11. Replay (determinism, golden gate)
12. External webhook spoofing (inbound secret, webhook egress whitelist)
13. SSRF (webhook runner allowlist)
14. Connector credentials (secrets Loader, 0600)
15. File uploads (content-addressed blob store)
16. Path traversal (files-dir, blobdisk)
17. Injection boundaries (DSL closed grammar, field whitelist)
18. Multi-user isolation (row-level where, portal)
19. Portal isolation (customer self-registration)
20. Federation trust (bridge, exactly-once keys)
21. Denial of service (rate-limit, MAX_BODY, retry caps, lease TTL)
22. Secret leakage (logs, debug endpoints, journal)
23. Logs (no PII/secret)
24. Debug endpoints (--insecure-dev-auth, healthz/metrics)

## Таблица угроз (заполнена Milestone G-обзором)

| # | Threat | Prerequisite | Mitigation (file:line) | Residual |
|---|---|---|---|---|
| 1 | Token theft/replay | network intercept | TLS (main.go:425); token hash-only (registry.go:136); constant-time compare (registry.go:303) | P2 (нет HSTS) |
| 2 | Эскалация через dev-headers | `--insecure-dev-auth` | off by default + warning (main.go:424/513); devAuth gate (server.go:245) | P1 если флаг в проде — конфиг-дисциплина |
| 3 | ABAC bypass | валидный токен | fail-closed default deny (permissions.go:88); deny>allow (52-61); field-mask (222) | P3 |
| 4 | human/agent impersonation | auth-запрос | self-or-admin setPassword (login.go:41); human-only register/disable | ✅ Closed A2 |
| 5 | MCP tool abuse | валидный токен | те же права, rate-limit 240/мин, engine.can() | P2 |
| 6 | Prompt injection | ingest | трипвайр + flag_ingest + ABAC backstop | P1 KNOWN (LIM-8) |
| 7 | DSL mutation без authority | auth-запрос | defApprover gate + proposal→signature | P3 |
| 8 | Signed diff forgery | auth-запрос | defApprover подпись + atomic под mu | P3 |
| 9 | HITL bypass/TOCTOU | auth-запрос | challenge+headSeq, one-shot, guard re-check (Cycle-003) | P3 |
| 10 | Journal tampering | DB access | SHA-256 chain + PG trigger + checkpoint подписи | P2 |
| 11 | Replay non-determinism | — | канонический JSON + golden replay | P3 |
| 12 | Webhook spoofing (ingress) | сеть | shared secret + worker identity | P2 |
| 13 | SSRF (egress) | auth-воркер | KALITA_EGRESS_DOMAINS fail-closed + no-redirect | P1 KNOWN (LIM-9 IDN/DNS) |
| 14 | Credential leakage | файл/OS | 0600-check Loader, <NAME>_FILE, без логов | P3 |
| 15 | File upload abuse | auth-запрос | content-addressed SHA256, без execution | P2 |
| 16 | Path traversal | auth-запрос | hash-only lookup, нет user-path | P3 |
| 17 | DSL injection/code exec | pack author | closed grammar, data not executable | ✅ Closed |
| 18 | Multi-user leak | auth-токен | row-level where + field mask | P3 |
| 19 | Portal self-registration abuse | open endpoint | staff-only (SEC-5 accepted exception) | P1 ACCEPTED |
| 20 | Federation trust | bridge | exactly-once keys; demo не E2E | P2 (LIM-6) |
| 21 | DoS | auth-запрос | rate-limit, MAX_BODY, retry cap 5, authFail throttle | P2 |
| 22 | Secret leakage в логах | log access | секреты не логируются, только hash | P3 |
| 23 | PII leakage | DB access | sealPII до журнала, tombstone | P2 (SEC-4 AAD) |
| 24 | Debug endpoints | сеть | /healthz liveness, /metrics Prometheus, dev-auth off | P2 |

## Вердикт Milestone G

**SECURITY REVIEW: RELEASE CANDIDATE ACCEPT** (staff-only pilot). Новых P0/P1 нет;
все остаточные P1 — задокументированы как KNOWN LIMITATION / accepted exception
(SEC-2, SEC-5, LIM-9, SEC-4, LIM-6). Pre-pilot требования (не блокеры): SEC-4 (G8 gate),
операторская дисциплина (TLS, dev-auth off, 0600 secrets).
