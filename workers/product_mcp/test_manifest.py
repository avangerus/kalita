#!/usr/bin/env python3
"""Tests for manifest.py — parsing + offline validation of product.mcp:
sections. No live stack required.

Run: python -m pytest test_manifest.py -v
"""
import os
import sys
import textwrap
import unittest

_HERE = os.path.dirname(os.path.abspath(__file__))
if _HERE not in sys.path:
    sys.path.insert(0, _HERE)

import manifest  # noqa: E402
import compose  # noqa: E402

_CONTRACTS_PACK = os.path.normpath(os.path.join(_HERE, "..", "..", "packs", "contracts"))


def _write_pack(tmp_dir, files):
    for name, content in files.items():
        with open(os.path.join(tmp_dir, name), "w", encoding="utf-8") as fh:
            fh.write(textwrap.dedent(content))


class TestParseRealContractsManifest(unittest.TestCase):
    """LLD-06 §3 test 3 (TestFacadeToolFromManifest) starts here: the façade
    must read packs/contracts/product_mcp.dsl — the REAL manifest already
    checked into the repo — without any code change for "legal" specifically."""

    def test_parses_both_tools(self):
        tools = manifest.parse_product_mcp(_CONTRACTS_PACK)
        names = sorted(t["name"] for t in tools)
        self.assertEqual(names, ["legal.findings", "legal.submit_contract"])

    def test_submit_contract_shape(self):
        tools = {t["name"]: t for t in manifest.parse_product_mcp(_CONTRACTS_PACK)}
        t = tools["legal.submit_contract"]
        self.assertEqual([f["name"] for f in t["in"]], ["title", "counterparty", "terms"])
        self.assertTrue(all(f["required"] for f in t["in"]))
        self.assertEqual(t["in"][2]["type"], "text")
        self.assertEqual(t["out"], "contract")
        self.assertEqual(len(t["compose"]), 2)

    def test_validates_against_real_entities_and_workflow(self):
        tools = manifest.parse_product_mcp(_CONTRACTS_PACK)
        entities, actions = manifest.scan_entities_and_actions(_CONTRACTS_PACK)
        self.assertIn("Contract", entities)
        self.assertIn("send_to_legal", actions.get("Contract", set()))
        for tool in tools:
            compose.validate_tool(tool, entities, actions)  # must not raise


class TestOfflineFailFastValidation(unittest.TestCase):
    """TECHDEBT.md "P2 product.mcp's compose — сырой текст": the façade must
    catch a broken compose at STARTUP (fail fast), not on a caller's first
    real request."""

    def _tools(self, tmp_dir, product_mcp_body):
        _write_pack(tmp_dir, {
            "pack.dsl": "pack demo\nversion 0.1.0\n",
            "entities.dsl": """
                entity Widget "Widget":
                    title: string required
                    owner: ref[core.User]
            """,
            "product_mcp.dsl": product_mcp_body,
        })
        return manifest.parse_product_mcp(tmp_dir), manifest.scan_entities_and_actions(tmp_dir)

    def test_undeclared_var_in_create_args_is_rejected(self, tmp_path=None):
        import tempfile
        with tempfile.TemporaryDirectory() as d:
            tools, (entities, actions) = self._tools(d, """
                product.mcp:
                    tool demo.make "Make a widget":
                        in:
                            title: string required
                        compose:
                            create Widget(title: $title, owner: $ghost) -> $w
                        out: w
            """)
            with self.assertRaises(manifest.ManifestError):
                compose.validate_tool(tools[0], entities, actions)

    def test_unknown_entity_is_rejected(self):
        import tempfile
        with tempfile.TemporaryDirectory() as d:
            tools, (entities, actions) = self._tools(d, """
                product.mcp:
                    tool demo.make "Make a thing":
                        in:
                            title: string required
                        compose:
                            create Nonexistent(title: $title) -> $w
                        out: w
            """)
            with self.assertRaises(manifest.ManifestError):
                compose.validate_tool(tools[0], entities, actions)

    def test_out_binding_never_produced_is_rejected(self):
        import tempfile
        with tempfile.TemporaryDirectory() as d:
            tools, (entities, actions) = self._tools(d, """
                product.mcp:
                    tool demo.make "Make a widget":
                        in:
                            title: string required
                        compose:
                            create Widget(title: $title) -> $w
                        out: nope
            """)
            with self.assertRaises(manifest.ManifestError):
                compose.validate_tool(tools[0], entities, actions)

    def test_unknown_field_is_rejected(self):
        import tempfile
        with tempfile.TemporaryDirectory() as d:
            tools, (entities, actions) = self._tools(d, """
                product.mcp:
                    tool demo.make "Make a widget":
                        in:
                            title: string required
                        compose:
                            create Widget(nickname: $title) -> $w
                        out: w
            """)
            with self.assertRaises(manifest.ManifestError):
                compose.validate_tool(tools[0], entities, actions)

    def test_unsupported_verb_is_rejected(self):
        import tempfile
        with tempfile.TemporaryDirectory() as d:
            tools, (entities, actions) = self._tools(d, """
                product.mcp:
                    tool demo.make "Make a widget":
                        in:
                            title: string required
                        compose:
                            frobnicate Widget(title: $title) -> $w
                        out: w
            """)
            with self.assertRaises(manifest.ManifestError):
                compose.validate_tool(tools[0], entities, actions)

    def test_valid_tool_passes(self):
        import tempfile
        with tempfile.TemporaryDirectory() as d:
            tools, (entities, actions) = self._tools(d, """
                product.mcp:
                    tool demo.make "Make a widget":
                        in:
                            title: string required
                        compose:
                            create Widget(title: $title) -> $w
                        out: w
            """)
            compose.validate_tool(tools[0], entities, actions)  # must not raise


if __name__ == "__main__":
    unittest.main()
