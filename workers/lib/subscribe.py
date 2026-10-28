#!/usr/bin/env python3
"""Durable event subscription library (LLD-21 §2) — the generalisation of
`workers/bridge/bridge.py`'s proven exactly-once loop into a reusable,
stdlib-only building block any worker can subscribe with:

    sub = subscribe(node_url, token, subscription_id="coverage-sync",
                     events=["OutOfOffice", "EmployeeDeactivated"],
                     handler=my_handler, cursor_path="coverage_cursor.json")

`handler(fact, act)` is the ONLY custom code a subscriber writes. The library
owns the whole reliability mechanism: `read_events(since=cursor,
kinds=[event.emitted])`, filtering to the subscription's events, calling
`handler(fact, act)` once per matching fact, and persisting the cursor to
disk ONLY after the handler returns without error (save-after-confirm).

**exactly-once (LLD-21 §3, not up for re-litigation).** The guarantee is
at-least-once delivery + an idempotent effect, NOT a durable cursor. `act`
hands the handler a deterministic key, `<subscription_id>|<source_seq>`
(namespaced per subscription so two subscriptions on the same event never
collide), or `<subscription_id>|<source_seq>|<step>` for a handler that makes
several mutating calls per fact. The handler passes this key as the MCP
`idempotency_key` on its own calls; the node's mandatory idempotency (LLD-01
§5) collapses any replay. The cursor is a pure optimisation: crash between
the handler's action and the cursor save, or lose/reset the cursor file
entirely, and the worst case is REPROCESSING the same fact(s) — the
deterministic key makes that a no-op on the node, never a second effect.

**governed vs transient (LLD-21 §4, improves on bridge.py's ad hoc handling,
I-3).** Two, and only two, error classes:
  * transient (network/timeout/node unavailable) — anything that is NOT a
    `GovernedError`. Retried in place, same fact, with a fixed backoff sleep;
    the cursor is untouched because nothing was confirmed. A worker that
    stays down keeps retrying forever — that is correct, it is genuinely
    temporary.
  * governed (`GovernedError`: permission/validation — deterministic, a retry
    cannot change the verdict) — NOT retried. Reported as a `subscription.failed`
    dead-letter event (best-effort: a failure to emit it is swallowed, it must
    never re-introduce the very stall it exists to avoid) and the cursor is
    ADVANCED past the poisoned fact. One bad record no longer wedges the whole
    subscription shut (the bug this library fixes in bridge.py's old ad hoc
    loop, where a rejected emit blocked the cursor forever).

Zero dependencies: stdlib only, same shape as every other kalita worker
(workers/bootstrap.py, workers/connectors/inbound.py, workers/bridge/bridge.py).
"""
import json
import os
import time
import urllib.request
import urllib.error


# Retry/backoff mirrors workers/bootstrap.py and workers/bridge/bridge.py
# exactly (same strategy + constants) — a transient network failure inside
# one MCP call is retried on a fixed sleep before ever surfacing to the
# subscriber loop above it.
RETRY_ATTEMPTS = 30
RETRY_SLEEP_SEC = 2


class GovernedError(Exception):
    """The node rejected the call on its merits (permission/validation). Not
    transient — retrying the same request cannot fix it, so the subscriber
    loop never retries it; it dead-letters and moves on (§4)."""

    def __init__(self, code, message):
        super().__init__(f"{code}: {message}")
        self.code = code
        self.message = message


# --------------------------------------------------------------------------- #
# MCP transport (stdlib urllib) — moved here from bridge.py verbatim; bridge
# now imports it rather than owning a private copy (proof of reuse, §2).
# --------------------------------------------------------------------------- #

