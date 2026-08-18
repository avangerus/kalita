#!/usr/bin/env python3
"""frameworks.py — curated business-framework loader (product mission A).

Layer 2 of PRODUCT_ARCHITECTURE.md: Kalita Business Frameworks. The loader is
the ONLY way framework content enters the builder worker, and it enforces the
FRAMEWORK TRUST rules (BUSINESS_FRAMEWORKS.md §3):

  - integrity: every framework file's sha256 must match frameworks/manifest.json;
  - identity/version: meta.id/version must match the manifest entry;
  - compatibility: builder_protocol must be satisfied (>=1.0 today);
  - fail-closed: a missing file, hash mismatch or schema violation raises
    FrameworkError and the builder refuses to start — never a silent skip;
  - read-only: loaded content is never mutated and never written back.

Frameworks never change a running company (PRODUCT_ARCHITECTURE.md §3.1): they
are knowledge for FUTURE sessions; a migration is an ordinary proposal.

Zero non-stdlib dependencies (workers policy). Run tests: pytest test_frameworks.py
"""
import hashlib
import json
import os
import sys

_HERE = os.path.dirname(os.path.abspath(__file__))
_REPO_ROOT = os.path.abspath(os.path.join(_HERE, "..", ".."))

FRAMEWORKS_DIR = os.environ.get(
    "KALITA_FRAMEWORKS_DIR", os.path.join(_REPO_ROOT, "frameworks"))
BUILDER_PROTOCOL = (1, 0, 0)  # this worker implements builder protocol 1.0


class FrameworkError(Exception):
    """A framework failed a trust/schema check — the builder must not start."""


def _parse_version(v):
    parts = []
    for p in str(v).split("."):
        if not p.isdigit():
            raise FrameworkError("bad version %r" % (v,))
        parts.append(int(p))
    return tuple(parts + [0] * (3 - len(parts)))


def _protocol_ok(spec):
    """Check a '>=1.0'-style protocol requirement against BUILDER_PROTOCOL."""
    if not isinstance(spec, str) or not spec.startswith(">="):
        raise FrameworkError("unsupported builder_protocol spec %r" % (spec,))
    return BUILDER_PROTOCOL >= _parse_version(spec[2:].strip())


def _schema_check(fw):
    """Minimal but real schema v1 validation: the shape the Expert depends on."""
    meta = fw.get("meta")
    if not isinstance(meta, dict):
        raise FrameworkError("framework missing meta")
    for key in ("id", "name", "version", "origin", "compatibility"):
        if not meta.get(key):
            raise FrameworkError("meta.%s missing for framework %r" % (key, meta.get("id")))
    disc = fw.get("discovery")
    if not isinstance(disc, dict):
        raise FrameworkError("framework %r missing discovery" % (meta.get("id"),))
    for section in ("required", "important", "optional"):
        items = disc.get(section, [])
        if not isinstance(items, list):
            raise FrameworkError("framework %r discovery.%s must be a list" % (meta.get("id"), section))
        for q in items:
            if not isinstance(q, dict) or not q.get("id") or not q.get("question") or not q.get("why"):
                raise FrameworkError(
                    "framework %r discovery.%s question must have id/question/why: %r"
                    % (meta.get("id"), section, q))
    for key in ("vocabulary", "entities", "roles", "process_patterns",
                "segregation_of_duties", "control_points", "risks",
                "alternatives", "anti_patterns", "automations",
                "agent_roles", "approvals", "dashboards", "integrations"):
        if not isinstance(fw.get(key), list):
            raise FrameworkError("framework %r section %s must be a list" % (meta.get("id"), key))
    if not _protocol_ok(meta["compatibility"].get("builder_protocol", "")):
        raise FrameworkError(
            "framework %r requires unsupported builder_protocol %r"
            % (meta.get("id"), meta["compatibility"].get("builder_protocol")))


def load_manifest(manifest_path=None):
    """Read frameworks/manifest.json, return the raw manifest dict."""
    path = manifest_path or os.path.join(FRAMEWORKS_DIR, "manifest.json")
    try:
        with open(path, "r", encoding="utf-8") as f:
            manifest = json.load(f)
    except (OSError, ValueError) as exc:
        raise FrameworkError("cannot read manifest %s: %s" % (path, exc))
    if not isinstance(manifest.get("frameworks"), list) or not manifest["frameworks"]:
        raise FrameworkError("manifest %s: 'frameworks' must be a non-empty list" % (path,))
    return manifest


def _sha256(data):
    return hashlib.sha256(data).hexdigest()


def load_frameworks(manifest_path=None):
    """Load every framework listed in the manifest, fully verified.

    Returns {framework_id: framework_dict} sorted by manifest order.
    Raises FrameworkError on any trust/schema failure (fail-closed).
    Framework files resolve relative to the MANIFEST's directory (so a test
    can supply a temp dir); the default manifest lives in FRAMEWORKS_DIR.
    """
    manifest_path = manifest_path or os.path.join(FRAMEWORKS_DIR, "manifest.json")
    base_dir = os.path.dirname(os.path.abspath(manifest_path))
    manifest = load_manifest(manifest_path)
    loaded = {}
    for entry in manifest["frameworks"]:
        fid = entry.get("id")
        rel = entry.get("file")
        if not fid or not rel:
            raise FrameworkError("manifest entry without id/file: %r" % (entry,))
        # path traversal guard: the manifest must reference a plain filename
        # inside the manifest directory, never an absolute or ..-escaping path.
        if os.path.isabs(rel) or ".." in rel.split("/"):
            raise FrameworkError("manifest entry %r has unsafe file path %r" % (fid, rel))
        path = os.path.join(base_dir, rel)
        try:
            with open(path, "rb") as f:
                raw = f.read()
        except OSError as exc:
            raise FrameworkError("framework %r file %r missing: %s" % (fid, rel, exc))
        want = entry.get("sha256")
        if not isinstance(want, str) or len(want) != 64:
            raise FrameworkError("manifest entry %r lacks a valid sha256" % (fid,))
        got = _sha256(raw)
        if got != want:
            raise FrameworkError(
                "framework %r integrity mismatch: manifest %s, file %s" % (fid, want, got))
        try:
            fw = json.loads(raw.decode("utf-8"))
        except ValueError as exc:
            raise FrameworkError("framework %r is not valid JSON: %s" % (fid, exc))
        if fw.get("meta", {}).get("id") != fid:
            raise FrameworkError(
                "framework file %r meta.id %r != manifest id %r"
                % (rel, fw.get("meta", {}).get("id"), fid))
        if fw.get("meta", {}).get("version") != entry.get("version"):
            raise FrameworkError(
                "framework %r version %r != manifest version %r"
                % (fid, fw.get("meta", {}).get("version"), entry.get("version")))
        _schema_check(fw)
        loaded[fid] = fw
    return loaded


def discovery_questions(fw):
    """Flatten a framework's discovery section into
    [{id, question, why, class: required|important|optional}]."""
    out = []
    for cls in ("required", "important", "optional"):
        for q in fw.get("discovery", {}).get(cls, []):
            out.append({"id": q["id"], "question": q["question"],
                        "why": q["why"], "class": cls})
    return out


def question_by_topic(fw, topic):
    """The framework's own question text for a discovery topic id (so a
    CHANGE conversation re-uses the curated wording, not ad-hoc prose)."""
    for q in discovery_questions(fw):
        if q["id"] == topic:
            return q["question"]
    return None
