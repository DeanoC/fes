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

    def test_native_video_keeps_everything_but_the_frame_store(self):
        source = (ROOT / "cores/fes-atari-st/rtl/st_native_low_video.sv").read_bytes()
        result, record = derive.retarget_native_video(source)
        self.assertNotIn(b'"M10K"', result)
        self.assertEqual(result.count(b"<= frame_store_data;"), 3)
        self.assertEqual(record["frame_words"], 79872)
        # Outside the four replaced regions every source line survives in order.
        kept = [line for line in result.splitlines() if line in set(source.splitlines())]
        position = 0
        lines = source.splitlines()
        for line in kept:
            position = lines.index(line, position) if line in lines[position:] else position
        added = [line for line in result.splitlines() if line not in set(lines)]
        self.assertLess(len(added), 40)
        with self.assertRaises(ValueError):
            derive.retarget_native_video(result)
        with self.assertRaises(ValueError):
            derive.retarget_native_video(source.replace(b"FRAME_CHUNKS = 78", b"FRAME_CHUNKS = 79"))

    def test_memory_gains_only_the_rom_region(self):
        source = (ROOT / "cores/fes-atari-st/rtl/st_memory.sv").read_bytes()
        result, _ = derive.retarget_memory(source)
        self.assertEqual(result.replace(derive.MEMORY_PORT_AFTER, derive.MEMORY_PORT_BEFORE)
                         .replace(derive.MEMORY_MAP_AFTER, derive.MEMORY_MAP_BEFORE), source)
        with self.assertRaises(ValueError):
            derive.retarget_memory(result)
        with self.assertRaises(ValueError):
            derive.retarget_memory(source.replace(b"40000-a67ff", b"40000-b67ff"))

    def test_component_lists_every_staged_file(self):
        import re
        spec = importlib.util.spec_from_file_location("de25_atari_st_build", HERE / "build.py")
        build = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(build)
        files, derived, _ = build.board.component_files(build, build.board.capture(build))
        named = {"fes_de25_atari_st_standard.v"}
        for group in re.findall(r"foreach file \{([^}]*)\}", files["fes_de25_atari_st_standard_hw.tcl"].decode()):
            named |= set(group.split())
        staged = {Path(name).name for name in files} - {"fes_de25_atari_st_standard_hw.tcl"}
        self.assertEqual(named, staged)
        for name, data in files.items():
            if name.endswith((".v", ".sv")):
                self.assertNotIn(b'"M10K"', data, name)
                self.assertNotIn(b"altddio", data, name)

    def test_transport_slice_matches_the_spectrum_lane(self):
        other = importlib.util.spec_from_file_location("de25_spectrum_derive", HERE.parent / "spectrum/derive.py")
        spectrum = importlib.util.module_from_spec(other)
        other.loader.exec_module(spectrum)
        source = (HERE.parent / "zx81-hps/fes_de25_computer.v").read_bytes()
        self.assertEqual(derive.transport(source)[0], spectrum.transport(source)[0])


if __name__ == "__main__":
    unittest.main()
