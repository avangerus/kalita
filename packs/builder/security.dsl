# Roles and permissions for the builder pack (product mission B). Same shape
# as packs/assistant/security.dsl (LLD-06 §2): `where owner = $me` /
# `where session.owner = $me` — a participant sees and drives only their OWN
# design sessions. The builder worker runs pass-through under the caller's
# bearer (workers/chat_runner precedent): it needs no grants of its own, and
# the journal attributes every session write to the human.
#
# `create` is granted UNSCOPED for the same documented reason as the assistant
# pack (engine.can: a nil record makes any conditional allow fail to match) —
# `owner: ref[core.User] default=$me` fills the owner from the caller's actor
# id; this is the PRE-EXISTING owner-scoped pack class gap shared with
# packs/assistant, packs/crm, packs/hr, packs/servicedesk (see docs/TECHDEBT.md).
#
# Note on authority: this pack governs the design CONVERSATION only. Applying a
# model is gated by the kernel's definition pipeline (internal/engine/
# proposals.go: human + defApprover role + signature), which no grant here can
# widen. The builder has no path to authority through this pack.

roles:
    ProcessOwner
    # supervisory role — cleanup/retention and undo of a session write
    # (revert_record requires an unconditional `full` grant by kernel rule)
    Admin

permissions:
    ProcessOwner:
        create [DesignSession, SessionMessage, DesignDecision]
        read   DesignSession where owner = $me
        update DesignSession where owner = $me
        read   SessionMessage where session.owner = $me
        read   DesignDecision where session.owner = $me
        # a participant does not edit history after the fact — only the
        # kernel's supervisory revert/rewind may amend a recorded turn.
    Admin:
        full [DesignSession, SessionMessage, DesignDecision]
