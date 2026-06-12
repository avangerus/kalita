pack hr
version 0.1.0
requires kalita >= 0.1
depends core >= 0.1

entity Employee:
    user: ref[core.User]
    full_name: string required
    email: email
    position: string
    hire_date: date
    annual_quota: int default=28
    used_days: int computed = sum(LeaveRequest.days where employee = $self)
    remaining_days: int computed = annual_quota - sum(LeaveRequest.days where employee = $self)
    status: enum[Active, OnLeave, Terminated] default=Active

entity LeaveRequest:
    number: serial format="LR-{year}-{seq:4}"
    employee: ref[Employee] on_delete=restrict
    kind: enum[Vacation, Unpaid] default=Vacation
    start_date: date required
    days: int required
    status: enum[Draft, Submitted, Approved, Rejected] default=Draft

entity SickLeave:
    number: serial format="SL-{year}-{seq:4}"
    employee: ref[Employee] on_delete=restrict
    certificate: file
    start_date: date required
    status: enum[Open, Confirmed, Closed] default=Open

workflow LeaveRequest on status:
    Draft     -> Submitted: submit
    Submitted -> Approved:  approve requires approval(Manager)
    Submitted -> Rejected:  reject requires approval(Manager)

workflow SickLeave on status:
    Open      -> Confirmed: confirm when certificate != null
    Confirmed -> Closed:    close

roles:
    Manager
    Employee

permissions:
    Manager:
        full    [Employee, LeaveRequest, SickLeave]
        act     [submit, approve, reject, confirm, close]
        approve [approve, reject]
    Employee:
        read Employee where user = $me
        read LeaveRequest where employee.user = $me
        create [LeaveRequest, SickLeave]
        act    [submit]
        deny   [delete *, update Employee.*]

ui LeaveRequest:
    list: [number, employee, kind, start_date, days, status] sort=-start_date
        filters: [status, kind]
    form:
        section "Request": [employee, kind, start_date, days]
    board: by status
