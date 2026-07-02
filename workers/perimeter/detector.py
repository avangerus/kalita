#!/usr/bin/env python3
"""Perimeter detector — heuristics for "instructions in data" (prompt
injection) in ingested content.

Spec: docs/lld/LLD-01-journal-mechanics.md §6, docs/HLD-V1.md §H4
(LLM-perimeter, layer 3 of 4: "ingest-detection"). Word list v1, en+ru.

    score(text) -> (score: float in [0, 1], reasons: list[str])

score >= THRESHOLD (0.5) is the "flag it" line; the caller (a Go-side ingest
path, not yet wired — see LLD-01 §6 last line: "Ядро знает только kind
ingest.flagged") is expected to Append `ingest.flagged{source, reasons,
score}` and mark the source record `suspicious: true`. This module does none
of that itself — it is a pure, side-effect-free library: no I/O, no journal
writes, no network.

This is a tripwire, not a filter — it NEVER blocks. False positives on
legalese and software manuals are expected and accepted (HLD-V1 §H4): the
real backstops are permissions (§H4 layer 1, independent of prompt content)
and the structural prompt isolation in workers/agent_runner/promptbox.py
(§H4 layer 2). This module only raises a signal for a human/task to see.

Rule table (RULES below) is the whole word list: every row is
(id, weight, reason, pattern). `score` sums the weights of every rule that
matches (capped at 1.0) and returns the de-duplicated `reason` tags of the
rules that fired, in table order. There is no ML here on purpose — the table
is the entire, auditable word list.

Zero dependencies: stdlib only (re, unicodedata).
"""
import re
import unicodedata
from collections import namedtuple

THRESHOLD = 0.5

# Zero-width / invisible codepoints sometimes spliced mid-word to break up a
# trigger phrase for a naive substring/regex match (e.g. "ign<ZWSP>ore"). Kept
# as plain int codepoints (not string literals) so nothing invisible sits in
# this source file itself. U+200B ZERO WIDTH SPACE, U+200C ZERO WIDTH
# NON-JOINER, U+200D ZERO WIDTH JOINER, U+FEFF ZERO WIDTH NO-BREAK SPACE/BOM,
# U+2060 WORD JOINER.
_ZERO_WIDTH_CODEPOINTS = (0x200B, 0x200C, 0x200D, 0xFEFF, 0x2060)

# MCP tool names, read verbatim off internal/mcp/tools.go `toolDefs` (do not
# invent names here — if tools.go adds/renames a tool, update this list; the
# corpus test cross-checks this list is a subset of tools.go's names).
#
# Split in two: snake_case names contain an underscore, which essentially
# never occurs in ordinary English/Russian prose — safe to flag on sight with
# a real weight. The single-word names below (query, act, comment, dashboard)
# are ordinary dictionary words in both languages, so they are only treated as
# a signal in exact call syntax (name immediately followed by "(", no space,
# exact case) and at a lower weight — e.g. "act(" must not fire on "the
# Data Protection Act (2018)" (capitalized, space before the parenthesis).
MCP_TOOLS_SNAKE = (
    "describe_system", "describe_entity", "get_grammar", "get_record",
    "create_record", "update_record", "revert_record", "rewind_record",
    "diff_record", "list_my_tasks", "wait_for_task", "take_task",
    "report_progress", "complete_task", "fail_task", "read_comments",
    "read_journal", "compose_pack", "field_types", "validate_dsl",
    "propose_change", "get_proposal", "list_dashboards",
)
MCP_TOOLS_PLAIN = ("query", "act", "comment", "dashboard")


def _loose(word):
    """Regex fragment matching `word`'s letters in order with 0-2 filler
    characters (space/dash/underscore/dot/asterisk) tolerated between each
    pair — catches "i-g-n-o-r-e" / "i.g.n.o.r.e" / "i g n o r e" spacing
    obfuscation. Only used for a couple of high-value phrases below; it does
    not rewrite the input, just tolerates gaps within a single regex."""
    filler = r"[\s\-_.*]{0,2}"
    return filler.join(re.escape(c) for c in word)


def _normalize(text):
    """NFKC-fold (collapses fullwidth/compatibility unicode forms used to
    dodge literal keyword matches, e.g. fullwidth Latin letters) and drop
    zero-width characters sometimes inserted mid-word to break up a trigger
    phrase (U+200B..U+200D, U+FEFF, U+2060 — see _ZERO_WIDTH_CODEPOINTS)."""
    t = unicodedata.normalize("NFKC", text or "")
    for cp in _ZERO_WIDTH_CODEPOINTS:
        t = t.replace(chr(cp), "")
    return t


_Rule = namedtuple("Rule", "id weight reason pattern")
_DEFAULT_FLAGS = re.IGNORECASE | re.MULTILINE


def _r(rule_id, weight, reason, pattern, flags=_DEFAULT_FLAGS):
    return _Rule(rule_id, weight, reason, re.compile(pattern, flags))


