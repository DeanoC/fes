import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest

from scripts.native_video_clock import normalize_native_clock_inputs, prepare_native_clock


def fixture():
    # Match the actual native framebuffer's Yosys primitive shape: 48 equal
    # 1024x10 mixed-width SDPs, active-high write enable, two identical clocks.
    cells = {
        "FPGA_CLK1_50_MISTRAL_IB_PAD": {"type": "MISTRAL_IB", "parameters": {},
            "port_directions": {"PAD": "input", "O": "output"}, "connections": {"PAD": [2], "O": [63]}},
        "part.clock_MISTRAL_CLKBUF_Q": {"type": "MISTRAL_CLKBUF", "parameters": {},
            "port_directions": {"A": "input", "Q": "output"}, "connections": {"A": [63], "Q": [64]}},
        "ff": {"type": "MISTRAL_FF", "parameters": {}, "attributes": {"keep": "1"},
            "port_directions": {"CLK": "input", "DATAIN": "input", "Q": "output"},
            "connections": {"CLK": [64], "DATAIN": [452], "Q": [8000]}},
    }
    for i in range(48):
        connections = {"CLK1": [64], "CLK2": [64], "A1EN": [452], "B1EN": ["1"],
            "ACLR0": ["0"], "ACLR1": ["0"], "A1ADDR": list(range(100, 110)),
            "B1ADDR": list(range(200, 210)), "A1DATA": list(range(300, 310)),
            "B1DATA": list(range(1000 + i*10, 1010 + i*10))}
        cells[f"part.framebuffer.ram.0.{i}"] = {"type": "MISTRAL_M10K", "attributes": {"src": "native-video"},
            "parameters": {"CFG_ABITS": f"{10:032b}", "CFG_DBITS": f"{10:032b}",
                "CFG_RD_ABITS": f"{10:032b}", "CFG_RD_DBITS": f"{10:032b}",
                "CFG_MIXED_WIDTH": f"{1:032b}", "CFG_DUAL_CLOCK": f"{1:032b}", "INIT": "01"*5120},
            "port_directions": {k: "output" if k == "B1DATA" else "input" for k in connections}, "connections": connections}
    return {"creator": "Yosys", "modules": {"cart": {"ports": {
        "FPGA_CLK1_50": {"direction": "input", "bits": [2]}}, "cells": cells,
        "netnames": {"part.clock": {"bits": [64], "attributes": {}}}},
        "MISTRAL_M10K": {"attributes": {"blackbox": "1"}}}}


