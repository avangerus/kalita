# The contract lifecycle — the part that makes договорная работа hard is not the
# document, it is the SEQUENCE OF SIGNATURES. A draft does not become a live
# contract until a legal agent has reviewed it AND two different humans have each
# signed their part: the lawyer clears the legal risk, then the commercial
# director approves the money. Two distinct approvers, in order, each Ed25519.

workflow Contract on status:
    Draft        -> LegalReview:  send_to_legal label="Send to legal"
    # entering LegalReview queues a review task for the legal-agent pool; the agent
    # writes its clause-level assessment and moves it to Reviewed
    LegalReview  -> Reviewed:     review assignee=agent(Legal) label="Review"
    LegalReview  -> Draft:        recall label="Recall to draft"
    # SIGNATURE 1 — the lawyer clears (or bounces) the legal review
    Reviewed     -> Negotiation:  clear_legal requires approval(Lawyer) label="Clear legal"
    Reviewed     -> Draft:        request_changes requires approval(Lawyer) label="Request changes"
    # SIGNATURE 2 — a different human, the commercial director, approves the money
    Negotiation  -> Approved:     approve_terms requires approval(CommercialDirector) label="Approve terms"
    Negotiation  -> Draft:        renegotiate label="Renegotiate"
    Approved      -> Signed:      sign label="Mark signed"
    Signed        -> Active:      activate label="Activate"
    # past its end date a live contract lapses on its own
    Active        -> Expired:     auto when days_left < 0
    any           -> Terminated:  terminate label="Terminate"

automation:
    on create Contract:
        notify email(counterparty)
    # a deal stuck in negotiation too long is escalated to the commercial director
    on stuck Contract in Negotiation for 7d:
        escalate_to CommercialDirector
