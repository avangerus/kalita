# AP — incoming invoices (кредиторка), the founder's scenario as an executable
# pack: an invoice arrives from "system X", an AGENT verifies the contract, the
# amount and the requisites, and the approval ladder is threshold-based — up to
# 100k the manager signs, up to 1M the finance director, above that the general
# director. After the signature the invoice is handed to "system Y".
#
# Honest v1 boundaries (see docs/DECISIONS.md, hypotheses H1/H2):
#   - H1 proven: the threshold ladder is plain guarded transitions, no kernel
#     change (three ApprovalPending -> Approved transitions with different
#     `requires approval(Role)` + `when amount ...` guards).
#   - H2 gap: cross-pack refs do not exist (internal/dsl/semantic.go), so the
#     contract is an external number the agent cross-checks (contract_ok); a
#     real ref[contracts.Contract] waits for the cross-pack decision.
#   - "System Y" is the egress seam: a send_to_payment agent task + a
#     `webhook out` rule (ops.dsl); the real connector belongs to the Next
#     layer. Intake ("system X") is the APClerk role; a least-privilege
#     connector (workers/connectors/inbound.py pattern) can replace it later.

entity Vendor "Supplier":
    include core.Named
    inn:          string label="Tax ID"
    kpp:          string label="KPP"
    bank_bic:     string label="Bank BIC"
    bank_account: string label="Bank account"

entity VendorInvoice "Incoming invoice":
    number:      serial format="AP-{year}-{seq:5}" label="Number"
    vendor:      ref[Vendor] on_delete=restrict label="Supplier"
    # the contract is external today (cross-pack ref gap, H2) — the checker
    # agent verifies it and stamps contract_ok; a human sees the number
    contract_no: string label="Contract No"
    amount:      money required label="Amount"
    received_at: datetime default=$now label="Received"
    due_date:    date label="Due date"
    # checker-agent verdicts (field-scoped allow-list, see security.dsl)
    contract_ok:   bool label="Contract ok"
    amount_ok:     bool label="Amount ok"
    requisites_ok: bool label="Requisites ok"
    check_notes:   text label="Check notes"
    status:      enum[Received, Checking, ApprovalPending, Approved, Rejected, SentToPayment] default=Received label="Status"
