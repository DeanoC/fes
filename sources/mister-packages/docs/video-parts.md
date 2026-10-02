# Timed video parts

[`packages/fabric/fes_fabric_video_raster_rgb888.yaml`](../packages/fabric/fes_fabric_video_raster_rgb888.yaml)
owns the internal `fes.fabric.video.raster-rgb888` 1.0 socket constants.
The existing Verilog constant emitter produces `fes_video_part.vh` for
misteross. This contract connects an FPGA picture/timing source to one selected
video part before output; it is separate from the operational
`fes.video.fixed-720p60` interface and its GP capability. A package's optional
fabric marker describes the linkable source/socket. It does not add a GP bit,
change the host ABI, or admit another output mode to the runtime.

The implemented proof processes a 74.25 MHz, 1650 by 750 full raster with
1280 by 720 active RGB888 pixels, positive horizontal sync at 1390..1429
and positive vertical sync at 725..729. One always-running pixel clock drives
the source, socket and part. CE is one in this fixed mode. The source is
already scaled to HDMI timing; this socket does not expose a native machine
raster or move the existing capture buffers out of their cores.

## Packed signals

The public signals are `video_request[31:0]` and `video_response[27:0]`.
The producer's physical linker can map them onto its packed input/output
anchors; those backend anchor names are not the public video vocabulary.

| Request bits | Meaning |
| --- | --- |
| 23..0 | RGB888: red 23..16, green 15..8, blue 7..0 |
| 24 | DE, active display pixel |
| 25 | HS, output horizontal sync level |
| 26 | VS, output vertical sync level |
| 27 | CE, qualifies this pixel and timing/markers |
| 28 | SOF, first pixel of the full raster (including blanking) |
| 29 | EOL, final pixel of each full raster line (including blanking) |
| 30 | HOLD, substitute black picture while timing continues |
| 31 | Reserved, producer must send zero |

| Response bits | Meaning |
| --- | --- |
| 23..0 | Processed RGB888, black during blanking or HOLD |
| 24..26 | DE, HS and VS aligned with that RGB value |
| 27 | CE, valid processed pixel and timing |

There is no ready signal or source backpressure. A part must finish its
combinational pixel transformation within the pixel-clock period. CE zero
means no pixel/timing delivery and no SOF/EOL state transition. Its response
is zero. A reserved-one request is invalid and similarly produces zero without
advancing part state. Gating each response field by qualified CE retains real
drivers when the part is synthesized independently of the shell.

SOF/EOL describe the complete raster, not the active picture or machine
capture image. They are qualified by CE and stay aligned with their RGB/sync
bundle. SOF establishes line zero for its own pixel; EOL finishes the current
line and changes state for the next pixel. A one-pixel line can carry both.
HOLD mutes RGB without changing DE/HS/VS/CE or suspending raster tracking;
releasing HOLD mid-frame therefore preserves the line phase. The first valid
source pixel after FPGA startup establishes SOF. Timing continues through
machine execution hold/reset; the shell owns reset synchronization and the
initial muted values in both socket registers.

## Latency and implemented parts

Every composition uses the same two registered boundaries: the shell captures
the entire request, the selected part transforms it, and the shell captures
the entire response on the following edge. RGB and DE/HS/VS/CE cross both
boundaries together. Parts do not add a third pixel pipeline stage. A vacant
socket selects the shell's equivalently delayed original picture. The part
does not own HDMI pads, PLLs, HPS atoms, GP identity or execution reset.

`fes_video_part_direct` passes qualified timing and active RGB through.
`fes_video_part_scanlines` additionally halves each RGB channel on odd full
raster lines; it tracks CE-qualified SOF/EOL in one parity register. This is a
small output effect, not a CRT simulation. The dedicated
`scripts/sim_fes_video_parts.py` simulation tests both real modules through
the actual `coleco_video_socket` simulation path over full 720p frames, CE gaps, hold picture mute,
blanking and sync, invalid requests, EOL ordering and SOF recovery.

## Other source standards

Geometry, source cadence and output mode are composition metadata, not
inferred from a core's display name or the RGB bus. The present admission
bound is exactly the fixed 720p60 mode above. CE gaps are defined and tested
but do not authorize variable-rate, interlaced, native low-resolution or
high-definition output profiles. A future native profile must define its
dimensions, border/blanking meaning, aspect ratio, pixel rate, frame-rate bounds
and clock crossings, including the capture/scaling ownership. It must also
define complete-frame delivery or repeat/drop behavior before a DDR consumer
can provide tear-free output. These future profiles and DDR/filter consumers
are not implemented by this contract proof.
