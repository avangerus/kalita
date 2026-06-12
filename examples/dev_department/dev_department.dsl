# Development department on rails — acceptance pack #2 (DSL-SPEC-v0 §11).
# The conveyor that builds Kalita itself runs on this pack (dogfood gate, week 8+).

entity ADR:
    title: string required
    context: text required
    decision: text required
    alternatives: text required
    expected_outcome: text required
    status: enum[Proposed, Accepted, Superseded, Revoked] default=Proposed
    superseded_by: ref[ADR] on_delete=set_null

entity Task:
    title: string required
    spec: text
    acceptance_criteria: text required
    complexity: enum[Routine, Complex, Architectural] default=Routine
    based_on: ref[ADR] on_delete=restrict
    review_cycles: int default=0
    status: enum[Backlog, Spec, InWork, Review, Tests, Merge, Done] default=Backlog

entity Defect:
    title: string required
    lens: enum[Security, Regression, Performance]
    task: ref[Task] on_delete=set_null
    status: enum[Found, Checked, Confirmed, False] default=Found

workflow ADR on status:
    Proposed -> Accepted: accept requires approval(Architect)
    Accepted -> Revoked:  revoke requires approval(Architect)

workflow Task on status:
    Backlog -> Spec:   draft_spec assignee=agent(Decomposer)
    Spec    -> InWork: accept_spec requires approval(Owner)
    InWork  -> Review: submit assignee=agent(Developer)
    Review  -> Tests:  approve_review assignee=agent(Reviewer)
    Review  -> InWork: return_for_rework assignee=agent(Reviewer)
    Tests   -> Merge:  auto when review_cycles < 3
    Merge   -> Done:   merge requires approval(Owner)

workflow Defect on status:
    Found   -> Checked:   challenge assignee=agent(Skeptic)
    Checked -> Confirmed: confirm assignee=agent(Skeptic)
    Checked -> False:     dismiss assignee=agent(Skeptic)

roles:
    Owner
    Architect
    Engineer
    Decomposer agent
    Developer agent
    Reviewer agent
    Skeptic agent
    Hunter agent

permissions:
    Decomposer:
        read [Task, ADR]
        act  [draft_spec]
        deny [update Task.acceptance_criteria, delete *, act [merge]]
    Developer:
        read [Task, ADR]
        act  [submit]
        deny [act [merge, approve_review], update Task.spec, update ADR.*, delete *]
    Reviewer:
        read [Task, ADR]
        act  [approve_review, return_for_rework]
        deny [act [submit, merge], update Task.*, delete *]
    Skeptic:
        read [Defect, Task]
        act  [challenge, confirm, dismiss]
        deny [update Task.*, delete *]
    Hunter:
        read   [Task]
        create [Defect]
        deny   [update Defect.status, delete *]
    Engineer:
        full [Task, Defect]
        read [ADR]
    Owner:
        approve [accept_spec, merge]
        full    all
    Architect:
        approve [accept, revoke]
        full    [ADR]
        read    all

automation:
    on schedule daily at 02:00:
        agent Hunter: hunt(lens = Security)
        agent Hunter: hunt(lens = Regression)
        agent Hunter: hunt(lens = Performance)

    on update Task when review_cycles > 2:
        escalate_to Engineer

    on stuck Task in InWork for 2d:
        escalate_to Engineer

ui Task:
    list: [title, complexity, status, review_cycles] sort=-status
        filters: [status, complexity]
        view "Needs my signature": where status in [Spec, Merge]
    form:
        section "What": [title, spec, acceptance_criteria]
        section "How":  [complexity, based_on, review_cycles]
    board: by status

ui Defect:
    list: [title, lens, status] sort=-status
        filters: [lens, status]
    board: by status

ui ADR:
    list: [title, status] sort=-status
    form:
        section "Decision": [title, context, decision]
        section "Honesty":  [alternatives, expected_outcome]
