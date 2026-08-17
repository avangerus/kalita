# Cycle-003 — Cartographer trace: park → pending approval → Decide (D-010)

> Cartographer Flash (re-scoped run). Read-only trace of the full HITL path feeding
> the guard re-validation fix. Verdict: **no contradictions** with the design
> (docs/forge/proposals/cycle-003.md) or the implementation.

## FROZEN AT PARK (journaled in approval.requested)

- Payload (actCore, workflow.go:168-170): `approvalPayload{Entity, RecordID, Action, From, To, Role}`.
  RequestedBy НЕ в payload.
- Event Actor (workflow.go:172): полный `eventstore.Actor` (Type/ID/Role/Attrs map) — requester.
- Replay (engine.go:312-321): `RequestedBy` восстанавливается из `ev.Actor` → replay-консистентно.

## REVALIDATED AT DECIDE (порядок проверок в Decide, workflow.go:250-394)

basis → lock → approval pending → canApprove → record consistent → политика подписи →
**guard re-validation (D-010, grant-only, $me = RequestedBy, withComputed+ctxFor)** →
журналирование решения → reject-путь (безусловный) → `current == a.From` → применение.

## MUTATION SURFACES park→decide (может ли изменить guard-входы)

| Поверхность | Свои поля | Связанные записи (ref/агрегаты) | Workflow state | Evidence |
|---|---|---|---|---|
| Update (updateCore) | ДА | ДА (resolvePath→lookupAny) | НЕТ (checkWorkflowField отказ) | records.go:429-518, computed.go:427-461 |
| Create/Delete связанных | — | ДА (lookupAny) | — | computed.go:454-461 |
| Revert/RewindTo | ДА | ДА | ДА (revert.go:100-102) | revert.go:26-125 |
| Automation update/event-триггеры | ДА | ДА | только через переходы | records.go:516-517 |
| Erase | ДА (запись исчезает) | ДА (resolve → nil → guard false, fail-closed) | — | computed.go:444-446 |

Вывод: окно закрывается ТОЛЬКО полной ре-оценкой guard в Decide; state-дрейф
дополнительно ловится существующей проверкой `current != a.From`.

## GUARD SURFACE (что может читать `when`)

`$me` / `$me.attr` / `$self` / `$now` / поля / barewords (enum-литералы) / in-списки /
ref-пути до 2 хопов (resolvePath) / computed-поля: арифметика, days_since,
business_*_since, агрегаты count/sum/avg/min/max (fixpoint withComputed).
`$self.attr` не поддерживается (только `$me.attr`).

## DECIDE_CONTEXT (что доступно для ре-оценки)

- actor: `a.RequestedBy` (workflow.go:71, actCore:184, replay engine.go:320) ✓
- now: `e.now()` (computed.go:470) ✓
- resolve: `e.resolvePath` (computed.go:471) ✓
- values: живые `rec.Values` на момент Decide ✓

## REPLAY IMPACT

Failed-decide ничего не журналирует → replay-фолд не меняется; аппрувал остаётся
pending в проекции. TestWorkflowReplay/TestGoldenReplay НЕ вызывают Decide
(golden_replay_test.go:83-85) — детерминизм не затронут. Подтверждено зелёным
полным прогоном.

## CONTRADICTIONS

Нет. Все допущения дизайна подтверждены кодом (7 пунктов, включая Revert как
реальную поверхность мутации полей и workflow-state).
