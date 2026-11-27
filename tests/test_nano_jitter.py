"""Exercise private Jitter RNG source staging and the final ELF/link-map joins."""
from pathlib import Path
import shutil
import struct
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
sys.path[:0] = [str(ROOT / "platform/embedded/firmware"), str(ROOT / "tests")]
import nano_jitter as jitter
from test_nano_firmware_mmu import SCRIPT


class JitterSourceTests(unittest.TestCase):
    def test_install_and_reject_changed_staged_file(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            result = jitter.install(root, "seed")
            rules = (root / jitter.INCLUDE).read_text()
            self.assertIn("--mode seed", rules)
            self.assertIn("$(filter BL31,$(BL))", rules)
            self.assertEqual(jitter.install(root, "seed"), result)
            self.assertFalse((root / "bl31/bl31.mk").exists())
            with self.assertRaisesRegex(ValueError, "conflicting"):
                jitter.install(root, "capture")
            path = root / jitter.SERVICE / jitter.VENDOR / "src/jitterentropy-health.c"
            path.write_bytes(path.read_bytes() + b"\n")
            with self.assertRaisesRegex(ValueError, "inputs changed"):
                jitter.verify(root, b"", "seed")

    def test_relocation_targets(self):
        # BL, ADRP and ADD against the same address, with independently
        # calculated immediates for PC+0x2344 and page+2.
        code = struct.pack("<III", 0x94000000, 0x90000000, 0x91000000)
        refs = [(i * 4, kind, "target", "", 0) for i, kind in enumerate((283, 275, 277))]
        linked = jitter.relocated_bytes(code, refs, 0x10000, {}, {"target": (0x12344, 0)})
        self.assertEqual(struct.unpack("<III", linked), (0x940008d1, 0xd0000000, 0x910d1000))
        with self.assertRaisesRegex(ValueError, "unlinked"):
            jitter.relocated_bytes(code, refs, 0x10000, {}, {})
        with self.assertRaisesRegex(ValueError, "out-of-range"):
            jitter.relocated_bytes(code, refs, 0x10000, {}, {"target": (0x91000000, 0)})
        with self.assertRaisesRegex(ValueError, "unsupported"):
            jitter.relocated_bytes(code, [(0, 0, "target", "", 0)], 0, {}, {"target": (0, 0)})


@unittest.skipUnless(shutil.which("cc") and shutil.which("ld"), "requires a host compiler and GNU ld")
class JitterLinkTests(unittest.TestCase):
    def fixture(self, directory, mode, extra=""):
        # The fixture consists of fixed words and has no instruction relocations.
        # Use host GNU ld to emit real load headers, symbol tables, map and MMU
        # tables. Mark the resulting ELF64 fixture for the target architecture.
        firmware = directory / "firmware.S"
        firmware.write_text(".globl bl31_entrypoint\n.text\nbl31_entrypoint: .long 0\n.bss\n.space 0x100\n")
        assembly = directory / "jitter.S"
        asm = []
        for name in jitter.HOOKS[mode]:
            asm.extend([f'.section .text.{name},"ax",@progbits', f".global {name}",
                        f".type {name},@function", name + ": .long 0xd65f03c0", f".size {name},.-{name}"])
        asm.extend(['.section .data.fes_jent,"aw",@progbits', ".balign 16", ".zero 196608", extra])
        assembly.write_text("\n".join(asm) + "\n")
        script = directory / "bl31.ld"
        script.write_text(jitter.mmu.inject_linker(SCRIPT).replace("*(.text)", "*(.text*)")
                          .replace("*(.data)", "*(.data*)"))
        obj = directory / "fes_jitter.o"
        base = directory / "firmware.o"
        elf = directory / "bl31.elf"
        mapfile = directory / "bl31.map"
        for argv in (["cc", "-c", str(firmware), "-o", str(base)], ["cc", "-c", str(assembly), "-o", str(obj)],
                     ["ld", "-T", str(script), "-Map", str(mapfile), str(base), str(obj), "-o", str(elf)]):
            subprocess.run(argv, check=True, capture_output=True)
        def target(path):
            data = bytearray(path.read_bytes())
            struct.pack_into("<H", data, 18, 183)
            return bytes(data)
        return target(elf), obj, target(obj), mapfile.read_text()

    def test_linked_modes_and_tampering(self):
        for mode in jitter.HOOKS:
            with self.subTest(mode=mode), tempfile.TemporaryDirectory() as tmp:
                elf, obj, raw, map_text = self.fixture(Path(tmp), mode)
                proof = jitter.verify_link(elf, mode, obj, raw, map_text)
                self.assertEqual(set(proof["hooks"]), set(jitter.HOOKS[mode]))
                self.assertEqual(proof["mutable"]["bytes"], 196608)
                opposite = "capture" if mode == "seed" else "seed"
                with self.assertRaises(ValueError):
                    jitter.verify_link(elf, opposite, obj, raw, map_text)
                damaged = elf.replace(struct.pack("<I", 0xd65f03c0), b"BAD!", 1)
                with self.assertRaisesRegex(ValueError, "input bytes changed"):
                    jitter.verify_link(damaged, mode, obj, raw, map_text)
                with self.assertRaisesRegex(ValueError, "incomplete"):
                    jitter.verify_link(elf, mode, obj, raw, map_text.replace(str(obj), "different.o"))
                table = jitter.ram.symbols(elf)
                start = proof["mutable"]["address"]
                expected = jitter.mmu.expected_tables(jitter.mmu.layout(table))["firmware"]
                pos = elf.index(expected) + ((start - jitter.mmu.DDR_BASE) // jitter.mmu.PAGE) * 8
                nonsecure = bytearray(elf)
                nonsecure[pos] |= jitter.mmu.NS
                with self.assertRaisesRegex(ValueError, "table permissions"):
                    jitter.verify_link(nonsecure, mode, obj, raw, map_text)

    def test_bss_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            _, _, raw, _ = self.fixture(Path(tmp), "seed", '.section .bss.forgotten,"aw",@nobits\n.space 16')
            with self.assertRaisesRegex(ValueError, "unrecognized.*bss"):
                jitter.object_layout(raw)


if __name__ == "__main__":
    unittest.main()
