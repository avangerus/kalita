# Cycle-003 — ARCHITECT VERDICT (architect_pro)

> **Provenance note (ведущий)**: этот файл — вердикт ПЕРВОГО запуска architect_pro,
> записанный на диск в момент прерывания (interrupt) долгого прогона; финальная
> строка не была доставлена в сессию. Управляющий вердикт цикла — второй,
> foreground-запуск architect_pro: **ACCEPT** (все 11 пунктов AGREE,
> см. cycle-003.md, IMPLEMENT directive реализован 1:1). Расхождения двух
> вердиктов (пункты C и E ниже — контекст `$me` и placement до/после
> журналирования) переданы на ruling skeptic_pro/review_security_pro; результат
> фиксируется в D-013 (DECISIONS.md). Замечания G (логирование fail-open edge) и
> K (fallback-тест) — учтены в реализации (лог в workflow.go + тест
> TestDecideFallbackWhenTransitionRemoved).

---

## A. Freeze-at-park / Revalidate-at-decide split semantics

**AGREE.**

The split is architecturally correct. Park-time data (entity, record_id, action, from, to, role, requested_by, defVersion, approval_id) is immutable by nature — it's the "snapshot at request time." Revalidation-time data (approval status, canApprove, record existence, `current == from`, guard `when`, signature policy) is inherently live.

**Code-grounded:** `actCore` (workflow.go:168-185) journals `approval.requested` with the frozen payload. `Decide` (workflow.go:262-319) checks the live state. The table in the proposal accurately maps to these two code regions.

**No correction needed.**

---

## B. Full re-evaluation (withComputed + ctxFor + resolve) as TOCTOU closure

**AGREE.**

Partial field-level checks would leave computed fields (`days_since`, `business_*`), aggregates (`count/sum/avg/min/max`), ref-paths (`project.owner`), and `$now` open to TOCTOU exploitation. Full re-evaluation via the same machinery (`withComputed` fixpoint → `ctxFor` → `evalWhere`) is the only correct approach.

**Code-grounded:** `withComputed` (computed.go:27-61) evaluates to fixpoint; `ctxFor` (computed.go:464-473) injects `e.now()` and `resolve` closure; `evalWhere` (expr.go:42-61) is the unified evaluator. This is the same path used in `actCore` (workflow.go:147-148).

**No correction needed.**

---

## C. Actor context for $me — requester vs approver

**DISAGREE — needs correction.**

The proposal states that `$me` should resolve to the **approve actor** (the person calling Decide), not the requester. While this is a defensible semantic choice (the guard should reflect the *current* world as seen by the *current* actor), it creates a subtle semantic shift that the proposal understates:

> **Problem:** A guard written as `$me.attr == "finance"` at park-time checked the *requester's* attributes. After this change, the same guard checks the *approver's* attributes. This is not a bug in the general case (guards describe conditions on the world, not on a specific person), but it IS a semantic change that existing DSL packs may not anticipate.

**Correction required:**
1. The proposal MUST document this semantic shift explicitly in the "What changes" section.
2. The `approvalPayload` in `approval.requested` already stores `requested_by` (workflow.go:168-185, eventstore/event.go:28). If any existing pack relies on `$me` meaning "the person who requested this," the guard should optionally expose `$requester` as a Decide-time context override.
3. **Recommendation:** For Cycle-003, accept the current design (approver context) but add a TODO in the code: `// TODO: expose $requester for backward-compat with packs that used $me at park-time`.

---

## D. Reject path guard handling

**AGREE.**

No guard re-evaluation needed on the reject path. Rejecting a transition does not modify the record — it leaves the record in its current state. The guard is a *permission to apply* a transition, not a condition on the right to refuse.

**Code-grounded:** `Decide` reject path (workflow.go:337-339) returns early after setting `a.Status = ApprovalRejectedStatus`. No record mutation, no guard needed.

**No correction needed.**

---

## E. Guard check placement (pre-journal vs post-journal)

