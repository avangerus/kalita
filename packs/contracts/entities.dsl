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
    assessment:   text label="Legal assessment" # the legal agent writes its clause-level review here
    risk:         enum[Unknown, Low, Medium, High] default=Unknown label="Risk"
    # how long the deal still runs, for the desk — negative once it has expired
    days_left:    int computed = days_until(end_date) label="Days left"
    status:       enum[Draft, LegalReview, Reviewed, Negotiation, Approved, Signed, Active, Expired, Terminated] default=Draft label="Status"
