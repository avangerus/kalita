# Finance — invoices, payments and receivables (дебиторка), the part 1C owns and
# every business needs. It closes the revenue loop of the other packs (a CRM deal
# or an e-shop order becomes an invoice) and brings back the founder's original
# "Дебиторка" agent: when an invoice goes overdue, a collections AGENT takes it
# from the pool, chases the client, and a human signs any write-off (HITL).

entity Client "Client":
    name:  string required label="Name"
    inn:   string label="Tax ID"
    email: email label="Email"
    owner: ref[core.User] default=$me label="Owner"

entity Invoice "Invoice":
    number:        serial format="INV-{year}-{seq:5}" label="Number"
    client:        ref[Client] on_delete=restrict label="Client"
    amount:        money label="Amount"
    issued_at:     datetime default=$now label="Issued"
    due_date:      date label="Due date"
    last_reminder: datetime label="Last reminder"     # the collections agent updates this
    # money roll-ups: how much is paid, and what is still owed
    paid_amount:   money computed = sum(Payment.amount where invoice = $self) label="Paid"
    balance:       money computed = amount - sum(Payment.amount where invoice = $self) label="Balance"
    days_overdue:  int   computed = days_since(due_date) label="Days overdue"
    status:        enum[Draft, Issued, Overdue, InCollection, Paid, WrittenOff, Cancelled] default=Draft label="Status"

workflow Invoice on status:
    Draft        -> Issued:       issue label="Issue"
    # past the due date with money still owed -> overdue; entering Overdue queues
    # a chase task for the collections agent pool
    Issued       -> Overdue:      auto when days_overdue > 0
    Issued       -> Paid:         settle when balance <= 0 label="Mark paid"
    Overdue      -> InCollection: chase assignee=agent(Collector) label="Chase"
    Overdue      -> Paid:         settle_overdue when balance <= 0 label="Mark paid"
    InCollection -> Paid:         collected when balance <= 0 label="Collected"
    # writing off a debt is a human decision, signed
    InCollection -> WrittenOff:   write_off requires approval(FinanceManager) label="Write off"
    InCollection -> Overdue:      reschedule label="Reschedule"
    any          -> Cancelled:    cancel label="Cancel"

entity Payment "Payment":
    invoice: ref[Invoice] on_delete=cascade label="Invoice"
    amount:  money label="Amount"
    paid_at: datetime default=$now label="Paid at"
    method:  enum[Bank, Card, Cash] default=Bank label="Method"

roles:
    Accountant
    FinanceManager
    Collector agent

permissions:
    # books the invoices and payments, marks settlements
    Accountant:
        full [Client, Invoice, Payment]
        act  [issue, settle, settle_overdue, collected, reschedule, cancel]
    # owns the money process and signs write-offs
    FinanceManager:
        full    [Client, Invoice, Payment]
        approve [write_off]
        act     [issue, settle, settle_overdue, chase, collected, write_off, reschedule, cancel]
    # the collections agent: chases overdue invoices, logs contact, escalates;
    # cannot change the amount or the workflow state, cannot sign a write-off
    Collector:
        read   [Invoice, Client, Payment]
        update [Invoice]
        act    [chase]
        deny   [delete *, update Invoice.status, update Invoice.amount, update Invoice.client]

automation:
    on create Invoice:
        notify email(client)
    on stuck Invoice in InCollection for 14d:
        escalate_to FinanceManager

dashboard Receivables "Receivables":
    tile "Outstanding": sum balance Invoice where status = Issued or status = Overdue or status = InCollection
    tile "Overdue":     sum balance Invoice where status = Overdue or status = InCollection
    tile "In collection": count Invoice where status = InCollection
    tile "By status":   count Invoice group by status
