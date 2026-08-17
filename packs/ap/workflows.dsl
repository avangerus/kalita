# Workflow: intake -> agent check -> threshold approval ladder -> hand-off to Y.
#
# The ladder (H1): three guarded transitions between the SAME pair of states.
# Action names are unique within the workflow (compiler rule); the `when`
# guard is checked against the actor calling the transition BEFORE anything is
# parked (same timing as hr's SoD guard), so "the wrong tier" can never park.
#
# Cycle-003 (D-010): the kernel now RE-CHECKS the guard at Decide time — a
# decision whose `when` no longer holds fails GUARD_FAILED and the approval
# stays pending. The pack still locks amount/vendor/contract below as
# defense-in-depth (APClerk cannot touch them in
# ApprovalPending/Approved/SentToPayment, security.dsl deny rows), so the
# window is closed twice: the engine refuses the stale decision, and the deny
# rows prevent the drift in the first place.
#
# The agent never signs: it drives start_checking/submit_check/send_to_payment
# (its pool tasks) and can flag a mismatch, nothing more (security.dsl).

event InvoiceApproved "Incoming invoice approved":
    number:      string
    amount:      money
    contract_no: string

event InvoiceSentToPayment "Incoming invoice handed to payment":
    number:      string
    amount:      money
    contract_no: string

workflow VendorInvoice on status:
    Received        -> Checking:        start_checking assignee=agent(APChecker) label="Start check"
    Checking        -> ApprovalPending: submit_check label="Submit for approval"
    Checking        -> Rejected:        flag_mismatch label="Flag mismatch"
    # threshold ladder: ≤100k manager, ≤1M finance director, above — general director;
    # amount must be positive (≤0 has no tier and must be corrected or rejected)
    ApprovalPending -> Approved:        approve_small  requires approval(Manager) when amount > 0 and amount <= 100000 label="Approve (≤100k)" emits InvoiceApproved(number = number, amount = amount, contract_no = contract_no)
    ApprovalPending -> Approved:        approve_mid    requires approval(FinanceDirector) when amount > 100000 and amount <= 1000000 label="Approve (≤1M)" emits InvoiceApproved(number = number, amount = amount, contract_no = contract_no)
    ApprovalPending -> Approved:        approve_large  requires approval(GeneralDirector) when amount > 1000000 label="Approve (>1M)" emits InvoiceApproved(number = number, amount = amount, contract_no = contract_no)
    # any approver tier may reject; rejection is a human decision, journaled.
    # Deliberate asymmetry (see DECISIONS D-009): reject is a plain act, not
    # HITL — rejection is reversible via reopen and cannot move money;
    # approval is the security-critical direction and is signature-gated.
    ApprovalPending -> Rejected:        reject label="Reject"
    Rejected        -> Checking:        reopen label="Reopen check"
    # the hand-off to "system Y": the agent task emits InvoiceSentToPayment,
    # and the automation rule in ops.dsl turns that fact into the declared
    # webhook egress task (node-side delivery runner — kernel gap, DECISIONS)
    Approved        -> SentToPayment:   send_to_payment assignee=agent(APChecker) label="Send to Y" emits InvoiceSentToPayment(number = number, amount = amount, contract_no = contract_no)
