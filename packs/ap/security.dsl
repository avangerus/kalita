# Roles and permissions. Separation of duties, threshold ladder style:
# the clerk registers the invoice, the AGENT checks it, and each approver tier
# signs only its own range. The agent can sign nothing and cannot touch the
# amount, the supplier or the workflow state — by construction (deny), not by
# convention.

roles:
    APClerk
    Manager
    FinanceDirector
    GeneralDirector
    APChecker agent

permissions:
    # intake from "system X" (v1: a human; a least-privilege connector can
    # replace this entry point without touching the rest of the pack).
    # Skeptic P1 fix: once submitted, the money fields and the record itself
    # are locked for the clerk — the approval ladder signs what was checked,
    # not what someone edits while the signature is pending. Since Cycle-003
    # the engine ALSO re-checks the transition guard at Decide time (kernel,
    # D-010), so these deny rows are defense-in-depth: the kernel refuses a
    # stale decision for EVERY pack, and the rows prevent the drift here.
    APClerk:
        full [Vendor, VendorInvoice]
        deny [delete VendorInvoice where status = ApprovalPending or status = Approved or status = SentToPayment, update VendorInvoice.amount where status = ApprovalPending or status = Approved or status = SentToPayment, update VendorInvoice.vendor where status = ApprovalPending or status = Approved or status = SentToPayment, update VendorInvoice.contract_no where status = ApprovalPending or status = Approved or status = SentToPayment]
    # --- the threshold ladder: each tier approves only its own range ---
    Manager:
        read    [Vendor, VendorInvoice]
        approve [approve_small]
        act     [approve_small, reject, reopen]
    FinanceDirector:
        read    [Vendor, VendorInvoice]
        approve [approve_mid]
        act     [approve_mid, reject, reopen]
    GeneralDirector:
        read    [Vendor, VendorInvoice]
        approve [approve_large]
        act     [approve_large, reject, reopen]
    # the checker agent: reads the invoice, writes ONLY its verdict fields
    # (LLD-20 true field-scoped allow-list — a field added to VendorInvoice
    # later is default-denied for the agent), drives its pool transitions and
    # flags mismatches. No approve grant exists for it, ever.
    APChecker:
        read   [Vendor, VendorInvoice]
        update [VendorInvoice.contract_ok, VendorInvoice.amount_ok, VendorInvoice.requisites_ok, VendorInvoice.check_notes]
        act    [start_checking, submit_check, flag_mismatch, send_to_payment]
        deny   [delete *, update VendorInvoice.status, update VendorInvoice.amount, update VendorInvoice.vendor, act [approve_small, approve_mid, approve_large, reject, reopen]]
