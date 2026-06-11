# A Jira-like issue tracker as ONE pack — the completeness test for the type
# system (TYPE-SYSTEM-V1). Projects, issues with rich custom fields, labels,
# components, named issue links, a board, a workflow with HITL on release.
# Zero kernel changes — if this works, the primitives are enough.

entity Project:
    name: string required unique
    key: string required unique          # PROJ — issue keys are KEY-NNN
    lead: ref[core.User]
    color: color

entity Issue:
    project: ref[Project] on_delete=restrict
    title: string required
    description: text
    type: enum[Bug, Story, Task, Epic] default=Task
    priority: enum[Lowest, Low, Medium, High, Highest] default=Medium
    assignee: ref[core.User]
    reporter: ref[core.User] default=$me
    story_points: int
    estimate: duration
    progress: percent
    labels: array[string]
    components: array[enum[Backend, Frontend, Infra, Docs, Design]]
    due: date
    status: enum[Backlog, Todo, InProgress, InReview, Done] default=Backlog

# Jira issue links — both directions kept in sync by the runtime
link Issue -> Issue as blocks / blocked_by
link Issue -> Issue as relates_to / relates_to     # symmetric
link Issue -> Issue as duplicates / duplicated_by
link Issue -> Issue as parent_of / child_of

workflow Issue on status:
    Backlog    -> Todo:       plan
    Todo       -> InProgress: start assignee=agent(Assistant)
    InProgress -> InReview:   submit_for_review
    InReview   -> InProgress: request_changes
    InReview   -> Done:       approve requires approval(Lead)
    any        -> Backlog:    reopen

roles:
    Lead
    Developer
    Assistant agent

permissions:
    Lead:
        full    [Project, Issue]
        act     [plan, start, submit_for_review, request_changes, approve, reopen]
        approve [approve]
    Developer:
        read   [Project]
        full   [Issue]
        act    [plan, start, submit_for_review, request_changes, reopen]
        deny   [delete Project, update Project.*]
    Assistant:
        read   [Project, Issue]
        update [Issue]
        act    [start, submit_for_review]
        deny   [delete *, update Project.*, update Issue.status where status = Done]

automation:
    on stuck Issue in InReview for 3d:
        escalate_to Lead

ui Issue:
    list: [title, type, priority, assignee, story_points, status] sort=-priority
        filters: [project, type, priority, status, assignee]
        view "My issues": where assignee = $me
    form:
        section "Issue":    [project, title, description, type]
        section "Planning": [priority, assignee, story_points, estimate, due]
        section "Detail":   [labels, components, progress]
    board: by status

ui Project:
    list: [key, name, lead]
