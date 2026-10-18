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
  - the render-thread frame copy
  - queue wait
  - status round trip
  - the client's `menu_frame_begin`, staging copy and commit round trip
  - from the runtime's commit reply: the DDR copy, slot submit and scanout wait (`libmister-runtime/docs/menu-display.md`)
  - `staged_pixels` and `runtime_stored_pixels`: pixels the client copied into staging and the runtime wrote to DDR
  - `damage_rects` for a damaged present, `status_skipped` when the worker trusted its cached status, and `retried` when a shortcut was refused and the frame went again whole

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
```

It joins each tick that queued a frame with that frame's present and reports
p50/p95/max and the mean share per stage. The stages are `lua`, `tick_other`,
`snapshot`, `raster`, `frame_copy`, `queue`, `status`, `begin`, `stage`,
`rt_copy`, `rt_submit`, `rt_scanout` and `commit_io`. Those changed-frame
figures include only presents that reached the screen (`presented: true`). A
rejected present is reported separately, with its own count and stage
breakdown, so menu failures do not move the averages. It also reports each
view's displayed-frame total and input→picture latency per command. Latency
uses the first present that reached the screen. A `transport` line counts
damaged, status-skipped and retried presents and the mean pixels staged and
stored per displayed frame.

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
4. Copy the `fogcast-frame-trace-*.jsonl` files off the kit and run the summary on each.
5. Re-activate the previous bundle.

## Reading the result

- **`lua` dominates:** room scripts are the cost. Consider LuaJIT or a faster
  Lua VM, or less per-frame script work.
- **`rt_copy`, `stage` or `frame_copy` dominate:** moving pixels is the cost.
  Check `transport`: if few presents are damaged, find why the shortcut is
  refused; if most are, the remaining step is a write-combining DDR mapping.
- **`raster` dominates and stays over budget after that:** that is the case for
  FPGA 2D acceleration.
- **`rt_scanout` is the remainder:** the wait for the next vsync after submit.
  It is bounded by the display rate rather than the CPU.
