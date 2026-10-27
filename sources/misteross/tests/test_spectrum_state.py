"""Snapshot conversion for the Spectrum development runner (sim/spectrum_state.py)."""
import importlib.util
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
_spec = importlib.util.spec_from_file_location(
    "spectrum_state", ROOT / "cores/fes-spectrum/sim/spectrum_state.py")
state_module = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(state_module)


def bank_bytes(fill):
    return bytes([fill]) * 16384


def le(value):
    return bytes([value & 0xff, value >> 8])


def sna_header(sp, iff2=1, im=1, border=2):
    # I, HL', DE', BC', AF', HL, DE, BC, IY, IX, IFF2 (bit 2), R, AF, SP, IM, border
    return (bytes([0x3f]) + le(0x1111) + le(0x2222) + le(0x3333) + le(0x4444) + le(0x5555) +
            le(0x6666) + le(0x7777) + le(0x8888) + le(0x9999) + bytes([iff2 << 2, 0x42]) +
            le(0xaabb) + le(sp) + bytes([im, border]))


def z80_header(pc, r=0x85, flags=0x05):
    # A, F, BC, HL, PC, SP, I, R, flags (R bit 7, border, compression), DE, BC', DE', HL',
    # A', F', IY, IX, IFF1, IFF2, IM.
    return (bytes([0x12, 0x34]) + le(0x0102) + le(0x0304) + le(pc) + le(0xfff0) +
            bytes([0x3f, r & 0x7f, flags]) + le(0x0506) + le(0x0708) + le(0x090a) + le(0x0b0c) +
            bytes([0x0d, 0x0e]) + le(0x0f10) + le(0x1112) + bytes([1, 0, 2]))


def page_block(page, data, compress):
    if not compress:
        return le(0xffff) + bytes([page]) + data
    out = bytearray()
    i = 0
    while i < len(data):
        run = 1
        while i + run < len(data) and data[i + run] == data[i] and run < 255:
            run += 1
        if run >= 5 or data[i] == 0xed and run >= 2:
            out += bytes([0xed, 0xed, run, data[i]])
        elif data[i] == 0xed:
            # A lone ED is followed by one literal byte, so ED ED only marks a run.
            out += data[i:i + 2]
            run = len(data[i:i + 2])
        else:
            out += bytes([data[i]]) * run
        i += run
    return le(len(out)) + bytes([page]) + bytes(out)


def z80_v3(hardware, pages, port7ffd=0, port1ffd=None):
    extra = bytearray(55 if port1ffd is not None else 54)
    extra[0:2] = le(0x8000)
    extra[2] = hardware
    extra[3] = port7ffd
    if port1ffd is not None:
        extra[54] = port1ffd
    return z80_header(0) + le(len(extra)) + bytes(extra) + b"".join(pages)


