"""#1984 S1: what runs in the pc386 seal process, and the ROM the seal checks.

The seal must not run the lockstep oracle's provisioner, the lockstep suite and
runners, or the firmware recipe: code there could change what the compilers
read (case H1 of docs/validation/2026-11-18-1984-pc386-identity-scope) while
the design parameters stay the same. The seal hashes the oracle and suite as
data, and checks the ROM hex the firmware recipe wrote, as its own process,
against the digest firmware.lock pins (option F2; cases R3, R4 and H2).
"""
from __future__ import annotations

import ast
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

from scripts import build_fes_pc386_quartus as quartus
from scripts import fes_build_common as board
from scripts import source_closure

ROOT = Path(__file__).resolve().parents[1]
TOOLS = ROOT / "cores/fes-pc386/tools"
# The modules the registered producer loads: itself, its siblings and the
# evidence helper it imports lazily.
PRODUCER_MODULES = ("build_pc386_product", "pc386_execution", "pc386_owned_job", "pc386_seal_evidence")
# Evidence and firmware-generator code (#1984 rule (b)): the oracle's
# provisioner, the lockstep suite's Python and the two pc386 lockstep runners,
# and the firmware recipe. yosys_netlist.py is in the suite list but is also
# the seal's Yosys program, so it stays in the seal.
SEAL_EXCLUDED = (
    quartus.ORACLE_PROVISIONER, quartus.FIRMWARE_RECIPE,
    "cores/fes-pc386/tools/build_pc386_with_lockstep.py", "cores/fes-pc386/tools/pc386_run_lockstep.py",
    *(name for name in quartus.SUITE_SOURCES if name.endswith(".py") and name != "scripts/yosys_netlist.py"),
)
# The ROM hex digest firmware.lock pins: the one #1951's seal recorded.
SEALED_ROM_HEX = "815620361ef28a2bbdd97b1964700a4678a26c0ffc9582bb95544473fe099d18"


def _module_name(relative: str) -> str:
    path = Path(relative)
    return ".".join(path.with_suffix("").parts) if path.parts[0] == "scripts" else path.stem


def _run_python(code: str) -> str:
    result = subprocess.run([sys.executable, "-c", code], cwd=ROOT, capture_output=True, text=True, timeout=300)
    if result.returncode:
        raise AssertionError(result.stdout + result.stderr)
    return result.stdout


class SealProcessTest(unittest.TestCase):
    def test_the_excluded_files_exist(self):
        for relative in SEAL_EXCLUDED:
            self.assertTrue((ROOT / relative).is_file(), relative)
        self.assertIn("scripts/fes_i386_oracle.py", SEAL_EXCLUDED)
        self.assertIn("scripts/build_pc386_firmware.py", SEAL_EXCLUDED)

    def test_importing_the_producer_loads_no_evidence_or_firmware_code(self):
        # A fresh interpreter: this test process imports the oracle elsewhere.
        loaded = json.loads(_run_python(f"""
import json, sys
sys.path.insert(0, {str(TOOLS)!r})
import build_pc386_product, pc386_seal_evidence
print(json.dumps(sorted(getattr(module, '__file__', None) or '' for module in list(sys.modules.values()))))
"""))
        loaded = {Path(path).resolve() for path in loaded if path}
        for relative in SEAL_EXCLUDED:
            self.assertNotIn((ROOT / relative).resolve(), loaded, relative)
        self.assertIn((ROOT / "scripts/build_fes_pc386_quartus.py").resolve(), loaded)

    def test_no_evidence_or_firmware_code_is_in_the_static_import_closure(self):
        # Every import in the producer's modules, function bodies included,
        # so a lazy import counts as much as a top-level one.
        closure, pending, seen = set(), list(PRODUCER_MODULES), set()
        while pending:
            name = pending.pop()
            if name in seen:
                continue
            seen.add(name)
            relative = f"cores/fes-pc386/tools/{name}.py"
            closure.add(relative)
            for node in ast.walk(ast.parse((ROOT / relative).read_text(), filename=relative)):
                names = []
                if isinstance(node, ast.Import):
                    names = [alias.name for alias in node.names]
                elif isinstance(node, ast.ImportFrom) and node.module == "scripts":
                    names = [f"scripts.{alias.name}" for alias in node.names]
                elif isinstance(node, ast.ImportFrom) and node.module:
                    names = [node.module]
                for imported in names:
                    if imported.startswith("scripts."):
                        closure |= source_closure.imports(imported.split(".")[1], ROOT)
                    elif (TOOLS / f"{imported}.py").is_file():
                        pending.append(imported)
        self.assertIn("scripts/build_fes_pc386_quartus.py", closure)
        self.assertIn("cores/fes-pc386/tools/pc386_seal_evidence.py", closure)
        self.assertFalse(closure & set(SEAL_EXCLUDED), sorted(closure & set(SEAL_EXCLUDED)))

    def test_the_seal_runs_with_the_evidence_and_firmware_code_unimportable(self):
        # Case H1: an import-time edit to the provisioner cannot reach the QSF,
        # the Yosys program or the ROM check, because nothing in the seal runs
        # it. Every module in SEAL_EXCLUDED raises on import here.
        blocked = sorted(_module_name(relative) for relative in SEAL_EXCLUDED)
        output = _run_python(f"""
import importlib.abc, json, sys
from pathlib import Path
BLOCKED = set({blocked!r})
class Refuse(importlib.abc.MetaPathFinder):
    def find_spec(self, name, path=None, target=None):
        if name in BLOCKED:
            raise ImportError('the seal imported ' + name)
        return None
sys.meta_path.insert(0, Refuse())
sys.path.insert(0, {str(TOOLS)!r})
import build_pc386_product as seal, pc386_seal_evidence
root = seal.ROOT
identity = seal.oracle_identity(root)
seal.require_same_oracle(root, identity)
qsf = seal.project_qsf(root, root / 'build/fes-pc386-quartus/project', '0' * 32, 1)
program = seal.machine_program(Path(seal.MACHINE_NETLIST), '$MACHINE_SOURCES')
print(json.dumps({{'identity': identity, 'firmware': seal.firmware_digests(root),
                  'rom': seal.pinned_rom_digest(root), 'qsf': len(qsf), 'program': len(program),
                  'loaded': sorted(name for name in BLOCKED if name in sys.modules)}}))
""")
        result = json.loads(output)
        self.assertEqual(result["loaded"], [])
        self.assertEqual(result["identity"], quartus.oracle_identity(ROOT))
        self.assertEqual(result["rom"], SEALED_ROM_HEX)


