# FES Atari 520ST

`fes.atari-st` is FES's first 16-bit computer. The functional motherboard
uses a full 8 MHz 68000, 512 KiB RAM, a pluggable 192 KiB ROM, MFP timers and
interrupts, keyboard and relative mouse ACIA/IKBD, YM2149 audio and a writable floppy/DMA path.
An unmodified EmuTOS 1.4 image boots in host simulation. Board synthesis,
routing and hardware acceptance are separate qualifications.

The source-build owner is [misteross](../../docs/cores.md). Shared host media
semantics live in [mister-packages](../../../mister-packages/docs/computer-io.md).

## CPU, memory and reset

The shared [FX68K](../fes-common/rtl/fx68k/README.md) retains exact upstream
bytes and GPL-3.0-or-later licensing. Alternating fractional phase enables
produce an average 8.021247 MHz PAL CPU in the 52.224 MHz system domain.
`st_system` supplies this board rate to the CPU and derives PSG/ACIA rates
of 2,005,311/501,327 Hz. Standalone machine/I/O fixtures retain their
parameterized nominal defaults. CPU bus
transactions latch address, function code, write data and big-endian byte
strobes. A held request completes once; DTACK/BERR persists until AS or both
data strobes release. Rearming between strobes supports TAS. An unanswered
request faults after 128 CPU half cycles. The CPU RESET instruction resets
peripherals through the same exported reset signal as host Hold.
Palette-register reads and writes wait for a four-cycle shifter-bus window.
`st_system` supplies that window from native cycle phase 1; `st_machine`
retains the latched address/data/lanes until acceptance, then commits once.
A palette byte write copies the selected byte to both halves before masking
RGB to three bits per channel. Both byte addresses therefore replace the full
color; word writes keep their normal sixteen-bit input. Hatari's
[original-ST palette handler](https://github.com/hatari/hatari/blob/v2.5.0/src/video.c#L4619)
documents the bus alignment and byte mirroring. New raster traces retain raw
bus data/lanes and add the complete accepted `color`; analysis preserves the
older source-bound lane-merge traces when that field is absent.
This cycle label differs from Hatari's completed-access timestamp; absolute
raster alignment requires a separate comparison. The isolated motherboard
fixture has no GLUE counter and supplies an always-ready palette bus.

`st_memory.sv` shares the existing addon-SDRAM controller among CPU, scanout,
floppy DMA, upload and media reads. Round-robin arbitration bounds contention.
The MiSTer addon wires chip DQML/DQMH to A11/A12. The shared controller keeps
the full row during ACTIVATE, then places byte masks on those shared pins
before the column command and clears them for reads. The separate logical
DQM outputs alone cannot mask writes on this board.
The board selects phase-based grants from the native CPU/raster counter:
phase 0 serves CPU reads and other non-video clients, phase 1 CPU writes,
and phase 2 video. A grant that bypasses an earlier pending non-video client
preserves that client's queue position. Protected RAM transactions launch only
with valid byte strobes. Other motherboard targets retain registered dispatch.

One CPU write can be acknowledged after its address, data and masks are retained.
Physical completion and recovery precede subsequent clients. Request withdrawal,
changed inactive CPU pins and warm reset preserve an accepted write and suppress
stale completion. This is one ordered command, with no read cache. Hardware Hold
leaves the arbiter running for media/upload work. CPU RESET preserves RAM.

An empty video slot also completes the preceding CPU write's video obligation.
Later video demand retains normal slot eligibility without using that empty
opportunity to block a CPU write. This covers line-prefetch requests starting
after the previous line's fetches have ended, as well as continuous video traffic.

Runtime refresh selects four wait counts, leaving six chip clocks (114.9 ns at
52.224 MHz) before the next command, above the ISSI IS42S16320D's 60 ns period.
RAM and media use rank zero, so runtime refresh selects that rank; initialization
keeps the conservative two-rank sequence. The 335-clock schedule uses phase 2.
A pending refresh defers a late physical write that could occupy its admission
window. Pending slot refresh replaces the CPU write's owed video turn, including
refresh that becomes due after write admission. This avoids delaying the next
CPU write for a video slot already consumed by refresh; the next ordinary write
restores the video obligation. `raster_reset` opens the window while the native counter is held.
Slot refresh requires phase slots and single-rank refresh; posted writes require
phase slots and early completion. Board instances and Slang roots select the
same policy explicitly. Shared controller defaults retain RAM Tester behavior.

The ST consumes the first rate-0 DDR rising sample. The board retains its fabric
pad capture and the simulation fixture models the matching DDIO edge.
`make sim-fes-atari-st-memory` checks byte masks, physical posted-write commits
and following read order, cancellation, held requests, warm reset, all five
clients and every fabric offset of no-video read/write slots. A synthetic CPU
client also sustains 512 writes at four native CPU clocks per word with VIDEO
continuously pending, across all 336 refresh-counter offsets and three modeled
DQM delays. Physical payload, byte ordering and notifications are checked.
An additional sweep spaces VIDEO requests by one through sixteen CPU clocks,
checking 64-word CPU bursts across the same refresh offsets and DQM delays.
It retains the
exclusive 180-clock fairness limit and checks physical refresh intervals against
408 clocks at 52.224 MHz. Registered-input and late-completion profiles remain
focused timing comparisons.

`make sim-fes-atari-st-ram-bus` runs authored firmware on FX68K with physical
SDRAM/DDIO and independent-clock video. The selected policy measures 511 byte
write intervals of 16 CPU cycles and 510 read intervals of 16 plus one of 17.
It also executes 64 ten-register `MOVEM.L D0-D7/A3-A4,-(A7)` bursts, checking all
1,280 physical word writes and the 1,216 within-burst four-clock intervals.
The authored firmware delays those saves into active video; retained native line
numbers and physical VIDEO reads verify that bursts cross shifter line starts.
It checks both byte lanes, TAS rearming and RESET retention. An authored ROM-fed
palette loop alternates the CPU bus alignment with terminal DBF instructions
and checks fifteen 32-cycle intervals in each direction, exactly-once acceptance, both
palette byte addresses, mirroring before the RGB mask and readback. Its RAM-resident
NOP/DBF workload checks 12,299 palette intervals of 12 cycles and 300 of 512
while video fetches continue without underruns. These host regressions do not
establish original BIG compatibility, routed timing or kit acceptance.

Physical halfword offsets `$00000–$3FFFF` contain RAM; `$40000–$A67FF` contain
the separate disk buffer (up to 820 KiB). Masks select even high or odd low bytes.
The controller preserves its packed row/bank/column mapping.

The original ST MMU configures two logical banks. This machine has one
physical 512 KiB bank and an empty second bank. `$FF8001` changes logical bank
sizes and the CPU's multiplexed address mapping, including the row/column
aliases ROMs use to discover RAM. Empty-bank reads retain the last CPU-read
word, including instruction prefetch, and writes have no effect. The remainder of the original ST RAM decode window
below `$400000` also acknowledges accesses and discards writes, even beyond
the configured logical bank sizes. CPU byte strobes select the retained word's
even upper or odd lower lane. Writes and shifter/DMA activity do not replace
this CPU-read value, following the STF behavior documented in
[Hatari's memory implementation](https://github.com/hatari/hatari/blob/v2.5.0/src/cpu/memory.c).
The real-CPU MFP bus diagnostic also exercises empty-bank word/byte reads with
delayed prefetch responses and discarded empty writes. Addresses
from `$400000` upward remain unmapped until a separately decoded cartridge,
ROM or peripheral window. The populated bank follows the original ST wiring,
rather than the later STe mapping. In the normal `$04` configuration:

| Address | Behavior |
| --- | --- |
| `$000000–$000007` | Supervisor ROM-vector alias; writes fault |
| `$000008–$07FFFF` | RAM; user access below `$800` faults |
| `$080000–$3FFFFF` | Unpopulated RAM: reads acknowledge the retained CPU-read word, writes discarded |
| `$FA0000–$FBFFFF` | Read-only cartridge connector; empty reads acknowledge `$FFFF` |
| `$FC0000–$FEFFFF` | Exact 196,608-byte firmware image; writes fault |
| `$FF8001` | Logical bank configuration |
| `$FF8201/03` | Screen-base high/middle bytes, aligned to 256 bytes |
| `$FF8205/07/09` | Read-only functional scan counter |
| `$FF820A`, `$FF8260` | Sync mode and low/medium/high resolution |
| `$FF8240–$FF825F` | Sixteen palette words, masked to `$0777` |
| `$FF8604/06/09/0B/0D` | WD1772/DMA registers |
| `$FF8800/02` | YM2149 address/read and data-write registers |
| `$FFFA01–$FFFA2F` | MFP registers on the low byte lane |
| `$FFFC00/02`, `$FFFC04/06` | Keyboard and MIDI ACIAs on the high byte lane |
| Other `$FFxxxx` | Supervisor expansion MMIO |
| Other addresses | Bus error |

Primary references are Atari's [520ST service manual](https://www.atarimania.com/documents/atari_520st_service_manual.pdf),
Motorola's [68000 manual](https://www.nxp.com/docs/en/reference-manual/MC68000UM.pdf)
and the pinned [EmuTOS memory initialization](https://github.com/emutos/emutos/blob/978e37569bff95841e42675d11fcc6799aad8483/bios/memory.S).
Cycle-exact GLUE/MMU behavior beyond the selected RAM grant policy remains
outside this implementation.

## Peripherals

`st_mfp.sv` implements GPIP, edge selection, direction, interrupt enable,
pending, in-service, mask and vector registers, authentic priority and
software/automatic EOI, and timers A/B/C/D. MFP IRQ6 uses a supplied vector;
other IRQ levels use the CPU's VPA autovectors. The motherboard times IRQ6
acknowledgement from the CPU's AS/FC and data-strobe pins. A free-running
CPU/2 bus clock admits the request after at least 50 ns setup, then waits seven
half periods before releasing DTACK. This clock is separate from the
independent 2.4576 MHz timer crystal. Its timer clock enable gives EmuTOS's timer C `/64 × 192` exactly 200 Hz. Native
VBL/HBL and display enable share the CPU's phase-2 enable, independently of
HDMI. PAL uses 313 × 512 CPU cycles, NTSC 263 × 508, and monochrome
501 × 224. The fixed PAL board rate gives approximately
50.053/60.037/71.475 Hz. STF WS1 IRQ2 asserts at PAL cycle 508, NTSC
cycle 504 or monochrome cycle 220; the native line boundary follows four
CPU cycles later. IRQ4 asserts 60 CPU cycles after the native frame boundary.
The 60 Hz sync bit retains the PAL crystal; a
physical NTSC crystal policy remains separate. Timer B receives active display enable, including opened bottom
lines, so blank lines do not contribute events. The live shifter address counter advances by two bytes per four native CPU
cycles during DMA, including opened bottom lines. Its read visibility follows
Hatari 2.5.0's eight-cycle offset. Low/medium resolution reloads the aligned
live screen base at cycle 56 of PAL line 310 or NTSC line 260 during VSYNC.
The NTSC restart requires an NTSC vertical frame; a brief 60 Hz line in a
PAL frame retains its DMA address. A color line within a monochrome frame
also retains the counter until the frame-boundary reload.
The frame-boundary reload also samples the base; the counter holds its address
between DMA reads and reloads. Exact MMU arbitration and horizontal border tricks
remain unimplemented.
The MFP UART has disconnected RX and timed TX status, without a physical serial port.

`st_io` defaults to four native CPU wait states per MFP register access.
It delays the selected peripheral request, including writes and non-timer
read sampling, in native CPU cycles. Timer data registers return the count
captured when the raw CPU lower-byte strobe releases; they do not sample the
live counter when the delayed request is admitted. All four snapshots update
on that strobe independently of MFP selection, including RAM/ROM and other
device cycles. This follows Motorola's original
[MC68901 timer-register specification](https://ipv4.pccomponents.com/datasheets/MOTOR-MC68901FN.pdf)
(section 5.2.1) and Atari's
[C070231 schematic](https://wiki.retrotechcollection.com/images/5/5a/Atari_520ST_Schematic_C070231.pdf)
(sheet 1, U11 DS pin 47 connected to LDS). Held accesses retain their returned
data while the counters continue to run. Reset clears the snapshots, matching
the model's deterministic initialization of undefined timer data.
The focused MFP tests check all four snapshots, unselected strobe cycles,
counter expiration during held reads and consecutive event-counter polls.
The producer and simulator select that same wait default,
following
[Hatari 2.5.0's MFP accesses](https://github.com/hatari/hatari/blob/v2.5.0/src/mfp.c#L2468).
`make sim-fes-atari-st-mfp-bus` checks the real 68000's read/write instruction
timing across its fractional-clock phases and crosses a Timer B edge during
the delay while preserving the preceding strobe snapshot. It also checks
held reads, cancelled writes, reset, byte lanes and
PSG accesses. The real-CPU fixture uses the motherboard defaults. Physical
clock phase and exact-artifact hardware acceptance remain separate.

The motherboard selects four sound-bus waits at the first access of a
contiguous FF88 burst, including two/four-byte MOVEP transfers. Intervening
instruction/data accesses rearm the burst; no opcode or PC is inspected.
Writes acknowledge the accepted edge once, while reads retain their sampled
data through completion. Standalone peripheral fixtures retain parameterized
zero-wait defaults. This changes bus delivery timing, not the YM counter,
nonlinear mixer or analogue filter.

`st_acia.sv` models MC6850 registers, timed byte transport, IRQs and receive
errors. The keyboard ACIA connects to original `st_ikbd.sv` protocol logic:
reset acknowledgement, HID make/break, mouse commands, joystick events,
inquiries and BCD calendar. Its 64-byte response FIFO writes packets through
eight address banks, preserving atomic enqueue and simultaneous receive.
Complete HID rows settle for 1 ms before use so
keys and separately delivered modifiers form one snapshot. Controller port 0
maps to ST joystick 1 and controller port 1 to ST joystick 0. Mouse and
joystick commands select ownership of the shared ST port 0. Relative mouse input uses the optional `fes.mouse.relative` mailbox extension;
local USB/SDL/evdev and host browser input deliver packets through the same
IKBD/ACIA path. Button state survives source merging; movement is never
replayed after disconnection or an uncertain acknowledgement. Physical UART/MIDI pins,
HD6301 program loading, monitor sampling and accelerated cursor modes are
absent; unsupported command modes consume their parameters safely.

`st_ym2149.sv` reuses the shared AY engine with a YM2149 option: sixteen
registers, 32-step envelopes, a CPU/4 enable (2.005311 MHz on the PAL board) and 48 kHz PCM.
YM noise increments its counter on alternate /8 ticks, but checks its period
on every tick: period zero advances the generator each /8 tick; shrinking a
positive period can advance it on the inactive half. Default AY retains its
existing divider behavior and both modes retain their reset seed.
`make sim-fes-atari-st-ym-counter` compares gated channel levels before the
mixer/filter against unchanged Hatari 2.5 counter functions. It covers all
noise periods, all period transitions on both counter halves, the full LFSR
cycle and mixed tone/noise/envelope gates including quiet fixed volume 1. The test
declares an equivalent Galois/Fibonacci initial noise state; this is not a
musical phase fit or reset-phase qualification. Port A drives
floppy side and active-low drive selects. Fixed volumes 0/1 select envelope
levels 0/1; volumes 2..15 select odd levels 5..31. The ST wrapper converts
unquantized gated channel levels with the normalized Hatari STF nonlinear
three-channel table, including its order-dependent interpolation rounding.
`st_ym_mixer.sv` snapshots levels at each /8 engine counter tick, sorts them
for a 5,984-entry canonical ROM, and searches 1,159 sparse one-unit corrections.
The generated ROM uses fifteen explicit synchronous 512-word banks; FPGA
resource and timing qualification remain required. Its exhaustive simulation
compares all 32,768 ordered inputs against an independent primary-table oracle
and checks bounded completion and reset cancellation. The generator and unchanged
measurement source, provenance and license are retained in the component.
The nonlinear mix reaches at most 32,767. `st_ym_filter.sv` applies Hatari's
asymmetric STF C10 low-pass filter at every engine counter sample, averages all
samples over each 48 kHz interval and removes DC with the signed 32-bit IIR
state. Averaging uses the exact rational chip/output clock ratio, retaining the
fractional counter rate instead of Hatari's truncated Q16 interval. Two
registered products and a bounded restoring divider avoid a combinational
divide. A four-entry register FIFO supplies the regular PCM cadence, delayed
320 fabric edges after reset so each interval is complete before consumption.
This is the filter selected by Hatari's audio initialization for ST output at
40 kHz and above, including the production 48 kHz cadence. Both rising and
falling edges retain input/output history; the lower-rate PWM path is different.
The production filter finishes within 39 fabric edges; the mixer plus filter
finish before the next roughly 208-edge counter period. Reset clears filters,
queued samples and in-flight work. Standalone tests cover both production and
scaled clocks against an independent streaming oracle, including impulses,
high-frequency signals, DC decay, reset and FIFO faults. Whole-demo waveform
and exact FPGA/hardware qualification remain required. Board transport uses the
existing signed stereo PCM/I2S path, duplicating mono into both channels.

`st_floppy.sv` implements original WD1772 Type I positioning, Type II sector
reads and writes, force interrupts and ST DMA. Drive A accepts raw `.st`
images with 80–82 tracks, one or two sides, and nine or ten 512-byte sectors
per track. The required `fes.media.atari-st-floppy-geometry` 1.0 extension
opts into these twelve uniquely sized shapes; the base interface retains its
exact 737,280-byte contract for older packages. Committed upload length fixes
physical geometry even if the guest changes its boot BPB. Drive B is absent. Requests stay stable under storage/DMA stalls; DMA stays inside RAM.
The writable extension clears write protect. A complete sector is staged from
RAM before publication to the disk buffer; once publication starts it drains
through warm reset or force interrupt. Begin and Eject reject while collection
or an accepted sector commit is busy, including volatile disks; an explicit
later replacement cannot overlap the old image's writes. Freeze fences new writers and drains
accepted work before whole-image capture. Raw development loads stay volatile;
library loads explicitly bind durable data to the game and immutable base disk.
The runtime publishes a checksummed full image through a synced atomic rename,
then authorizes eject/replacement. A save failure retains the same frozen owner.
Motor/index behavior is functional; flux, CRC/deleted-sector metadata, formatting,
read-address/track, PIO streams and ACSI are absent.

## Pluggable ROM, expansion and video

The firmware socket accepts exactly 192 KiB. Mutually exclusive lane decodes
and a byte reduction select the 192 synchronous RAM outputs. This preserves the five-clock word-read latency,
held-request behavior and named ROM lanes used by the linker. The hardware ROM
simulation checks every lane and undefined bank, reset cancellation and changed
addresses during a held request. The board ROM is a synchronous
M10K implementation whose blank base can be linked using the existing
format-3 ROM map; no proprietary Atari ROM is distributed. EmuTOS is a
separate, freely available GPL test input.

The expansion connector exports a stable 23-bit word address, 16-bit data,
byte lanes, direction, function code, reset, phase enables and IACK level.
Responses contain read data, ACK/BERR, encoded IRQ and card presence.
`st_expansion_socket.sv` registers both boundaries in the reserved
`fes.atari-st-bus.socket/1` rectangle. The shared linker confines slot 1 to
that rectangle. The optional manifest interface is
`fes.expansion.atari-st-bus` 1.0; it grants no GP capability bit.
`expansions/st_probe.sv` exercises cartridge data, MMIO, wait states, faults
and byte lanes. `expansions/probe_cart.sv` wraps it in the registered connector.
`scripts/build_atari_st_slot_card.py` synthesizes the card onto the frozen
shell, checks all clock pins and timing, and rejects any change outside the
socket CRAM rectangle before publishing an exact-shell expansion archive.

`st_video.sv` reads interleaved RAM bitplanes: 320×200 four-plane low,
640×200 two-plane medium and 640×400 monochrome. The fixed 720p60 output uses
integer nearest-neighbor scaling: native low 2×2, indexed medium 2×3,
monochrome 2×1. The focused indexed low fallback retains 4×3. High mode
supports palette-bit inversion; color borders use palette entry 0.
`st_video_adapter.sv` defaults to native low-resolution capture. In the
52.224 MHz system domain, `st_native_low_video.sv` prefetches alternating
RAM row windows. Row zero fetches 80 words; subsequent rows fetch 81,
including a one-word leading margin. That margin lets a row prefetched before
a late frequency switch use either the ordinary or shortened DMA address.
The shared memory port reports retired physical RAM writes with their retained
address, data and byte lanes, including posted writes whose CPU acknowledgement
occurred earlier. The capture updates prefetched words until their individual modeled MMU loads:
word zero six CPU cycles after DE, then one word every four cycles. Each word
retains its loaded value throughout the group's sixteen pixels; later planes
can still receive writes after an earlier plane has loaded. The missing final
plane of a shortened row is decided after its DMA deadline, so a late frequency
switch resolves all sixteen affected pixels to palette entry 0 even when
their coordinates were already prepared. Three final loads cannot reload
the shifter's four-plane group. The row's leading
margin is retained through late shortened-line counter changes. Completed
RGB banks remain immutable.
This model follows the ordinary ST-compatibility raster and MMU with the
[decap-based shifter](https://github.com/MiSTer-devel/AtariST_MiSTer/blob/426ef8765fc16cb547e7ff5deb7d9be61003b36d/rtl/gstmcu/shifter_video.v)
at reference revision `426ef8765fc16cb547e7ff5deb7d9be61003b36d`.
The reference MMU originates in STe schematics; physical STF RAM sampling,
CPU-slot alignment, and mode-switch fetch edges remain separately unqualified.
`make sim-fes-atari-st-cache-write-coherence` checks whole-word writes, both
byte lanes, future and consumed words, and writes between plane loads. Its
32-case grid checks all four planes on both sides of every group-load boundary
through the production adapter.
Memory fixtures pair each notification with
the actual physical WRITE command, including warm-reset drain and posted writes.
Canvas coordinates pass through 28 actual CPU-derived native pulses. Bitplane
indices resolve at pulse 20, after all four word loads, within that same
pipeline. The sampled token remains until the next raw CPU phase-2 edge closes
palette admission. Actual phase-1 writes can
commit four or five fabric edges after the native pulse; lookup sees those
committed colours without advancing the token's coordinates or index.
Border palette-zero runs use that same delayed coordinate
and lookup timeline. This is a calibrated net-output candidate derived from
Hatari v2.5.0 Spec512's STF-tested write placement; it does not establish one
literal 28-cycle hardware Shifter delay. Raw DE, MMU counters, CPU bus phases and
Timer B remain unchanged. Reset and Hold cancel pending tokens; complete-frame
publication waits for pending output tokens and the final RGB RAM write
to drain. `make sim-fes-atari-st-shifter-output` independently checks completed
palette-access timestamps 80/84/88 against first changed active pixels -3/1/5
in PAL and 1/5/9 in NTSC. It uses the actual 512/508-cycle line lengths,
56/52-cycle DE starts, fixed bitplane markers, side/bottom borders, line wrap
and publication/reset/Hold for both profiles. Pulse-edge, following-edge and
actual late phase-1 commits all use the same independent pixel-coordinate oracle. The independent-clock adapter
fixture also uses true NTSC 263-line/508-cycle frames and DE cycles 52..371
while retaining PAL, resolution transitions and bank ownership checks.
Its registered accepted-write source isolates the output model; real-CPU palette
admission, RAM 12/512 intervals and Timer B use their existing fixtures.
Three RGB333 banks hold up to 320×247 pixels. Each bank uses 78 independent
1024-word chunks, avoiding the mapper's less compact deep one-bit layout.
Each bank's five groups of sixteen chunks register their own hit and chunk
index with the RAM read, then their selected word; a miss clears the word, so
the output is the OR of the fifteen group words. Address and bank tags stay
aligned with the two-edge read; invalid chunks return zero. Frame publication and pixel capacity are unchanged.
Fresh resource and routed timing qualification remain required.
The banks cross to the 74.25 MHz pixel domain through
publish/release toggles. Only complete native frames are published; the HDMI
reader pins one bank until output SOF and selects the newest available frame.
This repeats or drops complete source frames when native and output rates differ.
Registered RAM write data and a separate three-pixel RGB address forecast keep
native sampling and scaled output aligned. Border replay retains its two-pixel
forecast, preserving fixed output geometry and the existing socket boundary.
Ordinary and opened-bottom frames use the same centred 2×2 canvas, preserving
square native pixels and image position. All 247 PAL or 226 NTSC rows remain visible.
Height travels with each immutable bank and changes only at output SOF.
`st_native_border.sv` records lossless palette-zero runs across the visible
border raster in those same three owned banks. Every row has an initial colour
and run offset/count, so repeated HDMI rows replay within-line changes. The
8192-event budget accommodates BIG psycho screen 3's dense palette stream;
the bound remains explicit: overflow increments capture underruns and
rejects the entire frame. HOLD and consumer backpressure retain the same bank
ownership rules for border runs and display pixels. Every low-resolution frame
uses a centred 416×276 PAL or 416×255 NTSC canvas at 2×2 scaling, with black
outside it. Border changes do not alter scaling or image position. The
canvas includes the 29 top-border rows and 48 pixels at each side of the
ordinary display. These are live palette-zero borders; they do not add top or
horizontal display-enable opening.
A blanking-time row counter avoids a vertical-coordinate multiply on the read path.
Border replay prefetches the registered successor while holding the current event
through output pauses. Only the held event is compared with the output position,
so no event RAM output reaches the consume decision; the edge after SOF, line
start or a consume loads the successor. The 2×2 canvas holds each native pixel
for two output edges and starts rows in blanking, so every event is still
consumed on its original pixel edge. Row descriptors take one more pixel edge
into fabric registers; SOF and line start load the original event-zero/row
descriptor addresses. The event RAM address does not depend on the colour comparison.
The surrounding capture pins published banks until SOF, and SOF runs in blanking.
RGB and black-line validity have separate write registers. Three-bit publication
numbers order the bounded pending bank set across wrap and consumer pauses.
A missing RAM row displays black for its whole line; HOLD aborts an unfinished
capture, and an invalid base never wraps into populated RAM.

The [native palette qualification record](../../../../docs/validation/2026-10-08-atari-st-native-palette.md)
binds the sealed shell/Direct/Scanlines pair and bounded Kit A test. The B
scroller shows rainbow bands through the prepared Direct part; menu flashing
and exact native timing remain open. The original image and populated menu
are restored after the diagnostic.

Medium and monochrome retain the indexed double-line-cache renderer and its
coherent configuration bundle, activated at output frame boundaries. Their
palette remains fixed for that output frame. Synchronous cache reads and
ownership tags meet the original plane-capture edges. A held, fair arbiter
shares the existing video-memory port between the two renderers.
The indexed renderer compares constant current/next-line windows before mode
selection, preserving its pixel and lookup cycles while shortening timing paths.

The reduced native timing uses ordinary PAL lines 63–262 with DE cycles
56–375, and NTSC lines 34–233 with DE cycles 52–371, following the ordinary
[Hatari 2.5.0 timing table](https://github.com/hatari/hatari/blob/v2.5.0/src/video.c).
VBL's frame/capture pulse remains at the native frame boundary. IRQ4 asserts
60 CPU cycles later, matching the same STF WS1 wake-up timing selected for
line and bottom-stop samples. It remains pending until IACK; reset cancels a
scheduled event. This is independent of HDMI frame publication.

GLUE vertical blanking is a separate level. At STF WS1 cycle 502, live
frequency selects blank-off after PAL line 24 or NTSC line 15, and blank-on
after PAL line 307 or NTSC line 257. The change takes effect at the next
HBL. A second frequency switch can suppress the normal bottom check; VSYNC
still forces blanking at line 310 or 260. Blanking does not stop DMA, the
video counter or Timer B. Native low-resolution tokens carry the blank bit
through the same delay as their coordinates, and both active RGB and border
runs resolve to black regardless of palette zero. Complete opened-bottom
captures still contain 247 PAL or 226 NTSC rows.

The short-line fixture checks black output and immutable border replay with
nonblack palette zero; the I/O fixture checks both frequencies, suppressed
blank-on samples, VSYNC forcing and uninterrupted DMA/Timer B. Hatari's
`Video_CopyScreenLine` instead clears blanked planar data to index zero and
documents physical black output as a renderer TODO. This known reference
limitation must remain distinct from physical output acceptance.

The MFP's Timer B input follows both DE edges by 24 CPU cycles, using
[Hatari's Timer B offset](https://github.com/hatari/hatari/blob/v2.5.0/src/includes/video.h).
Color DE selects its porch at cycle 50, before either color profile can
enable display. A PAL-to-60 Hz switch during cycles 56–371 shortens the
current row to 79 DMA words if it remains selected at cycle 372. Returning
to PAL before that decision cancels the shortening; returning later cannot
reopen DE. The video counter omits the final word, and native capture carries
the cumulative word displacement into following rows. The absent fourth load
prevents reloading the final sixteen-pixel group, which resolves to palette
entry 0. The last four output positions also have DE disabled.
`make sim-fes-atari-st-short-line-video` checks every captured pixel,
immutable replay, two cumulative short rows and prefetch completed before
the late switch, with shortening decisions at cycles 300 and 372.
The I/O fixture separately checks early cancellation, late
restore, counter movement and next-line porch selection.
Color line length samples sync separately
at STF WS1 cycle 54: a bottom-stop pulse restored early on the following
line preserves the ordinary HBL and frame period. The vertical bottom-stop condition also
samples live sync at cycle 502 on the last ordinary color line, using the
STF WS1 timing table. Opposite sync extends PAL DE through line 309, or NTSC
DE through line 259, and clears at the next frame. This lets Timer B handlers
continue through the opened bottom region. The native RGB buffer includes
those extra lines. Publication waits until all potentially opened rows finish;
ordinary captures publish 200 rows. Only the first possible border row is
prefetched before DE confirms opening. The remaining mode samples are approximate, and prefetching
does not reproduce exact MMU/shifter arbitration, mid-line base writes,
horizontal or top-border opening.

Video uses the existing [RGB888 part contract](../../../mister-packages/docs/video-parts.md)
and shared direct/scanline implementations through two registered boundaries.
`st_video_socket.sv` provides the optional frozen raster socket
`fes.atari-st-video.socket/1`. The physical producer layout is
`fes.atari-st-video.parts/2`; the public Go/C++ composition layout remains
`fes.atari-st-video.parts/1`.
An empty socket uses the built-in Direct output; independently sealed Direct
and Scanlines archives bind the exact shell package and may coexist with the
CPU expansion and linked firmware. The socket owns 93 pinned boundary/clock
FFs with paired route-through buffers in LAB24..28 rows41..58. Its strict
half-open CRAM rectangle is (1769,3442)–(2806,5162). Full M10K configuration
footprints remain outside both CPU/video fences; RAM guards include rows40/59.
The part producer requires the frozen shell's manifest, RBF and ROM map to
match the sealed package. `routed.json` and `socket.qsf` are not package
members; the part recipe digest covers their bytes. It checks all three shell
clocks, pixel-only part state and every outside CRAM bit, including companion
columns. Rebuild parts after any shell identity change. HOLD blacks pixels while
timing continues. Horizontal/top display-enable opening, cycle-exact raster
timing and aspect-ratio correction remain absent.

## Validation

```sh
make -C sources/misteross sim-fes-atari-st
make -C sources/misteross sim-fes-atari-st-media-lifecycle
make -C sources/misteross fetch-fes-atari-st-emutos
make -C sources/misteross sim-fes-atari-st-emutos \
  EMUTOS_ROM=build/roms/emutos-1.4/etos192us.img
make -C sources/misteross sim-fes-atari-st-emutos-memory \
  EMUTOS_ROM=build/roms/emutos-1.4/etos192us.img
python3 scripts/sim_atari_st_disk_diagnostic.py \
  --rom sources/misteross/build/roms/emutos-1.4/etos192us.img \
  --output out/validation/atari-st/disk-guest --seconds 8
python3 scripts/atari_st_io_diagnostic.py --output out/validation/atari-st/io-media
python3 scripts/sim_atari_st_io_diagnostic.py \
  --rom sources/misteross/build/roms/emutos-1.4/etos192us.img \
  --output out/validation/atari-st/io-guest --seconds 10 --audio-phase-ticks 20
```

`make build-fes-atari-st` authenticates `cores/fes-atari-st/toolchain.lock`. The
factory recipe stays HIP. `--gpu-router CUDA` builds a separate package.
The [2026-10-04 private-pin record](../../../../docs/validation/2026-10-04-atari-st-nextpnr-head.md)
qualifies the earlier read-only shell and does not qualify this shared-pin
seal. The October kit notes do not accept these bytes. Video parts and slot
cards take the same `--gpu-router` and `--toolchain-root` as the shell.

```sh
make -C sources/misteross build-fes-atari-st CACHE_ROOT=/absolute/toolchain-cache
make -C sources/misteross sim-fes-atari-st-video-parts
make -C sources/misteross build-fes-atari-st-video-part \
  ST_SHELL=/absolute/frozen/build/fes-atari-st-oss \
  ST_PACKAGE=/absolute/sealed/package ST_VIDEO_PART=direct \
  CACHE_ROOT=/absolute/toolchain-cache
make -C sources/misteross build-fes-atari-st-card \
  ST_SHELL=/absolute/frozen/build/fes-atari-st-oss \
  ST_PACKAGE=/absolute/sealed/package CACHE_ROOT=/absolute/toolchain-cache
```

The original `AUTO/IOTEST.PRG` diagnostic checks exact keyboard make/break,
modifier aliases and both joystick ports through the guest ACIA, then repeats
three-second YM channel A/B/C tone, noise, envelope and silence phases. Its
generated `schedule.json` contains the normal host input events and expected
guest bytes. On hardware, wait for each visible WAIT banner before sending its
group through the attached session input API; require PASS before continuing.
Launch the generated `io-auto.st` using normal library Play and Stop normally
after measurement. This tests host-delivered input; it does not qualify a
physical USB keyboard or controller. The host simulation executes stock
EmuTOS and the real CPU/peripheral RTL with bounded RAM/disk callbacks. Its
optional shorter audio holds have separately identified PRG/disk hashes; they
are not the hardware fixture, physical SDRAM or HDMI acceptance.

Before importing a demo, inspect an independently acquired classic MSA image:

```sh
python3 scripts/atari_st_demo_media.py /absolute/demo.msa --raw-output /absolute/demo.st
```

This offline tool checks every compressed track and reports original geometry
and the decoded raw-image hash. Explicit raw output preserves every decoded
byte without padding. Original BIG (80×1×10) and Cuddly (82×2×10) disks fit
the geometry extension. Admission and sector correctness do not qualify their
loaders or raster/border effects.


`scripts/sim_atari_st_demo.py` runs an original raw disk with independently
pinned 192 KiB firmware on the actual FX68K/chipset model. Its default ROM
digest selects stock EmuTOS; an explicit `--rom-sha256` admits an independently
identified original TOS image without changing its bytes. It freezes source,
ROM, disk and generated-model identities before execution. The diagnostic
accepts 1..120 guest seconds (default 15), allowing several sampled
tracks and the main-menu disk reload in one continuous run. The host deadline
remains independent and must allow the longer simulation to finish. The fault
log records the first 64 bus errors with the latched address, direction and
function code; the first eight faults also capture RAM. The exported CPU PC
is diagnostic prefetch/exception state, not an instruction-retirement trace.
The diagnostic also records changed palette entries by native frame/line
after six simulated seconds (first eight changed bundles per frame and full
per-frame counts). Horizontal positions are CPU cycles within the native line.
It saves the first sixteen complete native RGB captures after 6.5 seconds,
and records IACK/MMIO request starts and sync changes between six and seven seconds.
These bounded traces locate guest handlers without an instruction-retirement
claim. `--trace-start` / `--trace-end` select a bounded interval within the run;
that interval records every committed palette write, including writes of the
same value, and every completed native capture's static-logo crop and row
hashes. Shared-memory demo runs also save 512 KiB physical-RAM snapshots
at per-second status events throughout the bounded run. `demo-ram-snapshots.jsonl`
records each snapshot's elapsed system cycle; the corresponding file is
`demo-ram-cycle-{system_cycle}.bin`. Copying RAM advances no guest clocks and
changes no input or memory state. Optional shared-memory `--ram-write-range START:END`
records accepted physical SDRAM WRITE commands during `--trace-start` / `--trace-end`.
Repeat up to 16 nonoverlapping word-aligned byte ranges within 512 KiB, using
decimal or hex addresses (for example `0x380:0x392`). `demo-ram-writes.jsonl`
records elapsed system and physical-model cycles, byte address, driven data and
actual byte masks, including unchanged values and DMA writes. Its PC is the
current diagnostic prefetch state, not instruction retirement or writer identity.
The range file and emitted trace are source-bound in the proof. The observer
changes no commands, memory, inputs or guest clocks. These observations allow sample pointers and
data to be compared without inferring their state from the audio waveform.
The first eight distinct crop hashes save their complete native
RGB images, so a rare outlier can be inspected after the run. The logo crop is native x=65..244, y=0..63. FNV-1a-64 hashes compare
expanded RGB bytes for equality; they are not cryptographic artifact identities.
Per-capture counters report video-prefetch and CPU RAM latency. Optional
`--ram-extra-wait 0..64` adds system-clock delay to each CPU RAM callback;
this probes sensitivity and does not model shared SDRAM arbitration.
`--ram-fixed-wait 0..64` instead replaces the variable CPU RAM callback delay
with a fixed system-clock delay, before any extra wait. Zero provides an
ideal-storage timing comparison; it does not change the physical controller
or establish that real SDRAM can meet that timing. Shared-memory mode rejects
callback overrides, so its reported timing always comes from the actual arbiter.
The simulator defaults to the motherboard's clocked AS/FC IRQ acknowledgement
and four-state sound-bus policy. `--mfp-iack-phase -1 --no-mfp-iack-raw-pin`
selects an immediate-ACK diagnostic, while `--psg-wait-states 0` bypasses the
sound-bus wait. These overrides change the recorded compiled model.

`--mfp-wait-states 0..8` explicitly overrides the standard four-state MFP access policy;
its selected value is recorded in the source-bound result. Run
`python3 scripts/analyse_atari_st_raster_trace.py OUTPUT --output SUMMARY.json`
from the FES root after completion to check artifact digests and summarize
logo equality, connected-bit palette repeats and interrupt positions. `--shared-memory` instead uses the existing `st_boot_memory_sim_top` fixture:
the original disk is preloaded into the separate SDRAM buffer before CPU release,
and the real memory arbiter, addon controller, DDIO digital model, floppy DMA,
native capture and independent-clock 720p renderer execute together. It does not
exercise mailbox upload, electrical pad timing or a physical FPGA. This mode
requires at least six seconds; callback RAM delays are rejected. The original diskless EmuTOS
invocation keeps its boot assertions and supplies no media. A changing
logo is reported as noncanonical, without automatically declaring a visual fault. `--key-b-at SECOND` selects the scroller through the normal
HID/IKBD path with a 150 ms B press. Both storage models accept
`--key-at SECOND --key-usage USAGE` for other screens (1/2/3 use HID
usages 30/31/32). For several actions, repeat `--key-event SECOND:HID[:HOLD_MS]`
in chronological order, for example `--key-event 7:81 --key-event 7.2:40
--key-event 8:30 --key-event 10:44` for Down, Return, 1, then Space.
Up is HID 82. These are ordinary keyboard presses; the fixture does not alter
ROM or disk bytes or bypass the guest's menu. Up to 256 events are accepted;
holds default to 150 ms, must be positive, and must finish before the run ends.
Allow at least 2 ms with all keys released between events, including repeated
presses of the same key. Very short holds may not reach the IKBD's sampling path.
Decimal timestamps round upward to the first 52,224,000 Hz system clock.
Time zero is allowed: the callback fixture first applies it after its 64 reset
clocks, while shared-memory timestamps begin when SDRAM initialization releases
CPU reset. Existing single-key flags retain their timing and cannot be combined
with `--key-event`. The source archive includes the common stimulus header;
`keyboard-events.tsv` and its SHA-256 plus exact requested cycle schedule appear
in the proof. `demo-input.jsonl` records actual keyboard row transitions relative
to the respective anchor; it does not establish guest acknowledgement or demo
compatibility. Every run saves `demo-audio.wav`, the system-domain signed
mono 48 kHz chip PCM, and `demo-audio.json` with sample counts, amplitude
statistics and system-clock cadence. Shared-memory runs also save `demo-rendered.jsonl`: each complete HDMI
frame whose SOF falls in the trace window records its immutable pixel-domain
native publication sequence, PAL/NTSC geometry and active height. The first
eight distinct valid displayed native sequences save actual 1280×720 HDMI
PPMs. The PAL canvas is xywh 224,84,832,552 (NTSC 224,105,832,510). A reduced
416×276/255 canvas is saved only when all four displayed RGB samples in every
2×2 block agree. These images observe border replay as well as active pixels;
pre-replay `-trace-logo-*-native.ppm` images cover only the active capture.
All saved images and the rendered trace are included in the helper's artifact
digests. Displayed native identity is asserted stable through each active
HDMI frame. This is digital output evidence, not a physical GLUE/HDMI oracle.
Shared-memory runs also save
`demo-ym.jsonl`: every accepted valid-register PSG data write, with its raw
value, selected register, system-cycle timestamp and diagnostic PC. The trace
covers the entire guest run independently of the raster trace interval. It
observes the PSG acceptance edge without changing its clock or audio model.
The PC is exception-unit state, not an instruction-retirement assertion.
Quiet boot audio is valid; these files
do not establish analog sound fidelity. `--timeout-seconds` controls only
the host deadline, independently of the guest duration. These are observations of the reduced
native timing model, not original GLUE/shifter timing equivalence. Per-second
static framebuffer images do not prove video timing or border behavior. Neither successful capture completion nor removal of one loader
fault establishes demo compatibility.

The aggregate uses original diagnostic firmware and focused CPU, video,
MFP, keyboard/audio, floppy, physical SDRAM, dual-clock cache and real GP
upload, writable sector/snapshot, mouse handshake and media arbitration tests. The CPU diagnostic covers ROM replacement, supervisor
protection, MMU aliases, big-endian byte/word/long access, TAS, exceptions,
expansion waits/errors and CPU-written pixels. The memory test checks real
SDRAM commands, CAS-2 capture, refresh, byte masks, fairness and warm reset.
Write masks are asserted during row setup two fabric clocks before WRITE; the
model checks zero, one and two clocks of added DQM delay and its two-clock read
latency.
The media test uploads a complete disk with odd chunk boundaries and checks
CRC, execution-independent insert/eject and completion before acknowledgement.

The optional EmuTOS test checks stock-ROM RAM discovery, screen setup and
running system/VBL timers. The memory boot test concurrently uses the physical
SDRAM command model and the dual-clock video adapter, checks bounded access
latency and zero underruns, and captures RGB after both video-part boundaries.
`scripts/fetch_atari_st_emutos.py` pins both the
official 1.4 archive and exact US image by SHA-256 and retains its license.
It does not put downloaded ROM bytes in Git or seal them into the blank ROM.
Tests copy pinned CPU microcode to a private working directory. FX68K needs
its documented simulator warning suppressions; the focused device, memory
and video tests use strict `-Wall`.

These are host checks. They establish neither SDRAM electrical timing nor
exact-artifact hardware acceptance. A board build must pass authenticated
OSS synthesis, routing, timing and ROM/socket checks before it can publish a
package. STE blitter, enhanced palette and DMA sound are outside the original
520ST scope.

`make sim-fes-atari-st-psg-wait` checks the four-access sound-bus burst with
held fourth writes and reads, ignored lower-byte accesses, exactly-once write
admission and a fresh wait on the fifth transaction. A completed request keeps
its acknowledgement until withdrawal.

`make sim-fes-atari-st-mfp-iack-timing` checks setup below and above the
50 ns threshold, seven-half-period completion, held acknowledgement and
IACK/data-strobe/reset cancellation. This checks the bus handshake separately
from original-demo raster and audio phase comparisons.
