# DevTrack — the opus magnum: a tracker where the tasks are done by AGENTS.
# A human files an issue; entering the backlog queues it for an Engineer agent,
# which takes it from the pool over MCP, does the work (writes its result and a
# progress report), and submits. A human accepts the result — a signed,
# human-in-the-loop gate. Every step is one journaled event. This is what no ERP
# framework or issue tracker offers natively: agents as employees, audited,
# under human control, with the whole thing described in ~40 lines.

entity Issue "Issue":
    number:      serial format="DEV-{seq:5}" label="Number"
    title:       string required label="Title"
    description: text label="Description"
    priority:    enum[Low, Normal, High] default=Normal label="Priority"
    reporter:    ref[core.User] default=$me label="Reporter"
    result:      text label="Agent result"          # the agent writes its output here
    status:      enum[Backlog, InProgress, Review, Done, Rejected] default=Backlog label="Status"

workflow Issue on status:
    # entering Backlog queues a pick_up task for the Engineer agent pool
    Backlog    -> InProgress: pick_up assignee=agent(Engineer) label="Pick up"
    InProgress -> Review:     submit label="Submit for review"
    # a human accepts (or rejects) the agent's work — the HITL gate
    Review     -> Done:       accept requires approval(Lead) label="Accept"
    Review     -> Rejected:   reject requires approval(Lead) label="Reject"
    Rejected   -> Backlog:    requeue label="Requeue"

roles:
    Lead
    Engineer agent

permissions:
    # a human lead files issues and signs off on the agent's results
    Lead:
        full    [Issue]
        approve [accept, reject]
        act     [accept, reject, requeue]
    # the agent works issues but cannot accept its own output, and the workflow
    # state is moved only through transitions (never written directly)
    Engineer:
        read   [Issue]
        update [Issue]
        act    [pick_up, submit]
        deny   [delete *, update Issue.status, update Issue.reporter]

dashboard DevBoard "Agent work board":
    tile "Waiting for an agent": count Issue where status = Backlog
    tile "Agents working":       count Issue where status = InProgress
    tile "Awaiting human review": count Issue where status = Review
    tile "By status":            count Issue group by status
