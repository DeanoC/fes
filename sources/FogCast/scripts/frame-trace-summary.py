#!/usr/bin/env python3
"""Summarise a fogcast-tenfoot frame trace: where each changed frame's time went.

A trace is the JSON Lines file tenfoot writes on SIGUSR1 (see
docs/frame-trace.md). Each tick that queued a new menu frame is joined with
that frame's present record. A present that reached the screen is one changed
frame. A present the worker rejected is counted on its own and left out of
the changed-frame averages.

Stages:

  lua        room Lua: async callbacks + update + draw (inside Tick)
  tick_other the rest of Tick (host polling, state, non-Lua work)
  snapshot   building the render snapshot
  raster     CPU drawing into the framebuffer (render minus frame copy)
  frame_copy handing the rendered frame to the display worker: a buffer swap
             now (older traces copied the whole frame); a frame that does not
             start with Clear copies the previous one back inside raster
  queue      waiting for the display worker
  status     runtime status round trip before presenting
  begin      dial + menu_frame_begin round trip (and a new generation's grant)
  stage      writing the frame's pixels into the granted slot (before the
             fes-window grants: mapping a staging memfd, copying, sealing)
  rt_copy    runtime copying the frame into FPGA DDR (traces from before the
             fes-window grants; zero once the client writes the slot)
  rt_clean   runtime: writing back the client's lines for the FPGA
  rt_blit    runtime: the menu blitter drawing a list frame into the slot
  rt_submit  runtime: submitting the slot
  rt_scanout runtime: waiting until scanout shows the new slot
  commit_io  commit round trip not accounted for by the runtime fields

Transport counts how displayed frames travelled: how many were damaged
presents (only changed rectangles staged), skipped the status round trip, or
were retried whole after a shortcut was refused, and the mean pixels staged
by the client and stored to DDR by the runtime per frame. For frames the
blitter drew it counts them and gives their mean list length and heap
upload; their raster is recording commands, and stage is writing the list
and uploads.

Input-to-picture latency is measured from each input record to the first
present that reached the screen and was queued at or after it.

Every dump holds the whole ring, so consecutive dumps overlap. --after
PREVIOUS.jsonl keeps the records appended since that dump. The cut is the
shared ring tail, matched by record identity, so a backward clock step still
leaves new records in the interval. A tick and its present can fall on
opposite sides of the cut; the earlier half is kept and the frame is counted
in the interval where the pair becomes complete. Input metrics include only
commands appended since the earlier dump.

Usage: frame-trace-summary.py TRACE.jsonl [--after PREVIOUS.jsonl] [--json]
"""

import argparse
import json
import statistics
import sys
from collections import defaultdict

STAGES = ["lua", "tick_other", "snapshot", "raster", "frame_copy", "queue", "status",
          "begin", "stage", "rt_copy", "rt_clean", "rt_blit", "rt_submit", "rt_scanout", "commit_io"]


def load(path):
    with open(path, encoding="utf-8") as handle:
        return [json.loads(line) for line in handle if line.strip()]


def _identity(record):
    return json.dumps(record, sort_keys=True, separators=(",", ":"))


def _overlap_len(previous, records):
    """How many leading records of `records` repeat the tail of `previous`.

    The ring drops the oldest records and appends new ones, so the records
    still shared are a suffix of the earlier dump and a prefix of the later
    one. The border of `records + sentinel + previous` is that length. Zero
    means the ring has wrapped past every shared record.
    """
    if not previous or not records:
        return 0
    prev_ids = [_identity(record) for record in previous]
    rec_ids = [_identity(record) for record in records]
    sentinel = object()
    sequence = rec_ids + [sentinel] + prev_ids
    border = [0] * len(sequence)
    matched = 0
    for index in range(1, len(sequence)):
        while matched and sequence[index] != sequence[matched]:
            matched = border[matched - 1]
        if sequence[index] == sequence[matched]:
            matched += 1
        border[index] = matched
    return border[-1]


def _join_seq(record):
    """Sequence of a tick that queued a frame, or of a non-probe present."""
    if record.get("probe") or not record.get("seq"):
        return None
    kind = record.get("kind")
    if kind == "present" or (kind == "tick" and record.get("queued")):
        return record["seq"]
    return None


