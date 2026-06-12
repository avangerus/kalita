# Accounts receivable / debt collection — acceptance pack #1 (DSL-SPEC-v0 §11)

entity Contract:
    company: string required
    due_date: date required
    amount: money
    classified: bool default=false

entity Debtor:
    company: string required
    contract: ref[Contract] on_delete=restrict
    debt: money
    overdue_days: int computed = days_since(contract.due_date)
    status: enum[OnTime, Overdue, Claim, Legal, Settled] default=OnTime
    manager: ref[core.User] default=$me

constraints:
    unique(company, contract)

workflow Debtor on status:
    OnTime  -> Overdue: auto when overdue_days > 0
    Overdue -> Claim:   send_claim assignee=agent(Collector)
    Claim   -> Legal:   escalate requires approval(FinDirector)
    any     -> Settled: auto when debt = 0

roles:
    Accountant
    FinDirector
    Collector agent

permissions:
    Collector:
        read  [Debtor, Contract]
        act   [send_claim]
        deny  [update Debtor.debt, delete *, read Contract where classified = true]
    Accountant:
        full  [Debtor, Contract]
        act   [escalate]
    FinDirector:
        approve [escalate]
        read    all

automation:
    on schedule daily at 09:00 for Debtor when status = Overdue and overdue_days in [3, 7, 14]:
        agent Collector: draft_reminder(tone = soft if overdue_days < 7 else firm)
        notify email(manager)

    on stuck Debtor in Claim for 10d:
        escalate_to FinDirector

ui Debtor:
    list: [company, debt, overdue_days, status] sort=-overdue_days
        filters: [status, manager]
        view "My debtors": where manager = $me
    form:
        section "General": [company, contract, debt]
        section "Status":  [status, overdue_days]
    board: by status