class MCPClient:
    """One kalita node reached over its /mcp endpoint under one bearer token."""

    def __init__(self, url, token):
        self.url = url.rstrip("/")
        self.token = token

    def call(self, name, arguments):
        """Invoke one MCP tool. Returns the decoded result payload. Retries a
        transient network failure (bootstrap.py strategy); raises GovernedError
        when the node itself reports the call as an error."""
        body = json.dumps({"jsonrpc": "2.0", "id": 1, "method": "tools/call",
                           "params": {"name": name, "arguments": arguments}}).encode()
        last_err = None
        for attempt in range(RETRY_ATTEMPTS):
            try:
                req = urllib.request.Request(self.url + "/mcp", data=body, headers={
                    "Authorization": "Bearer " + self.token, "Content-Type": "application/json"})
                with urllib.request.urlopen(req, timeout=30) as resp:
                    out = json.load(resp)
                break
            except (urllib.error.URLError, TimeoutError) as ex:
                last_err = ex
                if attempt < RETRY_ATTEMPTS - 1:
                    time.sleep(RETRY_SLEEP_SEC)
        else:
            raise last_err
        res = out.get("result", {})
        content = res.get("content", [])
        payload = json.loads(content[0]["text"]) if content else {}
        if res.get("isError"):
            raise GovernedError(payload.get("code", "REJECTED"),
                                payload.get("message", "call rejected"))
        return payload


# --------------------------------------------------------------------------- #
# Event source: pull event.emitted facts since a cursor, node-wide (LLD-08).
# --------------------------------------------------------------------------- #

class EventSource:
    """Reads `event.emitted` facts from a node past a cursor via the
    node-wide MCP tool `read_events` (LLD-08 §1) — one governed call per page,
    O(new events), not O(records). Requires the subscriber's actor to hold a
    `tail [event.emitted]` grant; everything else on that role can be denied.

    Identical mechanics to bridge.py's original MCPSource (moved here
    unchanged so bridge becomes the library's second consumer, not a fork)."""

    PAGE_LIMIT = 500

    def __init__(self, client):
        self.client = client

    def read_since(self, last_seq):
        """Return event.emitted facts with seq > last_seq, sorted by seq asc.
        Each fact: {seq, name (canonical), payload, entity, record_id}.

        `last_seq` is the subscriber's cursor convention: the seq already
        confirmed handled (exclusive — "give me anything newer"). read_events'
        `since` is INCLUSIVE (LLD-08 §1: next_since = last examined seq + 1),
        so the first page asks for last_seq + 1; every following page reuses
        next_since from the previous response, which is already in that
        inclusive form."""
        facts = []
        cursor = last_seq + 1
        while True:
            res = self.client.call("read_events", {
                "since": cursor, "kinds": ["event.emitted"], "limit": self.PAGE_LIMIT})
            for ev in res.get("events", []):
                p = ev.get("payload") or {}
                subj = ev.get("subject") or {}
                facts.append({"seq": ev.get("seq", 0), "name": p.get("name", ""),
                              "payload": p.get("payload") or {},
                              "entity": subj.get("entity", ""),
                              "record_id": subj.get("record_id", "")})
            next_since = res.get("next_since", cursor)
            if next_since <= cursor:
                break  # cursor stopped advancing: the stream is exhausted
            cursor = next_since
        facts.sort(key=lambda f: f["seq"])
        return facts


# --------------------------------------------------------------------------- #
# Cursor: persistent last-handled source seq.
# --------------------------------------------------------------------------- #

class FileCursor:
    """last_seq persisted to a JSON file. Advanced ONLY after a fact is
    confirmed handled, so a crash before persistence replays (idempotency
    absorbs it, §3)."""

    def __init__(self, path):
        self.path = path

    def load(self):
        try:
            with open(self.path, encoding="utf-8") as f:
                return int(json.load(f).get("last_seq", 0))
        except (FileNotFoundError, ValueError):
            return 0

    def save(self, seq):
        tmp = self.path + ".tmp"
        with open(tmp, "w", encoding="utf-8") as f:
            json.dump({"last_seq": int(seq)}, f)
        os.replace(tmp, self.path)


# --------------------------------------------------------------------------- #
# Act: the deterministic idempotency-key namespace handed to handler().
# --------------------------------------------------------------------------- #

