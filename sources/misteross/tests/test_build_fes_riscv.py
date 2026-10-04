from pathlib import Path
import json
import shutil
import subprocess
import tempfile
import tomllib
import unittest
from unittest.mock import patch

from scripts import build_fes_riscv as riscv
from scripts import fes_build_common as board
from scripts.export_core_package import build_identity
from tests.producer_fixture import EXECUTION

ROOT = Path(__file__).resolve().parents[1]


class RiscvProducerTests(unittest.TestCase):
    def test_cli_accepts_standard_package_resolver_arguments(self):
        with patch("sys.argv", ["build_fes_riscv.py", "--root", str(ROOT),
                "--package-output", str(ROOT / "build/packages"), "--cache-root", "/tmp/cache",
                "--identity-version", "2"]), patch.object(riscv, "build", return_value=Path("sealed")) as build:
            self.assertEqual(riscv.main(), 0)
        build.assert_called_once_with(ROOT, ROOT / "build/packages", cache_root=Path("/tmp/cache"),
                                      identity_version=2, gpu_device=0)

    def test_commands_pin_the_system_sources_build_id_and_pixel_clock(self):
        synth, route = riscv.build_commands("a" * 32,
            {"yosys": Path("/auth/yosys"), "nextpnr-mistral": Path("/auth/nextpnr")}, gpu_device=1)
        for path in riscv.RTL_SOURCES:
            self.assertIn(path, synth[2])
        self.assertIn("chparam -set BUILD_ID 128'h" + "a" * 32 + " top", synth[2])
        self.assertIn("synth_intel_alm -nolutram -nodsp -top top", synth[2])
        self.assertNotIn("-nobram", synth[2])          # the RAM and framebuffer are M10K
        self.assertIn("build/fes-riscv/core.rbf", route)
        self.assertIn(riscv.QSF, route)
        self.assertEqual(route[route.index("--freq") + 1], "74.25")
        self.assertEqual(route[route.index("--gpu-device") + 1], "1")
        self.assertEqual(route[route.index("--router") + 1], "gpu")
        with self.assertRaises(board.BuildError):
            riscv.build_commands("zz", {"yosys": Path("/y"), "nextpnr-mistral": Path("/n")})

    def test_firmware_images_are_pinned_and_current(self):
        for relative in (riscv.FIRMWARE_SOURCE, riscv.FIRMWARE_ASSEMBLER, *riscv.FIRMWARE_IMAGES):
            self.assertIn(relative, riscv.PINNED_INPUTS)
            self.assertTrue((ROOT / relative).is_file(), relative)
        riscv.require_current_firmware(ROOT)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            shutil.copytree(ROOT / "cores/fes-riscv/firmware", root / "cores/fes-riscv/firmware",
                            ignore=shutil.ignore_patterns("__pycache__"))
            stale = root / riscv.FIRMWARE_IMAGES[1]
            stale.write_text(stale.read_text().replace("13", "00", 1))
            with self.assertRaisesRegex(board.BuildError, "stale firmware image"):
                riscv.require_current_firmware(root)

    def test_resource_policy_allows_m10k_and_forbids_mlab_dsp_and_sdram(self):
        self.assertIn("MISTRAL_M10K", riscv.ORDINARY_RESOURCES)
        self.assertIn("MISTRAL_MLAB", riscv.FORBIDDEN_RESOURCES)
        self.assertIn("MISTRAL_MUL18X18", riscv.FORBIDDEN_RESOURCES)
        self.assertIn("cyclonev_hps_interface_fpga2sdram", riscv.FORBIDDEN_RESOURCES)
        self.assertEqual(riscv.REQUIRED_RESOURCES["cyclonev_hps_interface_mpu_general_purpose"], 1)

    def test_placement_search_is_a_bounded_first_pass_at_the_pixel_clock(self):
        invocation = type("Invocation", (), {"env": {}})()
        with patch.object(riscv, "route_after_synth") as route:
            riscv.route_placement(ROOT, ROOT / riscv.OUTPUT, Path("/nextpnr"), invocation, 0)
        options = route.call_args.kwargs
        self.assertEqual(options["seeds"], riscv.PLACER_SEEDS)
        self.assertEqual(options["mode"], "first-pass")
        self.assertEqual(options["required"], ((None, 74.25),))
        self.assertEqual(options["budget"], len(riscv.PLACER_SEEDS))
        self.assertEqual(options["weights"], (riscv.PLACER_TIMING_WEIGHT,))
        self.assertIn("--router", options["extra"])

    def test_real_source_identity_and_manifest(self):
        temporary = tempfile.TemporaryDirectory(ignore_cleanup_errors=True)
        self.addCleanup(temporary.cleanup)
        repo = Path(temporary.name)
        root = repo / "sources/misteross"
        for relative in ("scripts", "boards", "cores/fes-riscv", "cores/fes-common", "cores/fes-pong"):
            shutil.copytree(ROOT / relative, root / relative, ignore=shutil.ignore_patterns("__pycache__"))
        shutil.copy(ROOT / "toolchain.lock", root / "toolchain.lock")
        def git(*args):
            return subprocess.check_output(["git", "-C", str(repo), *args], text=True).strip()
        git("init", "-q"); git("config", "user.name", "Test"); git("config", "user.email", "test@example.invalid")
        git("add", "."); git("commit", "-qm", "source")
        self.addCleanup(shutil.rmtree, repo / ".git", ignore_errors=True)
        def record():
            return riscv.create_build_record(root, "https://example.invalid/source", git("rev-parse", "HEAD"),
                                             {"yosys": "test"}, execution=EXECUTION)
        before = record()
        fields = json.loads(before)
        self.assertEqual(fields["format"], 2)
        self.assertEqual(fields["source_path"], "sources/misteross")
        self.assertTrue(set(riscv.CPU_SOURCES) <= fields["source_inputs"].keys())
        self.assertTrue(set(riscv.FIRMWARE_IMAGES) <= fields["source_inputs"].keys())
        (repo / "README.md").write_text("Unrelated docs")
        git("add", "."); git("commit", "-qm", "docs")
        self.assertEqual(build_identity(before), build_identity(record()))
        path = root / riscv.FIRMWARE_IMAGES[1]
        path.write_text(path.read_text() + "00\n")
        self.assertNotEqual(build_identity(before), build_identity(record()))
        manifest = tomllib.loads(riscv.manifest(before, {"rbf": {"size": 4, "sha256": "b" * 64}},
            "https://example.invalid/source", fields["revision"], {"yosys": "test"}).decode())
        self.assertEqual(manifest["core"]["id"], "fes.riscv")
        self.assertEqual(manifest["core"]["version"], "0.1.0")
        self.assertEqual(manifest["abi"], {"id": "fes.application", "major": 1, "minor": 0})
        self.assertEqual({i["id"] for i in manifest["interfaces"]}, {"fes.gamepad", "fes.video.fixed-720p60"})
        self.assertTrue(all(i["required"] for i in manifest["interfaces"]))
        self.assertEqual(manifest["target"]["programming_profile"], "fes-gp-v1")
        self.assertNotIn("media", manifest)


if __name__ == "__main__":
    unittest.main()