def _join_only(record):
    marked = dict(record)
    marked["_join_only"] = True
    return marked


def after(records, previous):
    """Interval since an earlier dump of the same ring.

    The cut is the shared tail, matched by record identity. `t_us` is Unix
    wall-clock microseconds, so a clock step backward must still leave the
    appended records in the interval. When the dumps share nothing, the ring
    has wrapped and the whole later dump is the interval.

    The tick is appended when the iteration finishes and carries the
    iteration's start time. The present is appended when the display worker
    finishes, which can be before that tick is written. Whichever half is
    already in the earlier dump is kept, marked `_join_only`, so the pair
    joins once: in the interval where both records are on disk. Input metrics
    skip those join records and include only commands appended since the dump.
    """
    if not previous:
        return list(records)
    overlap = _overlap_len(previous, records)
    if overlap == 0:
        return list(records)
    fresh = list(records[overlap:])
    fresh_present = set()
    fresh_tick = set()
    for record in fresh:
        seq = _join_seq(record)
        if seq is None:
            continue
        if record.get("kind") == "present":
            fresh_present.add(seq)
        else:
            fresh_tick.add(seq)
    kept = []
    for record in records[:overlap]:
        seq = _join_seq(record)
        if seq is None:
            continue
        if record.get("kind") == "tick" and seq in fresh_present:
            kept.append(_join_only(record))
        elif record.get("kind") == "present" and seq in fresh_tick:
            kept.append(_join_only(record))
    return kept + fresh


def _latest_presents(records):
    """Last non-probe present for each seq. A later record replaces an earlier one."""
    presents = {}
    for record in records:
        if record.get("kind") != "present" or not record.get("seq") or record.get("probe"):
            continue
        presents[record["seq"]] = record
    return presents


def _frame_row(tick, present):
    """One changed or failed frame, with each stage in milliseconds."""
    us = lambda record, key: record.get(key, 0) or 0
    lua = us(tick, "lua_step_us")
    rt = (us(present, "runtime_copy_us") + us(present, "runtime_clean_us") + us(present, "runtime_blit_us") +
          us(present, "runtime_submit_us") + us(present, "runtime_scanout_wait_us"))
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
        "rt_clean": us(present, "runtime_clean_us"),
        "rt_blit": us(present, "runtime_blit_us"),
        "rt_submit": us(present, "runtime_submit_us"),
        "rt_scanout": us(present, "runtime_scanout_wait_us"),
        "commit_io": max(0, us(present, "commit_us") - rt),
        "presented": bool(present.get("presented")),
        "damaged": bool(present.get("damage_rects")),
        "status_skipped": bool(present.get("status_skipped")),
        "retried": bool(present.get("retried")),
        "staged_pixels": us(present, "staged_pixels"),
        "stored_pixels": us(present, "runtime_stored_pixels"),
        "blitted": bool(present.get("blitted")),
        "commands": us(present, "commands"),
        "upload_bytes": us(present, "upload_bytes"),
        "t_us": tick["t_us"],
        "done_us": present["t_us"],
    }
    for key in STAGES:
        row[key] = row[key] / 1000.0
    row["total"] = (row["done_us"] - row["t_us"]) / 1000.0
    return row


def frames(records):
    """Displayed frames, then failed attempts. Both are stage rows in milliseconds.

    A present with presented false never reached the screen, so it is not a
    displayed frame. Probes are ignored.
    """
    presents = _latest_presents(records)
    displayed, failed = [], []
    for tick in records:
        if tick.get("kind") != "tick" or not tick.get("queued"):
            continue
        present = presents.get(tick.get("seq"))
        if present is None:
            continue
        row = _frame_row(tick, present)
        if row["presented"]:
            displayed.append(row)
        else:
            failed.append(row)
    return displayed, failed


def latencies(records):
    """Input command -> ms until the first on-screen present queued at or after it.

    A record kept only to finish a frame that straddles two dumps is not an
    input, and its present is not a picture for this interval.
    """
    def in_interval(record):
        return not record.get("_join_only")

    presents = sorted((r for r in records if in_interval(r) and r.get("kind") == "present"
                       and r.get("presented") and not r.get("probe")),
                      key=lambda r: r.get("queued_t_us", 0))
    out = defaultdict(list)
    for record in records:
        if record.get("kind") != "input" or not in_interval(record):
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


