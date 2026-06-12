# Contract management (договорная работа) — the founder's "Договорная работа" and
# "Юристы" agents on a mature platform. A contract manager drafts; a LEGAL AGENT
# reviews the terms and writes a clause-level risk assessment; then the document
# clears TWO sequential human signatures — the lawyer (legal) and the commercial
# director (money) — before it is signed and goes live. Multi-party HITL, audited.

entity Counterparty "Counterparty":
    name:  string required label="Name"
    inn:   string label="Tax ID"
    email: email label="Email"
    kind:  enum[Client, Vendor, Partner] default=Client label="Type"
    owner: ref[core.User] default=$me label="Owner"

entity Contract "Contract":
    number:       serial format="CTR-{year}-{seq:5}" label="Number"
    title:        string required label="Title"
    counterparty: ref[Counterparty] on_delete=restrict label="Counterparty"
    kind:         enum[NDA, Services, Supply, License] default=Services label="Kind"
    amount:       money label="Amount"
    start_date:   date label="Start date"
    end_date:     date label="End date"
    terms:        text label="Terms"            # what the manager negotiated — the agent reads this
    assessment:   text label="Legal assessment" # the legal agent writes its narrative summary here
    risk:         enum[Unknown, Low, Medium, High] default=Unknown label="Risk"
    # how long the deal still runs, for the desk — negative once it has expired
    days_left:    int computed = days_until(end_date) label="Days left"
    # the structured review: how many clause-level findings still need a lawyer
    open_findings: int computed = count(Finding where contract = $self) label="Findings"
    status:       enum[Draft, LegalReview, Reviewed, Negotiation, Approved, Signed, Active, Expired, Terminated] default=Draft label="Status"

# Playbook: the firm's standard legal positions. The legal agent reviews every
# contract AGAINST these — this is what makes the review the firm's, not a
# generic model's. A senior lawyer curates them; later they feed RAG retrieval.
entity Playbook "Playbook rule":
    topic:        string required label="Topic"        # e.g. "Liability cap"
    our_position: text required label="Our position"   # the standard the firm holds
    severity:     enum[Low, Medium, High] default=Medium label="Severity if breached"
    active:       bool default=true label="Active"

# Finding: one clause-level issue raised on a contract, checked against the
# playbook — the structured work product of the legal review. The agent raises
# them; a lawyer triages each (address / waive). Risk rolls up to the contract.
entity Finding "Finding":
    contract: ref[Contract] on_delete=cascade label="Contract"
    clause:   string required label="Clause / topic"
    risk:     enum[Low, Medium, High] default=Medium label="Risk"
    issue:    text label="Issue"
    redline:  text label="Suggested redline"
    rule:     ref[Playbook] label="Playbook rule"
    status:   enum[Open, Addressed, Waived] default=Open label="Status"
