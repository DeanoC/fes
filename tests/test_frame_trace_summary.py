import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location(
    "frame_trace_summary", ROOT / "sources/FogCast/scripts/frame-trace-summary.py")
summary_tool = importlib.util.module_from_spec(spec)
spec.loader.exec_module(summary_tool)


class FrameTraceSummaryTest(unittest.TestCase):
    def test_joins_ticks_and_presents_and_finds_the_dominant_stage(self):
        records = [
            {"t_us": 1_000_000, "kind": "input", "cmd": "right"},
            {"t_us": 1_005_000, "kind": "tick", "tick_us": 9000, "lua_step_us": 6000, "snapshot_us": 500,
             "render_us": 24000, "queued": True, "seq": 1, "view": "room:example.fes-cores"},
            {"t_us": 1_100_000, "kind": "present", "seq": 1, "queued_t_us": 1_034_000, "frame_copy_us": 4000,
             "queue_wait_us": 100, "status_us": 2000, "present_us": 60000, "begin_us": 1000, "stage_us": 8000,
             "commit_us": 51000, "runtime_copy_us": 40000, "runtime_submit_us": 50, "runtime_scanout_wait_us": 9000,
             "presented": True},
            # An unchanged tick and a probe present are not frames.
            {"t_us": 1_140_000, "kind": "tick", "tick_us": 300},
            {"t_us": 1_150_000, "kind": "present", "seq": 1, "probe": True, "presented": True},
        ]
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "trace.jsonl"
            path.write_text("".join(json.dumps(r) + "\n" for r in records))
            summary = summary_tool.summarise(summary_tool.load(path))
        self.assertEqual(summary["frames"], 1)
        stages = summary["stages"]
        self.assertAlmostEqual(stages["lua"]["p50"], 6.0)
        self.assertAlmostEqual(stages["tick_other"]["p50"], 3.0)
        self.assertAlmostEqual(stages["raster"]["p50"], 20.0)
        self.assertAlmostEqual(stages["rt_copy"]["p50"], 40.0)
        self.assertAlmostEqual(stages["commit_io"]["p50"], 1.95)
        self.assertAlmostEqual(stages["total"]["p50"], 95.0)
        self.assertEqual(summary["dominant"], "rt_copy")
        self.assertAlmostEqual(summary["inputs"]["right"]["p50"], 100.0)
        self.assertIn("dominant stage (by mean): rt_copy", summary_tool.render(summary))


if __name__ == "__main__":
    unittest.main()
