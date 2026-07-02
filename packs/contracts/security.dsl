# Roles and permissions. The point of договорная работа is separation of duties:
# the manager runs the process, the legal AGENT reviews, and two DIFFERENT humans
# sign — the lawyer (legal risk) and the commercial director (money). No single
# actor can carry a contract from draft to active, and the agent can sign nothing.

roles:
    ContractManager
    Lawyer
    CommercialDirector
    Legal agent

permissions:
    # owns the process: drafts contracts, drives them, records the signature
    ContractManager:
        full [Counterparty, Contract]
        read [Playbook, Finding]
        act  [send_to_legal, recall, sign, activate, renegotiate, terminate]
    # the lawyer signs off the legal review (signature 1), curates the playbook,
    # and triages each finding — cannot approve money
    Lawyer:
        read    [Contract, Counterparty]
        full    [Playbook, Finding]
        approve [clear_legal, request_changes]
        act     [clear_legal, request_changes, address, waive, reopen]
    # the commercial director approves the terms (signature 2) — the money decision;
    # sees the findings to judge the risk before signing
    CommercialDirector:
        read    [Contract, Counterparty, Finding, Playbook]
        approve [approve_terms]
        act     [approve_terms, terminate]
    # the legal agent: reads the terms and the playbook, RAISES clause-level
    # findings and writes the narrative assessment, then submits. It cannot change
    # the amount or the workflow state, cannot triage its own findings, signs nothing.
    #
    # BUG-02: a pure field-level ALLOW-LIST (`update [Contract.assessment,
    # Contract.risk]` with no unscoped `update [Contract]`) is NOT enforceable
    # today -- CORE GAP, not a grammar gap: the grammar parses field-scoped
    # items fine, but engine.Update (engine/records.go) gates every update
    # through TWO checks: (1) an unscoped `can(actor,"update",entity,"",rec)`
    # record-level gate, THEN (2) checkFieldWrites per touched field. Gate (1)
    # queries field="" and engine/permissions.go's itemMatches only matches
    # field="" against an item whose OWN Field is "" or "*" -- a field-scoped
    # allow (Field="assessment") never matches field="", so gate (1) always
    # denies and the update never reaches gate (2), no matter what is granted
    # per field. Confirmed by running TestDogfoodContractsMultiSignature with
    # only `update [Contract.assessment, Contract.risk]` granted: the agent's
    # own in-bounds write to assessment/risk was rejected at gate (1).
    # Minimal repro: role R has ONLY `update [X.f]`; `can(actor,"update","X","",rec)`
    # is `false` for R regardless of f, so Update() never proceeds to check f.
    # Not hacked around here (see task instructions: do not patch internal/
    # engine for this). Logged as TECHDEBT (core: language/compiler) with a
    # proposed core fix (gate (1) should be satisfied by ANY matching update
    # rule for the entity, scoped or not -- checkFieldWrites already re-checks
    # every field, so gate (1) only needs to prove SOME update access exists).
    #
    # Maximum expressible today: an unscoped `update [Contract]` (satisfies
    # gate 1) PAIRED WITH an exhaustive per-field `deny` covering every
    # Contract field except the agent's own work product (assessment, risk).
    # This list is closed against entities.dsl's current Contract fields
    # (number, title, counterparty, kind, amount, start_date, end_date, terms,
    # status -- assessment/risk excluded, days_left/open_findings are computed
    # and already read-only). A field added to Contract later that is NOT
    # meant for the agent must be added here too -- unlike a real allow-list,
    # a missed field HERE reopens the hole; tracked as the residual risk of
    # this workaround in TECHDEBT.
    Legal:
        read   [Contract, Counterparty, Playbook, Finding]
        update [Contract]
        create [Finding]
        act    [review]
        deny   [delete *, act [address, waive, reopen]]
        deny   [update Contract.number, update Contract.title, update Contract.counterparty, update Contract.kind]
        deny   [update Contract.amount, update Contract.start_date, update Contract.end_date, update Contract.terms, update Contract.status]
