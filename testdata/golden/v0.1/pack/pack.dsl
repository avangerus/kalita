pack goldenv0
version 0.1.0
requires kalita >= 0.1

entity Case:
    title:      string required
    notes:      text
    priority:   enum[Low, Normal, High] default=Normal
    amount:     money
    opened_on:  date required
    attachment: file
    status:     enum[New, Active, Review, Approved, Rejected, Closed] default=New

entity Memo:
    case:   ref[Case] required
    body:   text required
    author: string

workflow Case on status:
    New     -> Active:   open        assignee=agent(Worker)
    Active  -> Review:   submit
    Review  -> Approved: finalize    requires approval(Owner)
    Review  -> Rejected: reject_case
    any     -> Closed:   close

roles:
    Owner
    Reviewer
    Worker agent

permissions:
    Owner:
        full    [Case, Memo]
        act     [open, submit, reject_case, close, finalize]
        approve [finalize]
    Reviewer:
        read    [Case, Memo]
        create  [Case]
        update  [Case]
        act     [submit, reject_case, finalize]
        deny    [delete *, update Case.status]
    Worker:
        read    [Case, Memo]
        create  [Memo]
        act     [open]
        deny    [update Case.*, delete *]

automation:
    on create Case:
        agent Worker: triage(priority = tag)
        notify email(author)
        webhook out "https://example.invalid/hooks/case-created"

    on update Case when priority = High:
        notify email(author)

    on schedule every 5m for Case when status = Active:
        agent Worker: chase()

    on stuck Case in Review for 2d:
        escalate_to Owner
