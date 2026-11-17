"""Derivation contracts for the DE25 Atari ST component (no simulator or Quartus)."""
from pathlib import Path
import importlib.util
import unittest

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
SDRAM = ROOT / "cores/fes-ramtest/rtl/sdram_addon_port.v"

spec = importlib.util.spec_from_file_location("de25_atari_st_derive", HERE / "derive.py")
derive = importlib.util.module_from_spec(spec)
spec.loader.exec_module(derive)


class Derivations(unittest.TestCase):
    def test_only_the_forwarded_clock_cell_changes(self):
        source = SDRAM.read_bytes()
        result, record = derive.retarget_sdram(source)
        self.assertNotIn(b"altddio", result)
        self.assertNotIn(b"Cyclone", result)
        self.assertEqual(result.count(derive.CLOCK_AFTER), 1)
        kept = source.splitlines()
        derived = result.splitlines()
        added = [line for line in derived if line not in set(kept)]
        self.assertEqual(added, [derive.CLOCK_AFTER.rstrip(b"\n")])
        # Everything before and after the cell is byte-identical.
        begin = source.index(derive.CLOCK_BEGIN)
        self.assertEqual(result[:begin], source[:begin])
        self.assertTrue(source.endswith(result[begin + len(derive.CLOCK_AFTER):]))
        self.assertEqual(record["source_sha256"], derive.sha(source))

    def test_a_changed_controller_is_refused(self):
        source = SDRAM.read_bytes()
        for changed in (source.replace(b".outclock(clk_pin)", b".outclock(clk)"),
                        source.replace(b'"Cyclone V"', b'"Cyclone 10"'),
                        source + derive.CLOCK_BEGIN,
                        source.replace(derive.CLOCK_END, b"    );\n")):
            with self.assertRaises(ValueError):
                derive.retarget_sdram(changed)

    def test_transport_slice_matches_the_spectrum_lane(self):
        other = importlib.util.spec_from_file_location("de25_spectrum_derive", HERE.parent / "spectrum/derive.py")
        spectrum = importlib.util.module_from_spec(other)
        other.loader.exec_module(spectrum)
        source = (HERE.parent / "zx81-hps/fes_de25_computer.v").read_bytes()
        self.assertEqual(derive.transport(source)[0], spectrum.transport(source)[0])


if __name__ == "__main__":
    unittest.main()
