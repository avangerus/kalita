# ROADMAP — Now / Next / Later / Explicitly not building

> Не список фантазий. У каждого Now-пункта — acceptance criteria.
> Пересматривается каждый цикл. Источник фактов: CURRENT_STATE.md, agent/* (бэклог паков), docs/ROADMAP.md.

## Now (активные циклы)

### N1 · Cycle-001 «Входящие счета как executable model» — ВЫПОЛНЯЕТСЯ (IMPLEMENT+VERIFY ✅, REVIEW идёт)
Модель сценария владельца исполняемым паком `packs/ap` (см. proposals/cycle-001.md).
**Acceptance criteria:**
- [x] `packs/ap` компилируется (`./kalita check --pack packs/ap`), все 16 паков + 3 примера не сломаны;
- [x] workflow счёта: приём → проверка агентом → ApprovalPending → Approved/Rejected; после Approved — задача/событие для «системы Y» (send_to_payment + webhook-шов + event InvoiceApproved);
- [x] пороговая лестница: ≤100 000 — руководитель, ≤1 000 000 — финдир, >1 000 000 — гендир (guarded-переходы, каждый паркуется в HITL своей роли; «чужой» диапазон не паркуется);
- [x] агент-роль APChecker: deny-блок, field-scope allow-list (LLD-20), не держит ни одного approve;
- [x] dogfood-тест полного цикла (TestDogfoodAPIncomingInvoice): 3 уровня лестницы + границы 100000/1000000 + tamper-окно + mismatch/reopen + agent-boundedness + emits-факты + webhook-задача + dashboard;
- [x] live acceptance на реальной ноде (serve --demo, REST+MCP, tmp/ap-live-acceptance.ps1): 19/19 PASS;
- [x] полная регрессия зелёная (go test ./... -count=1 — 304 PASS / 0 FAIL), go vet чистый;
- [x] Skeptic-обзор: REJECT (2 P1) → исправления → повторный обзор **ACCEPT**;
- [x] EXPLAIN: обновлены CURRENT_STATE/ROADMAP/AGENT_STATUS/DECISIONS, ответ владельцу «что/зачем/где/как/что увижу/что сломается».

### N2 · Doc-truth sync (после N1)
Привести документы к фактам кода (Skeptic-верифицируемо).
**Acceptance criteria:**
- [ ] README: 32 MCP-инструмента (и любой другой счётчик — из `kalita mcp contract`);
- [ ] SECURITY «Known Gaps»: P1#4–7 помечены закрытыми (WebAuthn/secrets/authfail/lockdown), P2#8 (shredding) тоже;
- [ ] V1-GATE.md: пометка «пересмотрено после rc1 2026-07-06» + что закрыто;
- [ ] EVENT-STORE-v0 §4: либо реализованы подписи агентов над мутациями (gate — trust model), либо явная пометка «не реализовано, решение владельца»;
- [ ] DSL-SPEC §10 `integration` — «реализован»; DSL-CORE-LIB §4 — include/миксины есть;
- [ ] agent/* бэклог: datapulse переехал в код (пересинхронизация).

### N4 · Cycle-003 «Decide перепроверяет guard HITL-перехода» (G1/D-010) — ВЫПОЛНЕН (ACCEPT)
P1-инвариант ядра: окно park→decide закрыто ре-оценкой guard в Decide.
**Acceptance criteria:**
- [x] Decide: ре-оценка `when`-guard на grant-пути ДО журналирования (GUARD_FAILED; аппрувал pending/retryable; reject exempt; `$me` = requester) — workflow.go:321-359;
- [x] полная TOCTOU-поверхность: свои поля, ref-пути, агрегаты, computed, часы — 8 регрессий decide_guard_test.go;
- [x] replay/журнал не тронуты (failed-decide ничего не пишет; GoldenReplay/WorkflowReplay зелёные);
- [x] полный прогон `go test ./... -count=1` зелёный; 16 паков + 3 примера; контракт MCP не менялся;
- [x] reviews: architect_pro ACCEPT; skeptic_pro ACCEPT-WITH-NOTES (учтено: метрика DecideFallbackSynthetic); review_security_pro ACCEPT;
- [x] EXPLAIN: обновлены SYSTEM_TRUTH/CURRENT_STATE/DECISIONS (D-013)/DEVELOPMENT_GRAPH/ROADMAP/AGENT_STATUS; trace cycle-003-trace.md.

### N5 · Заморозка day-2 слоя (gate: решение владельца по объёму) — СЛЕДУЮЩИЙ
91 dirty-файла: backup/anchor/TSA/trace/metrics/pii-erase/dsh-integration/runbooks.
**Acceptance criteria:**
- [ ] инвентаризация всех файлов с классификацией (core/plugin/deploy/docs/артефакт);
- [ ] аудит .gitignore: .env, kalita-data/node.key, tmp/, bin/ не попадут в коммит;
- [ ] серия осмысленных коммитов на main (локально; push — владелец).

## Next (после Now)

- **Kernel: webhook-раннер ноды** (D-011) — доставка TaskWebhook (retries, таймауты, egress-whitelist) — делает интеграционные швы паков живыми.
- **Kernel: fail-closed для findTransition==nil edge в Decide** (D-013) — synthetic-переход без guard после удаления перехода из DSL; сейчас логируется + метрика.
- **Kernel-долг**: LLD-10 PR-1/PR-2 (атомарный Register, инвайт-проекция O(1)) и LLD-18 §2/§3 (эпохи ключа узла, верификация подписей по ключу-на-момент) — требует Kernel-цикла и Skeptic;
- **PII AAD по LLD-16** ({event, field}, не только subject);
- **Кросс-пак связи**: счёт↔договор (ref или явный workaround) — решение в DECISIONS (H2 подтвердила: ref нет);
- **Орг-иерархия** (Department, лестницы утверждений как first-class или канонический приём) — приём уже есть в packs/ap (H1);
- **Слой business frameworks** (дизайн Business Framework Architect: 4 уровня знания, curated-каталог, вопросы владельцу);
- **CI**: pytest воркеров + тест inbound.py; браузерные smoke-тесты UI;
- **Deploy contracts** (beachhead) — product-compose по эталону knowvault.

## Later

- WASM escape hatch (отложен в 1.1), федерация в ядре (по спросу), tiered storage (по порогу из ADR-002),
  marketplace паков, i18n-рендер, calendar-view, payments, коннекторы mail/git/1C, рейтинги агентов/drift.

## Explicitly not building

- **Не превращаем Kalita в agent harness** — model execution, loops, sandbox, skills, subagents — ответственность DSH/аналогичных runtime.
- **Не строим собственный LLM-runtime/провайдеров** — это commodity.
- **Не разрешаем произвольный код внутри модели** — WASM только как последний эвакуационный люк с границами.
- **Не строим синхронный RPC между нодами** (конституционный запрет; федерация — событиями).
- **Не строим generic BI/отчётность** — только дашборды, выводимые из модели с ABAC.
