package main

// packV1Source is the genesis pack for the v0.1 golden scenario — the model
// engine.New actually boots from. It deliberately has NO Case.resolution
// field: that field arrives later, live, via ProposeChange/DecideProposal
// (see scenario.go), so the recorded journal also exercises additive
// definition evolution mid-stream (HLD-V1 §H3), not just record/workflow/
// task/approval/comment/file events. "goldenv0" is a fixture pack: it is not
// one of packs/*, on purpose (LLD-03 §3: "самодостаточный тестовый пак внутри
// testdata, не зависит от packs/").
const packV1Source = `pack goldenv0
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
`

// packV2Source is packV1Source plus Case.resolution — proposal A (approved).
const packV2Source = `pack goldenv0
version 0.1.0
requires kalita >= 0.1

entity Case:
    title:      string required
    notes:      text
    priority:   enum[Low, Normal, High] default=Normal
    amount:     money
    opened_on:  date required
    attachment: file
    resolution: string
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
`

// packV3Source is packV2Source plus Memo.tag — proposal B (rejected: it is
// compiled to prove the proposal itself is valid, but DecideProposal(grant=
// false) is called on it, so this text never becomes the live model; it
// exists only inside the DefinitionProposed/DefinitionRejected event payloads).
const packV3Source = `pack goldenv0
version 0.1.0
requires kalita >= 0.1

entity Case:
    title:      string required
    notes:      text
    priority:   enum[Low, Normal, High] default=Normal
    amount:     money
    opened_on:  date required
    attachment: file
    resolution: string
    status:     enum[New, Active, Review, Approved, Rejected, Closed] default=New

entity Memo:
    case:   ref[Case] required
    body:   text required
    author: string
    tag:    string

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
`
