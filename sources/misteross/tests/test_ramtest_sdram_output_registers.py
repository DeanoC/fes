"""The OSS RAM Tester must seal only with registered SDRAM output pads."""

import copy
import json
from pathlib import Path
import unittest

from scripts import build_fes_ramtest as producer


# Minimized ports and output cells from a routed RAM Tester design; other
# unrelated cells, attributes and routing metadata are intentionally omitted.
FIXTURE = Path(__file__).parent / "fixtures" / "ramtest-routed-sdram-outputs.json"


class SDRAMOutputRegistersTest(unittest.TestCase):
    def setUp(self):
        self.design = json.loads(FIXTURE.read_text())
        self.top = self.design["modules"]["top"]

    def check(self):
        return producer.sdram_output_register_evidence(self.design)

    def test_actual_routed_outputs_all_have_packed_registers(self):
        evidence = self.check()
        self.assertEqual(evidence["count"], 20)
        self.assertEqual(set(evidence["pads"]),
                         {f"SDRAM_A[{i}]" for i in range(13)} |
                         {f"SDRAM_BA[{i}]" for i in range(2)} |
                         {"SDRAM_CKE", "SDRAM_nCS", "SDRAM_nRAS", "SDRAM_nCAS", "SDRAM_nWE"})

    def test_each_missing_or_unregistered_output_fails_even_with_an_extra_cell(self):
        original = copy.deepcopy(self.design)
        for name in original["modules"]["top"]["cells"]:
            with self.subTest(cell=name):
                self.design = copy.deepcopy(original)
                cells = self.design["modules"]["top"]["cells"]
                cells[name]["type"] = "MISTRAL_OB"
                # Total SDROUT count alone must not let an unrelated register
                # compensate for the missing register at the target pad.
                cells["unrelated"] = {"type": "MISTRAL_SDROUT", "port_directions": {"PAD": "output"},
                                      "connections": {"PAD": [900000]}}
                with self.assertRaises(producer.board.BuildError):
                    self.check()

    def test_duplicate_driver_and_aliased_pads_fail(self):
        cells = self.top["cells"]
        name = next(iter(cells))
        cells["duplicate"] = copy.deepcopy(cells[name])
        with self.assertRaisesRegex(producer.board.BuildError, "one routed output driver"):
            self.check()
        del cells["duplicate"]
        self.top["ports"]["SDRAM_A"]["bits"][1] = self.top["ports"]["SDRAM_A"]["bits"][0]
        with self.assertRaisesRegex(producer.board.BuildError, "pads share net"):
            self.check()

    def test_wrong_pad_missing_clock_and_invalid_ports_fail(self):
        original = copy.deepcopy(self.design)
        for corruption in ("wrong_pad", "missing_clock", "missing_port", "constant", "wrong_direction"):
            with self.subTest(corruption=corruption):
                self.design = copy.deepcopy(original)
                top = self.design["modules"]["top"]
                cells = top["cells"]
                cell = cells["SDRAM_nWE_MISTRAL_OB_PAD"]
                if corruption == "wrong_pad":
                    cell["connections"]["PAD"] = [999999]
                elif corruption == "missing_clock":
                    cell["connections"]["CLK"] = []
                elif corruption == "missing_port":
                    del top["ports"]["SDRAM_BA"]
                elif corruption == "constant":
                    top["ports"]["SDRAM_nWE"]["bits"] = ["0"]
                else:
                    cell["port_directions"]["PAD"] = "inout"
                with self.assertRaises(producer.board.BuildError):
                    self.check()

    def test_malformed_design_fails_closed(self):
        for value in ({}, {"modules": None}, {"modules": {"top": {}}}):
            with self.subTest(value=value), self.assertRaises(producer.board.BuildError):
                producer.sdram_output_register_evidence(value)


if __name__ == "__main__":
    unittest.main()
