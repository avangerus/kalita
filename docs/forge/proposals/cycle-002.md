# Cycle-002 PROPOSE — «Doc-truth sync + P2-фиксы» (G3 + G9)

> Фаза 3 цикла Forge. Evidence: SYSTEM_TRUTH.md (Claims vs reality, 23 утверждения)
> и долг-реестр (P2-строки). Классификация: DOCUMENTATION + точечные BUG-фиксы.

## Проблема и важность

Владелец не может доверять документации: 8 STALE + 6 NOT IMPLEMENTED + 2 PARTIAL из 23
проверенных утверждений; часть долга P2 — реальные баги ops-слоя. Понимание владельца —
приоритет №1 (правило Forge: разработка не быстрее понимания).

## Варианты

- A. Только правки доков (docs_flash) — дёшево, но P2-баги остаются.
- B. Доки + точечные P2-фиксы кода с тестами (рекомендован): verify-panic, anchor-консистентность,
  часы в erase.go, комментарии к лимитам 8 vs 10.
- C. Полный doc-overhaul включая LLD/ADR — дольше, риск раздуть цикл.

Выбран B.

## Состав работ

**Доки (docs_flash, 2 параллельных агента):**
- README.md: «31 tools»→32 (генерировать из `mcp contract` нельзя — README не гейтится;
  правим число и добавляем перекрёстную ссылку); счётчики паков (15→16, +ap);
  P1-статус актуализировать.
- SECURITY.md: Known Gaps P1#4–7, P2#8 — пометить ЗАКРЫТО (со ссылками на LLD/тесты).
- V1-GATE.md: пометка «пересмотрено после rc1 2026-07-06» + что закрыто.
- EVENT-STORE-v0.md: §1 jsonb→TEXT (прав код); §5 проекции in-memory (ADR-001); §4 подписи
  агентов — пометка OPEN QUESTION (решение владельца, G14), не резолвим.
- DSL-SPEC-v0.md: §4 reachability/auto-guard conflicts — «не реализовано»; §5 verbs — 13;
  §9 rollback — «not implemented»; §10 integration — «реализован».
- DSL-CORE-LIB.md §4, PLATFORM-STDLIB.md B2/B3/C, TYPE-SYSTEM-V1 geo, PORTAL-VISION
  self-registration, MCP-CONTRACT-v0 (маркер superseded→v1), HLD (React→preact, WASM/stdio
  пометки), ROADMAP.md («10 паков»→16), ROADMAP-TREE актуализация.
- docs/README.md — ссылка на docs/forge/ как карту владельца.
- docs/lld/LLD-10.md, LLD-18.md — пометки «PR-1/PR-2 не реализованы (planned → G6/G7)» с
  указанием целевого цикла (правка Architect Review №1).

**Код (implement_flash):**
- cmd/kalita/main.go:107 — verify: пустой журнал → ошибка вместо паники + тест.
- internal/engine/erase.go:63 — `e.now()` вместо `time.Now().UTC()` (P3).
- backup.go:97-100 vs anchor.go:53-59 — **семантическая нормализация** anchor к sealed-head
  журнала (правка №3: это изменение значения артефакта backup manifest; тест обязан
  утверждать sealed-head-семантику).
- op.go:27 vs workflow.go:460 — зафиксировать D-012 (осознанное решение, поведение не меняем).
- Вне скоупа (зафиксировано): verify PG-vs-jsonl остаётся (P2, отдельный цикл).

## Acceptance criteria

- [ ] go build ./... и go vet ./... чисто; go test ./... -count=1 зелёный (+новые тесты);
- [ ] все 16 паков + 3 примера компилируются;
- [ ] **ноль STALE/PARTIAL** в исправленных доках; **каждое NOT IMPLEMENTED либо помечено в
  доке, либо явно отложено с целевым циклом** (правка Architect Review №2); таблица
  Claims в SYSTEM_TRUTH обновлена;
- [ ] README-счётчики совпадают с фактом (32 тула, 16 паков);
- [ ] Skeptic ACCEPT; SYSTEM_TRUTH/CURRENT_STATE/DECISIONS обновлены.

## Gates / риски

Human gates не затрагиваются (EVENT-STORE §4 только помечается). Риск: правки доков
затрагивают витрину — минимальные диффы, без переписывания стиля. Код-фиксы — 3 файла,
все с регрессионными тестами.

## Reviewers

skeptic_pro (обязательно); review_security_pro — не требуется (не затронуты
permissions/ABAC/HITL/signatures/identity/journal semantics).
