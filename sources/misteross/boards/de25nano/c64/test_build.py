"""Derivation contracts for the DE25 Commodore 64 component (no simulator or Quartus)."""
from pathlib import Path
import importlib.util
import re
import unittest

HERE = Path(__file__).resolve().parent
ROOT = HERE.parents[2]
C64 = ROOT / "cores/fes-c64/rtl"


def load(name):
    spec = importlib.util.spec_from_file_location("de25_c64_" + name, HERE / (name + ".py"))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


derive = load("derive")


class Derivations(unittest.TestCase):
    def test_machine_gains_only_the_firmware_port(self):
        source = (C64 / "c64_machine.sv").read_bytes()
        result, record = derive.retarget_machine(source)
        self.assertNotIn(b"c64_rom ", result)
        self.assertEqual(result.count(b"fes_c64_firmware_rom rom ("), 1)
        self.assertEqual(result.count(b"firmware_write_enable"), 2)
        self.assertEqual(record["source_sha256"], derive.sha(source))
        # Every other line of the DE10 machine is compiled as it is.
        kept = set(source.splitlines())
        added = [line for line in result.splitlines() if line not in kept]
        self.assertEqual(len(added), 10)

    def test_a_changed_machine_is_refused(self):
        source = (C64 / "c64_machine.sv").read_bytes()
        with self.assertRaises(ValueError):
            derive.retarget_machine(source.replace(b"    c64_rom rom (", b"    c64_rom  rom ("))
        with self.assertRaises(ValueError):
            derive.retarget_machine(source + derive.ROM_BEFORE)

    def test_block_memory_hints_are_the_only_ram_change(self):
        for name, changes in derive.RAM_FILES.items():
            source = (C64 / name).read_bytes()
            result, _ = derive.retarget_ram(name, source)
            self.assertNotIn(b"M10K", re.sub(rb"//[^\n]*", b"", result))
            for before, after, _ in changes:
                result = result.replace(after, before)
            self.assertEqual(result, source)

    def test_disk_store_is_two_single_write_lanes(self):
        source = (C64 / "c64_disk_store.v").read_bytes()
        result, record = derive.retarget_disk_store(source)
        self.assertNotIn(b"image", result.split(b"module c64_disk_store", 1)[1].split(b");", 1)[1])
        self.assertEqual(result.count(b'(* ramstyle = "M20K" *) reg [7:0] '), 2)
        # Each lane has one write statement and one read.
        for lane in (b"even", b"odd"):
            self.assertEqual(result.count(b"if (" + lane + b"_write) " + lane + b"["), 1)
            self.assertEqual(result.count(lane + b"_q <= " + lane + b"[read_addr[17:1]];"), 1)
        # The module's ports are the DE10's.
        self.assertEqual(result.split(b");", 1)[0], source.split(b");", 1)[0])
        self.assertEqual(record["source_sha256"], derive.sha(source))
        with self.assertRaises(ValueError):
            derive.retarget_disk_store(source.replace(b"read_data <= image[address_a];",
                                                      b"read_data <= image[read_addr];"))

    def test_frame_store_is_inferred_and_its_reader_reset_synchronized(self):
        source = (C64 / "c64_frame_store.sv").read_bytes()
        result, record = derive.retarget_frame_store(source)
        self.assertNotIn(b"MISTRAL", result)
        self.assertNotIn(b"FES_SIMULATION", result)
        self.assertEqual(result.count(b'(* ramstyle = "M20K" *) reg [3:0] page'), 3)
        # Each page has its own write statement in the system clock.
        for page in range(3):
            self.assertEqual(result.count(b"write_page == 2'd%d) page%d[write_address] <= write_data;" %
                                          (page, page)), 1)
        # The reader resets from a registered level through two pixel stages,
        # and nothing in the pixel clock reads the raw machine reset.
        self.assertIn(b"always @(posedge clk_sys) reset_source <= reset;", result)
        self.assertIn(b"reset_meta <= reset_source; reset_pixel <= reset_meta;", result)
        pixel = result[result.index(b"request_meta <= publish_toggle;"):]
        self.assertNotIn(b"(reset)", pixel)
        self.assertEqual(record["source_sha256"], derive.sha(source))
        with self.assertRaises(ValueError):
            derive.retarget_frame_store(source.replace(b"MISTRAL_M10K", b"MISTRAL_M20K"))
        with self.assertRaises(ValueError):
            derive.retarget_frame_store(source.replace(b"        if (reset) begin\n            read_page",
                                                       b"        if (reset)  begin\n            read_page"))

    def test_transport_slice_is_the_shared_module(self):
        source = (HERE.parent / "zx81-hps/fes_de25_computer.v").read_bytes()
        result, _ = derive.transport(source)
        self.assertEqual(result.count(b"\nmodule "), 1)
        self.assertIn(b"module fes_de25_computer_transport (", result)
        with self.assertRaises(ValueError):
            derive.transport(source.replace(derive.TRANSPORT_MARKER, b""))

    def test_no_cyclone_primitive_is_staged(self):
        build = load("build")
        files, _, _ = build.board.component_files(build, build.board.capture(build))
        for name, data in files.items():
            if name.endswith((".v", ".sv")):
                self.assertNotIn(b"MISTRAL_", data, name)
                self.assertNotIn(b"cyclonev_", data, name)

    def test_component_lists_every_staged_file(self):
        build = load("build")
        files, derived, _ = build.board.component_files(build, build.board.capture(build))
        tcl = files["fes_de25_c64_standard_hw.tcl"].decode()
        named = set()
        for group in re.findall(r"foreach file \{([^}]*)\}", tcl):
            named |= set(group.split())
        named.add("fes_de25_c64_standard.v")
        staged = {Path(name).name for name in files} - {"fes_de25_c64_standard_hw.tcl"}
        self.assertEqual(named, staged)
        self.assertEqual(set(derived), {"c64_machine_de25.sv", "c64_ram_de25.v", "c64_color_ram_de25.v",
            "c64_disk_store_de25.v", "c64_frame_store_de25.sv", "fes_de25_computer_transport.v",
            "fes_menu_reader_de25.v"})
        self.assertEqual(build.FIRMWARE_BYTES, {"pal": 36864})


if __name__ == "__main__":
    unittest.main()
