# Site — the federation demo's source pack (LLD-07 §6). A landing-page node
# captures leads; a `capture` transition emits the domain event LeadCaptured.
# The federation bridge (workers/bridge) reads that event.emitted from this
# node's journal and re-emits LeadMirrored on the CRM node, where a subscriber
# creates a MirroredLead carrying its origin — the cross-node mirror WRITE of
# LLD-07 §3 (no remote lookup, no sync-RPC between the nodes).
#
# One self-contained pack plays BOTH roles so the demo can load it on both
# nodes without touching the crm showcase (its golden/dogfood fixtures stay
# frozen). Node A uses the source half (Lead + capture + LeadCaptured); node B
# uses the receiver half (LeadMirrored + on event -> MirroredLead).

# --- source half (node A) ----------------------------------------------------

entity Lead "Lead":
    include core.Named
    company: string label="Company"
    email:   email label="Email"
    status:  enum[New, Captured] default=New label="Status"

event LeadCaptured "Lead captured on the site":
    name:    string
    company: string
    email:   email

workflow Lead on status:
    New -> Captured: capture label="Capture" emits LeadCaptured(name = name, company = company, email = email)

# --- receiver half (node B) --------------------------------------------------
# origin provenance (LLD-07 §3): two plain string fields, NOT a new core type —
# origin_node names the source node, origin_id is the source record id. This is
# the whole of "cross-node ref v1" for the mirror; DSL `remote[...]` stays
# reserved (layer 3).

entity MirroredLead "Mirrored lead":
    include core.Named
    company:     string label="Company"
    email:       email label="Email"
    origin_node: string label="Origin node"
    origin_id:   string label="Origin id"
    mirrored_at: datetime default=$now label="Mirrored"

event LeadMirrored "Lead mirrored from another node":
    name:        string
    company:     string
    email:       email
    origin_node: string
    origin_id:   string

# --- roles & permissions -----------------------------------------------------
# least privilege, split per bridge identity (LLD-07 §1, LLD-08 §4): the
# source-side actor may only TAIL event.emitted (node-wide, via read_events —
# no per-record read at all); the target-side actor may only EMIT the mirror
# event. Neither can do anything else — the whole point of the exactly-once
# boundary.

roles:
    Operator agent
    BridgeReader agent
    BridgeEmitter agent

permissions:
    # the site operator files leads and captures them (drives the source half);
    # on the CRM node the same role reads the mirrored leads to verify the demo
    Operator:
        create [Lead]
        read   [Lead, MirroredLead]
        act    [capture]
        deny   [delete *]
    # source-side bridge identity (LLD-08 §4): tail ONLY event.emitted, node-
    # wide via read_events — no per-record read grant at all. This is the
    # least-privilege surface the outbox primitive was built for: the bridge
    # sees the fact stream, never the records themselves.
    BridgeReader:
        tail [event.emitted]
        deny [delete *, create *, update *, act *, emit *, read *]
    # target-side bridge identity: emit ONLY the mirror event, nothing more
    BridgeEmitter:
        emit [LeadMirrored]
        deny [delete *, create *, update *, act *, read *]

# --- automation: the receiver subscriber (node B) ----------------------------
# the mirror WRITE: when LeadMirrored fires (the bridge emitted it), create the
# local MirroredLead from the payload, stamping origin from the delivered
# fields. Idempotency of the emit (LLD-07 §2) guarantees one mirror per event.

automation:
    on event LeadMirrored:
        create MirroredLead(name = name, company = company, email = email, origin_node = origin_node, origin_id = origin_id)
