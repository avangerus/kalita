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
        act  [send_to_legal, recall, sign, activate, renegotiate, terminate]
    # the lawyer signs off the legal review (signature 1) — cannot approve money
    Lawyer:
        read    [Contract, Counterparty]
        approve [clear_legal, request_changes]
        act     [clear_legal, request_changes]
    # the commercial director approves the terms (signature 2) — the money decision
    CommercialDirector:
        read    [Contract, Counterparty]
        approve [approve_terms]
        act     [approve_terms, terminate]
    # the legal agent: reads the terms, writes a clause-level risk assessment and
    # sets the risk level, then submits its review. It cannot change the amount or
    # the workflow state, and it signs nothing — both signatures stay human.
    Legal:
        read   [Contract, Counterparty]
        update [Contract]
        act    [review]
        deny   [delete *, update Contract.status, update Contract.amount, update Contract.counterparty]
