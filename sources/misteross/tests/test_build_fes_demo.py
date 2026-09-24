from pathlib import Path
import json
import tempfile
import subprocess
import tomllib
import unittest
from tests.producer_fixture import clean_module, init_source, EXECUTION, FakeInvocation
from unittest.mock import patch

from scripts import build_fes_demo as demo
from scripts import fes_build_common as board
from scripts.export_core_package import build_identity
from tests import test_build_fes_pong as pong_tests

ROOT = Path(__file__).resolve().parents[1]

class ApplicationProducerTests(unittest.TestCase):
    def test_unused_hps_sdram_timing_row_is_allowed_but_use_is_rejected(self):
        resource = "cyclonev_hps_interface_fpga2sdram"
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            pong_tests.BuildFesPongTests()._write_passing_outputs(output)
            timing_path = output / "timing.json"
            timing = json.loads(timing_path.read_text())
            timing["utilization"][resource] = {"used": 0, "available": 1}
            timing_path.write_text(json.dumps(timing))
            def validate():
                return demo.board_evidence.validate_build_evidence(
                    output, ROOT, ordinary_resources=demo.ORDINARY_RESOURCES,
                    required_resources=demo.REQUIRED_RESOURCES,
                    forbidden_resources=demo.FORBIDDEN_RESOURCES,
                    required_zero_resources=demo.REQUIRED_ZERO_RESOURCES)
            self.assertEqual(validate()["resources"][resource]["used"], 0)

            timing["utilization"][resource]["used"] = 1
            timing_path.write_text(json.dumps(timing))
            with self.assertRaisesRegex(board.BuildError, "unknown resources in use: .*fpga2sdram"):
                validate()

    def test_audio_variant_identity_interfaces_and_source_closure(self):
        record = demo.create_build_record(ROOT, "https://example.invalid/repo", "a" * 40,
                                         {"yosys": "test"}, audio=True, execution=EXECUTION)
        manifest = tomllib.loads(demo.manifest(record, {"rbf": {"size": 4, "sha256": "b" * 64}},
                    "https://example.invalid/repo", "a" * 40, {"yosys": "test"}, audio=True).decode())
        self.assertEqual(manifest["core"]["id"], "fes.demo-audio")
        self.assertEqual({i["id"] for i in manifest["interfaces"]},
                         {"fes.video.fixed-720p60", "fes.gamepad", "fes.audio.pcm-s16-stereo-48k"})
        self.assertTrue(all(i["required"] for i in manifest["interfaces"]))
        cmd = demo.build_commands(ROOT, build_identity(record),
                {"yosys": Path("/auth/yosys"), "nextpnr-mistral": Path("/auth/nextpnr")}, audio=True)
        self.assertIn("-D FES_DEMO_AUDIO", cmd[0][2])
        self.assertIn("-set ENABLE_GAMEPAD 1 -set ENABLE_MEDIA 0", cmd[0][2])
        for source in demo.AUDIO_SOURCES:
            self.assertIn(source, cmd[0][2])
        self.assertIn(demo.AUDIO_QSF, cmd[1])
        self.assertIn("build/fes-demo-audio/core.rbf", cmd[1])
        with self.assertRaisesRegex(board.BuildError, "separate"):
            demo.build_commands(ROOT, build_identity(record),
                {"yosys": Path("/auth/yosys"), "nextpnr-mistral": Path("/auth/nextpnr")}, audio=True, media=True)

    def audio_evidence(self, output):
        pong_tests.BuildFesPongTests()._write_passing_outputs(output)
        for filename in ("synth.json", "routed.json"):
            path = output / filename
            data = json.loads(path.read_text())
            top = data["modules"]["top"]
            top["cells"]["audio_clock.pll"] = {"type": "altera_pll", "parameters":
                {**demo.board_evidence.PLL_PARAMETERS, "output_clock_frequency0": "12.288 MHz"}}
            if filename == "routed.json":
                for index, (port, pin) in enumerate(demo.AUDIO_PINS.items()):
                    top["ports"][port] = {"direction": "output", "bits": [100+index]}
                    top["cells"][port] = {"type": "MISTRAL_OB",
                        "attributes": {"LOC": pin, "IO_STANDARD": "3.3-V LVTTL", "NEXTPNR_BEL": f"MISTRAL_IO.{index}.0.0"},
                        "connections": {"PAD": [100+index], "I": [200+index]}}
            path.write_text(json.dumps(data))
        path = output / "timing.json"
        data = json.loads(path.read_text())
        data["fmax"]["audio_clk"] = {"constraint": 12.288, "achieved": 100}
        data["utilization"]["altera_pll"]["used"] = 2
        path.write_text(json.dumps(data))
        with (output / "nextpnr.log").open("a") as stream:
            stream.write("Info: PLL 'audio_clock.pll': 50 MHz -> 12.288 MHz, direct, M=8 N=1 C6=32, bel altera_pll.0.14.1\n")

    def test_dual_pll_validation_and_single_pll_preservation(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            self.audio_evidence(output)
            self.assertEqual(pong_tests.build_fes_pong.validate_build_evidence(output, ROOT, audio=True)["timing"]["audio"]["status"], "pass")
            demo.audio_pin_evidence(ROOT, output)
            with self.assertRaises(board.BuildError):
                pong_tests.build_fes_pong.validate_build_evidence(output, ROOT)
            for filename, mutation in (
                ("timing.json", lambda d: d["fmax"]["audio_clk"].update(achieved=10)),
                ("timing.json", lambda d: d["fmax"].pop("audio_clk")),
                ("routed.json", lambda d: d["modules"]["top"]["cells"]["audio_clock.pll"]["parameters"].update(output_clock_frequency0="11.2896 MHz")),
            ):
                self.audio_evidence(output)
                path = output / filename
                data = json.loads(path.read_text()); mutation(data); path.write_text(json.dumps(data))
                with self.assertRaises(board.BuildError):
                    pong_tests.build_fes_pong.validate_build_evidence(output, ROOT, audio=True)
            for attribute, wrong in (("LOC", "PIN_U12"), ("IO_STANDARD", "2.5 V"), ("NEXTPNR_BEL", "")):
                self.audio_evidence(output)
                path = output / "routed.json"
                data = json.loads(path.read_text())
                data["modules"]["top"]["cells"]["HDMI_I2S"]["attributes"][attribute] = wrong
                path.write_text(json.dumps(data))
                with self.assertRaises(board.BuildError):
                    demo.audio_pin_evidence(ROOT, output)

    def test_variants_have_distinct_identity_and_exact_interfaces(self):
        ids = []
        for media in (False, True):
            record = demo.create_build_record(ROOT, "https://example.invalid/repo", "a" * 40,
                                               {"yosys": "test"}, media=media, execution=EXECUTION)
            ids.append(build_identity(record))
            manifest = tomllib.loads(demo.manifest(record,
                {"rbf": {"size": 4, "sha256": "b" * 64}}, "https://example.invalid/repo",
                "a" * 40, {"yosys": "test"}, media=media).decode())
            self.assertEqual(manifest["abi"], {"id": "fes.application", "major": 1, "minor": 0})
            self.assertEqual({entry["id"] for entry in manifest["interfaces"]},
                             {"fes.video.fixed-720p60", "fes.gamepad", "fes.media.blob"}
                             if media else {"fes.video.fixed-720p60"})
            self.assertTrue(all(entry["required"] for entry in manifest["interfaces"]))
            commands = demo.build_commands(ROOT, ids[-1],
                        {"yosys": Path("/auth/yosys"), "nextpnr-mistral": Path("/auth/nextpnr")}, media=media)
            self.assertIn(f"-set ENABLE_MEDIA {int(media)}", commands[0][2])
            self.assertIn(f"-set ENABLE_GAMEPAD {int(media)}", commands[0][2])
            self.assertIn("-nobram -nolutram -nodsp", commands[0][2])
            self.assertEqual(commands[1][commands[1].index("--router") + 1], "gpu")
            self.assertIn(str(demo.output_relative(media) / "core.rbf"), commands[1])
        self.assertNotEqual(*ids)

    def test_dirty_source_gate_precedes_tool_use(self):
        with patch.object(demo, "FunctionalInvocation", FakeInvocation), patch.object(demo, "require_clean_source", side_effect=board.BuildError("dirty")) as source, \
             patch.object(board, "_authenticate_tools") as tools:
            with self.assertRaisesRegex(board.BuildError, "dirty"):
                demo.build(ROOT)
            tools.assert_not_called()
            source.assert_called_once_with(ROOT, demo.PINNED_INPUTS)

    def test_failed_evidence_prevents_export_and_invalidates_outputs(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for relative in demo.PINNED_INPUTS:
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes((ROOT / relative).read_bytes())
            init_source(root)
            tools = {name: board.AuthenticatedTool(Path("/auth") / name, name)
                     for name in ("yosys", "nextpnr-mistral", "mistral")}
            output = root / demo.output_relative(False)
            def run(command, cwd, log, **kwargs):
                self.assertTrue((output / "build-inputs.json").is_file())
                (output / "core.rbf").write_bytes(b"unvalidated")
            with patch.object(demo, "FunctionalInvocation", FakeInvocation), patch.object(demo, "require_clean_source", return_value=("https://example.invalid/repo", "a" * 40)), \
                 patch.object(board, "_authenticate_tools", return_value=tools), \
                 patch.object(board, "_run_tool", side_effect=run), \
                 patch.object(demo.board_evidence, "validate_build_evidence", side_effect=board.BuildError("timing failed")), \
                 patch.object(demo, "export_package") as export:
                with self.assertRaisesRegex(board.BuildError, "timing failed"):
                    demo.build(root)
                export.assert_not_called()
            self.assertFalse((output / "core.rbf").exists())

    def test_completed_route_exit_one_uses_selected_demo_payload(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            tool = root / "nextpnr-mistral"
            tool.write_text("#!/bin/sh\nprintf '%s\\n' 'Info: Program finished normally.'\nexit 1\n")
            tool.chmod(0o755)
            stale_pong = root / "build/fes-pong/core.rbf"
            stale_pong.parent.mkdir(parents=True)
            stale_pong.write_bytes(b"stale pong")
            for media in (False, True):
                relative = demo.output_relative(media)
                output = root / relative
                output.mkdir(parents=True)
                with self.assertRaisesRegex(board.BuildError, "exit 1"):
                    board._run_tool((str(tool),), root, output / "nextpnr.log", output_relative=relative)
                payload = output / "core.rbf"
                payload.write_bytes(b"completed demo")
                board._run_tool((str(tool),), root, output / "nextpnr.log", output_relative=relative)
                payload.unlink()

if __name__ == "__main__":
    unittest.main()


def setUpModule():
    global ROOT, _source_fixture
    _source_fixture, ROOT = clean_module(ROOT)

def tearDownModule():
    _source_fixture.cleanup()
