# Cycle-003 · PROPOSE — Decide перепроверяет guard HITL-перехода (G1 / D-010)

> Forge-фаза 3. Документ для ARCHITECT REVIEW (architect_pro), затем SKEPTIC REVIEW
> (skeptic_pro + review_security_pro). Ничего не реализовано до ACCEPT архитектора.

## Проблема

Guard `when` HITL-перехода проверяется только в момент Act-парковки
(`actCore`, internal/engine/workflow.go:146-153). Decide (workflow.go:250-365)
проверяет лишь `current != a.From` (строка 347-352), но НЕ перепроверяет guard.
Пока подпись висит, guard-релевантные данные могут измениться — и подпись
применится к миру, который больше не удовлетворяет условию перехода.

**Evidence**: D-010 (docs/forge/DECISIONS.md:23-27); SYSTEM_TRUTH.md:113
(«Известный gap: guard при Decide НЕ перепроверяется»); TECHDEBT P1
(SYSTEM_TRUTH.md:245). Пак packs/ap закрывает окно вручную deny-строками
(packs/ap/security.dsl:4,23) — это нагрузка на каждого автора пака и защита,
которую легко забыть (уже повторялась бы в каждом новом HITL-паке).

**Классификация**: KALITA CORE (HITL/approvals — семантика предприятия). Правки
в engine — ядро, НЕ workaround в паках.

## Что замораживается в момент park, что перепроверяется в Decide

| Момент | Данные | Источник |
|---|---|---|
| **FROZEN при park** (журналируется в `approval.requested`) | entity, record_id, action, from, to, role, requested_by (актор-запросивший, включая attrs — eventstore/event.go:28), basis, defVersion, id аппрувала | workflow.go:168-185 |
| **REVALIDATED при Decide** | статус аппрувала = pending; canApprove по текущей модели прав; record существует и consistent; `current == from`; **guard `when` — по текущим данным**; политика подписи | workflow.go:262-319 |

Guard-текст НЕ замораживается: он перечитывается из декларации перехода текущей
модели (`findTransition`, workflow.go:355). Модель управляется подписанными
definition-событиями — «текущая модель правит» (тот же принцип, что у
`defVersion` на событиях). Если определения сменились между park и decide —
правит новый guard.

## Поверхность guard-выражений (TOCTOU-анализ, не только amount)

Guard — выражение грамматики `evalWhere` (internal/engine/expr.go:42-61) на
контексте `withComputed` + `ctxFor` (computed.go:27,464). Операторы-поверхности:

| Что может читать guard | Механизм | Может ли измениться park→decide |
|---|---|---|
| собственные поля записи (`amount`, `vendor`, …) | `c.values` | ДА — Update (records.go updateCore) |
| computed-поля (`days_since`, `business_*`) | withComputed fixpoint | ДА — зависят от данных и часов `e.now()` |
| агрегаты `count/sum/avg/min/max` над связанными записями | evalComputed → scan других записей | ДА — Create/Update связанных записей |
| ref-пути (`project.owner`) | `resolvePath` (hops через lookupAny — читает ТЕКУЩИЕ значения других записей) | ДА — Update связанных записей |
| `$now` | часы движка | ДА — время идёт |
| `$self` | id записи | нет (иммутабелен) |
| `$me` / `$me.attr` | актор | актор Decide ≠ актор Act — см. решение ниже |

Вывод: **перепроверка в Decide должна быть полной ре-оценкой выражения** в том же
машинерии (`withComputed` + `ctxFor` + resolve), а не точечной проверкой полей —
иначе закрыт только `amount`, а `days_since`, агрегаты, ref-пути и `$now` останутся
открытыми.

## Варианты решения

### Вариант A — ре-оценка guard в Decide до журналирования решения (рекомендуется)

В `Decide`, на grant-пути, после всех существующих проверок и ДО
`store.Append(approval.granted)`: ре-оценить `tr.When` на текущем мире; при false
вернуть `GUARD_FAILED` без записи события. Аппрувал остаётся `pending` —
человек может поправить данные и решить заново (подпись не «сжигается»).

- **Цена**: ~15 строк в workflow.go + тесты. Журнал/события не меняются
  (новых kind нет) → replay/детерминизм не затронуты вовсе.
- **Аудит-семантика**: «granted в журнале ⇒ переход применился» (честность
  журнала; неудачные попытки решения не журналируются так же, как не
  журналируются неверные подписи).
- **UX**: GUARD_FAILED с fix_hint «данные изменились, пока висел аппрувал;
  поправьте и решите заново». Аппрувал виден в очереди как pending.

### Вариант B — ре-оценка guard ПОСЛЕ журналирования решения (симметрия с `current != a.From`)

