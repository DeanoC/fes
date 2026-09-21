from pathlib import Path
import json
import os
import subprocess
import tempfile
import tomllib
import unittest
from unittest.mock import patch

from scripts import build_fes_pong as board
from scripts import build_fes_splash as splash
from scripts.export_core_package import build_identity
from tests import test_build_fes_pong as pong_tests

ROOT = Path(__file__).resolve().parents[1]


class SplashProducerTests(unittest.TestCase):
    def test_rtl_and_contract_omit_user_io_and_probe(self):
        splash.source_forbids_user_io(ROOT)
        contract = tomllib.loads((ROOT / splash.CONTRACT).read_text())
        self.assertFalse(contract["identity"]["probe"])
        self.assertEqual(contract["identity"]["core_id"], "")
        self.assertFalse(contract["user_io"]["command_0x0014_probe"])
        self.assertFalse(contract["user_io"]["command_0x002f_hps_fb"])
        self.assertEqual(contract["hdmi"]["h_total"], 1650)
        self.assertEqual(contract["hdmi"]["pixel_clock_hz"], 74_250_000)
        top = (ROOT / "cores/fes-splash/rtl/top.v").read_text()
        self.assertIn("cyclonev_hps_interface_peripheral_i2c", top)
        self.assertNotIn("fes_application_gp", top)
        self.assertNotIn("fes_gp", top)
        self.assertNotIn("menu.rbf", top)

    def test_commands_use_oss_lane_and_not_a_play_package(self):
        record = splash.create_build_record(
            ROOT, "https://example.invalid/repo", "a" * 40, {"yosys": "test"}
        )
        fields = json.loads(record)
        self.assertFalse(fields["parameters"]["format2_package"])
        self.assertFalse(fields["parameters"]["user_io"])
        self.assertFalse(fields["parameters"]["probe"])
        self.assertEqual(fields["parameters"]["gpu_router"], "OFF")
        self.assertEqual(fields["parameters"]["router"], "default")
        self.assertEqual(fields["abi_definition"], splash.CONTRACT)
        commands = splash.build_commands(
            ROOT, {"yosys": Path("/auth/yosys"), "nextpnr-mistral": Path("/auth/nextpnr")}
        )
        yosys = commands[0][2]
        nextpnr = commands[1]
        self.assertIn("cores/fes-splash/rtl/fes_splash_core.v", yosys)
        self.assertIn("cores/fes-splash/rtl/top.v", yosys)
        self.assertIn("-nobram -nolutram -nodsp", yosys)
        self.assertNotIn("fes-demo", yosys)
        self.assertNotIn("fes_application", yosys)
        self.assertEqual(nextpnr[nextpnr.index("--qsf") + 1], splash.QSF)
        self.assertEqual(nextpnr[nextpnr.index("--freq") + 1], "74.25")
        self.assertIn("build/fes-splash/core.rbf", nextpnr)
        self.assertNotIn("gpu", nextpnr)
        self.assertNotIn("--router", nextpnr)
        synth = splash.build_commands(ROOT, {"yosys": Path("/auth/yosys")}, synth_only=True)
        self.assertEqual(len(synth), 1)

    def test_native_inputs_reuse_splash_bytes_for_idle(self):
        snippet = tomllib.loads(splash.native_inputs_snippet(
            "https://github.com/DeanoC/misteross", "b" * 40,
            {"rbf": {"sha256": "c" * 64, "size": 12}},
        ))
        self.assertEqual(snippet["splash_rbf"]["fat_destination"], "/menu.rbf")
        self.assertEqual(snippet["idle_rbf"]["install_path"], "/usr/share/mister-runtime/idle.rbf")
        self.assertEqual(snippet["splash_rbf"]["sha256"], snippet["idle_rbf"]["sha256"])
        self.assertEqual(snippet["splash_rbf"]["path"], "build/fes-splash/core.rbf")
        self.assertNotIn("fes.", snippet["splash_rbf"]["path"])

    def splash_evidence(self, output: Path) -> None:
        pong_tests.BuildFesPongTests()._write_passing_outputs(output)
        for filename in ("synth.json", "routed.json"):
            path = output / filename
            data = json.loads(path.read_text())
            cells = data["modules"]["top"]["cells"]
            cells.pop("hps", None)
            path.write_text(json.dumps(data))
        log = (output / "nextpnr.log").read_text()
        (output / "nextpnr.log").write_text(
            log.replace("Info: backend hip:AMD Radeon RX 7900 XTX ready\n", "")
        )
        timing = json.loads((output / "timing.json").read_text())
        timing["utilization"].pop("cyclonev_hps_interface_mpu_general_purpose", None)
        (output / "timing.json").write_text(json.dumps(timing))

    def test_evidence_accepts_oss_route_and_rejects_gp(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            self.splash_evidence(output)
            evidence = splash.validate_build_evidence(output, ROOT)
            self.assertEqual(evidence["route"]["router"], "default")
            self.assertFalse(evidence["user_io"]["probe"])
            self.assertEqual(evidence["user_io"]["core_id"], "")
            self.assertEqual(evidence["timing"]["pixel"]["requested_mhz"], 74.25)
            timing = json.loads((output / "timing.json").read_text())
            timing["utilization"]["cyclonev_hps_interface_mpu_general_purpose"] = {
                "used": 1, "available": 1
            }
            (output / "timing.json").write_text(json.dumps(timing))
            with self.assertRaisesRegex(board.BuildError, "forbidden"):
                splash.validate_build_evidence(output, ROOT)

    def test_dirty_source_gate_precedes_tool_use(self):
        with patch.object(board, "_require_clean_source", side_effect=board.BuildError("dirty")) as source, \
             patch.object(splash, "authenticate_oss_tools") as tools:
            with self.assertRaisesRegex(board.BuildError, "dirty"):
                splash.build(ROOT)
            tools.assert_not_called()
            source.assert_called_once_with(
                ROOT, pinned_inputs=splash.PINNED_INPUTS, identity_version=splash.IDENTITY_VERSION
            )

    def test_shared_cache_is_rejected(self):
        with patch.dict(os.environ, {"FES_TOOLCHAIN_CACHE_ROOT": "/tmp/cache"}, clear=False):
            with self.assertRaisesRegex(board.BuildError, "local OSS lane"):
                splash.authenticate_oss_tools(ROOT)

    def test_failed_evidence_invalidates_rbf(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for relative in splash.PINNED_INPUTS:
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes((ROOT / relative).read_bytes())
            tools = {name: board.AuthenticatedTool(Path("/auth") / name, name)
                     for name in ("yosys", "nextpnr-mistral", "mistral")}
            output = root / splash.OUTPUT_RELATIVE

            def run(command, cwd, log, **kwargs):
                self.assertTrue((output / "build-inputs.json").is_file())
                (output / "core.rbf").write_bytes(b"unvalidated")

            with patch.object(board, "_require_clean_source", return_value=("https://example.invalid/repo", "a" * 40)), \
                 patch.object(splash, "authenticate_oss_tools", return_value=tools), \
                 patch.object(board, "_run_tool", side_effect=run), \
                 patch.object(splash, "validate_build_evidence", side_effect=board.BuildError("timing failed")):
                with self.assertRaisesRegex(board.BuildError, "timing failed"):
                    splash.build(root)
            self.assertFalse((output / "core.rbf").exists())
            self.assertFalse((output / "native-inputs-snippet.toml").exists())

    def test_build_identity_is_stable_for_same_record(self):
        record = splash.create_build_record(
            ROOT, "https://example.invalid/repo", "a" * 40, {"yosys": "y"}
        )
        self.assertEqual(build_identity(record), build_identity(record))
        other = splash.create_build_record(
            ROOT, "https://example.invalid/repo", "b" * 40, {"yosys": "y"}
        )
        self.assertNotEqual(build_identity(record), build_identity(other))

    def test_source_and_recheck_use_monorepo_identity(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for relative in splash.PINNED_INPUTS:
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes((ROOT / relative).read_bytes())
            tools = {name: board.AuthenticatedTool(Path("/auth") / name, name)
                     for name in ("yosys", "nextpnr-mistral", "mistral")}
            identity = ("https://github.com/DeanoC/fes.git", "a" * 40)

            def run(command, cwd, log, **kwargs):
                (root / splash.OUTPUT_RELATIVE / "core.rbf").write_bytes(b"ok")

            with patch.object(board, "_require_clean_source", return_value=identity) as source, \
                 patch.object(splash, "authenticate_oss_tools", return_value=tools), \
                 patch.object(board, "_run_tool", side_effect=run), \
                 patch.object(splash, "validate_build_evidence",
                              return_value={"rbf": {"sha256": "c" * 64, "size": 2}}):
                self.assertEqual(splash.build(root), root / splash.OUTPUT_RELATIVE / "core.rbf")
            self.assertEqual(source.call_count, 2)
            for call in source.call_args_list:
                self.assertEqual(call.args, (root,))
                self.assertEqual(call.kwargs["pinned_inputs"], splash.PINNED_INPUTS)
                self.assertEqual(call.kwargs["identity_version"], splash.IDENTITY_VERSION)

    def test_require_clean_source_accepts_fes_nested_module(self):
        with tempfile.TemporaryDirectory() as temporary:
            repo = Path(temporary)
            module = repo / "sources/misteross"
            module.mkdir(parents=True)
            subprocess.check_output(["git", "-C", str(repo), "init", "-q"])
            subprocess.check_output(["git", "-C", str(repo), "config", "user.name", "Fixture"])
            subprocess.check_output(["git", "-C", str(repo), "config", "user.email", "fixture@example.invalid"])
            subprocess.check_output(
                ["git", "-C", str(repo), "remote", "add", "origin", "https://example.invalid/fes.git"]
            )
            for relative in splash.PINNED_INPUTS:
                path = module / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes((ROOT / relative).read_bytes())
            subprocess.check_output(["git", "-C", str(repo), "add", "."])
            subprocess.check_output(["git", "-C", str(repo), "commit", "-qm", "source"])
            self.assertEqual(board._require_clean_source(module, pinned_inputs=splash.PINNED_INPUTS),
                             board._require_clean_source(module, pinned_inputs=splash.PINNED_INPUTS, identity_version=2))
            repository, revision = board._require_clean_source(
                module, pinned_inputs=splash.PINNED_INPUTS, identity_version=splash.IDENTITY_VERSION
            )
            self.assertEqual(repository, "https://example.invalid/fes.git")
            self.assertEqual(
                revision,
                subprocess.check_output(["git", "-C", str(repo), "rev-parse", "HEAD"], text=True).strip(),
            )


if __name__ == "__main__":
    unittest.main()
