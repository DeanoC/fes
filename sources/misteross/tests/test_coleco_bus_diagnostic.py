"""Coleco diagnostic publication guards for the frozen socket."""

import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from scripts import build_coleco_bus_diagnostic as diagnostic, coleco_expansion
from scripts.cyclonev_rbf import cram_set


class ColecoBusDiagnosticTest(unittest.TestCase):
    def test_contract_is_closed_over_the_physical_slot(self):
        self.assertEqual(diagnostic.CRAM_REGION, (1769, 32, 2806, 1034))
        self.assertEqual(diagnostic.SOURCES, ("cores/fes-coleco/expansions/diagnostic.v",))
        self.assertIn("cores/fes-coleco/rtl/coleco_bus_pack.vh", diagnostic.INPUTS)
        self.assertIn("toolchains/coleco-expansion.lock", diagnostic.INPUTS)

    def test_clock_gate_requires_all_three_declared_shell_clocks(self):
        rows = {name: {"constraint": expected + 0.0001, "achieved": expected + 1}
                for name, expected in diagnostic.REQUIRED_CLOCKS_MHZ.items()}
        diagnostic.validate_cart_timing({"fmax": rows})
        for name in tuple(rows):
            with self.subTest(name=name):
                missing = dict(rows)
                del missing[name]
                with self.assertRaises(ValueError):
                    diagnostic.validate_cart_timing({"fmax": missing})
                slow = {key: dict(value) for key, value in rows.items()}
                slow[name]["achieved"] = slow[name]["constraint"] - 0.5
                with self.assertRaises(ValueError):
                    diagnostic.validate_cart_timing({"fmax": slow})

    def test_sdc_braces_indexed_clock_names(self):
        encoded = diagnostic.cart_clock_constraints(diagnostic.ROOT).decode()
        self.assertIn("-name {system_clock.clocks[0]}", encoded)
        self.assertIn("[get_nets {system_clock.clocks[1]}]", encoded)

    def test_scaffold_removes_only_unused_dual_pll_pin_aliases(self):
        with tempfile.TemporaryDirectory() as temporary:
            source, dest = (Path(temporary) / name for name in ("routed.json", "scaffold.json"))
            pins = {"locked": [0, "locked"], "outclk": [0, "outclk"],
                    "outclk[0]": [0, "outclk[0]"], "outclk[1]": [0, "outclk[1]"],
                    "refclk": [0, "refclk"], "rst": [0, "rst"]}
            cell = {"type": "altera_pll", "connections": {"outclk": [4],
                    "refclk": [2], "locked": [6]},
                    "port_directions": {"outclk": "output", "refclk": "input", "locked": "output"},
                    "attributes": {"FES_PINMAP_V1": json.dumps({"count": 6, "pins": pins}).encode().hex()}}
            cells = {"system_clock.pll": cell, "system_clock.clocks_MISTRAL_CLKBUF_Q_1":
                     {"type": "MISTRAL_CLKBUF", "connections": {"A": [5]}}}
            for name, bel in coleco_expansion.socket_bels().items():
                cells[name] = {"type": "MISTRAL_FF", "attributes": {"NEXTPNR_BEL": bel}}
            source.write_text(json.dumps({"modules": {"top": {"cells": cells,
                "netnames": {"system_clock.pll_outclk_1": {"bits": [5]}}}}}))
            diagnostic.prepare_scaffold(source, dest)
            self.assertEqual(json.loads(source.read_text())["modules"]["top"]["cells"]["system_clock.pll"], cell)
            updated = json.loads(dest.read_text())["modules"]["top"]["cells"]
            got = updated["system_clock.pll"]
            for name, bel in coleco_expansion.socket_bels().items():
                self.assertEqual(updated[name]["attributes"]["NEXTPNR_BEL"], bel)
                self.assertNotIn("BEL", updated[name]["attributes"])
            mapped = json.loads(bytes.fromhex(got["attributes"]["FES_PINMAP_V1"]).decode())
            self.assertEqual(mapped, {"count": 5, "pins": {k: pins[k] for k in
                ("locked", "outclk", "outclk[1]", "refclk", "rst")}})
            self.assertEqual(got["connections"]["outclk[1]"], [5])
            self.assertEqual(bytes.fromhex(got["attributes"]["FES_PINMAP_V1"]).decode(),
                             json.dumps(mapped, sort_keys=True))
            cell["connections"]["outclk[0]"] = [4]
            source.write_text(json.dumps({"modules": {"top": {"cells": cells,
                "netnames": {"system_clock.pll_outclk_1": {"bits": [5]}}}}}))
            with self.assertRaises(ValueError):
                diagnostic.prepare_scaffold(source, dest)

    def test_cart_placement_reserves_only_empty_interior(self):
        shell = coleco_expansion.shell_qsf("pin constraints\n").encode()
        cart = diagnostic.cart_qsf(shell)
        self.assertIn(b'FES_RESERVED_RECT "25 1 27 11"', cart)
        self.assertNotIn(b'FES_RESERVED_RECT "24 1 28 11"', cart)
        with self.assertRaises(ValueError):
            diagnostic.cart_qsf(b"pin constraints\n")

    def test_outside_cram_report_keeps_coordinates_and_rejects_publication(self):
        coordinates = [[2917, 797], [2917, 799], [3328, 906]]
        changes = {
            "bits_inside_slot": 65,
            "bits_outside_slot": len(coordinates),
            "outside_slot_coordinates": coordinates,
            "outside_slot_coordinates_truncated": False,
        }
        with tempfile.TemporaryDirectory() as temporary:
            output = Path(temporary)
            report_path = diagnostic.write_cram_diff_report(output, b"cart-rbf", changes)
            report = json.loads(report_path.read_text())
            self.assertEqual(report["format"], 1)
            self.assertEqual(report["cram_region"], list(diagnostic.CRAM_REGION))
            self.assertEqual(report["cram_diff"]["outside_slot_coordinates"], coordinates)
            self.assertEqual(report["route_contract"], "failed")
            self.assertFalse(report["archive_published"])
            with self.assertRaisesRegex(ValueError, "outside.*not published.*cram-diff.json"):
                diagnostic.enforce_cram_region(changes, report_path)
            with self.assertRaisesRegex(ValueError, "cannot publish a cart"):
                diagnostic.write_cram_diff_report(
                    output, b"cart-rbf", changes, archive_published=True, expansion_id="unsafe"
                )

    def test_response_boundary_contract_accepts_only_declared_bits(self):
        die = SimpleNamespace(cram_sx=4096)
        cram_size = (die.cram_sx * 1162 + 7) // 8
        base = SimpleNamespace(die=die, cram=bytearray(cram_size))
        placed = SimpleNamespace(die=die, cram=bytearray(cram_size))
        for index, (x, y) in enumerate(diagnostic.RESPONSE_BOUNDARY_COORDINATES):
            old, new = index % 2, 1 - index % 2
            cram_set(base.cram, die, x, y, old)
            cram_set(placed.cram, die, x, y, new)
        coordinates = [[2917, 797], [2917, 799], [3328, 906]]
        changes = {
            "bits_inside_slot": 65,
            "bits_outside_slot": 3,
            "outside_slot_coordinates": coordinates,
            "outside_slot_coordinates_truncated": False,
        }
        patch_manifest = diagnostic.enforce_cram_region(changes, Path("cram-diff.json"), base, placed)
        self.assertEqual(patch_manifest, diagnostic.boundary_patch_for(placed))
        self.assertTrue(diagnostic.valid_boundary_patch(patch_manifest))
        with tempfile.TemporaryDirectory() as temporary:
            path = diagnostic.write_cram_diff_report(
                Path(temporary), b"cart-rbf", changes, archive_published=True,
                expansion_id="declared", boundary_patch=patch_manifest,
            )
            report = json.loads(path.read_text())
            self.assertEqual(report["route_contract"], "passed_with_response_boundary_patch")
            self.assertTrue(report["archive_published"])
            self.assertEqual(report["boundary_patch"], patch_manifest)

        extra_change = dict(changes, bits_outside_slot=4,
                            outside_slot_coordinates=coordinates + [[100, 100]])
        with self.assertRaisesRegex(ValueError, "outside the socket and declared response patch"):
            diagnostic.enforce_cram_region(extra_change, Path("cram-diff.json"), base, placed)
        invalid_patch = dict(patch_manifest)
        invalid_patch["bits"] = [dict(bit) for bit in patch_manifest["bits"]]
        invalid_patch["bits"][0]["value"] = 2
        self.assertFalse(diagnostic.valid_boundary_patch(invalid_patch))

    def test_wrong_shell_bytes_or_slot_rejects_before_toolchain(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            shell = root / "shell"
            shell.mkdir()
            (shell / "manifest.toml").write_bytes(b"sealed manifest")
            (shell / "core.rbf").write_bytes(b"sealed bitstream")
            package = SimpleNamespace(manifest_bytes=b"wrong manifest",
                payload_bytes=b"sealed bitstream", fields={"format": 2})
            with patch.object(diagnostic, "_require_clean_source", return_value=("repo", "a" * 40)), \
                 patch.object(diagnostic, "read_package", return_value=package), \
                 patch.object(diagnostic.shell_recipe, "authenticate_tools") as authenticate:
                with self.assertRaisesRegex(ValueError, "differs from sealed shell"):
                    diagnostic.build(root, shell, root / "package", 0)
                authenticate.assert_not_called()
            package.manifest_bytes = b"sealed manifest"
            package.fields["interfaces"] = []
            with patch.object(diagnostic, "_require_clean_source", return_value=("repo", "a" * 40)), \
                 patch.object(diagnostic, "read_package", return_value=package), \
                 patch.object(diagnostic.shell_recipe, "authenticate_tools") as authenticate:
                with self.assertRaisesRegex(ValueError, "optional Coleco expansion bus"):
                    diagnostic.build(root, shell, root / "package", 0)
                authenticate.assert_not_called()


if __name__ == "__main__":
    unittest.main()
