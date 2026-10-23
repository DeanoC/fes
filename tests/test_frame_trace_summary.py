import importlib.util
import io
import json
import tempfile
import unittest
from contextlib import redirect_stdout
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
        self.assertEqual(summary["failed"]["frames"], 0)

    def test_granted_slot_presents_report_the_runtime_clean(self):
        # The client writes the granted slot; the runtime only writes back its
        # lines, so rt_copy is gone and rt_clean is its own stage.
        records = [
            {"t_us": 2_000_000, "kind": "tick", "tick_us": 4000, "lua_step_us": 3000, "render_us": 20000,
             "queued": True, "seq": 2},
            {"t_us": 2_040_000, "kind": "present", "seq": 2, "queued_t_us": 2_020_000, "begin_us": 800,
             "stage_us": 1200, "commit_us": 12000, "runtime_clean_us": 700, "runtime_submit_us": 50,
             "runtime_scanout_wait_us": 11000, "staged_pixels": 4096, "presented": True},
        ]
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "trace.jsonl"
            path.write_text("".join(json.dumps(r) + "\n" for r in records))
            summary = summary_tool.summarise(summary_tool.load(path))
        stages = summary["stages"]
        self.assertAlmostEqual(stages["rt_clean"]["p50"], 0.7)
        self.assertAlmostEqual(stages["rt_copy"]["p50"], 0.0)
        self.assertAlmostEqual(stages["commit_io"]["p50"], 0.25)

    def test_blitter_frames_report_the_blit_and_the_list(self):
        records = [
            {"t_us": 3_000_000, "kind": "tick", "tick_us": 4000, "render_us": 2000, "queued": True, "seq": 3},
            {"t_us": 3_050_000, "kind": "present", "seq": 3, "queued_t_us": 3_002_000, "begin_us": 800,
             "stage_us": 300, "commit_us": 30000, "runtime_clean_us": 400, "runtime_blit_us": 18000,
             "runtime_submit_us": 50, "runtime_scanout_wait_us": 11000, "blitted": True, "commands": 412,
             "upload_bytes": 65536, "presented": True},
            {"t_us": 4_000_000, "kind": "tick", "tick_us": 4000, "render_us": 2000, "queued": True, "seq": 4},
            {"t_us": 4_050_000, "kind": "present", "seq": 4, "queued_t_us": 4_002_000, "commit_us": 30000,
             "runtime_blit_us": 18000, "blitted": True, "commands": 200, "presented": True},
        ]
        summary = summary_tool.summarise(records)
        stages = summary["stages"]
        self.assertAlmostEqual(stages["rt_blit"]["p50"], 18.0)
        self.assertAlmostEqual(stages["commit_io"]["max"], 12.0)
        moved = summary["transport"]
        self.assertEqual(moved["blitted"], 2)
        self.assertAlmostEqual(moved["commands"], 306)
        self.assertAlmostEqual(moved["upload_bytes"], 32768)
        self.assertIn("blitter: frames=2 mean commands=306 mean upload bytes=32768", summary_tool.render(summary))

    def test_failed_presents_stay_out_of_displayed_frames(self):
        records = [
            {"t_us": 1_000_000, "kind": "input", "cmd": "right"},
            {"t_us": 1_005_000, "kind": "tick", "tick_us": 9000, "lua_step_us": 6000, "snapshot_us": 500,
             "render_us": 24000, "queued": True, "seq": 1, "view": "room:example.fes-cores"},
            {"t_us": 1_100_000, "kind": "present", "seq": 1, "queued_t_us": 1_034_000, "frame_copy_us": 4000,
             "queue_wait_us": 100, "status_us": 2000, "present_us": 60000, "begin_us": 1000, "stage_us": 8000,
             "commit_us": 51000, "runtime_copy_us": 40000, "runtime_submit_us": 50, "runtime_scanout_wait_us": 9000,
             "presented": True},
            {"t_us": 1_900_000, "kind": "input", "cmd": "left"},
            {"t_us": 2_000_000, "kind": "tick", "tick_us": 1000, "lua_step_us": 800000, "render_us": 1000,
             "queued": True, "seq": 2, "view": "room:example.fes-cores"},
            {"t_us": 2_500_000, "kind": "present", "seq": 2, "queued_t_us": 2_010_000, "presented": False,
             "err": "menu unavailable", "status_us": 500000},
        ]
        summary = summary_tool.summarise(records)
        self.assertEqual(summary["frames"], 1)
        self.assertEqual(summary["failed"]["frames"], 1)
        self.assertAlmostEqual(summary["stages"]["lua"]["p50"], 6.0)
        self.assertEqual(summary["dominant"], "rt_copy")
        self.assertEqual(summary["views"]["room:example.fes-cores"]["frames"], 1)
        self.assertAlmostEqual(summary["failed"]["stages"]["lua"]["p50"], 800.0)
        self.assertAlmostEqual(summary["failed"]["stages"]["status"]["p50"], 500.0)
        self.assertNotIn("left", summary["inputs"])
        self.assertAlmostEqual(summary["inputs"]["right"]["p50"], 100.0)
        text = summary_tool.render(summary)
        self.assertIn("failed presents: 1", text)
        self.assertIn("failed present breakdown:", text)
        self.assertIn("dominant stage (by mean): rt_copy", text)

    def test_transport_counts_damaged_and_skipped_presents(self):
        records = []
        for seq, damaged in ((1, False), (2, True), (3, True)):
            t = seq * 1_000_000
            records.append({"t_us": t, "kind": "tick", "tick_us": 100, "render_us": 1000, "queued": True, "seq": seq})
            present = {"t_us": t + 50_000, "kind": "present", "seq": seq, "queued_t_us": t, "presented": True,
                       "staged_pixels": 3200 if damaged else 921600,
                       "runtime_stored_pixels": 6400 if damaged else 921600}
            if damaged:
                present.update({"damage_rects": 1, "status_skipped": True})
            if seq == 3:
                present["retried"] = True
            records.append(present)
        summary = summary_tool.summarise(records)
        moved = summary["transport"]
        self.assertEqual((moved["damaged"], moved["status_skipped"], moved["retried"]), (2, 2, 1))
        self.assertAlmostEqual(moved["staged_pixels"], (921600 + 3200 + 3200) / 3)
        self.assertIn("transport: damaged=2 status_skipped=2 retried=1", summary_tool.render(summary))

    def test_after_keeps_the_tick_when_the_present_completes_in_the_next_dump(self):
        # Dump taken after the tick was queued and before the worker wrote the present.
        tick = {"t_us": 1000, "kind": "tick", "tick_us": 100, "queued": True, "seq": 7, "view": "library"}
        previous = [{"t_us": 900, "kind": "input", "cmd": "left"}, tick]
        present = {"t_us": 1050, "kind": "present", "seq": 7, "queued_t_us": 1040, "presented": True,
                   "frame_copy_us": 10}
        sliced = summary_tool.after(previous + [present], previous)
        self.assertEqual([record["kind"] for record in sliced], ["tick", "present"])
        self.assertTrue(sliced[0]["_join_only"])
        summary = summary_tool.summarise(sliced)
        self.assertEqual(summary["frames"], 1)
        self.assertEqual(summary["inputs"], {})
        self.assertAlmostEqual(summary["stages"]["total"]["p50"], 0.05)

    def test_after_keeps_a_tick_appended_after_a_concurrent_present(self):
        # The worker appends the present during the iteration. tick() then
        # appends the tick with the iteration's earlier start time.
        present = {"t_us": 1050, "kind": "present", "seq": 7, "queued_t_us": 1040, "presented": True}
        tick = {"t_us": 1000, "kind": "tick", "tick_us": 100, "queued": True, "seq": 7, "view": "library"}
        stepped = {"t_us": 400, "kind": "input", "cmd": "down"}
        sliced = summary_tool.after([present, tick, stepped], [present])
        self.assertEqual([record["kind"] for record in sliced], ["present", "tick", "input"])
        self.assertTrue(sliced[0]["_join_only"])
        summary = summary_tool.summarise(sliced)
        self.assertEqual(summary["frames"], 1)
        self.assertEqual([record["cmd"] for record in sliced if record["kind"] == "input"], ["down"])
        self.assertEqual(summary["inputs"], {})

    def test_after_keeps_new_records_when_the_clock_steps_backward(self):
        previous = [
            {"t_us": 1_000_000, "kind": "input", "cmd": "left"},
            {"t_us": 1_000_100, "kind": "tick", "tick_us": 100, "queued": True, "seq": 1, "view": "library"},
            {"t_us": 1_000_200, "kind": "present", "seq": 1, "queued_t_us": 1_000_150, "presented": True},
        ]
        fresh = [
            {"t_us": 500_000, "kind": "input", "cmd": "right"},
            {"t_us": 500_100, "kind": "tick", "tick_us": 1000, "lua_step_us": 1000, "queued": True,
             "seq": 2, "view": "room:example"},
            {"t_us": 500_200, "kind": "present", "seq": 2, "queued_t_us": 500_150, "presented": True},
        ]
        summary = summary_tool.summarise(summary_tool.after(previous + fresh, previous))
        self.assertEqual(summary["frames"], 1)
        self.assertEqual(summary["views"]["room:example"]["frames"], 1)
        self.assertEqual(set(summary["inputs"]), {"right"})
        self.assertAlmostEqual(summary["inputs"]["right"]["p50"], 0.2)

    def test_after_uses_the_later_dump_when_the_ring_has_wrapped(self):
        previous = [
            {"t_us": 1_000_000, "kind": "tick", "queued": True, "seq": 1, "view": "library"},
            {"t_us": 1_000_100, "kind": "present", "seq": 1, "queued_t_us": 1_000_050, "presented": True},
        ]
        later = [
            {"t_us": 30, "kind": "input", "cmd": "right"},
            {"t_us": 40, "kind": "tick", "queued": True, "seq": 9, "view": "library"},
            {"t_us": 80, "kind": "present", "seq": 9, "queued_t_us": 50, "presented": True},
        ]
        self.assertEqual(summary_tool.after(later, previous), later)
        summary = summary_tool.summarise(summary_tool.after(later, previous))
        self.assertEqual(summary["frames"], 1)
        self.assertEqual(set(summary["inputs"]), {"right"})

    def test_after_drops_a_rotated_prefix_and_keeps_the_shared_tail(self):
        dropped = {"t_us": 1, "kind": "input", "cmd": "up"}
        tick = {"t_us": 2, "kind": "tick", "tick_us": 10, "queued": True, "seq": 4, "view": "library"}
        present = {"t_us": 50, "kind": "present", "seq": 4, "queued_t_us": 40, "presented": True}
        summary = summary_tool.summarise(summary_tool.after([tick, present], [dropped, tick]))
        self.assertEqual(summary["frames"], 1)
        self.assertEqual(summary["inputs"], {})

    def test_after_matches_a_shared_tail_when_the_last_record_is_not_unique(self):
        # The ring dropped the older A and then appended a new A, B. The last
        # record of the earlier dump also occurs at the end of the later one.
        old_a = {"t_us": 1, "kind": "input", "cmd": "a"}
        old_b = {"t_us": 2, "kind": "input", "cmd": "b"}
        new_a = {"t_us": 3, "kind": "input", "cmd": "a"}
        new_b = {"t_us": 4, "kind": "input", "cmd": "b"}
        self.assertEqual(summary_tool.after([old_b, new_a, new_b], [old_a, old_b]), [new_a, new_b])

    def test_after_does_not_repeat_a_frame_already_in_the_earlier_dump(self):
        tick = {"t_us": 100, "kind": "tick", "queued": True, "seq": 1, "view": "library"}
        present = {"t_us": 200, "kind": "present", "seq": 1, "queued_t_us": 150, "presented": True}
        added = {"t_us": 300, "kind": "input", "cmd": "right"}
        previous = [tick, present]
        self.assertEqual(summary_tool.after(previous, previous), [])
        self.assertEqual(summary_tool.after(previous + [added], previous), [added])
        self.assertEqual(summary_tool.summarise(summary_tool.after(previous + [added], previous))["frames"], 0)

    def test_cli_after_isolates_the_second_interaction(self):
        first = [
            {"t_us": 1_000, "kind": "input", "cmd": "left"},
            {"t_us": 1_100, "kind": "tick", "tick_us": 2000, "lua_step_us": 2000, "queued": True,
             "seq": 1, "view": "library"},
            {"t_us": 1_400, "kind": "present", "seq": 1, "queued_t_us": 1_200, "presented": True},
        ]
        second = [
            {"t_us": 2_000, "kind": "input", "cmd": "right"},
            {"t_us": 2_100, "kind": "tick", "tick_us": 3000, "lua_step_us": 3000, "queued": True,
             "seq": 2, "view": "room:example"},
            {"t_us": 2_500, "kind": "present", "seq": 2, "queued_t_us": 2_200, "presented": True},
        ]
        with tempfile.TemporaryDirectory() as tmp:
            previous = Path(tmp) / "dump-1.jsonl"
            later = Path(tmp) / "dump-2.jsonl"
            previous.write_text("".join(json.dumps(record) + "\n" for record in first))
            later.write_text("".join(json.dumps(record) + "\n" for record in first + second))
            whole = io.StringIO()
            with redirect_stdout(whole):
                self.assertEqual(summary_tool.main([str(later), "--json"]), 0)
            interval = io.StringIO()
            with redirect_stdout(interval):
                self.assertEqual(summary_tool.main([str(later), "--after", str(previous), "--json"]), 0)
        whole_summary = json.loads(whole.getvalue())
        interval_summary = json.loads(interval.getvalue())
        self.assertEqual(whole_summary["frames"], 2)
        self.assertEqual(set(whole_summary["inputs"]), {"left", "right"})
        self.assertEqual(interval_summary["frames"], 1)
        self.assertEqual(set(interval_summary["inputs"]), {"right"})
        self.assertEqual(interval_summary["views"]["room:example"]["frames"], 1)


if __name__ == "__main__":
    unittest.main()
