from __future__ import annotations

import hashlib
import tomllib
import subprocess
import sys
import tempfile
import unittest
from tests.producer_fixture import clean_module, init_source, EXECUTION, FakeInvocation
from pathlib import Path

from scripts.build_fes_sg1000 import (
    OUTPUT_RELATIVE as QUARTUS_OUTPUT,
    PINNED_INPUTS as QUARTUS_PINNED_INPUTS,
    SYSTEMVERILOG_SOURCES,
    VERILOG_SOURCES,
    _manifest,
    project_qsf,
)
from scripts.build_fes_sg1000_oss import (
    OUTPUT_RELATIVE as OSS_OUTPUT,
    PINNED_INPUTS as OSS_PINNED_INPUTS,
    RTL_SOURCES as OSS_RTL_SOURCES,
    SEED,
    SG1000_GPU_ARCHITECTURES,
    SG1000_GPU_BACKEND,
    SG1000_TOOLCHAIN_LOCK,
    SG1000_TOOLCHAIN_ROOT,
    SG1000_TOOL_COMMITS,
    build_commands,
    create_build_record,
    _manifest as _oss_manifest,
)
from scripts.lockfile import load_lock


ROOT = Path(__file__).resolve().parents[1]
GENERATOR = ROOT / "cores/fes-sg1000/diagnostic/generate.py"


