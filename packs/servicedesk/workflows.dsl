# Workflow state machines for the servicedesk pack.

# --- problems and known errors -----------------------------------------------

workflow Problem on status:
    New                 -> Investigating:       investigate_problem label="Investigate"
    Investigating       -> RootCauseIdentified: identify_rca label="Root cause found"
    RootCauseIdentified -> KnownErrorState:     register_known_error label="Register as known error"
    KnownErrorState     -> Resolved:            resolve_problem label="Resolve"
    Resolved            -> Closed:              close_problem label="Close"

# --- knowledge base (FTS native in kalita — search on text/string) --------

workflow KBArticle on status:
    Draft     -> Published: publish_article requires approval(KbEditor) label="Publish"
    Published -> Archived:  archive_article label="Archive"
    Archived  -> Draft:     revise_article label="Return to draft"

# --- incidents ---------------------------------------------------------------

workflow Incident on status:
    New           -> Investigating: investigate assignee=OperatorL2 label="Investigate"
    Investigating -> Identified:    identify label="Cause identified"
    Identified    -> Resolved:      resolve_incident label="Resolve"
    Resolved      -> Closed:        close_incident label="Close"
    Resolved      -> Investigating: reopen_incident label="Reopen"
    New           -> Closed:        auto_close when source = Tivoli label="Auto-close"

# --- service requests from catalog -------------------------------------------

workflow ServiceRequest on status:
    Submitted       -> ApprovalPending: require_approval when approval_required = true label="Require approval"
    Submitted       -> Fulfilling:      auto_approve when approval_required = false label="Start fulfillment"
    ApprovalPending -> Approved:        approve_request requires approval(Supervisor) label="Approve"
    ApprovalPending -> Rejected:        reject_request requires approval(Supervisor) label="Reject"
    Approved        -> Fulfilling:      start_fulfillment label="Start fulfillment"
    Fulfilling      -> Fulfilled:       fulfill label="Fulfilled"
    Fulfilled       -> Closed:          close_request label="Close"

# --- change requests (RFC) with CAB approval --------------------------------

workflow Change on status:
    Draft        -> Assessment:   submit_change label="Submit for assessment"
    Assessment   -> CabApproval:  request_cab label="Request CAB approval"
    CabApproval  -> Approved:     approve_change requires approval(ChangeManager) label="Approve (CAB)"
    CabApproval  -> Rejected:     reject_change requires approval(ChangeManager) label="Reject"
    Approved     -> Scheduled:    schedule_change label="Schedule"
    Scheduled    -> Implementing: implement_change label="Implement"
    Implementing -> Review:       complete_change label="Complete"
    Review       -> Closed:       close_change label="Close"

# --- work orders (assignments) -----------------------------------------------

workflow WorkOrder on status:
    Created    -> Assigned:   assign_wo label="Assign"
    Assigned   -> InProgress: start_wo label="Start"
    InProgress -> Completed:  complete_wo label="Complete"
    Created    -> Cancelled:  cancel_wo label="Cancel"
