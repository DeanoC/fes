#!/usr/bin/env python3
"""Summarise a fogcast-tenfoot frame trace: where each changed frame's time went.

A trace is the JSON Lines file tenfoot writes on SIGUSR1 (see
docs/frame-trace.md). Each tick that queued a new menu frame is joined with
that frame's present record, giving one row per changed frame:

  lua        room Lua: async callbacks + update + draw (inside Tick)
  tick_other the rest of Tick (host polling, state, non-Lua work)
  snapshot   building the render snapshot
  raster     CPU drawing into the framebuffer (render minus frame copy)
  frame_copy copying the rendered frame for the display worker
  queue      waiting for the display worker
  status     runtime status round trip before presenting
  begin      dial + menu_frame_begin round trip
  stage      mapping the staging memfd, copying the frame in, sealing it
  rt_copy    runtime: converting and writing the frame into FPGA DDR
  rt_submit  runtime: submitting the slot
  rt_scanout runtime: waiting until scanout shows the new slot
  commit_io  commit round trip not accounted for by the runtime fields

Input-to-picture latency is measured from each input record to the first
present that was queued at or after it.

Usage: frame-trace-summary.py TRACE.jsonl [--json]
"""

import argparse
import json
import statistics
import sys
from collections import defaultdict

STAGES = ["lua", "tick_other", "snapshot", "raster", "frame_copy", "queue", "status",
          "begin", "stage", "rt_copy", "rt_submit", "rt_scanout", "commit_io"]


def load(path):
    with open(path, encoding="utf-8") as handle:
        return [json.loads(line) for line in handle if line.strip()]


def frames(records):
    """One dict per changed frame, with each stage in milliseconds."""
    presents = {r["seq"]: r for r in records if r.get("kind") == "present" and r.get("seq") and not r.get("probe")}
    rows = []
    for tick in records:
        if tick.get("kind") != "tick" or not tick.get("queued"):
            continue
        present = presents.get(tick.get("seq"))
        if present is None:
            continue
        us = lambda record, key: record.get(key, 0) or 0
        lua = us(tick, "lua_step_us")
        rt = us(present, "runtime_copy_us") + us(present, "runtime_submit_us") + us(present, "runtime_scanout_wait_us")
        row = {
            "view": tick.get("view", ""),
            "lua": lua,
            "tick_other": max(0, us(tick, "tick_us") - lua),
            "snapshot": us(tick, "snapshot_us"),
            "raster": max(0, us(tick, "render_us") - us(present, "frame_copy_us")),
            "frame_copy": us(present, "frame_copy_us"),
            "queue": us(present, "queue_wait_us"),
            "status": us(present, "status_us"),
            "begin": us(present, "begin_us"),
            "stage": us(present, "stage_us"),
            "rt_copy": us(present, "runtime_copy_us"),
            "rt_submit": us(present, "runtime_submit_us"),
            "rt_scanout": us(present, "runtime_scanout_wait_us"),
            "commit_io": max(0, us(present, "commit_us") - rt),
            "presented": bool(present.get("presented")),
            "t_us": tick["t_us"],
            "done_us": present["t_us"],
        }
        for key in STAGES:
            row[key] = row[key] / 1000.0
        row["total"] = (row["done_us"] - row["t_us"]) / 1000.0
        rows.append(row)
    return rows


def latencies(records):
    """Input command -> ms until the first present queued at or after it."""
    presents = sorted((r for r in records if r.get("kind") == "present" and r.get("presented") and not r.get("probe")),
                      key=lambda r: r.get("queued_t_us", 0))
    out = defaultdict(list)
    for record in records:
        if record.get("kind") != "input":
            continue
        for present in presents:
            if present.get("queued_t_us", 0) >= record["t_us"]:
                out[record.get("cmd", "?")].append((present["t_us"] - record["t_us"]) / 1000.0)
                break
    return out


def pct(values, p):
    if not values:
        return 0.0
    ordered = sorted(values)
    index = min(len(ordered) - 1, max(0, int(round(p / 100.0 * (len(ordered) - 1)))))
    return ordered[index]


def summarise(records):
    rows = frames(records)
    summary = {"frames": len(rows), "stages": {}, "views": {}, "inputs": {}, "dominant": None}
    for key in STAGES + ["total"]:
        values = [row[key] for row in rows]
        summary["stages"][key] = {"p50": pct(values, 50), "p95": pct(values, 95), "max": max(values, default=0.0),
                                  "mean": statistics.fmean(values) if values else 0.0}
    if rows:
        means = {key: summary["stages"][key]["mean"] for key in STAGES}
        summary["dominant"] = max(means, key=means.get)
    by_view = defaultdict(list)
    for row in rows:
        by_view[row["view"]].append(row["total"])
    for view, values in by_view.items():
        summary["views"][view] = {"frames": len(values), "p50": pct(values, 50), "p95": pct(values, 95)}
    for cmd, values in latencies(records).items():
        summary["inputs"][cmd] = {"count": len(values), "p50": pct(values, 50), "p95": pct(values, 95), "max": max(values)}
    return summary


def render(summary):
    lines = [f"changed frames: {summary['frames']}"]
    if not summary["frames"]:
        lines.append("no changed frames with a matching present record")
        return "\n".join(lines)
    total_mean = summary["stages"]["total"]["mean"] or 1.0
    lines.append(f"{'stage':<11} {'p50 ms':>8} {'p95 ms':>8} {'max ms':>8} {'share':>6}")
    for key in STAGES + ["total"]:
        s = summary["stages"][key]
        share = "" if key == "total" else f"{100.0 * s['mean'] / total_mean:5.1f}%"
        lines.append(f"{key:<11} {s['p50']:8.2f} {s['p95']:8.2f} {s['max']:8.2f} {share:>6}")
    lines.append(f"dominant stage (by mean): {summary['dominant']}")
    if summary["views"]:
        lines.append("")
        lines.append("frame total by view:")
        for view, s in sorted(summary["views"].items()):
            lines.append(f"  {view:<32} n={s['frames']:<5} p50={s['p50']:.1f} ms  p95={s['p95']:.1f} ms")
    if summary["inputs"]:
        lines.append("")
        lines.append("input -> picture:")
        for cmd, s in sorted(summary["inputs"].items()):
            lines.append(f"  {cmd:<12} n={s['count']:<5} p50={s['p50']:.1f} ms  p95={s['p95']:.1f} ms  max={s['max']:.1f} ms")
    return "\n".join(lines)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("trace")
    parser.add_argument("--json", action="store_true", help="print the summary as JSON")
    args = parser.parse_args(argv)
    summary = summarise(load(args.trace))
    print(json.dumps(summary, indent=2) if args.json else render(summary))
    return 0


if __name__ == "__main__":
    sys.exit(main())