class BuildFesSg1000Tests(unittest.TestCase):
    def test_make_entrypoints_use_both_recipes(self) -> None:
        for target, recipe in (
            ("build-fes-sg1000", "scripts/build_fes_sg1000_oss.py"),
            ("build-fes-sg1000-quartus", "scripts/build_fes_sg1000.py"),
        ):
            result = subprocess.run(
                ["make", "-n", target],
                cwd=ROOT,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                check=False,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(
                result.stdout.strip(),
                f'python3 {recipe} --root "{ROOT}"',
            )

    def test_oss_simulation_entrypoint_compiles_conditional_branches(self) -> None:
        result = subprocess.run(
            ["make", "-n", "sim-fes-sg1000-oss"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("-DFES_SG1000_OSS=1", result.stdout)
        self.assertIn("-DFES_COLECO_OSS=1", result.stdout)
        self.assertIn('-CFLAGS "-DFES_SG1000_OSS=1 -DFES_COLECO_OSS=1"', result.stdout)
        self.assertIn("fes-sg1000-machine-oss", result.stdout)
        default = subprocess.run(
            ["make", "-n", "sim-fes-sg1000"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(default.returncode, 0, default.stderr)
        self.assertNotIn("FES_SG1000_OSS", default.stdout)
        self.assertNotIn("FES_COLECO_OSS", default.stdout)
        self.assertIn("fes-sg1000-machine", default.stdout)
        self.assertNotIn("fes-sg1000-machine-oss", default.stdout)

    def test_linked_simulation_uses_sealed_rom_path(self) -> None:
        result = subprocess.run(["make", "-n", "sim-fes-sg1000-rom-link"], cwd=ROOT,
                                text=True, capture_output=True, check=False)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("-DFES_SG1000_ROM_LINK=1", result.stdout)
        self.assertIn("sg1000_rom_link.v", result.stdout)
        self.assertIn("graphics-i-16k.hex", result.stdout)

    def test_oss_toolchain_entrypoint_selects_coleco_compatibility_lock(self) -> None:
        result = subprocess.run(
            ["make", "-n", "toolchain-fes-sg1000"],
            cwd=ROOT,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
        )
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("FES_TOOLCHAIN_LOCKFILE=", result.stdout)
        self.assertIn("toolchains/registered-memory.lock", result.stdout)
        self.assertIn("FES_TOOLCHAIN_ROOT=", result.stdout)
        self.assertIn("build/toolchain/fes-sg1000", result.stdout)
        self.assertIn("FES_TOOLCHAIN_GPU_ROUTER=HIP", result.stdout)
        self.assertIn("FES_TOOLCHAIN_HIP_ARCHITECTURES='gfx1100;gfx1201'", result.stdout)

    def test_oss_commands_use_dual_defines_and_coleco_lock(self) -> None:
        yosys, nextpnr = build_commands(
            ROOT,
            ROOT / OSS_OUTPUT,
            "00112233445566778899aabbccddeeff",
            {"yosys": Path("/tmp/yosys"), "nextpnr-mistral": Path("/tmp/nextpnr-mistral")},
        )
        program = yosys[2]
        self.assertIn("sg1000_machine.sv", program)
        self.assertIn("cores/fes-sg1000/rtl/top.v", program)
        self.assertIn("tv80_core.v", program)
        self.assertIn("coleco_vdp.sv", program)
        self.assertIn("coleco_dpram.v", program)
        self.assertIn("-DFES_SG1000_OSS=1", program)
        self.assertIn("-DFES_SG1000_ROM_LINK=1", program)
        self.assertIn("cores/fes-sg1000/rtl/sg1000_rom_link.v", OSS_RTL_SOURCES)
        self.assertIn("-DFES_COLECO_OSS=1", program)
        self.assertIn("-DTV80_REFRESH=1", program)
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", program)
        self.assertNotIn("coleco_machine.sv", program)
        self.assertNotIn("coleco_reset_rom", program)
        self.assertEqual(SEED, 4)
        self.assertEqual(nextpnr[nextpnr.index("--seed") + 1], str(SEED))
        self.assertEqual(nextpnr[nextpnr.index("--router") + 1], "gpu")
        self.assertIn("--timing-allow-fail", nextpnr)
        self.assertNotIn("--tmg-ripup", nextpnr)
        joined = " ".join(nextpnr)
        self.assertIn("cores/fes-sg1000/constraints-oss.qsf", joined)
        self.assertIn("cores/fes-sg1000/clocks-oss.sdc", joined)
        self.assertNotIn("cores/fes-sg1000/clocks.sdc", joined)
        self.assertEqual(SG1000_TOOLCHAIN_LOCK, "toolchains/registered-memory.lock")
        self.assertEqual(SG1000_TOOLCHAIN_ROOT, "build/toolchain/fes-sg1000")
        self.assertIn(SG1000_TOOLCHAIN_LOCK, OSS_PINNED_INPUTS)
        self.assertEqual(SG1000_TOOL_COMMITS["yosys"], "e2d425dee148cc60c50f4e9b354a10d90eab15f4")
        self.assertEqual(SG1000_TOOL_COMMITS["nextpnr"], "5dea3ecd5062f1187d0b4f04a56139d5f8680cf7")
        pins = load_lock(ROOT / SG1000_TOOLCHAIN_LOCK)
        from scripts.build_fes_coleco_oss import COLECO_TOOLCHAIN_LOCK
        self.assertEqual(SG1000_TOOLCHAIN_LOCK, COLECO_TOOLCHAIN_LOCK)
        self.assertEqual(pins["yosys"].commit, SG1000_TOOL_COMMITS["yosys"])
        self.assertEqual(pins["nextpnr"].commit, SG1000_TOOL_COMMITS["nextpnr"])
        global_pins = load_lock(ROOT / "toolchain.lock")
        self.assertEqual(global_pins["yosys"].commit, "fb879d81e0352f558297bdcc61bc7a4a922fa7b0")
        self.assertEqual(global_pins["nextpnr"].commit, "49ab82f54f801c6e0f3bdb2c3f53a33880c0bc94")
        record = create_build_record(
            ROOT,
            "https://example.invalid/misteross.git",
            "a" * 40,
            {"yosys": "test"}, execution=EXECUTION,
        )
        self.assertIn(f'"seed":{SEED}'.encode(), record)
        self.assertIn(b'"router":"gpu"', record)
        self.assertIn(b'"gpu_backend":"hip"', record)
        self.assertIn(b'"package_format":3', record)
        self.assertIn(b'"rom_source_size":16384', record)
        self.assertIn(SG1000_GPU_ARCHITECTURES.encode(), record)
        self.assertEqual(SG1000_GPU_BACKEND, "hip")
        self.assertIn("cores/fes-sg1000/rtl/top.v", OSS_RTL_SOURCES)
        self.assertIn("cores/fes-sg1000/rtl/sg1000_machine.sv", OSS_RTL_SOURCES)

    def test_oss_manifest_requires_linked_cartridge_without_media_mailbox(self) -> None:
        record = b'{"recipe_sha256":"' + b"b" * 64 + b'"}'
        evidence = {
            "build_id": "d" * 32,
            "rbf": {"size": 16, "sha256": "e" * 64},
            "rom": {"id": "cartridge-rom", "role": "cartridge", "source_size": 16384,
                    "file": "rom-map.json", "size": 12, "sha256": "f" * 64},
        }
        manifest = tomllib.loads(_oss_manifest(record, evidence, "https://example.invalid", "a" * 40, {"yosys": "test"}).decode())
        self.assertEqual(manifest["format"], 3)
        self.assertEqual(manifest["core"]["version"], "1.1.0")
        self.assertEqual(manifest["rom"]["source_size"], 16384)
        self.assertEqual({item["id"] for item in manifest["interfaces"]},
                         {"fes.keyboard", "fes.video.fixed-720p60"})

    def test_quartus_project_reuses_coleco_sibling_modules(self) -> None:
        qsf = project_qsf(ROOT, ROOT / QUARTUS_OUTPUT / "project", "00112233445566778899aabbccddeeff")
        self.assertIn('VERILOG_MACRO "QUARTUS=1"', qsf)
        self.assertIn('VERILOG_MACRO "FES_SG1000_BUILD_ID=', qsf)
        self.assertIn("cores/fes-sg1000/rtl/sg1000_machine.sv", qsf)
        self.assertIn("cores/fes-sg1000/rtl/top.v", qsf)
        self.assertIn("cores/fes-common/rtl/tv80/tv80_core.v", qsf)
        self.assertIn("cores/fes-common/rtl/coleco_vdp.sv", qsf)
        self.assertIn("cores/fes-common/rtl/fes_computer_gp.v", qsf)
        self.assertNotIn("VHDL_FILE", qsf)
        self.assertNotIn("coleco_reset_rom", qsf)
        self.assertNotIn("coleco_machine.sv", qsf)
        for relative in (*VERILOG_SOURCES, *SYSTEMVERILOG_SOURCES):
            self.assertIn(relative, QUARTUS_PINNED_INPUTS)
        self.assertTrue(all("reset_rom" not in item for item in QUARTUS_PINNED_INPUTS))

    def test_machine_uses_sg1000_map_and_coleco_vdp(self) -> None:
        machine = (ROOT / "cores/fes-sg1000/rtl/sg1000_machine.sv").read_text(encoding="utf-8")
        self.assertIn("coleco_vdp", machine)
        self.assertIn("coleco_dpram", machine)
        self.assertIn("2'b11", machine)
        self.assertIn("port_dc", machine)
        self.assertIn("port_dd", machine)
        self.assertNotIn("coleco_reset_rom", machine)
        self.assertNotIn("JP 0x8000", machine)

    def test_manifest_carries_sg1000_identity(self) -> None:
        record = (
            b'{"format":1,"repository":"https://github.com/DeanoC/misteross.git",'
            b'"revision":"' + (b"a" * 40) + b'","recipe":"scripts/build_fes_sg1000.py",'
            b'"recipe_sha256":"' + (b"b" * 64) + b'","abi_definition":"x",'
            b'"abi_definition_sha256":"' + (b"c" * 64) + b'","dependencies":{},'
            b'"tools":{},"parameters":{}}'
        )
        evidence = {"build_id": "d" * 32, "rbf": {"size": 16, "sha256": "e" * 64}}
        manifest = _manifest(
            record,
            evidence,
            "https://github.com/DeanoC/misteross.git",
            "a" * 40,
            "Quartus Prime Lite 17.0.2",
        )
        self.assertIn(b'id = "fes.sg1000"', manifest)
        self.assertIn(b"FES SG-1000", manifest)
        self.assertIn(b"fes.simple-computer", manifest)
        self.assertNotIn(b"fes.coleco", manifest)
        self.assertNotIn(b"recipe = ", manifest)

    def test_docs_record_quartus_and_oss_recipes(self) -> None:
        readme = (ROOT / "cores/fes-sg1000/README.md").read_text(encoding="utf-8")
        architecture = (ROOT / "docs/architecture.md").read_text(encoding="utf-8")
        root_readme = (ROOT / "README.md").read_text(encoding="utf-8")
        for text in (readme, architecture, root_readme):
            self.assertIn("fes-sg1000", text)
            self.assertIn("fes.sg1000", text)
            self.assertIn("build-fes-sg1000-quartus", text)
            self.assertIn("build-fes-sg1000", text)
            self.assertIn("sim-fes-sg1000-oss", text)
            self.assertIn("fes.simple-computer", text)
        self.assertNotIn("/home/deano/", readme)
        self.assertIn("Quartus", readme)
        self.assertIn("FES_COLECO_OSS", readme)
        self.assertIn("e2d425de", readme)
        self.assertIn("package-only", readme.lower())
        self.assertIn("sim-fes-sg1000-rom-link", readme)
        self.assertIn("not in the factory image", readme.lower())
        self.assertNotIn("remain later jobs", readme.lower())

    def test_diagnostic_is_reproducible_and_enters_at_reset(self) -> None:
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "graphics-i.rom"
            preview = Path(directory) / "graphics-i.ppm"
            first = subprocess.run(
                [sys.executable, str(GENERATOR), "--output", str(output), "--preview", str(preview)],
                cwd=ROOT,
                text=True,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                check=False,
            )
            self.assertEqual(first.returncode, 0, first.stderr)
            data = output.read_bytes()
            self.assertEqual(data[0], 0xF3)
            self.assertIn(b"\x32\x00\xc0", data)
            self.assertLessEqual(len(data), 16384)
            digest = hashlib.sha256(data).hexdigest()
            second = Path(directory) / "again.rom"
            subprocess.run(
                [sys.executable, str(GENERATOR), "--output", str(second)],
                cwd=ROOT,
                check=True,
            )
            self.assertEqual(second.read_bytes(), data)
            self.assertEqual(hashlib.sha256(second.read_bytes()).hexdigest(), digest)
            padded = Path(directory) / "padded.rom"
            hex_output = Path(directory) / "padded.hex"
            subprocess.run(
                [sys.executable, str(GENERATOR), "--output", str(padded), "--pad-to", "16384",
                 "--hex-output", str(hex_output)],
                cwd=ROOT,
                check=True,
            )
            self.assertEqual(padded.read_bytes(), data + b"\xff" * (16384 - len(data)))
            self.assertEqual(hex_output.read_text().splitlines(), [f"{byte:02x}" for byte in padded.read_bytes()])
            self.assertTrue(preview.is_file())
            self.assertGreater(preview.stat().st_size, 1000)


if __name__ == "__main__":
    unittest.main()


def setUpModule():
    global ROOT, _source_fixture
    _source_fixture, ROOT = clean_module(ROOT)

def tearDownModule():
    _source_fixture.cleanup()
