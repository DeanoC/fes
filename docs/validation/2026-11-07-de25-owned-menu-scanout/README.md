# Owned menu scanout host result

Source `2b361b998098b825e9ae0b6d2a294e8beaa589c9` connects the actual FES menu
video engine and FIFO to the owned embedded read endpoint and Nano 128/256-bit
DDR adapter. This produces the 720p RGB/sync stream missing from the previous
checksum-window test. No board image selects this source yet.

The actual Tenfoot App/Presenter renders two different 1280×720 frames, copied
through `scanout.Surface.Present` into packed XRGB. An independent AXI memory and
raster model checks 2,764,800 output pixels over slot 0 → slot 1 → slot 0, with
the last frame under a new reader generation. It checks every burst against the
visible slot extent and 4 KiB boundary, fixed sync timing, zero normal underflows
and acknowledgement only after the final pixel of a complete frame.

Further cases cover a stalled offered address during Quiesce, missing memory
responses and incomplete-frame rejection, bus errors, stale sequences, same-slot
reuse, ownership loss, video-only reset and replayed initial sequence. A mutant
that acknowledges prefetch instead of full display fails the final-pixel oracle.
A separately elaborated 4 MiB arena refuses Start and issues no DDR read. The
Agilex reader changes only the original guarded M10K hint to M20K; its FIFO logic
is unchanged. Verilator suppresses inherited empty-pin/unused-signal style
warnings, with other warnings fatal.

All 12 affected commands pass on that committed source, totaling 868.593 seconds.
They include 1,617 parent tests (3 skipped), 1,590 FPGA Python tests (78 skipped),
host Go/UI and browser tests, runtime software tests, generated-consumer and
shared-schema checks, and the existing firmware/FPGA producer integration.
The [result](result.json) records source, model and individual command hashes;
the retained model has 478 source-file hashes, compiler identity, frame digests,
executable digests and the failing negative-control logs.

This is host-only evidence. All video/control/reader ports currently share one
qualified pixel clock. Board clock crossings, HDMI setup, the native control
binding and secure 8 MiB double-buffer reservation are still required. Existing
4 MiB CRC-window firmware supplies no authority for this source. Local Stopped
must be followed by endpoint/all-master drain and bridge closure before a core
transition; it grants no memory release. No physical HDMI, ordinary package
admission, DDR timing or full DE25 core-load acceptance is established here.
TRWU8CBL was not touched by this change. Its preceding
[three-cycle physical result](../2026-11-07-de25-distinct-observer-trial/README.md)
remains bound to that test's recorded sources and payloads.

Reproduce from this committed source with
`python3 sources/misteross/prototypes/embedded_control/verify_menu.py`.
It creates a fresh private output under `out/` and never accesses a board.