# ---------------------------------------------------------------------------
# RULES — the transparent word-list table. Columns: id, weight, reason tag,
# regex. Grouped by the five LLD-01 §6 categories. Weights are calibrated so
# that ONE high-confidence signal alone clears THRESHOLD (recall), while
# ambiguous/contextual signals stay below it alone and only flag in
# combination (false-positive control on legalese/manuals/regs — see
# testdata/honeypot/benign for the adversarial cases this protects).
# ---------------------------------------------------------------------------
RULES = [
    # -- 1. "ignore/disregard previous instructions" (en+ru) ---------------
    _r("ignore-en-1", 0.60, "ignore-previous-instructions",
       r"\b(ignore|disregard|forget|override)\s+(all\s+|any\s+)?(the\s+)?"
       r"(previous|prior|above|earlier|preceding)\s+"
       r"(instructions?|prompts?|rules?|commands?|directives?|guidance)\b"),
    _r("ignore-en-2", 0.45, "ignore-previous-instructions",
       r"\b(disregard|ignore)\s+everything\s+(above|before|written above)\b"),
    _r("ignore-en-3", 0.45, "ignore-previous-instructions",
       r"\bnew\s+instructions?\s*(for you)?\s*:"),
    _r("ignore-en-loose", 0.55, "ignore-previous-instructions",
       _loose("ignore") + r"[\s\-_.*]{1,3}" + _loose("all") + r"[\s\-_.*]{1,3}"
       + _loose("previous") + r"[\s\-_.*]{1,3}" + _loose("instructions")),
    _r("ignore-ru-1", 0.60, "ignore-previous-instructions",
       r"\b(игнорируй|игнорируйте|проигнорируй\w*)\s+(все\s+|любые\s+)?"
       r"(вышеуказанные\s+|предыдущие\s*|прошлые\s*|предшествующие\s*)"
       r"(инструкции|указания|команды|правила|распоряжения)\w*\b"),
    _r("ignore-ru-2", 0.55, "ignore-previous-instructions",
       r"\b(забудь|забудьте)\s+(все\s+|любые\s+)?(предыдущие|прошлые)\s+(инструкции|указания)\w*\b"),
    _r("ignore-ru-3", 0.50, "ignore-previous-instructions",
       r"\bне\s+(обращай|обращайте)\w*\s+внимания\s+на\s+(предыдущие|прошлые|вышеуказанные)\s+(инструкции|указания)\w*\b"),
    _r("ignore-ru-4", 0.45, "ignore-previous-instructions",
       r"\bнов\w+\s+инструкци\w+\s+(для\s+тебя)?\s*:"),

    # -- 2. imperatives directed AT the assistant/system/model (en+ru) -----
    _r("imper-en-1", 0.40, "imperative-to-assistant",
       r"\b(dear|attention|hey)\s+(ai|assistant|model|chatbot|bot|llm)\b"),
    _r("imper-en-2", 0.45, "imperative-to-assistant",
       r"\bas an ai( language model)?,?\s+you\s+(must|should|will|need to|are required to)\b"),
    _r("imper-en-3", 0.55, "jailbreak-persona",
       r"\byou\s+are\s+now\s+(in\s+|operating\s+in\s+)?(a|an)?\s*"
       r"(dan|developer mode|jailbreak(ed)?|unrestricted|unfiltered|uncensored)\b"),
    _r("imper-en-4", 0.35, "imperative-to-assistant",
       r"\bassistant,?\s+(please\s+)?(do|execute|run|perform|proceed to)\b"),
    _r("imper-en-5", 0.45, "imperative-to-assistant",
       r"\b(system|assistant)\s*:\s*you\s+must\b"),
    _r("imper-en-6", 0.35, "imperative-to-assistant",
       r"\bif\s+you\s+are\s+(the\s+)?(ai|assistant|model|llm|agent)\s+(reviewing|reading|processing|handling)\b"),
    _r("imper-ru-1", 0.40, "imperative-to-assistant",
       r"\b(уважаем\w+|дорог\w+|внимание)[,:\s]+\s*(ассистент|модель|ии|нейросеть|бот)\b"),
    _r("imper-ru-2", 0.45, "imperative-to-assistant",
       r"\bты\s+(теперь|отныне|с этого момента)\s+(являешься|должен|обязан)\b"),
    _r("imper-ru-3", 0.35, "imperative-to-assistant",
       r"\bассистент,?\s+(сейчас\s+же\s+|немедленно\s+)?(сделай|выполни|запусти|проигнорируй)\w*\b"),
    _r("imper-ru-4", 0.40, "imperative-to-assistant",
       r"\bкак\s+(языков\w+\s+)?модел\w+,?\s+ты\s+(должен|обязан)\b"),

    # -- 3. imitation of system blocks: markdown/XML/role markers (en+ru) --
    _r("fake-sys-1", 0.55, "fake-system-block",
       r"<\s*system\s*>"),
    _r("fake-sys-2", 0.40, "fake-system-block",
       r"\[\s*system\s*\]"),
    _r("fake-sys-3", 0.45, "fake-system-block",
       r"^\s*#{1,6}\s*system\s*(instructions?|prompt|override|rules?|role)?\s*:?\s*$"),
    _r("fake-sys-4", 0.50, "chat-template-marker",
       r"<\|[a-z_]{2,20}\|>"),
    _r("fake-sys-5", 0.45, "chat-template-marker",
       r"\[/?INST\]|<<\s*/?SYS\s*>>"),
    _r("fake-sys-6", 0.50, "fake-system-block",
       r'"role"\s*:\s*"(system|developer)"'),
    _r("fake-sys-7", 0.30, "role-marker-injection",
       r"^\s*(system|assistant)\s*:\s*\S"),
    _r("fake-sys-8", 0.40, "fake-system-block",
       r"```\s*system\b"),
    _r("fake-sys-ru-1", 0.45, "fake-system-block",
       r"<\s*систем\w*\s*>"),
    _r("fake-sys-ru-2", 0.40, "fake-system-block",
       r"\[\s*систем\w*\s*\]"),
    _r("fake-sys-ru-3", 0.40, "fake-system-block",
       r"^\s*#{1,6}\s*(системн\w+\s+(инструкци\w+|промпт\w*)|систем\w*)\s*:?\s*$"),
    _r("fake-sys-ru-4", 0.30, "role-marker-injection",
       r"^\s*(систем[аы]?|ассистент)\s*:\s*\S"),

    # -- 4. tool-call syntax: JSON tool envelopes + real MCP tool names ----
    _r("tool-json-1", 0.55, "tool-call-syntax",
       r'\{\s*"tool"\s*:\s*"'),
    _r("tool-json-2", 0.50, "tool-call-syntax",
       r'"tool_name"\s*:\s*"'),
    _r("tool-mcp-snake", 0.45, "mcp-tool-call-syntax",
       r"\b(?:%s)\(" % "|".join(MCP_TOOLS_SNAKE)),
    _r("tool-mcp-plain", 0.30, "mcp-tool-call-syntax",
       r"\b(?:%s)\(" % "|".join(MCP_TOOLS_PLAIN), flags=re.MULTILINE),  # case-sensitive on purpose

    # -- 5. requests to reveal the prompt / keys / tokens (en+ru) ----------
    _r("reveal-en-1", 0.45, "reveal-prompt-request",
       r"\b(reveal|show|print|output|repeat|leak|share|give)\s+(me\s+)?(your\s+)?(the\s+)?"
       r"(system\s+|full\s+|entire\s+|original\s+)?(prompt|system prompt|instructions)\b"),
    _r("reveal-en-2", 0.40, "reveal-prompt-request",
       r"\bwhat\s+(is|are)\s+your\s+(system\s+)?(prompt|instructions|rules)\b"),
    _r("reveal-en-3", 0.45, "reveal-secret-request",
       r"\b(reveal|show|print|output|leak|dump|expose)\b[^.\n]{0,30}\b"
       r"(api[\s_-]?keys?|secret\s*keys?|access\s*tokens?|bearer\s*tokens?|passwords?|credentials?)\b"),
    _r("reveal-ru-1", 0.45, "reveal-prompt-request",
       r"\b(покажи|раскрой|выведи|напиши|сообщи|распечатай)\w*\s+(мне\s+)?(свой\s+|весь\s+|полностью\s+)?"
       r"(системн\w+\s+)?(промпт\w*|инструкци\w+)\b"),
    _r("reveal-ru-2", 0.40, "reveal-prompt-request",
       r"\bкак\w*\s+у\s+тебя\s+системн\w+\s+промпт\w*\b"),
    _r("reveal-ru-3", 0.45, "reveal-secret-request",
       r"\b(покажи|раскрой|выведи|сообщи)\w*\s+(мне\s+)?(свой\s+)?"
       r"(api[\s_-]?ключ\w*|токен\w*|пароль\w*|секрет\w*|ключ\s+доступа)\b"),
]

# id must be unique — a duplicate would silently shadow a rule in any
# id-keyed lookup and usually means a copy-paste mistake.
assert len(RULES) == len({r.id for r in RULES}), "duplicate rule id in RULES"


def score(text):
    """Heuristic instructions-in-data score for `text`.

    Returns (score, reasons): score is a float in [0, 1] (sum of matched
    rule weights, capped at 1.0); reasons is the list of matched rules'
    `reason` tags, de-duplicated, in RULES order. score >= THRESHOLD (0.5)
    is the "flag it" line — see module docstring.
    """
    t = _normalize(text)
    total = 0.0
    reasons = []
    seen = set()
    for rule in RULES:
        if rule.pattern.search(t):
            total += rule.weight
            if rule.reason not in seen:
                seen.add(rule.reason)
                reasons.append(rule.reason)
    return min(1.0, total), reasons


def is_flagged(text):
    """Convenience wrapper: score(text)[0] >= THRESHOLD."""
    return score(text)[0] >= THRESHOLD
