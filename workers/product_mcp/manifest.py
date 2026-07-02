"""manifest.py — reads a pack directory's `product.mcp:` section (LLD-06 §1)
plus a light scan of its `entity`/`workflow` blocks, entirely OFFLINE (no
node connection, no bearer token needed): the façade must be able to
fail-fast validate its own tool registry at STARTUP, before any caller's
bearer even exists (docs/TECHDEBT.md "P2 product.mcp's compose — сырой,
неинтерпретируемый компилятором текст" — this module is option (a) named
there: the façade validates compose itself, fail-fast, instead of waiting for
the first call to blow up).

This is NOT a DSL compiler. It reads exactly the subset of the grammar
`internal/dsl/parser_blocks.go` already defines for `product.mcp:`
(productMCP/productMCPTool) plus enough of `entity`/`workflow` syntax to
name-check compose steps — nothing here decides pack semantics; the real
compiler (`internal/dsl`) remains the single source of truth for whether a
pack COMPILES. This module only decides whether the façade's OWN reading of
`product.mcp:` is internally consistent enough to serve.

Zero dependencies: stdlib only, matching every other worker in this repo.
"""
import os
import re


class ManifestError(Exception):
    """Raised when a pack directory does not contain a well-formed
    `product.mcp:` section, or a tool's `compose:` fails offline validation
    (undeclared $var, out: binding never produced, unknown entity/field/
    action referenced) — fail LOUD at facade startup, not on the first call
    a real user makes (see module docstring)."""


def _strip_comment(line):
    """Drop a `#` end-of-line comment, respecting quoted strings (LLD-06's
    own example manifest has none, but pack sources elsewhere do)."""
    out = []
    in_str = False
    for ch in line:
        if ch == '"':
            in_str = not in_str
        if ch == "#" and not in_str:
            break
        out.append(ch)
    return "".join(out)


def _indent(line):
    return len(line) - len(line.lstrip(" "))


def _read_pack_files(pack_dir):
    files = {}
    for name in sorted(os.listdir(pack_dir)):
        if name.endswith(".dsl"):
            with open(os.path.join(pack_dir, name), "r", encoding="utf-8") as fh:
                files[name] = fh.read()
    if not files:
        raise ManifestError("no .dsl files in pack directory %r" % pack_dir)
    return files


# --- product.mcp: -----------------------------------------------------------

_TOOL_HEADER = re.compile(r'^tool\s+([\w.]+)\s+"([^"]*)"\s*:\s*$')
_FIELD_LINE = re.compile(r'^([\w]+)\s*:\s*(ref\[[\w.]+\]|[\w]+)\s*(required)?')
_OUT_LINE = re.compile(r'^out\s*:\s*(\S+)\s*$')


def parse_product_mcp(pack_dir):
    """Return the list of tool dicts declared under `product.mcp:` across
    every .dsl file in `pack_dir` (LLD-06 §1: the tool registry is pack DATA,
    read here exactly once, generically — no product name is hardcoded
    anywhere in this function).

    Each tool: {"name": str, "desc": str, "in": [{"name","type","required"}],
    "compose": [str, ...], "out": str}. Returns [] if no pack file declares a
    `product.mcp:` block (a product with zero façade tools is valid — the
    façade just serves an empty tool list for it).
    """
    tools = []
    for fname, src in _read_pack_files(pack_dir).items():
        lines = [_strip_comment(l.rstrip("\n")) for l in src.split("\n")]
        i = 0
        n = len(lines)
        while i < n:
            raw = lines[i]
            if raw.strip() == "product.mcp:" and _indent(raw) == 0:
                i = _parse_product_mcp_block(lines, i + 1, fname, tools)
            else:
                i += 1
    return tools