class OracleAsDataTest(unittest.TestCase):
    def test_the_seal_hashes_the_oracle_as_the_provisioner_does(self):
        # The lockstep runner names the oracle through the provisioner; the
        # seal must reach the same digests from the data file alone.
        from scripts import fes_i386_oracle as oracle
        self.assertEqual((quartus.BOCHS_REVISION, quartus.BOCHS_PATCH, quartus.ORACLE_SOURCES,
                          quartus.SUITE_SOURCES, quartus.LINKED_NAMES),
                         (oracle.BOCHS_REVISION, oracle.BOCHS_PATCH_RELATIVE, oracle.ORACLE_SOURCES,
                          oracle.SUITE_SOURCES, oracle.LINKED_NAMES))
        self.assertEqual(oracle.SOURCES_RELATIVE, quartus.ORACLE_SOURCES_FILE)
        self.assertEqual(quartus.oracle_identity(ROOT), {
            "revision": oracle.BOCHS_REVISION,
            "patch_sha256": hashlib.sha256(oracle.BOCHS_PATCH.read_bytes()).hexdigest(),
            "provisioner_sha256": oracle.PROVISIONER_SHA256,
            "adapter_sha256": oracle.adapter_source_sha256(oracle.SIM),
            "suite_sha256": oracle.suite_source_sha256(oracle.ROOT),
        })

    def test_the_data_file_and_everything_it_names_are_pinned(self):
        for relative in (quartus.ORACLE_SOURCES_FILE, quartus.ORACLE_PROVISIONER, quartus.BOCHS_PATCH,
                         *(f"{quartus.ORACLE_SIM}/{name}" for name in quartus.ORACLE_SOURCES),
                         *quartus.SUITE_SOURCES):
            self.assertIn(relative, quartus.PINNED_INPUTS)

    def test_a_malformed_source_list_is_refused(self):
        valid = (ROOT / quartus.ORACLE_SOURCES_FILE).read_text()
        for text in ("", valid.replace("linked_names", "linked"), valid + 'extra = "x"\n',
                     valid.replace('"i386_oracle.h", ', '"bochs_oracle.cpp", '),
                     valid.replace("30cfd566ecc8deb33e56e801e32b444a7041b6d2", "REL_3_1_FINAL")):
            with self.subTest(text=text[-60:]), tempfile.TemporaryDirectory() as temporary:
                path = Path(temporary) / quartus.ORACLE_SOURCES_FILE
                path.parent.mkdir(parents=True)
                path.write_text(text)
                with self.assertRaisesRegex(board.BuildError, "oracle source list|cannot read"):
                    quartus._oracle_sources(Path(temporary))


