#!/usr/bin/env python3
"""test_frameworks.py — trust/schema tests for workers/builder/frameworks.py.

Product mission A acceptance (DEVELOPMENT_GRAPH (git 5447fc5) P1): the builder starts ONLY
with a valid manifest; every discovery question carries id/question/why;
integrity failures are loud and fail-closed. Stdlib-only (pytest is the runner,
not a dependency of the worker).
"""
import hashlib
import json
import os
import sys
import tempfile

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)
import frameworks  # noqa: E402


def _write(dirpath, name, data):
    path = os.path.join(dirpath, name)
    with open(path, "w", encoding="utf-8") as f:
        f.write(json.dumps(data) if not isinstance(data, str) else data)
    return path


def _minimal_fw(fid="ap", version="1.0.0"):
    return {
        "meta": {"id": fid, "name": "Test", "version": version, "origin": "test",
                 "compatibility": {"builder_protocol": ">=1.0", "dsl_features": []},
                 "supersedes": []},
        "vocabulary": [], "entities": [], "roles": [], "process_patterns": [],
        "segregation_of_duties": [], "control_points": [], "kpis": [],
        "risks": [], "discovery": {"required": [{"id": "q1", "question": "Q?",
                                                "why": "because"}]},
        "alternatives": [], "anti_patterns": [], "regulatory": [],
        "automations": [], "agent_roles": [], "approvals": [], "dashboards": [],
        "integrations": [],
    }


def test_real_manifest_loads_and_verifies():
    """The shipped frameworks/{ap,itsm,crm}.json must load with hashes intact."""
    loaded = frameworks.load_frameworks()
    assert set(loaded) == {"ap", "itsm", "crm"}
    for fid, fw in loaded.items():
        assert fw["meta"]["id"] == fid
        for cls in ("required", "important", "optional"):
            for q in fw["discovery"].get(cls, []):
                assert q.get("id") and q.get("question") and q.get("why")
        # provenance required on every recommendation source
        assert fw["meta"]["origin"]


def test_manifest_hash_mismatch_fails_closed():
    with tempfile.TemporaryDirectory() as d:
        fw = _minimal_fw()
        _write(d, "ap.json", fw)
        manifest = {"frameworks": [{
            "id": "ap", "file": "ap.json", "version": "1.0.0",
            "sha256": "0" * 64}]}
        _write(d, "manifest.json", manifest)
        try:
            frameworks.load_frameworks(os.path.join(d, "manifest.json"))
            raise AssertionError("hash mismatch must raise FrameworkError")
        except frameworks.FrameworkError as exc:
            assert "integrity mismatch" in str(exc)


def test_missing_file_fails_closed():
    with tempfile.TemporaryDirectory() as d:
        manifest = {"frameworks": [{
            "id": "ap", "file": "ap.json", "version": "1.0.0",
            "sha256": "0" * 64}]}
        _write(d, "manifest.json", manifest)
        try:
            frameworks.load_frameworks(os.path.join(d, "manifest.json"))
            raise AssertionError("missing file must raise FrameworkError")
        except frameworks.FrameworkError as exc:
            assert "missing" in str(exc)


def test_path_traversal_in_manifest_rejected():
    with tempfile.TemporaryDirectory() as d:
        manifest = {"frameworks": [{
            "id": "ap", "file": "../../etc/passwd", "version": "1.0.0",
            "sha256": "0" * 64}]}
        _write(d, "manifest.json", manifest)
        try:
            frameworks.load_frameworks(os.path.join(d, "manifest.json"))
            raise AssertionError("unsafe path must raise FrameworkError")
        except frameworks.FrameworkError as exc:
            assert "unsafe" in str(exc)


def test_question_without_why_rejected():
    with tempfile.TemporaryDirectory() as d:
        fw = _minimal_fw()
        fw["discovery"]["required"][0].pop("why")
        _write(d, "ap.json", fw)
        manifest = {"frameworks": [{
            "id": "ap", "file": "ap.json", "version": "1.0.0",
            "sha256": hashlib.sha256(json.dumps(fw).encode()).hexdigest()}]}
        _write(d, "manifest.json", manifest)
        try:
            frameworks.load_frameworks(os.path.join(d, "manifest.json"))
            raise AssertionError("question without why must raise FrameworkError")
        except frameworks.FrameworkError as exc:
            assert "why" in str(exc)


def test_unsupported_protocol_rejected():
    with tempfile.TemporaryDirectory() as d:
        fw = _minimal_fw()
        fw["meta"]["compatibility"]["builder_protocol"] = ">=2.0"
        _write(d, "ap.json", fw)
        manifest = {"frameworks": [{
            "id": "ap", "file": "ap.json", "version": "1.0.0",
            "sha256": hashlib.sha256(json.dumps(fw).encode()).hexdigest()}]}
        _write(d, "manifest.json", manifest)
        try:
            frameworks.load_frameworks(os.path.join(d, "manifest.json"))
            raise AssertionError("unsupported protocol must raise FrameworkError")
        except frameworks.FrameworkError as exc:
            assert "builder_protocol" in str(exc)


def test_version_mismatch_fails_closed():
    with tempfile.TemporaryDirectory() as d:
        fw = _minimal_fw(version="1.0.0")
        _write(d, "ap.json", fw)
        manifest = {"frameworks": [{
            "id": "ap", "file": "ap.json", "version": "2.0.0",
            "sha256": hashlib.sha256(json.dumps(fw).encode()).hexdigest()}]}
        _write(d, "manifest.json", manifest)
        try:
            frameworks.load_frameworks(os.path.join(d, "manifest.json"))
            raise AssertionError("version mismatch must raise FrameworkError")
        except frameworks.FrameworkError as exc:
            assert "version" in str(exc)


def test_discovery_questions_flatten():
    fw = _minimal_fw()
    fw["discovery"]["important"] = [{"id": "q2", "question": "Q2?",
                                     "why": "why2", "default": "x"}]
    qs = frameworks.discovery_questions(fw)
    assert [q["class"] for q in qs] == ["required", "important"]
    assert qs[1]["id"] == "q2"
