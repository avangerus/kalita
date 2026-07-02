"""compose.py — the interpreter for `product.mcp:` `compose:` steps (LLD-06
§1). `compose:` is raw, uninterpreted text as far as the DSL compiler is
concerned (internal/dsl/parser_blocks.go's own comment: "This compiler only
captures the declaration as data") — THIS module is the interpreter the LLD
promises: "façade-воркер... исполнит её, читая как pass-through-рецепт под
токеном вызывающего".

Supported step grammar (the subset `packs/*/product_mcp.dsl` actually uses;
see ManifestError below for what happens on anything else — fail LOUD, not a
silent partial execution):

    create <Entity>(<field>: <value>, ...) -> $<var>
    act <action> on $<var>
    query <Entity> [where <cond>] -> $<var>

`<value>` is `$other_var` (a previously bound var — an `in:` param or the
output of an earlier step), a `"quoted string"`, or a bareword (number/
true/false/plain identifier, passed through as a string).

Nothing in this module ever holds or mints a token — see facade.py: every
call this module wants to make is handed BACK to the caller as a
`NodeCall(tool, args)`, and the caller (facade.py, which owns the inbound
request's bearer) is what actually reaches the node. That separation is
intentional and load-bearing for LLD-06 §1 (pass-through auth): this module
could not leak a façade-owned token even if one existed, because it never
sees one.
"""
import re

from manifest import ManifestError


class ComposeError(Exception):
    """A compose step failed at RUNTIME (the node MCP call it issued
    returned isError=true) — carries the node's own structured error payload
    through UNCHANGED (LLD-06 §1 test 2: façade "ничего не расширяет" —
    including not softening or hiding a permission denial)."""

    def __init__(self, step_line, node_error):
        self.step_line = step_line
        self.node_error = node_error
        super().__init__("compose step %r failed: %r" % (step_line, node_error))


_CREATE_RE = re.compile(r'^create\s+([\w.]+)\((.*)\)\s*->\s*\$(\w+)$')
_ACT_RE = re.compile(r'^act\s+(\w+)\s+on\s+\$(\w+)$')
_QUERY_RE = re.compile(r'^query\s+([\w.]+?)(?:\s+where\s+(.+?))?\s*->\s*\$(\w+)$')

_VAR_RE = re.compile(r'^\$(\w+)$')
_STR_RE = re.compile(r'^"(.*)"$')


def _split_args(s):
    """Split a `key: value, key2: value2` arg list on top-level commas (no
    nesting is used by any real compose line today — a bareword/$var/quoted
    string value never itself contains a comma or paren)."""
    parts = [p.strip() for p in s.split(",")]
    return [p for p in parts if p]


def _parse_kv(part, line):
    if ":" not in part:
        raise ManifestError("bad arg %r in compose step %r (expected key: value)" % (part, line))
    k, v = part.split(":", 1)
    return k.strip(), v.strip()


def parse_step(line):
    """Parse one compose line into a step dict. Raises ManifestError (a
    STARTUP-time, fail-fast concern — see manifest.py) for any line outside
    the supported grammar, rather than silently ignoring it."""
    m = _CREATE_RE.match(line)
    if m:
        entity, argstr, out = m.groups()
        args = [_parse_kv(p, line) for p in _split_args(argstr)]
        return {"kind": "create", "entity": entity, "args": args, "out": out, "line": line}
    m = _ACT_RE.match(line)
    if m:
        action, var = m.groups()
        return {"kind": "act", "action": action, "var": var, "line": line}
    m = _QUERY_RE.match(line)
    if m:
        entity, where, out = m.groups()
        return {"kind": "query", "entity": entity, "where": where or "", "out": out, "line": line}
    raise ManifestError(
        "unsupported compose step %r — this façade interprets only "
        "`create Entity(...) -> $x`, `act action on $x`, `query Entity [where ...] -> $x` "
        "(LLD-06 PR-2 scope; extend compose.py's grammar for more verbs)" % line)


def _in_var_types(tool):
    """{param_name: RefEntity} for every `in:` param typed `ref[Entity]` —
    seeds the "what entity does this var refer to" map so `act X on $y` can
    validate/execute even when $y is an INPUT ref, not something a prior
    `create` step bound."""
    out = {}
    for f in tool["in"]:
        mm = re.match(r'^ref\[([\w.]+)\]$', f["type"])
        if mm:
            out[f["name"]] = mm.group(1)
    return out