def _parse_product_mcp_block(lines, i, fname, tools):
    n = len(lines)
    while i < n:
        raw = lines[i]
        if raw.strip() == "":
            i += 1
            continue
        ind = _indent(raw)
        if ind == 0:
            break  # dedent out of product.mcp:
        m = _TOOL_HEADER.match(raw.strip())
        if not m:
            raise ManifestError("%s:%d: expected `tool <name> \"desc\":`, got %r" % (fname, i + 1, raw))
        tool_indent = ind
        tool = {"name": m.group(1), "desc": m.group(2), "in": [], "compose": [], "out": None,
                "file": fname, "line": i + 1}
        i += 1
        while i < n and (lines[i].strip() == "" or _indent(lines[i]) > tool_indent):
            bl = lines[i]
            if bl.strip() == "":
                i += 1
                continue
            bind = _indent(bl)
            head = bl.strip()
            if head == "in:":
                i += 1
                while i < n and (lines[i].strip() == "" or _indent(lines[i]) > bind):
                    fl = lines[i].strip()
                    if fl:
                        fm = _FIELD_LINE.match(fl)
                        if not fm:
                            raise ManifestError("%s:%d: bad `in:` field %r" % (fname, i + 1, fl))
                        tool["in"].append({"name": fm.group(1), "type": fm.group(2),
                                            "required": bool(fm.group(3))})
                    i += 1
            elif head == "compose:":
                i += 1
                while i < n and (lines[i].strip() == "" or _indent(lines[i]) > bind):
                    cl = lines[i].strip()
                    if cl:
                        tool["compose"].append(cl)
                    i += 1
            elif head.startswith("out:") or head.startswith("out "):
                om = _OUT_LINE.match(head)
                if not om:
                    raise ManifestError("%s:%d: bad `out:` line %r" % (fname, i + 1, head))
                tool["out"] = om.group(1)
                i += 1
            else:
                raise ManifestError("%s:%d: unknown tool field %r (expected in:/compose:/out:)" % (fname, i + 1, head))
        if not tool["compose"]:
            raise ManifestError("%s:%d: tool %s has no compose steps" % (fname, tool["line"], tool["name"]))
        if not tool["out"]:
            raise ManifestError("%s:%d: tool %s has no out: binding" % (fname, tool["line"], tool["name"]))
        tools.append(tool)
    return i


# --- entity / workflow name scan (for offline compose validation only) -----

_ENTITY_HEADER = re.compile(r'^entity\s+([\w.]+)\b')
_WORKFLOW_HEADER = re.compile(r'^workflow\s+([\w.]+)\s+on\s+\w+\s*:\s*$')
# a transition line: `From -> To: action_name [modifiers...]`
_TRANSITION_LINE = re.compile(r'^\S+\s*->\s*\S+\s*:\s*(\w+)')


def scan_entities_and_actions(pack_dir):
    """A light, best-effort scan of every entity's field names and every
    workflow's action names in `pack_dir` — enough for compose.py's offline
    ref-integrity check (does `create Contract(title: ...)` name a real field?
    does `act send_to_legal` name a real transition?). NOT a full parse: field
    TYPES, computed fields, and cross-file entity extension are not resolved;
    good enough to catch typos and dangling references, which is what
    fail-fast startup validation is for (the real compiler is still the
    authority on whether the pack itself compiles).

    Returns (entities: {name: set(field_names)}, actions: {name: set(action_names)}).
    """
    entities = {}
    actions = {}
    for fname, src in _read_pack_files(pack_dir).items():
        lines = [_strip_comment(l.rstrip("\n")) for l in src.split("\n")]
        n = len(lines)
        i = 0
        while i < n:
            raw = lines[i]
            stripped = raw.strip()
            em = _ENTITY_HEADER.match(stripped) if _indent(raw) == 0 else None
            if em:
                name = em.group(1)
                fields = entities.setdefault(name, set())
                base_indent = _indent(raw)
                i += 1
                while i < n and (lines[i].strip() == "" or _indent(lines[i]) > base_indent):
                    fl = lines[i].strip()
                    if fl:
                        fm = _FIELD_LINE.match(fl)
                        if fm:
                            fields.add(fm.group(1))
                    i += 1
                continue
            wm = _WORKFLOW_HEADER.match(stripped) if _indent(raw) == 0 else None
            if wm:
                name = wm.group(1)
                acts = actions.setdefault(name, set())
                base_indent = _indent(raw)
                i += 1
                while i < n and (lines[i].strip() == "" or _indent(lines[i]) > base_indent):
                    tl = lines[i].strip()
                    if tl:
                        tm = _TRANSITION_LINE.match(tl)
                        if tm:
                            acts.add(tm.group(1))
                    i += 1
                continue
            i += 1
    return entities, actions