class SnapshotTests(unittest.TestCase):
    def test_48k_sna_pops_pc_from_the_stack(self):
        ram = bytearray(bank_bytes(0x55) + bank_bytes(0x22) + bank_bytes(0x00))
        ram[0xfff0 - 0x4000:0xfff2 - 0x4000] = le(0x1234)
        state = state_module.read_sna(sna_header(0xfff0) + bytes(ram))
        self.assertEqual(state["model"], "48")
        self.assertEqual(state["regs"]["pc"], 0x1234)
        self.assertEqual(state["regs"]["sp"], 0xfff2)
        self.assertEqual((state["regs"]["iff1"], state["regs"]["iff2"], state["regs"]["im"]), (1, 1, 1))
        self.assertEqual((state["regs"]["af"], state["regs"]["hl_"], state["border"]), (0xaabb, 0x1111, 2))
        self.assertEqual({bank: data[0] for bank, data in state["banks"].items()}, {5: 0x55, 2: 0x22, 0: 0x00})

    def test_128k_sna_places_the_paged_bank_and_the_rest_in_order(self):
        rest = b"".join(bank_bytes(0x10 + bank) for bank in (0, 1, 4, 6, 7))
        data = (sna_header(0x8000) + bank_bytes(0x15) + bank_bytes(0x12) + bank_bytes(0x13) +
                le(0x4567) + bytes([0x13, 0]) + rest)
        self.assertEqual(len(data), 131103)
        state = state_module.read_sna(data)
        self.assertEqual((state["model"], state["regs"]["pc"], state["port7ffd"]), ("128", 0x4567, 0x13))
        self.assertEqual({bank: data[0] for bank, data in state["banks"].items()},
                         {5: 0x15, 2: 0x12, 3: 0x13, 0: 0x10, 1: 0x11, 4: 0x14, 6: 0x16, 7: 0x17})

    def test_z80_v1_decompresses_runs_and_literal_ed(self):
        memory = bytearray(49152)
        memory[0:3] = b"\xed\x01\xed"
        memory[100:400] = b"\x77" * 300
        packed = page_block(0, bytes(memory), compress=True)[3:] + b"\x00\xed\xed\x00"
        state = state_module.read_z80(z80_header(0x6000, r=0x85, flags=0x25) + packed)
        self.assertEqual(state["model"], "48")
        self.assertEqual((state["regs"]["pc"], state["regs"]["r"], state["border"]), (0x6000, 0x85, 2))
        self.assertEqual((state["regs"]["af"], state["regs"]["af_"]), (0x1234, 0x0d0e))
        self.assertEqual(state["banks"][5][0:3], b"\xed\x01\xed")
        self.assertEqual(state["banks"][5][100:400], b"\x77" * 300)

    def test_z80_v3_128k_pages_with_and_without_compression(self):
        pages = [page_block(3 + bank, bank_bytes(0x20 + bank), compress=bank % 2 == 0) for bank in range(8)]
        state = state_module.read_z80(z80_v3(4, pages, port7ffd=0x17))
        self.assertEqual((state["model"], state["regs"]["pc"], state["port7ffd"]), ("128", 0x8000, 0x17))
        self.assertEqual({bank: data[0] for bank, data in state["banks"].items()},
                         {bank: 0x20 + bank for bank in range(8)})

    def test_z80_v3_plus2a_reads_port_1ffd(self):
        pages = [page_block(3 + bank, bank_bytes(bank), compress=True) for bank in range(8)]
        state = state_module.read_z80(z80_v3(13, pages, port7ffd=0x10, port1ffd=0x05))
        self.assertEqual((state["model"], state["port7ffd"], state["port1ffd"]), ("+2A", 0x10, 0x05))

    def test_z80_v3_type_3_is_48k_with_mgt(self):
        pages = [page_block(8, bank_bytes(0x55), True), page_block(4, bank_bytes(0x22), True),
                 page_block(5, bank_bytes(0x00), True)]
        state = state_module.read_z80(z80_v3(3, pages))
        self.assertEqual(state["model"], "48")
        self.assertNotIn("port7ffd", state)
        self.assertEqual({bank: data[0] for bank, data in state["banks"].items()}, {5: 0x55, 2: 0x22, 0: 0x00})

    def test_unsupported_hardware_is_refused(self):
        with self.assertRaises(state_module.SnapshotError):
            state_module.read_z80(z80_v3(10, []))

    def test_written_state_lists_registers_ports_border_and_banks(self):
        pages = [page_block(3 + bank, bank_bytes(bank), compress=False) for bank in range(8)]
        state = state_module.read_z80(z80_v3(4, pages, port7ffd=0x05))
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "state.txt"
            state_module.write_state(state, path)
            lines = path.read_text(encoding="ascii").splitlines()
        self.assertIn("reg pc 8000", lines)
        self.assertIn("port7ffd 5", lines)
        self.assertIn("border 2", lines)
        self.assertEqual(sum(line.startswith("bank ") for line in lines), 8)
        self.assertEqual(len(lines), len(state["regs"]) + 2 + 8 * (1 + 512))


if __name__ == "__main__":
    unittest.main()
