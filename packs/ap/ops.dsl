# Automation, UI and dashboards.

automation:
    on stuck VendorInvoice in Checking for 3d:
        escalate_to FinanceDirector
    on stuck VendorInvoice in ApprovalPending for 5d:
        escalate_to GeneralDirector
    # the "system Y" egress seam: the send_to_payment transition emits
    # InvoiceSentToPayment, and this rule turns the fact into the declared
    # webhook egress task (engine journals intent as TaskWebhook). The node's
    # webhook runner (Cycle-B1, D-017) delivers it under KALITA_EGRESS_DOMAINS
    # (fail-closed, at-least-once + idempotent effect); URL is per-deployment.
    on event InvoiceSentToPayment:
        webhook out "https://payments.example.com/incoming-invoice"

ui VendorInvoice:
    list: [number, vendor, contract_no, amount, status] sort=-received_at
        filters: [status, vendor]
    form:
        section "Invoice": [vendor, contract_no, amount, due_date]
        section "Check":   [contract_ok, amount_ok, requisites_ok, check_notes]
    board: by status

ui Vendor:
    list: [name, inn, kpp, bank_bic]

dashboard APBoard "Accounts payable":
    tile "Awaiting approval": count VendorInvoice where status = ApprovalPending
    tile "In check":          count VendorInvoice where status = Checking
    tile "Sent to Y":         count VendorInvoice where status = SentToPayment
    tile "By status":         count VendorInvoice group by status
