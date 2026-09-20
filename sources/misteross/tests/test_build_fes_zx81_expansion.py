import copy
import json
from pathlib import Path
import tempfile
import unittest

from scripts import build_fes_zx81_oss as producer
from scripts import zx81_expansion as expansion


class ZX81SocketProducerTests(unittest.TestCase):
    def fixture(self):
        cells = {}
        requests = list(range(10, 47))
        for name, bel in expansion.socket_bels().items():
            connection = {"CLK": [2], "Q": [100]}
            if name.startswith("plug_addr_ff_"):
                connection["Q"] = [requests[int(name.removeprefix("plug_addr_ff_"))]]
            cells[expansion.SOCKET_PREFIX + name] = {
                "type": "MISTRAL_FF", "attributes": {"BEL": bel}, "connections": connection,
            }
        return {"modules": {"top": {"cells": cells, "netnames": {
            "clk_sys": {"bits": [2]}, "plug_addr": {"bits": requests},
        }}}}

    def test_real_socket_name_mapping_preserves_all_connectivity(self):
        before = self.fixture()
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "synth.json"
            path.write_text(json.dumps(before))
            expansion.prepare_shell_netlist(path)
            cells = json.loads(path.read_text())["modules"]["top"]["cells"]
        self.assertEqual(len(cells), 54)
        self.assertEqual(set(cells), set(expansion.socket_bels()))
        for name, cell in cells.items():
            self.assertEqual(cell, before["modules"]["top"]["cells"][expansion.SOCKET_PREFIX + name])

    def test_changed_geometry_clock_wiring_or_missing_plug_rejects_without_rewrite(self):
        original = self.fixture()
        for mode in ("geometry", "clock", "wiring", "missing", "collision"):
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as directory:
                design = copy.deepcopy(original)
                cells = design["modules"]["top"]["cells"]
                name = expansion.SOCKET_PREFIX + "plug_addr_ff_0"
                if mode == "geometry":
                    cells[name]["attributes"]["BEL"] = "MISTRAL_FF.25.1.2"
                elif mode == "clock":
                    cells[name]["connections"]["CLK"] = [3]
                elif mode == "wiring":
                    cells[name]["connections"]["Q"] = [999]
                elif mode == "missing":
                    del cells[name]
                else:
                    cells["plug_addr_ff_0"] = cells[name]
                path = Path(directory) / "synth.json"
                content = json.dumps(design)
                path.write_text(content)
                with self.assertRaises(ValueError):
                    expansion.prepare_shell_netlist(path)
                self.assertEqual(path.read_text(), content)

    def test_socket_build_keeps_factory_output_separate_and_reserves_socket(self):
        root = Path("/source")
        tools = {"yosys": Path("/tools/yosys"), "nextpnr-mistral": Path("/tools/nextpnr-mistral")}
        shell, route = producer.build_commands(root, root / producer.SOCKET_OUTPUT_RELATIVE,
                                               "a" * 32, tools, socketed=True)
        self.assertIn("chparam -set EXPANSION_SOCKET 1 top", shell[-1])
        self.assertIn("build/fes-zx81-socket/synth.json", shell[-1])
        self.assertIn("build/fes-zx81-socket/socket.qsf", route)
        self.assertIn('FES_RESERVED_RECT "25 1 27 32"', expansion.shell_qsf("existing pins\n"))
        fixed, _ = producer.build_commands(root, root / producer.OUTPUT_RELATIVE, "a" * 32, tools)
        self.assertNotIn("chparam -set EXPANSION_SOCKET", fixed[-1])
        self.assertIn("build/fes-zx81-oss/synth.json", fixed[-1])


if __name__ == "__main__":
    unittest.main()
