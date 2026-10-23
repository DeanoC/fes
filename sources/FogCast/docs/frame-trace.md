# Frame trace: where kit UI frame time goes

`fogcast-tenfoot` on the kit display loop (`-gfx menu-display`, and linuxfb)
keeps an always-on ring of recent frame timings. Nothing is written until the
process receives `SIGUSR1`, so a managed kit needs no configuration change.
Recording costs a few clock reads per tick, and no budget or behaviour depends
on it.

## What is recorded

Each JSON line has `kind`:

- **`tick`**, one loop iteration that rendered. `t_us` is when that iteration began work, not the ticker's scheduled time, so a late tick does not count the wait as poll or frame work:
  - poll
  - `Tick`, with the room Step that ran on this iteration: async callbacks, `update`, `draw` and the whole Step. These stay zero when the room did not step
  - `Snapshot`
  - render
  - `view`: `attract`, `now-playing`, `room:<id>`, or `library`, matching the frame that was drawn
  - `queued`/`seq`, when it produced a new menu frame
- **`input`:** a command (`cmd`) as `HandleCommand` received it.
- **`present`:** one frame the menu-display worker handled:
  - the render-thread hand-over of the frame (`frame_copy_us`): a framebuffer swap, so near zero; older traces copied the whole frame here. A frame that does not start with Clear copies the previous frame back while it draws, inside render time
  - queue wait
  - status round trip
  - the client's `menu_frame_begin` (with a new generation's grant), writing the granted slot (`stage_us`) and commit round trip
  - from the runtime's commit reply: the write-back of the client's lines (`runtime_clean_us`), the blitter's run for a list frame (`runtime_blit_us`), slot submit and scanout wait (`libmister-runtime/docs/menu-display.md`)
  - `staged_pixels`: pixels the client wrote into the slot
  - `blitted`, `commands` and `upload_bytes` for a frame the menu blitter drew: its list length and the texture bytes uploaded into the grant's heap. On such a frame `stage_us` is writing the list and uploads, and render time is recording commands rather than drawing
  - `damage_rects` for a damaged present, `status_skipped` when an attempt trusted cached status (a refused shortcut still counts), and `retried` when a shortcut was refused and the frame went again whole

The ring holds 16,384 records, which is several minutes at the 30 Hz loop. Each
`SIGUSR1` writes a new `fogcast-frame-trace-<pid>-<n>-<utc>.jsonl` to
`$FOGCAST_FRAME_TRACE_DIR` (default the temp directory, `/tmp` on a kit) and
prints that path on stderr. Earlier dumps stay on disk. The ring is kept, so
consecutive files overlap in time.

**Send `SIGUSR1` only to a tenfoot that includes this trace.** Older binaries
have no handler, and the signal ends the process.

## Summarise

From `sources/FogCast`, on the development host:

```sh
python3 scripts/frame-trace-summary.py trace.jsonl          # table
python3 scripts/frame-trace-summary.py trace.jsonl --json   # machine-readable
python3 scripts/frame-trace-summary.py dump-2.jsonl --after dump-1.jsonl
```

Each dump holds the whole ring, so a later dump repeats the earlier tail.
`--after` matches that shared tail by record identity and keeps what the ring
appended since the earlier dump. A clock step backward still leaves those new
records in the interval: a kit has no RTC, and the trace stores Unix
microseconds. When the ring has wrapped past every shared record, the later
dump is the whole interval.

A tick and its present are written apart. The tick is appended when the
iteration finishes and carries that iteration's start time; the present is
appended when the display worker finishes. A dump can hold either record
while the other arrives later, including a present the worker appends during
the iteration and a tick appended afterwards with an earlier timestamp.
`--after` keeps the earlier half so the pair still joins, and counts the
frame once, in the interval where both records are on disk. Input metrics
include only commands appended since the earlier dump.

It joins each tick that queued a frame with that frame's present and reports
p50/p95/max and the mean share per stage. The stages are `lua`, `tick_other`,
`snapshot`, `raster`, `frame_copy`, `queue`, `status`, `begin`, `stage`,
`rt_copy`, `rt_clean`, `rt_blit`, `rt_submit`, `rt_scanout` and `commit_io`. `rt_copy`
is only in traces from before the fes-window grants, when the runtime copied
each frame; since then the client writes the slot (`stage`) and the runtime
only writes back its lines (`rt_clean`). Those changed-frame
figures include only presents that reached the screen (`presented: true`). A
rejected present is reported separately, with its own count and stage
breakdown, so menu failures do not move the averages. It also reports each
view's displayed-frame total and input→picture latency per command. Latency
uses the first present that reached the screen. A `transport` line counts
damaged, status-skipped and retried presents and the mean pixels staged per
displayed frame (and, in older traces, stored by the runtime). A `blitter`
line counts the frames the blitter drew, with their mean list length and heap
upload.

## Kit measurement run

This needs an authorized kit session and the normal
[managed software path](../../../docs/kit-software-releases.md); it changes no
image or boot file.

1. Build a software bundle from this source with `make kit-software` against
   the kit's verified base image. Install and activate it at a free lease
   boundary, and record the previous bundle ID.
2. Wait until Home is settled, then dump once to discard startup:
   `kill -USR1 $(pidof fogcast-tenfoot)`.
3. Drive each interaction for about 20 seconds with the usual uinput pad,
   about two presses a second:
   - Home shelf Left/Right
   - Up to the routes and back
   - Library grid browse
   - Library Details open/close
   - Settings open/close
   - the ZX81 Workshop shelf

   Send `SIGUSR1` after each. Every signal writes a new file, and the
   process prints its path; the name includes the dump time.

   Each dump is about 2.5 MB, and `/tmp` on the kit is tmpfs, so dumps use
   kit RAM. Copy each file off the kit and delete it before the next
   interaction.
4. Summarise each interaction with `--after` the previous dump, for example
   `frame-trace-summary.py home-shelf.jsonl --after startup.jsonl`. Without
   `--after`, a summary covers every frame still in the ring.
5. Re-activate the previous bundle.

## Reading the result

- **`lua` dominates:** room scripts are the cost. Consider LuaJIT or a faster
  Lua VM, or less per-frame script work.
- **`rt_copy`, `rt_clean`, `stage` or `frame_copy` dominate:** moving pixels is the cost.
  Check `transport`: if few presents are damaged, find why the shortcut is
  refused. With the menu blitter
  ([#949](https://github.com/DeanoC/fes/issues/949) step 2) the CPU writes no
  slot pixels; check the `blitter` line for how many frames it drew.
- **`raster` dominates:** the CPU is drawing. On a blitter grant it only
  records commands, so check that the frames were `blitted`.
- **`rt_blit` dominates:** the fabric is drawing. It fills about 2 pixels and
  blends about 1 pixel per 74.25 MHz clock, so overdraw sets its time.
- **`rt_scanout` is the remainder:** the wait for the next vsync after submit.
  It is bounded by the display rate rather than the CPU.