**AGREE.**

The guard re-evaluation MUST occur **after** journaling `approval.granted` but **before** `applyTransitionAndEmit`. This is the correct placement because:

1. **Auditability:** The approval decision should be recorded regardless of guard status. A guard failure is not a rejection of the decision — it's a "decision cannot be applied to current world."
2. **Idempotency:** The approval stays `pending`, allowing the human to retry Decide after fixing data.
3. **Safety:** If guard check were *before* journaling, a guard failure would leave no record of the human's decision, making audit trails incomplete.

**Code-grounded:** Current `Decide` flow (workflow.go:321-335 journals `approval.granted`; 337-339 reject path; 341-364 grant path). The guard check should be inserted between line 341 (`a.Status = ApprovalGrantedStatus`) and line 359 (`applyTransitionAndEmit`).

**No correction needed.**

---

## F. Ordering relative to signature-policy verification

**AGREE.**

Signature verification (lines 302-319) should remain **before** guard re-evaluation. The ordering is correct because:

1. A verified signature is the authoritative proof that a human made a decision. If the guard fails *after* signature verification, the human still made a valid decision — it just can't be applied.
2. If guard check were before signature verification, a guard failure could prevent signature verification, making it unclear whether the human decided or the system rejected.

**Code-grounded:** Signature policy switch (workflow.go:302-319) → journal (321-335) → guard re-eval (new) → `current != a.From` (348-352) → apply (359-362).

**No correction needed.**

---

## G. findTransition==nil handling (fallback vs fail-closed)

**DISAGREE — needs correction.**

The proposal does not address the `findTransition == nil` case in `Decide` (workflow.go:355-358):

```go
tr := findTransition(e.model.Workflows[a.Entity], a.Action, current)
if tr == nil {
    tr = &dsl.TransitionDecl{From: a.From, To: a.To, Action: a.Action}
}
```

This is a **fail-open fallback**: if the transition declaration no longer exists (e.g., model was modified between park and decide), the code creates a synthetic transition with **no guard**. If guard re-evaluation is added *after* this fallback, it would correctly evaluate a nil `When` (which `evalWhere` treats as `true` — expr.go:44: "Empty expr matches").

**However**, the deeper issue is that the fallback itself is a semantic concern: if the transition was deleted from the model, should the approval still proceed via a synthetic transition?

**Correction required:**
1. Guard re-evaluation should use the **actual** `tr.When` from the found transition (or empty string if synthetic). An empty `When` → guard passes (fail-open is acceptable here — no guard was declared).
2. **Recommendation:** The `findTransition == nil` fallback should log a warning (or be tracked as a metric) because it indicates a model change invalidated the original transition declaration. This is a TOCTOU surface on the *model itself*, not just on the data.
3. For Cycle-003, accept the current fallback behavior but add a comment: `// D-010: synthetic transition when model changed between park and decide — guard is empty by design`.

---

## H. GUARD_FAILED error code reuse and MCP contract compliance

**AGREE.**

`GUARD_FAILED` (`CodeGuardFailed = "GUARD_FAILED"`, internal/engine/errors.go:16) is already:
- Defined in the error code registry
- Documented in the MCP contract (docs/MCP-CONTRACT-v0.md:77)
- Tested in dogfood tests (servicedesk_dogfood_test.go:267, hr_dogfood_test.go:116,126)

Reusing this code is correct. The error message should clearly distinguish "guard was false at park-time" from "guard became false between park and decide" — the fix_hint should guide the human to check data changes.

**Code-grounded:** The existing `CodeGuardFailed` error in `actCore` (workflow.go:149-151) already has a generic fix_hint. For the Decide path, the fix_hint should be slightly different: "data changed while approval was pending; fix data and retry decide, or request a new approval."

**No correction needed** (beyond the fix_hint wording distinction).

---

## I. Additional TOCTOU surfaces (erase/revert/rewind, auto transitions, emits)

**AGREE — but needs scope clarification.**