def validate_tool(tool, entities, actions):
    """Offline, fail-fast validation of one tool's compose (TECHDEBT.md
    "P2 product.mcp's compose — сырой текст", option (a): the façade
    validates at startup). Checks, in order:
      - every compose line parses as a known step form;
      - every `$var` a step READS was declared in `in:` or bound by an
        earlier step (no forward/undeclared references);
      - `create`'s entity is known (from the offline entity scan) and its
        field names are real fields of that entity (best-effort — see
        manifest.py's scan_entities_and_actions docstring on its limits);
      - `act`'s action name is a real transition action for the referenced
        var's entity, when the workflow scan found that entity;
      - `out:` binds to a var some step actually produced.
    Returns nothing; raises ManifestError with a message naming the tool and
    the offending line on the first problem found.
    """
    known = {f["name"] for f in tool["in"]}
    var_entity = _in_var_types(tool)
    for raw in tool["compose"]:
        step = parse_step(raw)
        if step["kind"] == "create":
            entity = step["entity"]
            if entities and entity not in entities:
                raise ManifestError("tool %s: compose step %r references unknown entity %s"
                                     % (tool["name"], raw, entity))
            fields = entities.get(entity, set())
            for k, v in step["args"]:
                if fields and k not in fields:
                    raise ManifestError("tool %s: compose step %r sets unknown field %s.%s"
                                         % (tool["name"], raw, entity, k))
                vm = _VAR_RE.match(v)
                if vm and vm.group(1) not in known:
                    raise ManifestError("tool %s: compose step %r references undeclared $%s "
                                         "(not in `in:` and not bound by an earlier step)"
                                         % (tool["name"], raw, vm.group(1)))
            known.add(step["out"])
            var_entity[step["out"]] = entity
        elif step["kind"] == "act":
            if step["var"] not in known:
                raise ManifestError("tool %s: compose step %r acts on undeclared $%s"
                                     % (tool["name"], raw, step["var"]))
            ent = var_entity.get(step["var"])
            if ent and ent in actions and step["action"] not in actions[ent]:
                raise ManifestError("tool %s: compose step %r — %s is not a known transition action on %s"
                                     % (tool["name"], raw, step["action"], ent))
        elif step["kind"] == "query":
            entity = step["entity"]
            if entities and entity not in entities:
                raise ManifestError("tool %s: compose step %r references unknown entity %s"
                                     % (tool["name"], raw, entity))
            for vm in re.finditer(r'\$(\w+)', step["where"]):
                if vm.group(1) not in known:
                    raise ManifestError("tool %s: compose step %r where-clause references undeclared $%s"
                                         % (tool["name"], raw, vm.group(1)))
            known.add(step["out"])
            var_entity[step["out"]] = entity
    out_var = tool["out"]
    if out_var not in known:
        raise ManifestError("tool %s: out: %s is never produced by any compose step"
                             % (tool["name"], out_var))


class NodeCall:
    """One node MCP tool call a compose step wants made. facade.py executes
    it (under the caller's own bearer) and hands the JSON result back via
    `resolve(...)` — this object never touches a token itself."""

    def __init__(self, tool_name, args):
        self.tool_name = tool_name
        self.args = args


def _resolve_value(v, env):
    vm = _VAR_RE.match(v)
    if vm:
        if vm.group(1) not in env:
            raise ComposeError(v, {"code": "INVALID_PARAMS", "message": "undeclared $%s" % vm.group(1)})
        return env[vm.group(1)]
    sm = _STR_RE.match(v)
    if sm:
        return sm.group(1)
    if v in ("true", "false"):
        return v == "true"
    try:
        return int(v)
    except ValueError:
        pass
    try:
        return float(v)
    except ValueError:
        pass
    return v  # bareword — pass through as a plain string (e.g. an enum value)


def _resolve_where(where, env):
    """Substitute every `$var` in a where-clause with its resolved value, AS
    A QUOTED STRING LITERAL — internal/engine/expr.go's bareword lexer only
    accepts [A-Za-z0-9_$.] (no '-'), so an unquoted UUID (which contains
    hyphens) fails to lex and the whole where-clause fails CLOSED (evalWhere
    returns false on any parse error). Quoting makes every substituted value
    a `"str"` token regardless of its shape — the one substitution that is
    always safe for the node's condition language."""
    def sub(m):
        var = m.group(1)
        if var not in env:
            raise ComposeError(where, {"code": "INVALID_PARAMS", "message": "undeclared $%s" % var})
        val = env[var]
        return '"%s"' % str(val).replace('"', '\\"')
    return re.sub(r'\$(\w+)', sub, where)


def run_tool(tool, in_values, call_node, idem_key_fn):
    """Execute `tool`'s compose steps in order. `call_node(name, args,
    step_line)` must return the node's decoded tool-result payload and raise
    ComposeError itself on isError — see facade.py's `_call_node` for the
    actual HTTP leg (that is where the caller's bearer is attached; this
    module never sees a token). `idem_key_fn(step_index)` returns a fresh
    idempotency_key for one mutating step. Returns
    `(value, entity_of_out_binding_or_None)` — the entity name lets
    facade.py attach a perimeter-flag comment (see facade.py `_maybe_flag`)
    to whatever record a tool's `out:` binding names, without this module or
    facade.py hardcoding any particular tool/entity name.

    `in_values` have ALREADY been validated as present for every required
    `in:` field by facade.py before this is called (basic input-shape
    checking belongs to the tool boundary, not the interpreter)."""
    env = dict(in_values)
    var_entity = _in_var_types(tool)
    for idx, raw in enumerate(tool["compose"]):
        step = parse_step(raw)
        if step["kind"] == "create":
            values = {k: _resolve_value(v, env) for k, v in step["args"]}
            basis = {"type": "human", "id": "product_mcp:" + tool["name"]}
            res = call_node("create_record", {
                "entity": step["entity"], "values": values, "basis": basis,
                "idempotency_key": idem_key_fn(idx)}, raw)
            env[step["out"]] = res
            var_entity[step["out"]] = step["entity"]
        elif step["kind"] == "act":
            target = env[step["var"]]
            rid = target["id"] if isinstance(target, dict) else target
            entity = var_entity.get(step["var"])
            basis = {"type": "human", "id": "product_mcp:" + tool["name"]}
            res = call_node("act", {
                "entity": entity, "id": rid, "action": step["action"], "basis": basis,
                "idempotency_key": idem_key_fn(idx)}, raw)
            # the acted-on record itself keeps its earlier binding (act's
            # response is a transition receipt, not the record) unless the
            # var was never bound to a record — defensive, not hit today.
        elif step["kind"] == "query":
            args = {"entity": step["entity"]}
            if step["where"]:
                args["where"] = _resolve_where(step["where"], env)
            res = call_node("query", args, raw)
            env[step["out"]] = res.get("records", [])
            var_entity[step["out"]] = step["entity"]
    out_var = tool["out"]
    return env[out_var], var_entity.get(out_var)
