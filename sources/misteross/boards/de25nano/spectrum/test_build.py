"""Derivation contracts for the DE25 ZX Spectrum component (no simulator or Quartus)."""
from pathlib import Path
import importlib.util
import unittest

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
SPECTRUM = ROOT / "cores/fes-spectrum/rtl"


def load(name):
    spec = importlib.util.spec_from_file_location("de25_spectrum_" + name, HERE / (name + ".py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


derive = load("derive")


class Derivations(unittest.TestCase):
    def test_machine_gains_only_the_firmware_port(self):
        source = (SPECTRUM / "spectrum_machine.sv").read_bytes()
        result, record = derive.retarget_machine(source)
        self.assertNotIn(b"spectrum_rom", result)
        self.assertNotIn(b"ROM_FILE", result)
        self.assertEqual(result.count(b"fes_spectrum_firmware_rom rom ("), 1)
        self.assertEqual(result.count(b"firmware_write_enable"), 2)
        self.assertEqual(record["source_sha256"], derive.sha(source))
        # Every other line of the DE10 machine is compiled as it is.
        kept = set(source.splitlines())
        added = [line for line in result.splitlines() if line not in kept]
        self.assertEqual(len(added), 10)

    def test_a_changed_machine_is_refused(self):
        source = (SPECTRUM / "spectrum_machine.sv").read_bytes()
        with self.assertRaises(ValueError):
            derive.retarget_machine(source.replace(b"spectrum_rom #(", b"spectrum_rom  #("))
        with self.assertRaises(ValueError):
            derive.retarget_machine(source + derive.ROM_BEFORE)

    def test_block_memory_hints_are_the_only_ram_change(self):
        for name in derive.RAM_FILES:
            source = (SPECTRUM / name).read_bytes()
            result, _ = derive.retarget_ram(name, source)
            self.assertNotIn(b"M10K", result.replace(b"M10K blocks", b""))
            if name == "spectrum_video.v":
                # The banks no longer ride a synchronizer: only the two toggles do.
                self.assertIn(b"ready_meta <= ready_toggle;", result)
                self.assertIn(b"shown_meta <= ready_seen;", result)
                self.assertEqual(result.count(b"shown_code <= ready_code;"), 1)
                self.assertEqual(result.count(b"shown_code_sys <= shown_code;"), 1)
                # Every register on a bank crossing powers up zero.
                for register in (b"ready_code", b"shown_code", b"shown_code_sys"):
                    self.assertIn(b"(* preserve, dont_merge *) reg [1:0] " + register + b" = 2'd0;", result)
                self.assertNotIn(b"{ready_toggle, ready_bank}", result)
                with self.assertRaises(ValueError):
                    derive.retarget_ram(name, source.replace(b"shown_sync <= shown_meta;", b"shown_sync <= 0;"))
            else:
                self.assertEqual(result.replace(b'"M20K"', b'"M10K"'), source)

    def test_transport_selects_native_fetch_explicitly(self):
        source = (HERE.parent / "zx81-hps/fes_de25_computer.v").read_bytes()
        result, _ = derive.transport(source)
        self.assertEqual(result.count(b"\nmodule "), 1)
        self.assertIn(b"module fes_de25_computer_transport (", result)
        self.assertIn(b"`ifdef FES_DE25_NATIVE_SPECTRUM48", result)
        self.assertIn(b"`FES_DE25_COMPUTER_FETCH_COUNT_OFFSET: read_value = fetch_sample[47:16]", result)
        self.assertIn(b"`FES_DE25_COMPUTER_VIDEO_STATUS_OFFSET: read_value = video_status", result)
        with self.assertRaises(ValueError):
            derive.transport(source.replace(derive.TRANSPORT_MARKER, b""))

    def test_component_lists_every_staged_file(self):
        build = load("build")
        files, derived, _ = build.board.component_files(build, build.board.capture(build))
        tcl = files["fes_de25_spectrum_standard_hw.tcl"].decode()
        named = set()
        for group in __import__("re").findall(r"foreach file \{([^}]*)\}", tcl):
            named |= set(group.split())
        named.add("fes_de25_spectrum_standard.v")
        staged = {Path(name).name for name in files} - {"fes_de25_spectrum_standard_hw.tcl"}
        self.assertEqual(named, staged)
        self.assertEqual(set(derived), {"spectrum_machine_de25.sv", "spectrum_ram_de25.v",
            "spectrum_video_de25.v", "spectrum_tape_de25.v", "fes_de25_computer_transport.v",
            "fes_menu_reader_de25.v"})


if __name__ == "__main__":
    unittest.main()
