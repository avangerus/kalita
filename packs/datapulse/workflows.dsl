# StewardshipTask is the ONLY workflow-governed entity in this pack (ENH-01):
# Source/MasterRecord/MatchGroup/QualityMetric are plain governed DATA that
# agent workers move through role-scoped create/update (security.dsl) — they
# have no lifecycle of their own, so no separate workflow block for them.

workflow StewardshipTask on status:
    Open     -> InReview: start_review  label="Start review"
    InReview -> Approved: approve       label="Approve"
    InReview -> Rejected: reject        label="Reject"
    # the hard governance invariant (ENH-01): only a human holding the
    # DataAdmin role can complete a merge — DataAdmin is the ONLY role that
    # holds act[merge]/approve[merge] (security.dsl); Steward, who runs every
    # other transition below, is explicitly denied act[merge]; no agent role
    # holds act or approve on ANY StewardshipTask transition at all.
    InReview -> Merged:    merge requires approval(DataAdmin) label="Merge (second signature)"
    any      -> Closed:   close         label="Close"