Ре-оценить guard после `store.Append(approval.granted)`, рядом с существующей
проверкой состояния: решение журналируется как granted, но переход НЕ применяется,
аппрувал финализируется.

- **Цена**: та же ~15 строк + тесты; журнал тоже не меняется.
- **Минусы**: (1) решение «сжигается» — повторно решить тот же аппрувал нельзя,
  requester должен Act заново; (2) журнал содержит granted без применённого
  перехода — аудитор не отличит от бага; (3) несогласованность с размещением
  `checkConsistent` (workflow.go:275-281: «a decision on an inconsistent record
  must not be recorded as if it could be applied» — ровно наша логика).

### Вариант C — пак-уровень (workaround, отклонён)

Расширить deny-строки во всех паках. Отклонён: G1 явно требует kernel-фикс
(«защищены ядром для ВСЕХ паков»), workaround уже доказан ненадёжным (паки его
забывают).

## Семантика-вопросы (требуют подтверждения Chief Architect)

1. **Reject-путь НЕ проверяет guard.** Отклонение ничего не применяет —
   запрещать его из-за устаревшего guard бессмысленно и вредно (сказать «нет»
   можно всегда). Guard перепроверяется ТОЛЬКО на grant-пути. (Симметрично
   D-009: reject остаётся безусловным.)
2. **Контекст `$me` при ре-оценке — запросивший актор (`a.RequestedBy`), НЕ
   решающий.** Guard описывает, кому разрешён переход; на park он оценивался
   контекстом requester. Ре-оценка должна отвечать на вопрос «запрос всё ещё
   обоснован» — с тем же актором. RequestedBy восстанавливается из журнала
   (engine.go:320, `ev.Actor` включает attrs — event.go:28) → replay-консистентно.
3. **`$now`/время-зависимые computed ре-оцениваются текущими часами** (`e.now()`).
   Guard с дедлайном (`when $now < …`) должен отказать после дедлайна — это
   желаемое поведение, а не артефакт.
4. **Порядок проверки**: guard — ДО верификации подписи или ПОСЛЕ? Рекомендация:
   после политики подписи (порядок существующих проверок не трогаем; обе должны
   пройти в любом случае). Реализуемо и до — на security-исход не влияет.
5. **`findTransition` вернул nil на decide (переход удалён из DSL)**: сегодня
   fallback-декларация без When (workflow.go:356-358) — переход применяется.
   Варианты: (i) оставить как есть (вне scope цикла; смена DSL — governed
   definition-события); (ii) fail-closed CONFLICT «transition no longer declared».
   Рекомендация: (i) сейчас, зафиксировать как известный edge в D-013.
6. **Размещение существующей проверки `current != a.From`** (после журналирования)
   НЕ трогаем — перестановка меняет журнальные семантики существующих журналов;
   фиксируется как осознанная асимметрия в D-013.

## Ожидаемое изменение архитектуры

- `internal/engine/workflow.go` — Decide: ре-оценка `tr.When` на grant-пути до
  журналирования (вариант A). Никаких новых событий, kind, payload-полей,
  изменений replay-фолда.
- deny-строки packs/ap остаются **defense-in-depth** (по D-010), комментарий в
  security.dsl обновляется: ядро теперь закрывает окно, deny — вторая линия.

## Acceptance criteria

1. Тест: guard по собственному полю — поле изменено между park и decide →
   Decide возвращает `GUARD_FAILED`, аппрувал остаётся pending, повторный Decide
   после возврата данных — применяется.
2. Тест: reject-путь работает при устаревшем guard (reject НЕ блокируется).
3. Тест: guard по ref-пути/агрегату — изменение СВЯЗАННОЙ записи между park и
   decide → GUARD_FAILED.
4. Тест: `$now`/время-зависимый guard — перевод часов после park → GUARD_FAILED.
5. Тест: `$me`-guard — ре-оценка контекстом requester (не approver).
6. Replay: TestWorkflowReplay / TestGoldenReplay зелёные без изменений фикстур;
   журнал с failed-decide (ничего не записано) реплеится идентично.
7. approve с неизменным миром — работает (регрессия существующего HITL-теста).
8. `go build ./...`, `go vet ./...`, `go test ./... -count=1` зелёные; 16 паков +
   3 примера компилируются; dogfood finance/ap — без изменений.

## Риски и blast radius

- **Blast radius**: internal/engine/workflow.go + новый тест-файл
  (или workflow_test.go). Пак-файлы не меняются (кроме комментария).
- **Риск**: medium — горячий путь Decide; семантическая ошибка (например,
  блокировка reject) ломает HITL-сценарии. Митигация: отдельные тесты на
  approve/reject; Pro-ревьюеры.
- **Совместимость журнала**: не затронута (нет новых событий/полей).
- **DSL**: без изменений → **HUMAN GATE НЕ ТРЕБУЕТСЯ** (усиление инварианта).

