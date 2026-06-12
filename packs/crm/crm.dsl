# CRM — a showcase pack: how to run a sales CRM on kalita.
# Demonstrates rich types, a pipeline workflow, forecast arithmetic in computed
# fields, HITL approval on big-discount wins, ABAC (reps see their own deals),
# and pipeline/forecast dashboards. English reference module.

# --- accounts & contacts -----------------------------------------------------

entity Account "Account":
    name:     string required unique label="Name"
    industry: enum[Tech, Finance, Retail, Manufacturing, Public, Other] default=Other label="Industry"
    website:  url label="Website"
    phone:    phone label="Phone"
    owner:    ref[core.User] default=$me label="Owner"

entity Contact "Contact":
    first_name: string required label="First name"
    last_name:  string required label="Last name"
    email:      email label="Email"
    phone:      phone label="Phone"
    account:    ref[Account] on_delete=cascade label="Account"
    is_primary: bool default=false label="Primary contact"
    owner:      ref[core.User] default=$me label="Owner"

# --- leads -------------------------------------------------------------------

entity Lead "Lead":
    number:     serial format="LEAD-{year}-{seq:5}" label="Number"
    name:       string required label="Name"
    company:    string label="Company"
    email:      email label="Email"
    source:     enum[Web, Referral, Event, Outbound, Partner] default=Web label="Source"
    est_value:  money label="Estimated value"
    owner:      ref[core.User] default=$me label="Owner"
    created_at: datetime default=$now label="Created"
    age_days:   int computed = days_since(created_at) label="Age, days"
    status:     enum[New, Working, Qualified, Unqualified, Converted] default=New label="Status"

workflow Lead on status:
    New        -> Working:     start_work label="Start working"
    Working    -> Qualified:   qualify label="Qualify"
    Working    -> Unqualified: disqualify label="Disqualify"
    Qualified  -> Converted:   convert label="Convert to opportunity"
    Unqualified -> Working:    reopen label="Reopen"

# --- opportunities (the pipeline) --------------------------------------------

entity Opportunity "Opportunity":
    number:      serial format="OPP-{year}-{seq:5}" label="Number"
    name:        string required label="Name"
    account:     ref[Account] on_delete=restrict label="Account"
    amount:      money label="Amount"
    probability: percent default=50 label="Probability, %"
    # weighted forecast: amount times win probability (arithmetic in computed)
    forecast:    money computed = amount * probability / 100 label="Forecast"
    discount:    percent default=0 label="Discount, %"
    close_date:  date label="Close date"
    owner:       ref[core.User] default=$me label="Owner"
    stage:       enum[Prospecting, Qualification, Proposal, Negotiation, Won, Lost] default=Prospecting label="Stage"

workflow Opportunity on stage:
    Prospecting   -> Qualification: qualify_opp label="Qualify"
    Qualification -> Proposal:      send_proposal label="Send proposal"
    Proposal      -> Negotiation:   negotiate label="Negotiate"
    # closing a win above a normal discount needs the sales manager's signature
    Negotiation   -> Won:           close_won requires approval(SalesManager) label="Close won"
    Negotiation   -> Lost:          close_lost label="Close lost"
    any           -> Qualification: reopen_opp label="Reopen"

# --- activities ---------------------------------------------------------------

entity Activity "Activity":
    subject:     string required label="Subject"
    kind:        enum[Call, Email, Meeting, Task] default=Call label="Type"
    due:         datetime label="Due"
    done:        bool default=false label="Done"
    opportunity: ref[Opportunity] on_delete=cascade label="Opportunity"
    owner:       ref[core.User] default=$me label="Owner"

# --- roles & permissions -----------------------------------------------------

roles:
    SalesRep
    SalesManager
    Admin

permissions:
    # a rep manages their own pipeline; sees the shared account/contact book
    SalesRep:
        read   [Account, Contact]
        create [Account, Contact, Lead, Opportunity, Activity]
        read Lead where owner = $me
        read Opportunity where owner = $me
        read Activity where owner = $me
        update [Lead, Opportunity, Activity, Account, Contact]
        act    [start_work, qualify, disqualify, convert, reopen, qualify_opp, send_proposal, negotiate, close_lost, reopen_opp]
    # the manager sees the whole pipeline and signs big-discount wins
    SalesManager:
        full    [Account, Contact, Lead, Opportunity, Activity]
        approve [close_won]
        act     [start_work, qualify, disqualify, convert, reopen, qualify_opp, send_proposal, negotiate, close_won, close_lost, reopen_opp]
    Admin:
        full [Account, Contact, Lead, Opportunity, Activity]
        act  [start_work, qualify, disqualify, convert, reopen, qualify_opp, send_proposal, negotiate, close_won, close_lost, reopen_opp]

# --- automation: follow-ups and stale-deal nudges ----------------------------

automation:
    on create Lead:
        notify email(owner)
    on stuck Opportunity in Negotiation for 14d:
        escalate_to SalesManager

# --- dashboards ---------------------------------------------------------------

dashboard Pipeline "Sales pipeline":
    tile "Open opportunities": count Opportunity where stage != Won and stage != Lost
    tile "Pipeline amount":    sum amount Opportunity where stage != Won and stage != Lost
    tile "Weighted forecast":  sum forecast Opportunity where stage != Lost
    tile "Unassigned":         count Opportunity where owner = null
    tile "By stage":           count Opportunity group by stage
    tile "Amount by stage":    sum amount Opportunity group by stage

dashboard LeadFunnel "Lead funnel":
    tile "Working leads": count Lead where status = Working
    tile "By status":     count Lead group by status
    tile "By source":     count Lead group by source
