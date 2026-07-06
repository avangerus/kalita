# Roles and permissions — the governance core of datapulse (ENH-01). Four
# AGENT workers propose; only the two HUMAN roles decide. The hard invariant
# under test (agent/datapulse/ENH-01-governed-model.md, the DATAPULSE-PACK-DONE
# deny tests): no agent role holds `act` or `approve` for ANY StewardshipTask
# transition — Approved/Rejected/Merged/Closed are reachable ONLY through a
# human `act` call, and Merged additionally requires a second human signature
# (DataAdmin, separate from Steward) via `requires approval(DataAdmin)`
# (workflows.dsl).
#
# Row-scoping is explicitly DEFERRED (ENH-01 open question #3, resolved by
# the curator): Steward reads every Source/MasterRecord/etc. unscoped, same
# pattern as Legal in packs/contracts/security.dsl — no `where owner = $me`
# anywhere below.

roles:
    DataAdmin
    Steward
    ConnectorAgent agent
    ProfilerAgent  agent
    MatchAgent     agent
    ExplainerAgent agent

permissions:
    # platform/admin role: owns Source configuration end to end (including
    # `path`, which ConnectorAgent may set once at registration but never
    # touch again — see ConnectorAgent's deny below) and is the SECOND human
    # signature on Merged (separate from Steward's day-to-day triage).
    # `deny [delete *]` blocks hard delete even though `full [Source]` would
    # otherwise include it — deny wins over grant (default-deny, deny>allow),
    # same blanket-safety idiom every other pack in this repo uses.
    DataAdmin:
        full    [Source]
        read    [MasterRecord, MatchGroup, QualityMetric, StewardshipTask]
        act     [merge]
        approve [merge]
        deny    [delete *, act [start_review, approve, reject, close]]

    # the human triage role: sees the whole pipeline unscoped, raises tasks,
    # and runs every StewardshipTask transition EXCEPT the merge signature.
    Steward:
        read   [Source, MasterRecord, MatchGroup, QualityMetric, StewardshipTask]
        create [StewardshipTask]
        act    [start_review, approve, reject, close]
        # explicit belt-and-suspenders (packs/contracts/security.dsl's Legal
        # precedent): Steward must never carry a task to Merged alone — that
        # grant exists on DataAdmin only, never here.
        deny   [delete *, act [merge]]

    # ingests a Source and the raw candidate rows it contains (ENH-02).
    # Registers `path` once at creation (create is unscoped); every later
    # touch is limited to the three ingest-progress fields. Never creates a
    # StewardshipTask and never modifies a MasterRecord once ingested —
    # corrections are the steward's job, not the connector's.
    # NOTE: like ProfilerAgent/MatchAgent below, no `deny [act [...]]` for
    # StewardshipTask here — ConnectorAgent holds no POSITIVE grant of any
    # kind on StewardshipTask (only the `create` deny below), and the
    # compiler rejects an `act` deny naming a workflow verb on an entity the
    # role has no grant on at all (E028_UNKNOWN_ACTION) — same lesson as
    # knowvault's BUG-11: a deny with nothing underneath is a false security
    # guarantee, not belt-and-suspenders. Absence of any grant already
    # default-denies every StewardshipTask transition for this role.
    ConnectorAgent:
        read   [Source]
        create [Source, MasterRecord]
        update [Source.ingest_status, Source.last_ingest, Source.record_count]
        deny   [update Source.path, update MasterRecord, create StewardshipTask, delete *]

    # profiles ingested data quality (ENH-03). Reads Source/MasterRecord to
    # compute its counters off-DSL, writes only QualityMetric — never the
    # records it is measuring.
    # NOTE: no `deny [act [...]]` for StewardshipTask here (unlike
    # ConnectorAgent/ExplainerAgent below) — ProfilerAgent holds no grant of
    # any kind on StewardshipTask, so the compiler rejects it as an unknown
    # action for this role (E028_UNKNOWN_ACTION: a deny must reference an
    # entity the role has SOME grant on, same lesson as knowvault's BUG-11 —
    # a deny with nothing underneath is a false security guarantee, not
    # belt-and-suspenders). Absence of any grant already default-denies it.
    ProfilerAgent:
        read   [Source, MasterRecord]
        create [QualityMetric]
        update [QualityMetric]
        deny   [update MasterRecord, delete *]

    # proposes duplicate groups from MasterRecord candidates (ENH-04). Reads
    # the records it groups, creates the group — never rewrites the records
    # themselves; that would be silently resolving the duplicate instead of
    # proposing it for a steward to review. (Same StewardshipTask note as
    # ProfilerAgent above — no grant there, so no deny [act [...]] either.)
    MatchAgent:
        read   [MasterRecord, MatchGroup]
        create [MatchGroup]
        deny   [update MasterRecord, delete *]

    # writes the human-readable narrative on a stewardship task (ENH-06) —
    # WHY a match/quality issue looks the way it does — either on a task
    # Steward/another agent already raised (field-scoped `explanation`
    # update) or by raising the task itself with the explanation attached
    # (create). It never decides: `status` is out of reach both by field
    # scope (explanation is the only field the update grant names) and by
    # explicit deny below.
    ExplainerAgent:
        read   [MatchGroup, MasterRecord, StewardshipTask]
        create [StewardshipTask]
        update [StewardshipTask.explanation]
        deny   [update StewardshipTask.status, delete *, act [start_review, approve, reject, merge, close]]