## Required reviewers

- `skeptic_pro` (correctness, architecture, regressions, vision).
- `review_security_pro` (HITL/approvals/signatures/journal — по списку Forge).

## IMPLEMENT directive (механика, выполняется после ARCHITECT ACCEPT)

Точка вставки — `internal/engine/workflow.go`, `Decide`, сразу после switch
политики подписи (строка ~319) и ДО `payload, _ := json.Marshal(...)` решения:

```go
	// G1 / D-010 (Cycle-003): the guard that justified the request at Act
	// time must STILL hold when the signed decision is about to apply —
	// fields, linked records, computed aggregates and the clock can all
	// change while a signature is pending (TOCTOU window park→decide).
	// Re-evaluated with the same machinery as actCore (withComputed +
	// ctxFor + ref-path resolve) and with the REQUESTER as the $me context:
	// the question is whether the request is still justified, not who is
	// deciding. Checked BEFORE the decision is journaled, for the same
	// reason as checkConsistent above: a decision that cannot apply must
	// not be recorded as if it could. The approval stays pending, so the
	// data can be fixed and the decision retried. The reject path is exempt
	// — a "no" applies nothing and must never be blocked by a stale guard
	// (D-009, reject stays unconditional).
	if grant {
		if rec, ok := e.records[a.Entity][a.RecordID]; ok {
			if wf, ok := e.model.Workflows[a.Entity]; ok {
				if tr := findTransition(wf, a.Action, a.From); tr != nil && tr.When != "" {
					if decl, ok := e.model.Entities[a.Entity]; ok {
						full := e.withComputed(decl, a.RecordID, rec.Values)
						if !evalWhere(tr.When, e.ctxFor(a.RecordID, a.RequestedBy, full)) {
							return nil, &Err{Code: CodeGuardFailed,
								Message: fmt.Sprintf("guard `%s` is false for %s %s: the world changed while the approval was pending", tr.When, a.Entity, a.RecordID),
								FixHint: "restore the data the guard depends on and decide again — the approval remains pending"}
						}
					}
				}
			}
		}
	}
```

Регрессионные тесты — новый файл `internal/engine/decide_guard_test.go` (8 тестов):
1. `TestDecideRevalidatesGuardOwnField` — поле изменено park→decide → GUARD_FAILED,
   аппрувал pending, в журнале нет approval.granted; восстановление данных →
   повторный Decide применяется; повторный Decide → CONFLICT (final).
2. `TestDecideRejectNotBlockedByStaleGuard` — guard устарел, reject проходит,
   переход не применяется, статус аппрувала rejected.
3. `TestDecideRevalidatesRefPathGuard` — guard `when item.amount >= 100` (ref-путь),
   Update связанной записи → GUARD_FAILED.
4. `TestDecideRevalidatesAggregateGuard` — guard `when total >= 100`,
   `total computed = sum(Item.amount where job = $self)`, Update Item → GUARD_FAILED.
5. `TestDecideRevalidatesClockGuard` — guard `when age <= 10`,
   `age computed = days_since(deadline)`, перевод инъекционных часов → GUARD_FAILED.
6. `TestDecideGuardUsesRequesterContext` — guard `when submitted_by = $me`,
   decide чужим актором → применяется (контекст requester, не approver).
7. `TestDecideGuardFailureReplays` — после failed-decide: New(…, store) → аппрувал
   pending, данные на месте; исправление + decide → applied; повторный replay
   идентичен; VerifyChain зелёный.
8. `TestDecideFallbackWhenTransitionRemoved` — переход удалён/переопределён из DSL
   между park и decide → legacy synthetic-переход без guard применяется, edge
   логируется (fail-open осознан, D-013; fail-closed — отдельное gated-решение).

Комментарии (defense-in-depth, D-010): packs/ap/workflows.dsl:8-12,
packs/ap/security.dsl:17-20, internal/mcp/ap_dogfood_test.go:25-28 —
«ядро перепроверяет guard в Decide; deny-строки — вторая линия».

Реализация дополнена логом на `findTransition == nil` в Decide (наблюдаемость
fail-open edge, замечание первого architect-обзора). Расхождение двух
architect-вердиктов (placement до/после журналирования; контекст `$me`) —
на ruling у skeptic_pro/review_security_pro, результат фиксируется в D-013.

## References

- G1: docs/forge/DEVELOPMENT_GRAPH.md:9-16.
- D-010: docs/forge/DECISIONS.md:23-27.
- Code: internal/engine/workflow.go:146-153 (guard в actCore), 250-365 (Decide),
  347-352 (current!=From), 355-358 (findTransition fallback); expr.go:42;
  computed.go:27,464; engine.go:312-329 (replay approvals).