class NativeVideoClockTests(unittest.TestCase):
    def test_actual_shape_preserves_memory_and_other_wiring(self):
        source = fixture()
        original = copy.deepcopy(source)
        result, receipt = normalize_native_clock_inputs(source)
        self.assertEqual(source, original)
        self.assertEqual(receipt["ram_blocks"], 48)
        self.assertEqual(receipt["clock_pins"], 97)
        self.assertEqual(receipt["rewritten_clock_pins"], 97)
        self.assertEqual(receipt["imported_bit"], 63)
        # Restore only the known clock connections; every parameter, INIT bit,
        # enable polarity, source annotation, bus alias and other cell agrees.
        for name, cell in result["modules"]["cart"]["cells"].items():
            if cell["type"] == "MISTRAL_FF":
                self.assertEqual(cell["connections"]["CLK"], [63])
                cell["connections"]["CLK"] = [64]
            elif cell["type"] == "MISTRAL_M10K":
                self.assertEqual(cell["connections"]["CLK1"], [63])
                self.assertEqual(cell["connections"]["CLK2"], [63])
                cell["connections"]["CLK1"] = cell["connections"]["CLK2"] = [64]
        self.assertEqual(result, original)

    def test_transparent_chain_and_legacy_tdp_clock_requirements(self):
        source = fixture()
        cells = source["modules"]["cart"]["cells"]
        cells["buffer"] = {"type": "MISTRAL_BUF", "parameters": {},
            "port_directions": {"A": "input", "Q": "output"}, "connections": {"A": [64], "Q": [65]}}
        cells["ff"]["connections"]["CLK"] = [65]
        legacy = copy.deepcopy(cells["part.framebuffer.ram.0.0"])
        for key in ("CFG_MIXED_WIDTH", "CFG_RD_ABITS", "CFG_RD_DBITS", "CFG_DUAL_CLOCK"):
            del legacy["parameters"][key]
        del legacy["connections"]["CLK2"]
        del legacy["port_directions"]["CLK2"]
        legacy["connections"]["B1DATA"] = list(range(7000, 7010))
        cells["legacy"] = legacy
        tdp = copy.deepcopy(legacy)
        tdp["type"] = "MISTRAL_M10K_TDP"
        tdp["connections"]["CLK2"] = [65]
        tdp["port_directions"]["CLK2"] = "input"
        tdp["connections"]["B1DATA"] = list(range(7100, 7110))
        cells["tdp"] = tdp
        result, receipt = normalize_native_clock_inputs(source)
        self.assertEqual(receipt["transparent_buffers"], 2)
        self.assertEqual(receipt["clock_pins"], 100)
        self.assertNotIn("CLK2", result["modules"]["cart"]["cells"]["legacy"]["connections"])
        self.assertEqual(result["modules"]["cart"]["cells"]["tdp"]["connections"]["CLK2"], [63])
        self.assertEqual(normalize_native_clock_inputs(result)[1]["rewritten_clock_pins"], 0)

    def test_rejects_unproven_or_malformed_clock_graph_without_mutation(self):
        def changed(change):
            value = fixture()
            change(value["modules"]["cart"])
            return value
        ram = "part.framebuffer.ram.0.0"
        cases = {
            "different RAM clock": changed(lambda t: t["cells"][ram]["connections"].update(CLK2=[200])),
            "constant RAM clock": changed(lambda t: t["cells"][ram]["connections"].update(CLK2=["0"])),
            "missing read clock": changed(lambda t: (t["cells"][ram]["connections"].pop("CLK2"), t["cells"][ram]["port_directions"].pop("CLK2"))),
            "missing FF clock": changed(lambda t: t["cells"]["ff"]["connections"].update(CLK=[])),
            "buffer cycle": changed(lambda t: t["cells"]["part.clock_MISTRAL_CLKBUF_Q"]["connections"].update(A=[64])),
            "derived buffer input": changed(lambda t: t["cells"]["part.clock_MISTRAL_CLKBUF_Q"]["connections"].update(A=[452])),
            "buffer inversion flag": changed(lambda t: t["cells"]["part.clock_MISTRAL_CLKBUF_Q"]["parameters"].update(INVERT=1)),
            "missing input": changed(lambda t: t["cells"].pop("FPGA_CLK1_50_MISTRAL_IB_PAD")),
            "duplicate input": changed(lambda t: t["cells"].update(other_input=copy.deepcopy(t["cells"]["FPGA_CLK1_50_MISTRAL_IB_PAD"]))),
            "conflicting alias driver": changed(lambda t: t["cells"]["ff"]["connections"].update(Q=[64])),
            "invalid port direction": changed(lambda t: t["cells"][ram]["port_directions"].update(CLK2="output")),
            "invalid binary flag": changed(lambda t: t["cells"][ram]["parameters"].update(CFG_DUAL_CLOCK="x")),
            "unsupported sequential cell": changed(lambda t: t["cells"]["ff"].update(type="MISTRAL_MLAB")),
        }
        for name, source in cases.items():
            with self.subTest(name=name):
                original = copy.deepcopy(source)
                with self.assertRaises(ValueError):
                    normalize_native_clock_inputs(source)
                self.assertEqual(source, original)

    def test_prepare_is_atomic_and_binds_raw_and_prepared_json(self):
        with tempfile.TemporaryDirectory() as temporary:
            path = Path(temporary) / "cart.json"
            raw = json.dumps(fixture()).encode()
            path.write_bytes(raw)
            path.chmod(0o640)
            receipt = prepare_native_clock(path)
            self.assertEqual(receipt["synth_sha256"], hashlib.sha256(raw).hexdigest())
            self.assertEqual(receipt["prepared_sha256"], hashlib.sha256(path.read_bytes()).hexdigest())
            self.assertEqual(path.stat().st_mode & 0o777, 0o640)
            self.assertEqual(receipt["ram_blocks"], 48)
            self.assertEqual(receipt["clock_pins"], 97)
            self.assertEqual({p.name for p in Path(temporary).iterdir()}, {"cart.json"})
            bad = fixture()
            bad["modules"]["cart"]["cells"]["ff"]["connections"]["CLK"] = [452]
            path.write_text(json.dumps(bad))
            before = path.read_bytes()
            with self.assertRaises(ValueError):
                prepare_native_clock(path)
            self.assertEqual(path.read_bytes(), before)
            self.assertEqual({p.name for p in Path(temporary).iterdir()}, {"cart.json"})
            path.write_text('{"modules":{},"modules":{}}')
            before = path.read_bytes()
            with self.assertRaises(ValueError):
                prepare_native_clock(path)
            self.assertEqual(path.read_bytes(), before)
            link = Path(temporary) / "link.json"
            link.symlink_to(path)
            with self.assertRaises(ValueError):
                prepare_native_clock(link)


if __name__ == "__main__":
    unittest.main()
