# Entities and links for the servicedesk pack.

# --- reference data / CMDB --------------------------------------------------

entity Service "Service":
    code: string required unique
    name: string required
    description: text
    category: string
    request_form_schema: json          # request form schema (as in spec JSONB)
    published: bool default=false

entity ConfigItem "Configuration Item":
    external_id: string required unique # ID from 1C
    name: string required
    ci_type: enum[Software, Hardware, ServiceCI, License, Document, Other] default=Other
    status: enum[Active, Retired, Planned] default=Active
    attributes: json
    owner: ref[core.User]
    source: enum[Sync1C, Manual] default=Manual

entity SLAPolicy "SLA Policy":
    name: string required
    applies_to: enum[Incident, ServiceRequest, Change, Ticket] default=Incident
    priority: enum[P1, P2, P3, P4] default=P3
    response_minutes: int
    resolution_minutes: int
    # each policy picks its working calendar (regional production calendars differ)
    calendar: ref[core.Calendar] label="Calendar"

# --- problems and known errors -----------------------------------------------

entity Problem "Problem":
    number: serial format="PRB-{year}-{seq:6}"
    title: string required
    description: text
    root_cause: text
    priority: enum[P1, P2, P3, P4] default=P3
    assignee: ref[core.User]
    opened: datetime default=$now
    age_days: int computed = days_since(opened)
    status: enum[New, Investigating, RootCauseIdentified, KnownErrorState, Resolved, Closed] default=New

entity KnownError "Known Error":
    problem: ref[Problem] on_delete=cascade
    symptoms: text
    workaround: text
    permanent_fix: text
    kb_article: ref[KBArticle]
    status: enum[Active, Resolved] default=Active

# --- knowledge base (FTS native in kalita — search on text/string) --------

entity KBArticle "KB Article":
    title: string required
    body: text
    category: string
    tags: array[string]
    author: ref[core.User] default=$me
    updated: datetime default=$now
    status: enum[Draft, Published, Archived] default=Draft

# --- incidents ---------------------------------------------------------------

entity Incident "Incident":
    number: serial format="INC-{year}-{seq:6}" label="Number"
    title: string required label="Subject"
    description: text label="Description"
    priority: enum[P1, P2, P3, P4] default=P3 label="Priority"
    impact: enum[High, Medium, Low] default=Medium label="Impact"
    urgency: enum[High, Medium, Low] default=Medium label="Urgency"
    source: enum[Manual, Tivoli, Email, Portal] default=Manual label="Source"
    ci: ref[ConfigItem] label="Configuration item"
    problem: ref[Problem] label="Problem"
    attachments: array[file] label="Attachments"          # screenshots + logs
    reporter: ref[core.User] default=$me label="Reporter"
    assignee: ref[core.User] label="Assignee"
    sla_policy: ref[SLAPolicy] label="SLA policy"
    opened: datetime default=$now label="Opened"
    resolved_at: datetime label="Resolved"
    age_days: int computed = days_since(opened) label="Age, days"
    # live SLA: minutes elapsed since opening and minutes remaining before threshold
    # from related policy (ref path sla_policy.resolution_minutes). Negative
    # sla_left = SLA breached.
    # SLA counts WORKING minutes (8x5 via the node's business calendar), not wall
    # clock — a ticket opened Friday evening is not "breached" by Monday morning
    minutes_open: int computed = business_minutes_since(opened, sla_policy.calendar) label="In progress, min"
    sla_left:     int computed = sla_policy.resolution_minutes - business_minutes_since(opened, sla_policy.calendar) label="Time to SLA, min"
    status: enum[New, Investigating, Identified, Resolved, Closed] default=New label="Status"

# incident duplicates — bidirectional link, synchronized at runtime
link Incident -> Incident as duplicates / duplicated_by

# --- service requests from catalog -------------------------------------------

entity ServiceRequest "Service Request":
    number: serial format="SR-{year}-{seq:6}"
    service: ref[Service] on_delete=restrict
    requester: ref[core.User] default=$me
    form_data: json
    approval_required: bool default=false
    opened: datetime default=$now
    status: enum[Submitted, ApprovalPending, Approved, Rejected, Fulfilling, Fulfilled, Closed] default=Submitted

# --- change requests (RFC) with CAB approval --------------------------------

entity Change "Change Request":
    number: serial format="CHG-{year}-{seq:6}"
    title: string required
    description: text
    change_type: enum[Standard, Normal, Emergency] default=Normal
    risk: enum[Low, Medium, High] default=Medium
    requester: ref[core.User] default=$me
    cab_approved_by: ref[core.User]
    planned_start: datetime
    planned_end: datetime
    affected_ci: array[ref[ConfigItem]]
    status: enum[Draft, Assessment, CabApproval, Approved, Scheduled, Implementing, Review, Closed, Rejected] default=Draft

# --- work orders (assignments) -----------------------------------------------
# Spec: polymorphic parent (parent_type + parent_id). kalita has no polymorphic
# refs (gap in APPS-GAP-PLAN), so we model with explicit refs +
# parent_type discriminator. One ref is populated by type.

entity WorkOrder "Work Order":
    number: serial format="WO-{year}-{seq:6}" label="Number"
    parent_type: enum[Incident, ServiceRequest, Change, Problem] default=Incident label="Source type"
    incident: ref[Incident] label="Source incident"
    request: ref[ServiceRequest] label="Source request"
    change: ref[Change] label="Source change"
    problem: ref[Problem] label="Source problem"
    assignee: ref[core.User] label="Assignee"
    due: datetime label="Due"
    result: text label="Result"
    status: enum[Created, Assigned, InProgress, Completed, Cancelled] default=Created label="Status"
