# Roles and permissions for the assistant pack (LLD-06 §2). One rule, stated
# once in the LLD and enforced here exactly: `where owner = $me` /
# `where session.owner = $me` — a chat participant sees and drives only
# their OWN sessions/messages. This is the SAME Permission Engine every other
# pack uses (`internal/engine/permissions.go`); the chat surface gets no
# special-cased ABAC of its own (LLD-06 §1: "façade добавляет СЕМАНТИКУ...
# но НОЛЬ полномочий" applies identically to chat_runner under §2).
#
# `create` has no row to test a `where` against yet (engine.can: a nil record
# makes any conditional allow fail to match — see internal/engine/
# permissions.go `can`), so Session/Message creation is granted UNSCOPED like
# every other owner-scoped pack in this repo (crm.dsl SalesRep, hr.dsl
# Employee, servicedesk.dsl LkpUser: create unscoped + read/update `where
# owner = $me`) — `owner: ref[core.User] default=$me` on Session fills the
# owner from the caller's own actor id unless the caller explicitly passes a
# different id in `values`, which the create-time gate cannot see or block
# (documented as a residual, PRE-EXISTING class gap shared with every
# owner-scoped pack already in this repo, not introduced here — see
# docs/TECHDEBT.md "Ядро: identity / HITL" appendix note added alongside
# this pack).
roles:
    User
    # supervisory role — retention/cleanup and the "undo a chat write" case
    # (LLD-06 §3 test 6: Message "поддерживает undo"). `revert_record`/
    # `rewind_record` (internal/engine/revert.go `hasFull`) require an
    # UNCONDITIONAL `full` grant on the entity by kernel rule, not something
    # this pack can weaken to `where owner = $me` — undo of a chat message is
    # a supervisory action, same as undo anywhere else in kalita, not a
    # chat-specific mechanism.
    Admin

permissions:
    User:
        create [Session, Message]
        read   Session where owner = $me
        update Session where owner = $me
        read   Message where session.owner = $me
        # a chat participant does not edit history after the fact — only the
        # kernel's own revert/rewind (a supervisory `full` action) may amend
        # a sent message; ordinary users create and read, never rewrite.
    Admin:
        full [Session, Message]
