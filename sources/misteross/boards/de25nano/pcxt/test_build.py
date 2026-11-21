"""Derivation contracts for the DE25 PC/XT component (no simulator or Quartus)."""
from pathlib import Path
import importlib.util
import re
import sys
import unittest

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]


def load(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


derive = load(HERE / "derive.py", "de25_pcxt_derive")


class Derivations(unittest.TestCase):
    def test_control_rom_matches_the_de10_builds(self):
        sys.path.insert(0, str(ROOT / "scripts"))
        import sim_fes_pcxt
        import tempfile
        with tempfile.TemporaryDirectory() as directory:
            sim_fes_pcxt.cpu_roms(Path(directory))
            files = derive.control_rom((ROOT / "cores/fes-pcxt/vendor/mcl86/mcl86_ucode.mem").read_bytes())
            self.assertEqual(set(files), {p.name for p in Path(directory).iterdir()} - {"mcl86_ucode.mem"})
            for name, data in files.items():
                self.assertEqual(data, (Path(directory) / name).read_bytes(), name)
        with self.assertRaises(ValueError):
            derive.control_rom(b"00000000\n")

    def test_framebuffer_ram_keeps_its_inferred_memory_in_m20k(self):
        source = (ROOT / "cores/fes-common/rtl/coleco_video_dpram.v").read_bytes()
        result, record = derive.retarget_video_ram(source)
        for removed in (b"altsyncram", b"Cyclone", b'"M10K"', b"`ifdef", b"`else", b"`endif"):
            self.assertNotIn(removed, result)
        self.assertEqual(result.count(b'(* ramstyle = "M20K" *)'), 1)
        # Everything but the Cyclone V branch and the RAM style survives in order.
        begin, middle = source.index(derive.RAM_BEGIN), source.index(derive.RAM_ELSE)
        end = source.index(derive.RAM_END)
        kept = (source[:begin] + source[middle + len(derive.RAM_ELSE):end] +
                source[end + len(derive.RAM_END):]).replace(*derive.RAM_STYLE)
        self.assertEqual(result, kept)
        self.assertEqual(record["source_sha256"], derive.sha(source))
        with self.assertRaises(ValueError):
            derive.retarget_video_ram(result)
        with self.assertRaises(ValueError):
            derive.retarget_video_ram(source.replace(b'"Cyclone V"', b'"Cyclone 10"'))

    def test_video_mailbox_crosses_through_free_synchronizers_and_a_held_copy(self):
        source = (ROOT / "cores/fes-pcxt/rtl/pcxt_video.sv").read_bytes()
        result, record = derive.retarget_video(source)
        self.assertEqual(len(record["changes"]), len(derive.VIDEO_EDITS))
        # Each first stage is assigned once, outside the reset branches.
        for name in (b"ack_meta <= ", b"sent_meta <= ", b"hold_meta <= ", b"hold_launch <= "):
            self.assertEqual(result.count(name), 1, name)
        for name in (b"ack_meta <= 0", b"sent_meta <= 0", b"hold_meta <= 0", b"hold_meta <= hold;"):
            self.assertNotIn(name, result)
        # The pixel clock reads the bank only through its copy; the machine
        # clock's own read of it stays.
        self.assertEqual(result.count(b"published_bank;"), 2)
        self.assertIn(b"source_front <= published_bank;", result)
        self.assertIn(b"if (sent_sync != received) published_pixel <= published_bank;", result)
        self.assertNotIn(b"? published_bank : front", result)
        with self.assertRaises(ValueError):
            derive.retarget_video(result)

    def test_component_lists_every_staged_file(self):
        build = load(HERE / "build.py", "de25_pcxt_build")
        files, derived, _ = build.board.component_files(build, build.board.capture(build))
        named = {"fes_de25_pcxt_standard.v"}
        for group in re.findall(r"foreach file \{([^}]*)\}", files["fes_de25_pcxt_standard_hw.tcl"].decode()):
            named |= set(group.split())
        staged = {Path(name).name for name in files} - {"fes_de25_pcxt_standard_hw.tcl"}
        self.assertEqual(named, staged)
        for name, data in files.items():
            if name.endswith((".v", ".sv")):
                self.assertNotIn(b'"M10K"', data, name)
                self.assertNotIn(b"altddio", data, name)
                self.assertNotIn(b"altsyncram", data, name)
                self.assertNotIn(b"cyclonev_", data, name)
        self.assertEqual(set(derived), {"coleco_video_dpram_de25.v", "sdram_addon_port_de25.v", "pcxt_video_de25.sv",
                                        "fes_de25_computer_transport.v", "fes_menu_reader_de25.v"})
        # The DE10 machine sources are staged unchanged.
        for name in build.MACHINE_FILES:
            self.assertEqual(files[Path(name).name], (ROOT / "cores/fes-pcxt" / name).read_bytes(), name)

    def test_clock_spec_is_the_de10_machine(self):
        build = load(HERE / "build.py", "de25_pcxt_build")
        machine_hz, pll_ip, parameters, sdram = build.board.clocks(build)
        self.assertEqual(machine_hz, 100000000)
        self.assertEqual(dict(sdram), {"clk_forward": 2, "clk_handoff": 3, "clk_capture": 4})
        display = load(HERE.parent / "display/build.py", "de25_pcxt_display")
        ip = (ROOT.parents[1] / pll_ip).read_bytes()
        self.assertEqual(build.board.machine_pll(build, display, ip)["gui_output_clock_frequency0"], "100.0")
        top = build.board.top_after(build)
        for line in (".outclk_2(machine_sdram_clk_forward), .outclk_3(machine_sdram_clk_handoff), "
                     ".outclk_4(machine_sdram_clk_capture),",
                     ".fes_scanout_sdram_clk_capture(machine_sdram_clk_capture),"):
            self.assertIn(line, top)
        # The shared PLL's machines keep the board top exactly.
        st = load(HERE.parent / "atari-st/build.py", "de25_pcxt_st_build")
        self.assertIs(st.board.top_after(st), st.board.TOP_AFTER)
        self.assertEqual(st.board.clocks(st)[0], 52222222)


if __name__ == "__main__":
    unittest.main()
