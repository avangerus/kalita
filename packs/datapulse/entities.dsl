# Entities for the datapulse pack (MDM-lite / НСИ-аудитор) — ENH-01, Sprint 1
# demo-актив: the governed DATA MODEL only. Matching/dedup/profiling LOGIC is
# explicitly out of scope here (ENH-02 connector, ENH-03 profiling, ENH-04
# dedup workers) — every field below is either operator/agent-set DATA or a
# value StewardshipTask's workflow governs (workflows.dsl); no algorithm
# lives in this file.

entity Source "Source":
    name: string required label="Name"
    kind: enum[Excel, CSV, SQL, API, Manual] default=Excel label="Kind"
    # plain DATA, not a workflow — Source has no lifecycle of its own in this
    # card; ENH-02's connector worker flips this directly via update_record
    # as it works through a file/table (security.dsl: ConnectorAgent's grant
    # is field-scoped to exactly these three fields).
    ingest_status: enum[New, Ingesting, Ingested, Failed] default=New label="Ingest status"
    last_ingest: datetime label="Last ingest"
    record_count: int default=0 label="Record count"
    # ConnectorAgent may set this once, at registration (its `create` grant
    # is unscoped); security.dsl then denies it write access to this field
    # afterwards — a source cannot be silently repointed to a different
    # file/table post-registration. Re-pointing is a DataAdmin decision
    # (DataAdmin holds `full [Source]`).
    path: string required label="Path"

entity MasterRecord "Master record":
    source: ref[Source] required on_delete=restrict label="Source"
    # the raw ingested row: kalita's `json` scalar (internal/dsl/ast.go
    # scalarTypes) holds whatever columns the source had — no explicit-field
    # workaround needed here (see PACK REPORT open questions).
    raw: json required label="Raw data"
    # convenience label for lists/boards; ConnectorAgent fills it from `raw`
    # at ingest time (a display mapping, not a matching/scoring decision).
    display_name: string label="Display name"
    status: enum[Draft, Active, Merged, Archived] default=Draft label="Status"

entity MatchGroup "Match group":
    records: array[ref[MasterRecord]] label="Candidate records"
    score: float label="Match score"
    status: enum[New, ToReview, Resolved, Dismissed] default=New label="Status"

entity StewardshipTask "Stewardship task":
    type: enum[Duplicate, QualityIssue, Conflict] required label="Type"
    match_group: ref[MatchGroup] label="Match group"
    master_record: ref[MasterRecord] label="Master record"
    proposed_action: text label="Proposed action"
    # ExplainerAgent's sole write surface on this entity (security.dsl's
    # field-scoped `update [StewardshipTask.explanation]`) — it narrates WHY,
    # it never decides: `status` moves only through the workflow
    # (workflows.dsl), and no agent role holds any act/approve grant on it.
    explanation: text label="Explanation"
    status: enum[Open, InReview, Approved, Rejected, Merged, Closed] default=Open label="Status"

entity QualityMetric "Quality metric":
    source: ref[Source] required on_delete=cascade label="Source"
    # DERIVED per ENH-01, but the counting RULES (what counts as empty / a
    # format anomaly / a duplicate) are exactly the profiling logic this card
    # must not encode (ENH-03's job) — so these are plain int counters the
    # ProfilerAgent worker computes off-DSL and writes via create/update, not
    # a kalita `computed=` aggregate. See PACK REPORT for the full reasoning
    # (a `computed = count(... where ...)` aggregate can express only a
    # single `reffield = $self` condition today — packs/contracts/entities.dsl
    # BUG-01 — nowhere near enough to express "is this field empty" / "is
    # this a format anomaly" as a query condition, and encoding that logic
    # in the DSL would BE the profiling logic this card must not contain).
    empties: int default=0 label="Empty fields"
    format_anomalies: int default=0 label="Format anomalies"
    duplicates: int default=0 label="Duplicates"
    computed_at: datetime label="Computed at"
