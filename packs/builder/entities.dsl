# builder — the conversational design session for the self-building product
# (product mission B, CONVERSATIONAL_BUILDER.md §2). A DesignSession is
# governed data like any other pack: the worker (workers/builder) writes these
# records through the SAME MCP create_record calls under the CALLER'S bearer
# that any product write uses — the precedent is packs/assistant +
# workers/chat_runner (LLD-06 §2). Every turn of the design conversation, every
# confirmed/rejected decision and the session status are journaled events with
# ABAC scoping; the transient working state (raw model JSON, validation error
# list) intentionally lives ONLY in the worker and never reaches the journal.
#
# Correlation (docs/builder/ARCHITECTURE.md §7): the proposal created at phase
# APPROVE carries this session's id in its description, so the journal chain
# user request -> session messages/decisions -> DefinitionProposed -> applied
# is reconstructible end to end.
#
# Statement classification (docs/builder/CONVERSATIONAL_BUILDER.md §3): SessionMessage.kind
# is the label the Expert assigns to each statement. An LLM assumption is
# NEVER journaled as fact/decision without user confirmation — the builder
# protocol enforces this, and the enum values below are the vocabulary the UI
# renders provenance badges from.

entity DesignSession "Design session":
    owner:       ref[core.User] default=$me label="Owner"
    goal:        text label="Goal"
    framework:   string label="Framework"
    mode:        enum[create, change] default=create label="Mode"
    status:      enum[open, modeling, validating, proposed, applied, rejected, abandoned] default=open label="Status"
    proposal_id: string label="Proposal"
    def_before:  int label="Definition version before"
    def_after:   int label="Definition version after"
    created_at:  datetime default=$now label="Started"
    updated_at:  datetime default=$now label="Updated"

entity SessionMessage "Design session message":
    session:    ref[DesignSession] required on_delete=cascade label="Session"
    author:     enum[user, expert] required label="Author"
    kind:       enum[fact, preference, answer, goal, decision, question, recommendation, risk, required_decision, explanation, validation_error, proposal_summary, simulation, status] required label="Kind"
    topic:      string label="Topic"
    body:       text required label="Body"
    provenance: string label="Provenance"
    turn:       int label="Turn"
    created_at: datetime default=$now label="Sent"

entity DesignDecision "Design decision":
    session:    ref[DesignSession] required on_delete=cascade label="Session"
    topic:      string required label="Topic"
    decision:   text required label="Decision"
    status:     enum[accepted, rejected] required label="Status"
    rationale:  text label="Rationale"
    decided_by: ref[core.User] default=$me label="Decided by"
    created_at: datetime default=$now label="Decided"
