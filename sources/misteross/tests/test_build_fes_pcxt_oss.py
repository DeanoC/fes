import json
import tempfile
import tomllib
import unittest
from pathlib import Path
from types import SimpleNamespace
from unittest import mock

from scripts import build_fes_pcxt_oss as producer


class FesPcxtPackageRecipeTest(unittest.TestCase):
    def test_build_uses_shared_output_helper_and_synthesis_tools(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            output = root / "package-build"
            tools = {
                "yosys": SimpleNamespace(identity="yosys-test", path=Path("/toolchain/bin/yosys")),
                "mistral": SimpleNamespace(identity="mistral-test", path=Path("/toolchain/bin/mistral-cv")),
                "nextpnr-mistral": SimpleNamespace(
                    identity="nextpnr-test", path=Path("/toolchain/bin/nextpnr-mistral")),
            }
            invocation = SimpleNamespace(inputs={}, close=mock.Mock())
            with (
                mock.patch.object(producer, "_require_clean_source",
                                  return_value=("https://github.com/DeanoC/fes", "a" * 40)),
                mock.patch.object(producer, "authenticate_tools", return_value=tools),
                mock.patch.object(producer, "FunctionalInvocation", return_value=invocation),
                mock.patch.object(producer, "create_build_record", return_value=b"record"),
                mock.patch.object(producer, "_prepare_output", return_value=output) as prepare_output,
                mock.patch.object(producer, "_write_atomic"),
                mock.patch.object(producer.board, "verify_vendor"),
                mock.patch.object(producer.board, "board_inputs"),
                mock.patch.object(producer, "build_identity", return_value="d" * 32),
                mock.patch.object(
                    producer, "synthesis_command",
                    side_effect=producer.BuildError("stop after synthesis command"),
                ) as synthesis_command,
                mock.patch.object(producer, "_invalidate_failed_artifact"),
            ):
                with self.assertRaisesRegex(producer.BuildError, "stop after synthesis command"):
                    producer.build(root)

            prepare_output.assert_called_once_with(
                root, relative=producer.OUTPUT_RELATIVE,
                build_outputs=producer.BUILD_OUTPUTS,
            )
            synthesis_command.assert_called_once_with(
                root, output, "d" * 32,
                {"yosys": tools["yosys"].path,
                 "nextpnr-mistral": tools["nextpnr-mistral"].path},
            )
            invocation.close.assert_called_once_with()

    def test_manifest_declares_inline_fixed_video_for_the_diagnostic_mailbox(self):
        record = json.dumps({"recipe_sha256": "a" * 64}).encode()
        encoded = producer.manifest(
            record,
            {"size": 1024, "sha256": "b" * 64},
            "https://github.com/DeanoC/fes",
            "c" * 40,
            {"yosys": "yosys-test", "nextpnr-mistral": "nextpnr-test"},
        )
        manifest = tomllib.loads(encoded.decode())
        self.assertEqual(manifest["format"], 2)
        self.assertEqual(manifest["core"]["id"], "fes.pcxt")
        self.assertEqual(manifest["abi"], {"id": "fes.computer", "major": 1, "minor": 0})
        self.assertEqual(manifest["interfaces"], [{
            "id": "fes.video.fixed-720p60", "major": 1, "minor": 0, "required": True,
        }])
        self.assertEqual(manifest["payload"]["file"], "core.rbf")
        # Fixed-video packages contain their Direct path in the RBF. They do
        # not use the separately published native-video-parts index.
        self.assertIn("cores/fes-common/rtl/fes_video_part_direct.v", producer.PINNED_INPUTS)
        video_output = (producer.ROOT / "cores/fes-pcxt/rtl/pcxt_video_output.sv").read_text()
        self.assertIn("fes_video_part_direct part", video_output)

    def test_registered_route_keeps_the_accepted_timing_miss_nonblocking(self):
        with tempfile.TemporaryDirectory() as temporary:
            env = {
                "PATH": "/authenticated/bin:/usr/bin:/bin",
                "HOME": temporary,
                "LANG": "C",
                "LC_ALL": "C",
                "TZ": "UTC",
            }
            command, _route_env = producer.route_command(
                producer.ROOT,
                producer.OUTPUT_RELATIVE,
                Path(temporary) / "nextpnr-mistral",
                0,
                env,
            )
        self.assertIn("--timing-allow-fail", command)
        self.assertIn("--freq", command)
        self.assertEqual(command[command.index("--freq") + 1], "100")
        self.assertIn("--rbf", command)
        self.assertIn("--compress-rbf", command)

    def test_synthesis_enables_mailbox_and_phased_hdmi_for_the_package_variant(self):
        tools = {
            "yosys": Path("/toolchain/bin/yosys"),
            "nextpnr-mistral": Path("/toolchain/bin/nextpnr-mistral"),
        }
        _executable, _option, program = producer.synthesis_command(
            producer.ROOT,
            producer.ROOT / producer.OUTPUT_RELATIVE,
            "d" * 32,
            tools,
        )
        self.assertIn("-set ENABLE_FES_COMPUTER 1", program)
        self.assertIn("-set PHASED_HDMI_CLOCK 1", program)
        self.assertIn("-set BUILD_ID 128'h" + "d" * 32, program)


if __name__ == "__main__":
    unittest.main()
