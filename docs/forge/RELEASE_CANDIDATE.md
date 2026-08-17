# RELEASE CANDIDATE — v1.0 (pilot, staff-only)

> Собирается Forge по §19 миссии. Финальный шаг — OWNER RELEASE APPROVAL.
> Release target: v1.0 (финализация тега v1.0-rc1, 2026-07-06). Ветка: 6 локальных
> коммитов на HEAD (6385409..b6d7d8b), push — владельцем.

## Что вошло в этот релиз (с rc1)

| Цикл | Изменение | Decision |
|---|---|---|
| Cycle-003 | Decide перепроверяет guard HITL (окно park→decide закрыто в ядре) | D-013 |
| A2 | identity admin boundary: setPassword/revokeCredential self-or-admin, disable admin-only, admin-role invites reserved | D-014 |
| A3 | operator trust: verify --file, TLS-флаги по умолчанию false, Prometheus ca_file | D-015 |
| A4 | registry atomicity: атомарные register/EnsureActor/Redeem + invite-проекция O(1) | D-016 |
| A5 | SEC-2 anti-prompt-injection верифицирован (детектор + flag_ingest) | — |
| F1 | version identity: Dockerfile ARG + Makefile build (-X main.version) | OPS-1 |
| F2 | pytest CI (network-free воркеры) + test_inbound.py | TST-1 |
| B1 | webhook delivery runner: KALITA_EGRESS_DOMAINS fail-closed, at-least-once + idempotent effect, egress-report audit | D-017 |
| B2 | reference-сценарий X→approve→Y с failure-injection (outage/retry/duplicate/restart) | Milestone B |

## Test evidence

- `go vet ./...` чистый; `go test ./... -count=1` — **345 PASS / 12 SKIP / 0 FAIL**.
- GoldenReplay / WorkflowReplay / MCP-контракт / 16 паков + 3 примера — зелёные.
- Python: network-free тесты (inbound/subscribe/detector) в CI после push (G5).
- Performance baseline: Create 6.6µs, Replay-1M 1.30s (budget 5s), PermittedSet-index 317µs.

## Milestones

- ✅ A (Trusted Kernel), ✅ B (Integration), ✅ C (Model completeness core-exit),
  🟢 F (Operability), 🟢 H (Performance), 🟡 I (clean-room чеклист, НЕ прогнан),
  🟢 J (3 сценария dogfood). G (Security) — в обзоре. D/E — осознанно после A–C.

## Known limitations (выпускаем с документацией)

См. RELEASE_BLOCKERS.md KNOWN LIMITATION (LIM-1..LIM-9): on-update-триггеры не из act,
UI без браузерных тестов, O(журнала) супервизорные, PG-прогон в CI, i18n-рендер,
federation-демо не прогнано, findTransition==nil fail-open (лог+метрика),
prompt-injection трипвайр, webhook IDN/DNS-rebinding (pilot-grade). Плюс: consumer-auth
отсутствует (staff-only pilot), EVENT-STORE §4 подписи агентов — open question владельца.

## Security notes

- THREAT_MODEL.md (24 поверхности) — заполняется обзором Milestone G.
- SECURITY.md; DECISIONS D-013..D-017 (HITL/identity/egress доверительные границы).

## Artifact inventory

- Бинарник `kalita` (Dockerfile: golang:1.25-alpine, version через ARG VERSION).
- deploy/ (docker-compose, prometheus, entrypoint, QUICKSTART), docs/runbooks/,
  packs/ (16), examples/ (3), web/ (buildless UI), packages/kalita-ui.
- Secret-модель: KALITA_SECRETS_FILE / <NAME>_FILE / env (0600-checked Loader).

## RC review (запускается после Milestone G ACCEPT)

1. RC Skeptic — доказывает «НЕ готова» (ищет реальные причины отказа).
2. RC Security — отдельная adversarial проверка.
3. RC Clean-room Operator — установка по docs (CLEAN_ROOM_ACCEPTANCE.md).
4. RC Business Acceptance — 3 reference-сценария.

_Финальный статус: в сборке. После Milestone G — заполняется вердикт security и
запускаются RC reviews._