class Act:
    """Passed to `handler(fact, act)` — the only thing beyond the fact itself
    a handler needs from the library: a deterministic idempotency key
    namespaced to (subscription_id, source_seq[, step]) per LLD-21 §3. The
    handler passes `act.idem_key()` (or `act.idem_key(step)` per mutating
    call when it makes several) as the MCP `idempotency_key` on its own
    writes — the node's mandatory idempotency collapses any replay."""

    def __init__(self, subscription_id, source_seq):
        self.subscription_id = subscription_id
        self.source_seq = source_seq

    def idem_key(self, step=None):
        if step is None:
            return f"{self.subscription_id}|{self.source_seq}"
        return f"{self.subscription_id}|{self.source_seq}|{step}"


# --------------------------------------------------------------------------- #
# Dead-letter: governed-failure visibility (LLD-21 §4, I-3).
# --------------------------------------------------------------------------- #

class DeadLetter:
    """Reports a governed (deterministic) handler rejection as a
    `subscription.failed` event — the audit trail an operator's dashboard
    reads (LLD-21 §7, stitched together in PR-4). Emission is idempotent
    itself (`<subscription_id>|<source_seq>|subscription.failed`), so a
    retried report after a crash does not double-emit."""

    def __init__(self, client):
        self.client = client

    def report(self, subscription_id, fact, error):
        # flat scalars only: pack event payloads admit scalars and refs,
        # never nested objects (LLD-01 §4.1)
        payload = {
            "subscription_id": subscription_id,
            "source_seq": fact["seq"],
            "error_code": str(getattr(error, "code", "ERROR")),
            "error_message": str(error),
        }
        key = f"{subscription_id}|{fact['seq']}|subscription.failed"
        self.client.call("emit_event", {
            "name": "subscription.failed", "payload": payload, "idempotency_key": key})


# --------------------------------------------------------------------------- #
# Subscriber — the durable read -> filter -> handle -> advance loop.
# --------------------------------------------------------------------------- #

