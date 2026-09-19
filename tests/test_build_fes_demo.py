from pathlib import Path
import tempfile
import tomllib
import unittest
from unittest.mock import patch

from scripts import build_fes_demo as demo
from scripts import build_fes_pong as board
from scripts.export_core_package import build_identity

ROOT = Path(__file__).resolve().parents[1]

class ApplicationProducerTests(unittest.TestCase):
    def test_variants_have_distinct_identity_and_exact_interfaces(self):
        ids = []
        for media in (False, True):
            record = demo.create_build_record(ROOT, "https://example.invalid/repo", "a" * 40,
                                               {"yosys": "test"}, media=media)
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
        with patch.object(board, "_require_clean_source", side_effect=board.BuildError("dirty")) as source, \
             patch.object(board, "_authenticate_tools") as tools:
            with self.assertRaisesRegex(board.BuildError, "dirty"):
                demo.build(ROOT)
            tools.assert_not_called()
            source.assert_called_once_with(ROOT, pinned_inputs=demo.PINNED_INPUTS)

    def test_failed_evidence_prevents_export_and_invalidates_outputs(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for relative in demo.PINNED_INPUTS:
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes((ROOT / relative).read_bytes())
            tools = {name: board.AuthenticatedTool(Path("/auth") / name, name)
                     for name in ("yosys", "nextpnr-mistral", "mistral")}
            output = root / demo.output_relative(False)
            def run(command, cwd, log, **kwargs):
                self.assertTrue((output / "build-inputs.json").is_file())
                (output / "core.rbf").write_bytes(b"unvalidated")
            with patch.object(board, "_require_clean_source", return_value=("https://example.invalid/repo", "a" * 40)), \
                 patch.object(board, "_authenticate_tools", return_value=tools), \
                 patch.object(board, "_run_tool", side_effect=run), \
                 patch.object(board, "validate_build_evidence", side_effect=board.BuildError("timing failed")), \
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
