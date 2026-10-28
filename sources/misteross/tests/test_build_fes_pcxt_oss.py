import json
import tempfile
import tomllib
import unittest
from pathlib import Path

from scripts import build_fes_pcxt_oss as producer


class FesPcxtPackageRecipeTest(unittest.TestCase):
    def test_manifest_declares_only_fixed_video_for_the_diagnostic_mailbox(self):
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

    def test_synthesis_enables_mailbox_only_for_the_package_variant(self):
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
        self.assertIn("-set BUILD_ID 128'h" + "d" * 32, program)


if __name__ == "__main__":
    unittest.main()