The proposal correctly identifies that the core guard re-evaluation in `Decide` covers the primary TOCTOU surface. However:

1. **Auto-transitions** (workflow.go:192, 363): After `applyTransitionAndEmit`, `runAutoTransitions` fires. These auto-transitions are evaluated in `actCore` which already has guard checks. **No change needed.**

2. **Erase/revert/rewind**: These operate on records directly, not through workflow transitions. If they bypass the HITL gate, they are a separate TOCTOU surface that Cycle-003 does not address. **Out of scope**, but should be noted in TECHDEBT.

3. **Emit side-effects**: The `emits` clauses fire after the transition is applied. If an emit triggers an external webhook or another Act call, those are separate operations with their own guard evaluations. **No change needed.**

4. **Replay determinism**: During replay (engine.go:312-329), approval events are replayed into in-memory projections. No guard evaluation occurs during replay — this is correct because replay reconstructs state, it does not execute decisions. **No change needed.**

**No correction needed** (but add a TECHDEBT note for erase/revert/rewind TOCTOU).

---

## J. HUMAN_GATE necessity assessment

**AGREE.**

The proposal correctly states that HUMAN_GATE is **NOT required** because:
- No DSL schema changes
- No new events or fields
- This is an **invariant strengthening** (adding a check that should have always been there)
- Existing DSL packs work unchanged (the guard is re-evaluated the same way it was at park-time)

**No correction needed.**

---

## K. Acceptance criteria completeness

**DISAGREE — needs expansion.**

The proposal's acceptance criteria cover the main scenarios but are missing:

1. **`$me` context test**: A test that verifies `$me` resolves to the approver (not requester) during Decide-time guard re-evaluation. This is the semantic change from point C.

2. **Model-change fallback test**: When the transition declaration is deleted between park and decide, verify the synthetic transition path works (guard is empty → passes).

3. **Replay determinism test**: Verify that replaying `approval.granted` events does not trigger guard evaluation (replay should be deterministic and side-effect-free).

4. **Guard re-eval on rejected approval**: Verify that a rejected approval does NOT trigger guard re-evaluation (already correct per point D, but needs a test).

5. **Computed field fixpoint test**: A guard that depends on a computed field (`days_since` or aggregate) where the underlying data changed between park and decide — verify the fixpoint evaluation captures the latest values.

**Correction required:** Add these 5 test cases to the acceptance criteria.

---

## Overall Verdict: ACCEPT-WITH-CORRECTIONS

### Summary

The proposal is architecturally sound. The core design — full guard re-evaluation in `Decide` using `withComputed` + `ctxFor` + `evalWhere`, placed after journaling and before `applyTransitionAndEmit`, returning `GUARD_FAILED` — is correct and minimal. The TOCTOU analysis is thorough. The cost estimate (~15 lines + tests) is accurate.

### Required corrections before implementation

| # | Point | Correction |
|---|-------|------------|
| 1 | C | Document the `$me` semantic shift (requester → approver); add TODO for `$requester` compat |
| 2 | G | Add comment on `findTransition==nil` synthetic transition behavior; log/warning for model-change detection |
| 3 | K | Add 5 test cases: `$me` context, model-change fallback, replay determinism, reject-path no-guard, computed fixpoint |

### Recommendations for skeptic_pro / review_security_pro

1. **Priority concern:** The `$me` semantic shift (point C) is the highest-risk change. Existing DSL packs that write guards referencing `$me.attr` may behave differently for different approvers. Recommend a migration note in the release.
2. **Security concern:** The `findTransition==nil` synthetic transition (point G) should be monitored — if an attacker can trigger model changes between park and decide, they could eliminate the guard. Recommend adding a `guardHash` to the approval payload for future integrity checking.
3. **Observability:** Consider logging guard re-evaluation results (pass/fail + guard text) for audit trails.

### Verdict: **ACCEPT-WITH-CORRECTIONS** (corrections 1-3 above are required before implementation begins)