class RomPinTest(unittest.TestCase):
    """check_rom: the file the RTL reads, against firmware.lock's rom_hex pin."""

    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        for relative in (quartus.FIRMWARE_LOCK, *quartus.ROM_HEX_READERS):
            (self.root / relative).parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / relative, self.root / relative)
        self.rom = self.root / quartus.ROM_HEX
        self.rom.parent.mkdir(parents=True)
        self.contents = b"00112233445566778899aabbccddeeff\n" * 4
        self.rom.write_bytes(self.contents)
        self.pin(hashlib.sha256(self.contents).hexdigest())

    def pin(self, digest):
        lock_path = self.root / quartus.FIRMWARE_LOCK
        lock = json.loads(lock_path.read_text())
        if digest is None:
            lock.pop("rom_hex", None)
        else:
            lock["rom_hex"] = {"file": "pc386-rom.hex", "sha256": digest}
        lock_path.write_text(json.dumps(lock, indent=2) + "\n")

    def test_the_pinned_rom_is_accepted(self):
        self.assertEqual(quartus.check_rom(self.root), self.rom)

    def test_a_rom_other_than_the_pin_is_refused(self):
        # R3 (a layout edit without a new pin) and H2 (in-process code that
        # rewrites the ROM): the bytes Yosys would read are not the pinned ones.
        self.rom.write_bytes(self.contents.replace(b"00", b"ff", 1))
        with self.assertRaisesRegex(board.BuildError, "firmware.lock pins"):
            quartus.check_rom(self.root)

    def test_a_missing_or_linked_rom_is_refused(self):
        # R4: the recipe wrote somewhere else, so nothing is where the RTL reads.
        self.rom.unlink()
        with self.assertRaisesRegex(board.BuildError, "is missing"):
            quartus.check_rom(self.root)
        other = self.root / "elsewhere.hex"
        other.write_bytes(self.contents)
        self.rom.symlink_to(other)
        with self.assertRaisesRegex(board.BuildError, "is missing"):
            quartus.check_rom(self.root)

    def test_an_rtl_path_other_than_the_checked_one_is_refused(self):
        # R4 from the other side: the RTL reads a path the seal does not check.
        for reader in quartus.ROM_HEX_READERS:
            with self.subTest(reader=reader):
                path = self.root / reader
                original = path.read_text()
                path.write_text(original.replace(quartus.ROM_HEX, "build/firmware/pc386/other.hex"))
                with self.assertRaisesRegex(board.BuildError, "reads the ROM from"):
                    quartus.check_rom(self.root)
                path.write_text(original)

    def test_an_unpinned_rom_is_refused(self):
        for digest in (None, "not a digest"):
            with self.subTest(digest=digest):
                self.pin(digest)
                with self.assertRaisesRegex(board.BuildError, "does not pin rom_hex.sha256"):
                    quartus.check_rom(self.root)

    def test_the_tree_pins_the_sealed_rom_where_the_rtl_reads_it(self):
        from scripts import build_pc386_firmware as firmware
        self.assertEqual(quartus.pinned_rom_digest(ROOT), SEALED_ROM_HEX)
        self.assertEqual(firmware.read_lock()["rom_hex"], {"file": firmware.ROM_HEX_FILE, "sha256": SEALED_ROM_HEX})
        self.assertEqual(firmware.ROM_HEX.relative_to(firmware.ROOT).as_posix(), quartus.ROM_HEX)
        for reader in quartus.ROM_HEX_READERS:
            self.assertEqual(quartus.ROM_HEX_PARAMETER_RE.findall((ROOT / reader).read_text()), [quartus.ROM_HEX])
        # The seal's Yosys program leaves the path at the RTL default.
        self.assertNotIn("ROM_HEX", quartus.guest_rates())
        self.assertNotIn("ROM_HEX", quartus.machine_program(Path(quartus.MACHINE_NETLIST)))


class FirmwareRomPinTest(unittest.TestCase):
    """The firmware recipe writes no ROM hex that differs from the pin."""

    def setUp(self):
        from scripts import build_pc386_firmware as firmware
        self.firmware = firmware
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        directory = Path(self.temporary.name)
        self.paths = {"bios": directory / "bios.bin", "vgabios": directory / "vgabios.bin"}
        self.paths["bios"].write_bytes(bytes(range(256)) * 256)
        self.paths["vgabios"].write_bytes(b"\x55\xaa" * 0x3000)
        self.lock = directory / "firmware.lock"
        self.rom = directory / "pc386-rom.hex"
        self.digest = hashlib.sha256(firmware.rom_hex(self.paths).encode()).hexdigest()
        for name, value in (("LOCK", self.lock), ("ROM_HEX", self.rom)):
            patcher = mock.patch.object(firmware, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)

    def write_lock(self, rom_hex):
        lock = json.loads((ROOT / quartus.FIRMWARE_LOCK).read_text())
        lock["rom_hex"] = rom_hex
        self.lock.write_text(json.dumps(lock))

    def test_the_pinned_rom_is_written(self):
        self.write_lock({"file": "pc386-rom.hex", "sha256": self.digest})
        self.assertEqual(self.firmware.write_rom_hex(self.paths), self.rom)
        self.assertEqual(hashlib.sha256(self.rom.read_bytes()).hexdigest(), self.digest)

    def test_a_rom_other_than_the_pin_is_not_written(self):
        for pin in ({"file": "pc386-rom.hex", "sha256": "0" * 64}, {"file": "other.hex", "sha256": self.digest}, {}):
            with self.subTest(pin=pin):
                self.write_lock(pin)
                with self.assertRaisesRegex(self.firmware.FirmwareError, "does not match firmware.lock"):
                    self.firmware.write_rom_hex(self.paths)
                self.assertFalse(self.rom.exists())


if __name__ == "__main__":
    unittest.main()
