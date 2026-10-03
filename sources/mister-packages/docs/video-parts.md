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

## Native active pixels

[`packages/fabric/fes_fabric_video_native_pixels.yaml`](../packages/fabric/fes_fabric_video_native_pixels.yaml)
defines the separate `fes.fabric.video.native-pixels` 1.0 contract and generates
`fes_native_video.vh`. Its request carries native active pixels before capture
and scaling. The selected part owns complete-frame capture and presentation;
the shell owns the source adapter, clock crossing, clocks and physical HDMI
output. This ABI does not change `fes.fabric.video.raster-rgb888` 1.0, its
request meanings or its two-boundary transformation latency.

The initial bounded profile has explicit width 256, height 192 and encoding
`Index4Tms9918` (value 0). The source emits only active pixels, in row-major
order, with no blanking pixels or input HS/VS levels. Each frame therefore has
exactly 49,152 pixel tokens. Geometry and encoding are parameters bound by the
producer/composition; they are not inferred from a system name, a pixel value
or the request width. The indexed encoding uses payload bits 3..0, requires
bits 23..4 to be zero and expands through the immutable RGB888 palette in the
YAML. Indices 0 and 1 both present black. RGB888 and RGB666 native encodings
are future profiles; this initial profile does not assign their encoding
values or claim capture support for them.

### Native tokens and clock crossing

The native socket has `native_request[31:0]` and `video_response[27:0]`.
Both physical socket register banks and all selected-part state run on the
always-running 74.25 MHz output pixel clock. The initial source runs on the
separate 52.224 MHz system clock. The shell registers and holds each entire
token, transfers a request toggle through synchronizer registers and captures
the held payload in the pixel domain. A synchronized acknowledgement permits
the source payload to change. Independently synchronizing payload bits or
sampling a changing source word does not satisfy this contract. The adapter
must preserve every admitted token and its markers. A transfer loss or
overflow emits an intentionally invalid token with VALID one and reserved
bit 30 one, invalidating the incoming frame rather than hiding the loss as
an idle cycle. The adapter separately retains the latest undelivered HOLD
control so an overrun cannot lose the final hold/release state.

| Native request bits | Meaning |
| --- | --- |
| 23..0 | Payload in the explicitly selected encoding |
| 24 | VALID, qualifies one pixel or control token |
| 25 | SOF, first active pixel at x=0, y=0 |
| 26 | EOL, final active pixel of each row at x=255 |
| 27 | EOF, final active pixel at x=255, y=191 |
| 28 | HOLD value in a control token |
| 29 | CONTROL, selects a control token instead of a pixel |
| 31..30 | Reserved, valid tokens must send zero |

VALID zero delivers no token and does not advance capture. A valid pixel token
has CONTROL and HOLD zero. SOF appears on exactly the first pixel; EOL appears
on exactly the last pixel of every row; EOF appears only on the final pixel
and coincides with EOL. Markers are part of that pixel's token, rather than
separate events. A valid control token has CONTROL one, zero payload and zero
SOF/EOL/EOF; only HOLD carries a value. It does not count as a pixel or advance
the capture coordinates.

An accepted HOLD-one control mutes the picture, aborts an incomplete capture
and discards a pending handoff while retaining the currently presented front
frame. The consumer starts held and requires an accepted HOLD-zero control
before capturing or displaying native pixels. HOLD wins over a bank swap on
the same output-SOF edge. The output
continues to emit black RGB with uninterrupted DE/HS/VS/CE. Pixel tokens
received while held cannot complete a presentable frame. Releasing HOLD
permits capture from a new valid SOF; it does not resume the aborted frame.
HOLD crosses clocks as a control token even when the machine has stopped
producing pixels.

### Complete-frame admission and presentation

Capture validates the encoding, reserved fields and exact sequential geometry
and markers. An unexpected SOF, missing or misplaced EOL/EOF, extra pixel,
invalid token or adapter drop cannot make an incomplete frame presentable.
Capture recovery starts with a new correctly formed SOF. A completed back
frame becomes pending only after its final pixel has been stored and checked.
The part uses two frame banks and changes the front bank only at output SOF;
RGB and sync must remain aligned through memory reads and the response
boundary. A part drops an entire incoming frame when the other bank already
holds a pending frame; it does not overwrite that bank or queue a partial
frame for later presentation.

The output repeats the most recent complete front frame when no new frame is
ready, including when native delivery stops. Before any complete frame is
available, RGB is black. Frame capture and output timing are independent:
the initial output remains 1280 by 720 active pixels in a 1650 by 750 raster
at 74.25 MHz, with positive HS at 1390..1429 and VS at 725..729. The response
bit meanings match the existing raster response: RGB888 in 23..0, DE/HS/VS
in 24..26 and CE in 27. CE remains one throughout this fixed output mode,
including blanking, HOLD and repeated frames; it is independent of native
VALID. Capturing a frame takes many native tokens, so native input and HDMI
output do not have the raster ABI's per-pixel transformation relationship.

The native contract and generated constants do not seal a physical socket,
admit a native part to a factory image or establish hardware acceptance.
A native producer must declare separate exact-shell slot/map/layout identities
and validate its clock ownership, memory placement, timing and configuration
containment. Existing factory raster parts retain their current identities
and selection behavior.

Device-table inspection finds 55 M10K sites in placement columns 5..38,
rows 23..38, which is disjoint from the Coleco CPU placement rows 1..19.
Packing two index4 pixels into each byte gives 24,576 words per frame bank;
the current compiler's 1024-word memory mapping suggests 24 M10Ks per bank,
or 48 for two banks. The native Direct/Scanlines diagnostic synthesis
(`make synth-fes-native-video` in misteross) maps each consumer to 48 M10Ks
with one RAM clock. This is synthesis evidence, not routed-part evidence.
The wider reservation requires a fresh shell route and broader frozen-clock
coverage before native parts can be sealed or composed.

## Other source standards

Geometry, source cadence and output mode are composition metadata, not
inferred from a core's display name or the RGB bus. The present admission
bound is exactly the fixed 720p60 mode above. CE gaps are defined and tested
but do not authorize variable-rate, interlaced or high-definition output
profiles. The separate native contract above defines its initial active-pixel
profile; extending either contract requires explicit bounds for
dimensions, border/blanking meaning, aspect ratio, pixel rate, frame-rate bounds
and clock crossings, including the capture/scaling ownership. It must also
define complete-frame delivery or repeat/drop behavior before a DDR consumer
can provide tear-free output. Additional source encodings, output modes and
DDR/filter consumers are not implemented by the raster contract proof.
