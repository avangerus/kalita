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
    Legal:
        read   [Contract, Counterparty, Playbook, Finding]
        update [Contract]
        create [Finding]
        act    [review]
        deny   [delete *, update Contract.status, update Contract.amount, update Contract.counterparty, act [address, waive, reopen]]
