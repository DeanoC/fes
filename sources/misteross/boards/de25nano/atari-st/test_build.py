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
        self.assertLess(len(added), 80)
        # The pixel side chooses among its own copies of the publication metadata.
        self.assertEqual(len(record["crossing_changes"]), len(derive.NATIVE_CROSSING_EDITS))
        self.assertIn(b"newer_publication(publication_pixel[1], publication_pixel[0])", result)
        self.assertNotIn(b"height[choose_bank]", result)
        self.assertNotIn(b"pub_sync ^ seen", result)
        for name in (b"pub_meta0", b"pub_meta2", b"release_meta1", b"published_launch0", b"released_launch2"):
            self.assertEqual(result.count(name + b" <= "), 1, name)
        self.assertNotIn(b"pub_meta <= ", result)
        with self.assertRaises(ValueError):
            derive.retarget_native_video(result)
        with self.assertRaises(ValueError):
            derive.retarget_native_video(source.replace(b"FRAME_CHUNKS = 78", b"FRAME_CHUNKS = 79"))

    def test_video_adapter_reads_each_bundle_from_a_receiving_clock_copy(self):
        source = (ROOT / "cores/fes-atari-st/rtl/st_video_adapter.sv").read_bytes()
        result, record = derive.retarget_video_adapter(source)
        self.assertEqual(len(record["changes"]), len(derive.ADAPTER_EDITS))
        # The configuration bundle crosses once, into its copy; the job's
        # start word and bounds check once each, into their copies.
        self.assertEqual(result.count(b"config_bundle_pixel <= config_bundle_sys;"), 1)
        self.assertEqual(result.count(b" <= config_bundle_sys;"), 1)
        self.assertEqual(result.count(b"ALLOW_SYNCH_CTRL_USAGE OFF"), 3)
        self.assertEqual(result.count(b"ALLOW_REGISTER_RETIMING OFF"), 3)
        self.assertNotIn(b"pending_config", result)
        for line in (b"job_start_sys[0] <= job_start[0][17:0];", b"job_start_ok_sys[0] <= job_start_ok[0];"):
            self.assertEqual(result.count(line), 1, line)
        # Every toggle and level has a launch register and a scalar synchronizer, assigned once, unreset.
        for name in (b"hold_meta", b"config_ack_meta", b"config_req_meta", b"job_req_meta0", b"job_req_meta1",
                     b"job_done_meta0", b"job_done_meta1", b"job_ok_meta0", b"job_high_meta1",
                     b"job_request_launch0", b"job_done_launch1", b"job_ok_launch0", b"job_high_launch1"):
            self.assertEqual(result.count(name + b" <= "), 1, name)
        for name in (b"job_start[selected_bank]", b"job_start_ok[selected_bank]", b"job_high[selected_bank]",
                     b"job_req_sync[selected_bank]", b"job_ok[bank]", b"job_req_meta <=", b"job_done_meta <="):
            self.assertNotIn(name, result)
        self.assertEqual(result.count(b'(* ramstyle = "MLAB" *)'), 2)
        with self.assertRaises(ValueError):
            derive.retarget_video_adapter(result)
        with self.assertRaises(ValueError):
            derive.retarget_video_adapter(source.replace(b"reg [169:0] pending_config;", b"reg [170:0] pending_config;")
                                          .replace(b"pending_config <= config_bundle_sys;", b"pending_config <= 0;"))

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
