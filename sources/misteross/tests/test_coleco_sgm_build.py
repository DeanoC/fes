"""The SGM build lane has a separate, closed physical socket contract."""

import json
import tempfile
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from scripts import coleco_expansion
from scripts import build_fes_coleco_socket_v2 as shell
from scripts import build_coleco_sgm as sgm


class ColecoSgmBuildTest(unittest.TestCase):
    def test_v2_shell_accepts_factory_producer_arguments(self):
        output = Path("/tmp/fes-coleco-packages")
        with patch.object(shell, "build", return_value=output) as build:
            self.assertEqual(shell.main(["--root", str(shell.ROOT),
                                         "--package-output", str(output),
                                         "--identity-version", "2"]), 0)
        self.assertEqual(build.call_args.args, (shell.ROOT, output))
        self.assertEqual(build.call_args.kwargs["identity_version"], 2)

    def test_v2_shell_exposes_canonical_record_call(self):
        with patch.object(shell, "functional_record_fields",
                          side_effect=lambda root, fields, *args, **kwargs: fields), \
             patch.object(shell, "encode_build_record", side_effect=lambda fields: fields):
            record = shell.create_build_record(
                Path(__file__).parents[1], "repository", "a" * 40, {},
                identity_version=2, execution={})
        self.assertEqual(record["recipe"], shell.RECIPE)

    def test_shell_closure_and_router_pin(self):
        self.assertIn("cores/fes-coleco/rtl/coleco_expansion_ram.v", shell.PINNED_INPUTS)
        self.assertIn("cores/fes-coleco/rtl/coleco_audio_mix.v", shell.PINNED_INPUTS)
        self.assertIn("cores/fes-coleco/rtl/coleco_expansion_socket_v2.v", shell.PINNED_INPUTS)
        self.assertEqual(shell.TOOL_COMMITS["nextpnr"],
                         "f7370550adb324163ed24e54f7e6756a13569758")
        self.assertEqual(shell.TOOLCHAIN_LOCK, "toolchains/coleco-sgm.lock")
        with patch.object(shell, "functional_record_fields",
                          side_effect=lambda root, fields, *args, **kwargs: fields), \
             patch.object(shell, "encode_build_record", side_effect=lambda fields: fields):
            record = shell.create_build_record(Path(__file__).parents[1],
                                               "repository", "a" * 40, {}, {})
        self.assertEqual(record["parameters"]["expansion_rect"],
                         coleco_expansion.SOCKET_RECT_V2)
        self.assertEqual(record["parameters"]["seed_order"], "3,4,5,1,2,6,7,8,9,10")
        for source in ("sgm.v", "sgm_control.v", "sgm_ay.v"):
            self.assertIn(f"cores/fes-coleco/expansions/{source}", sgm.INPUTS)
        self.assertIn("toolchains/coleco-sgm.lock", sgm.INPUTS)

    def test_v2_boundary_has_59_unique_physical_ffs(self):
        bels = coleco_expansion.socket_bels_v2()
        self.assertEqual(len(bels), 59)
        self.assertEqual(len(set(bels.values())), 59)
        self.assertTrue(all(bel.startswith("MISTRAL_FF.24.") and
                            int(bel.split(".")[2]) <= 3 for bel in bels.values()))
        self.assertIn(b'FES_RESERVED_RECT "24 1 28 19"',
                      sgm.cart_qsf(coleco_expansion.shell_qsf("", version=2).encode()))
        self.assertEqual(sgm.CRAM_REGION, (1769, 32, 2806, 1800))
        self.assertIn('FES_RESERVED_RECT "24 1 28 11"',
                      coleco_expansion.shell_qsf(""))
        rtl = (Path(__file__).parents[1] /
               "cores/fes-coleco/rtl/coleco_expansion_socket_v2.v").read_text()
        for name, bel in bels.items():
            raw = coleco_expansion.raw_cell_name(name)
            self.assertIn(f'`COLECO_V2_SOCKET_FF({raw.removeprefix("socket.")}, "{bel}",', rtl)

    def test_v2_netlist_rejects_wrong_response_placement(self):
        bels = coleco_expansion.socket_bels_v2()
        design = {"modules": {"top": {"netnames": {
            "system_clock.pll_outclk": {"bits": [902]},
            "bus_request": {"bits": list(range(500, 531))},
            "plug_request": {"bits": list(range(31))},
            "bus_response": {"bits": list(range(100, 128))},
        }, "cells": {"system_clock.clocks_MISTRAL_CLKBUF_Q": {
            "type": "MISTRAL_CLKBUF", "connections": {"A": [902], "Q": [900]},
        }}}}}
        cells = design["modules"]["top"]["cells"]
        for name, bel in bels.items():
            bit = int(name.rsplit("_", 1)[1])
            cells[coleco_expansion.raw_cell_name(name)] = {
                "type": "MISTRAL_FF", "attributes": {"BEL": bel},
                "connections": {"CLK": [900],
                    "DATAIN": [bit + 500] if name.startswith("plug_addr") else ["0"],
                    "Q": [bit if name.startswith("plug_addr") else bit + 100]},
            }
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "synth.json"
            path.write_text(json.dumps(design))
            coleco_expansion.prepare_shell_netlist(path, version=2)
            reordered = json.loads(json.dumps(design))
            reordered['modules']['top']['cells']['system_clock.clocks_MISTRAL_CLKBUF_Q_1'] = \
                reordered['modules']['top']['cells'].pop('system_clock.clocks_MISTRAL_CLKBUF_Q')
            reordered['modules']['top']['cells']['system_clock.clocks_MISTRAL_CLKBUF_Q'] = {
                'type': 'MISTRAL_CLKBUF', 'connections': {'A': [901], 'Q': [899]}}
            path.write_text(json.dumps(reordered))
            coleco_expansion.prepare_shell_netlist(path, version=2)
            changed = json.loads(path.read_text())["modules"]["top"]["cells"]
            self.assertIn("plug_rdata_ff_27", changed)
            routed = {name: {"type": "MISTRAL_FF", "attributes": {"NEXTPNR_BEL": bel}}
                      for name, bel in bels.items()}
            path.write_text(json.dumps({"modules": {"top": {"cells": routed}}}))
            coleco_expansion.validate_routed_shell(path, version=2)
            routed["plug_rdata_ff_27"]["attributes"]["NEXTPNR_BEL"] = "MISTRAL_FF.28.6.2"
            path.write_text(json.dumps({"modules": {"top": {"cells": routed}}}))
            with self.assertRaisesRegex(ValueError, "plug_rdata_ff_27"):
                coleco_expansion.validate_routed_shell(path, version=2)
            routed["plug_rdata_ff_27"]["attributes"]["NEXTPNR_BEL"] = bels["plug_rdata_ff_27"]
            routed["intruding_shell_cell"] = {"type": "MISTRAL_COMB", "attributes": {
                "NEXTPNR_BEL": "MISTRAL_COMB.24.19.0"}}
            path.write_text(json.dumps({"modules": {"top": {"cells": routed}}}))
            with self.assertRaisesRegex(ValueError, "intruding_shell_cell"):
                coleco_expansion.validate_routed_shell(path, version=2)
            del routed["intruding_shell_cell"]
            for field, value in (("Q", [999]), ("DATAIN", [999])):
                malformed = json.loads(json.dumps(design))
                malformed["modules"]["top"]["cells"]["socket.plug_response_ff_27"]["connections"][field] = value
                path.write_text(json.dumps(malformed))
                with self.subTest(field=field), self.assertRaisesRegex(ValueError, "plug_rdata_ff_27"):
                    coleco_expansion.prepare_shell_netlist(path, version=2)
            malformed = json.loads(json.dumps(design))
            malformed["modules"]["top"]["cells"]["socket.plug_request_ff_30"]["connections"]["DATAIN"] = [999]
            path.write_text(json.dumps(malformed))
            with self.assertRaisesRegex(ValueError, "plug_addr_ff_30"):
                coleco_expansion.prepare_shell_netlist(path, version=2)

    def test_sgm_contract_rejects_unlisted_external_cram(self):
        changes = {"bits_inside_slot": 8, "bits_outside_slot": 1,
                   "outside_slot_coordinates": [[1, 1]],
                   "outside_slot_coordinates_truncated": False}
        with self.assertRaises(ValueError):
            sgm.enforce_cram_region(changes, Path("cram-diff.json"))
        self.assertEqual(sgm.CRAM_REGION, (1769, 32, 2806, 1800))

    def test_v1_shell_rejected_before_compiler(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            producer = root / "shell"
            producer.mkdir()
            (producer / "manifest.toml").write_bytes(b"manifest")
            (producer / "core.rbf").write_bytes(b"payload")
            package = SimpleNamespace(manifest_bytes=b"manifest", payload_bytes=b"payload",
                fields={"format": 2, "interfaces": [{"id": "fes.expansion.coleco-bus",
                    "major": 1, "minor": 0, "required": False}]})
            with patch.object(sgm, "_require_clean_source", return_value=("repo", "a" * 40)), \
                 patch.object(sgm, "read_package", return_value=package), \
                 patch.object(sgm.shell_recipe, "authenticate_tools") as compiler:
                with self.assertRaisesRegex(ValueError, "bus 2.0"):
                    sgm.build(root, producer, root / "package", 0)
                compiler.assert_not_called()


if __name__ == "__main__":
    unittest.main()
