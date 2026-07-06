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
    # LLD-20 (docs/lld/LLD-20-field-scoped-update-perms.md): a true field-level
    # ALLOW-LIST is now enforceable -- the prior CORE GAP is closed. Gate (1) in
    # engine.Update (engine/records.go) used to be an unscoped
    # `can(actor,"update",entity,"",rec)` record-level check, which a
    # field-scoped-only grant could never satisfy (see git history / TECHDEBT
    # for the old workaround this replaces). Gate (1) is now an existence check
    # (engine/permissions.go: canUpdateEntity) -- does the role hold ANY update
    # rule on the entity, any field scope? -- and gate (2) (checkFieldWrites,
    # unchanged) remains the sole per-field allow+deny decision. So the agent's
    # grant below is the true allow-list: it names exactly the fields it may
    # write (assessment, risk); every other Contract field, present or added
    # later, is default-denied with no maintenance burden -- unlike the old
    # exhaustive per-field `deny` list, a field added to Contract in the future
    # is safe by default here, not by omission.
    Legal:
        read   [Contract, Counterparty, Playbook, Finding]
        update [Contract.assessment, Contract.risk]
        create [Finding]
        act    [review]
        # N4 depth gap A: a dedicated grant for the perimeter detector's
        # tripwire on Contract.terms -- journaling ingest.flagged is a
        # stronger surface than the `update [Contract.assessment, Contract.risk]`
        # grant above and is NEVER inferred from it (internal/engine's canFlag /
        # dsl's `flag` verb); only Legal, the sole role that ever runs the
        # detector on contract review, holds it -- mirrors
        # packs/knowvault/knowvault.dsl's Indexer grant.
        flag   [ingest.flagged]
        deny   [delete *, act [address, waive, reopen]]