def stage_stats(rows):
    stats = {}
    for key in STAGES + ["total"]:
        values = [row[key] for row in rows]
        stats[key] = {"p50": pct(values, 50), "p95": pct(values, 95), "max": max(values, default=0.0),
                      "mean": statistics.fmean(values) if values else 0.0}
    return stats


def transport(rows):
    mean = lambda key, of=rows: statistics.fmean(row[key] for row in of) if of else 0.0
    blitted = [row for row in rows if row["blitted"]]
    return {"damaged": sum(row["damaged"] for row in rows), "status_skipped": sum(row["status_skipped"] for row in rows),
            "retried": sum(row["retried"] for row in rows), "staged_pixels": mean("staged_pixels"),
            "stored_pixels": mean("stored_pixels"), "blitted": len(blitted),
            "commands": mean("commands", blitted), "upload_bytes": mean("upload_bytes", blitted)}


def summarise(records):
    rows, failed = frames(records)
    summary = {"frames": len(rows), "stages": stage_stats(rows), "views": {}, "inputs": {}, "dominant": None,
               "failed": {"frames": len(failed), "stages": stage_stats(failed)}, "transport": transport(rows)}
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


def _stage_table(stats):
    total_mean = stats["total"]["mean"] or 1.0
    lines = [f"{'stage':<11} {'p50 ms':>8} {'p95 ms':>8} {'max ms':>8} {'share':>6}"]
    for key in STAGES + ["total"]:
        stage = stats[key]
        share = "" if key == "total" else f"{100.0 * stage['mean'] / total_mean:5.1f}%"
        lines.append(f"{key:<11} {stage['p50']:8.2f} {stage['p95']:8.2f} {stage['max']:8.2f} {share:>6}")
    return lines


def render(summary):
    failed = summary.get("failed") or {"frames": 0}
    lines = [f"changed frames: {summary['frames']}"]
    if failed["frames"]:
        lines.append(f"failed presents: {failed['frames']}")
    if not summary["frames"]:
        lines.append("no changed frames with a matching present record")
    else:
        lines.extend(_stage_table(summary["stages"]))
        lines.append(f"dominant stage (by mean): {summary['dominant']}")
        moved = summary.get("transport")
        if moved:
            lines.append(f"transport: damaged={moved['damaged']} status_skipped={moved['status_skipped']} retried={moved['retried']}"
                         f"  mean pixels staged={moved['staged_pixels']:.0f} stored={moved['stored_pixels']:.0f}")
            if moved.get("blitted"):
                lines.append(f"blitter: frames={moved['blitted']} mean commands={moved['commands']:.0f}"
                             f" mean upload bytes={moved['upload_bytes']:.0f}")
        if summary["views"]:
            lines.append("")
            lines.append("frame total by view:")
            for view, stage in sorted(summary["views"].items()):
                lines.append(f"  {view:<32} n={stage['frames']:<5} p50={stage['p50']:.1f} ms  p95={stage['p95']:.1f} ms")
    if failed["frames"]:
        lines.append("")
        lines.append("failed present breakdown:")
        lines.extend(_stage_table(failed["stages"]))
    if summary["inputs"]:
        lines.append("")
        lines.append("input -> picture:")
        for cmd, stage in sorted(summary["inputs"].items()):
            lines.append(f"  {cmd:<12} n={stage['count']:<5} p50={stage['p50']:.1f} ms  p95={stage['p95']:.1f} ms  max={stage['max']:.1f} ms")
    return "\n".join(lines)


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("trace")
    parser.add_argument("--after", metavar="PREVIOUS",
                        help="keep the interval since the earlier dump PREVIOUS")
    parser.add_argument("--json", action="store_true", help="print the summary as JSON")
    args = parser.parse_args(argv)
    records = load(args.trace)
    if args.after:
        records = after(records, load(args.after))
    summary = summarise(records)
    print(json.dumps(summary, indent=2) if args.json else render(summary))
    return 0


if __name__ == "__main__":
    sys.exit(main())