class Subscriber:
    """The exactly-once-effect loop of LLD-21 §2/§3/§4. `source` provides
    read_since, `cursor` persists last_seq, `handler(fact, act)` is the
    subscriber's only custom code.

    `events` selects which event names this subscription cares about:
      * None            -> every event.emitted fact is handled
      * an iterable      -> matched by exact name or by the short suffix
                            after the last "." (canonical vs short form)
      * a callable        -> called with the fact name; a truthy return
                            (e.g. bridge.py's `_match`, which returns the
                            matched subscription dict or None) means "wanted"

    A fact that is not wanted still advances the cursor (seen, not reprocessed
    forever) but is never handed to `handler`.
    """

    def __init__(self, source, cursor, subscription_id, events, handler,
                 dead_letter=None, retry_sleep_sec=RETRY_SLEEP_SEC, sleep=time.sleep):
        self.source = source
        self.cursor = cursor
        self.subscription_id = subscription_id
        self.events = events
        self.handler = handler
        self.dead_letter = dead_letter
        self.retry_sleep_sec = retry_sleep_sec
        self.sleep = sleep
        if events is None or callable(events):
            self._names = None
        else:
            self._names = set(events)

    def _wanted(self, name):
        if callable(self.events):
            return self.events(name)
        if self._names is None:
            return True
        return name in self._names or name.rsplit(".", 1)[-1] in self._names

    def _handle_fact(self, fact):
        """Invoke handler(fact, act) for one fact, retrying a transient
        failure in place (same fact, cursor untouched) until it either
        succeeds or raises GovernedError (§4). Returns once the fact is
        resolved one way or the other — the cursor is always safe to
        advance past it when this returns."""
        act = Act(self.subscription_id, fact["seq"])
        attempt = 0
        while True:
            try:
                self.handler(fact, act)
                return
            except GovernedError as ex:
                if self.dead_letter is not None:
                    try:
                        self.dead_letter.report(self.subscription_id, fact, ex)
                    except Exception:
                        # best-effort: a failure to emit the dead-letter must
                        # NEVER re-introduce the stall this exists to avoid —
                        # the cursor still advances past the poison fact.
                        pass
                return
            except Exception as ex:
                # transient (network/timeout/node unavailable): retry the
                # SAME fact, cursor untouched, until it resolves. LOUDLY —
                # a handler bug lands here too (deterministic, will never
                # resolve), and a silent busy-loop on one fact is exactly
                # the invisible stall I-3 bans. The log line is what lets an
                # operator (and PR-4's lag heartbeat) tell "node is down"
                # from "this fact never succeeds — go read the handler".
                attempt += 1
                print(f"[subscribe:{self.subscription_id}] seq={fact['seq']} "
                      f"attempt {attempt} failed, retrying: {ex}", flush=True)
                self.sleep(self.retry_sleep_sec)
                continue

    def run_once(self):
        """One read -> filter -> handle -> advance pass over currently
        available facts. Returns the number of facts handed to `handler`
        (matched facts only; unwanted facts still advance the cursor but are
        not counted). The cursor advances after each fact is resolved, so a
        crash mid-pass replays only unconfirmed (or idempotently-absorbed)
        facts on restart."""
        last = self.cursor.load()
        handled = 0
        for fact in self.source.read_since(last):
            if not self._wanted(fact["name"]):
                # not subscribed — still advance past it so we do not rescan
                # it forever (the cursor tracks "seen", the filter tracks
                # "wanted").
                self.cursor.save(fact["seq"])
                continue
            self._handle_fact(fact)
            # resolved one way or another (delivered, or dead-lettered) —
            # only now is it safe to advance the cursor past this seq.
            self.cursor.save(fact["seq"])
            handled += 1
        return handled

    def run_forever(self, poll_sec=2.0):
        """Blocking loop for a real worker process. A failed pass (e.g. the
        source node itself is unreachable) is logged and retried on the next
        poll — it never crashes the worker."""
        while True:
            try:
                n = self.run_once()
                if n:
                    print(f"[subscribe:{self.subscription_id}] handled {n} fact(s)", flush=True)
            except Exception as ex:
                print(f"[subscribe:{self.subscription_id}] pass failed, retrying: {ex}", flush=True)
            time.sleep(poll_sec)


# --------------------------------------------------------------------------- #
# subscribe() — the one-line worker body LLD-21 §2 describes.
# --------------------------------------------------------------------------- #

def subscribe(node_url, token, subscription_id, events, handler, cursor_path,
              dead_letter_client=None, poll_sec=2.0, retry_sleep_sec=RETRY_SLEEP_SEC,
              run=True, sleep=time.sleep):
    """Build (and, by default, run) a durable subscriber against one node.

    `node_url`/`token` reach the node this subscription reads `event.emitted`
    from via `read_events` (the subscriber's own least-privilege `tail` role).
    `handler(fact, act)` mutates wherever it needs to (the same node, a
    different node, an external endpoint) using its OWN MCP client(s)/tokens —
    this library only supplies the reliability loop and the idempotency-key
    namespace, never the handler's write path (that stays the worker's
    least-privilege identity, LLD-21 §2/§5).

    `dead_letter_client` is an optional MCPClient (often the same node) used
    to emit `subscription.failed` on a governed rejection (§4); pass None to
    skip that emission (the cursor still advances past the poison fact
    either way — dead-lettering is an audit nicety, not what makes the
    pipeline unstick).

    Pass `run=False` to get the wired-but-not-started Subscriber back instead
    of blocking — for tests, a worker with its own `--once` mode, or a worker
    that drives several subscriptions interleaved in one process."""
    client = MCPClient(node_url, token)
    source = EventSource(client)
    cursor = FileCursor(cursor_path)
    dead_letter = DeadLetter(dead_letter_client) if dead_letter_client is not None else None
    sub = Subscriber(source, cursor, subscription_id, events, handler,
                     dead_letter=dead_letter, retry_sleep_sec=retry_sleep_sec, sleep=sleep)
    if run:
        sub.run_forever(poll_sec=poll_sec)
    return sub
