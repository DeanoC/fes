# Menu framebuffer display

The `fes.menu` described package is runtime-presented on the designated kit:
the runtime configures its shared-DDR scanout, accepts sealed RGBA frames and
restores the menu after a game stops. That path copies each frame into one
of the two DDR slots and submits it through menu-display GP. It is a
diagnostic runtime path with host tests and exact-artifact kit evidence, not
factory-image acceptance. The sealed splash remains the product idle
fallback until a separate acceptance gate. The
earlier framebuffer and DDR-free test-pattern wrappers remain separate
diagnostics for the reader, FIFO, timing and HDMI board path. None is a game.

Run from misteross:

```sh
python3 scripts/sim_fes_menu.py
```

The read-only 128-bit Avalon reader fetches exactly 921,600 XRGB8888 pixels
from one of two 4 MiB slots. It bounds each burst to its frame, reserves FIFO
space for every response and keeps one burst outstanding. Cancellation drains
issued reads and discards their data before the reader reports idle. `rst`
here requests that drain; it must not destroy the outstanding-transfer count.

`WINDOW_BASE` is supplied from the generated shared HPS DDR contract by
the DDR diagnostic wrapper. Its default zero disables starts. Simulation supplies the proposed
window solely to verify addresses. The reader does not allocate Linux memory
or expose an arbitrary read address. Shared DDR RTL, boot port layout and
runtime admission are owned by the merged HPS DDR support.

The video controller uses fixed 1650x750 timing at the intended 74.25 MHz
pixel clock. Initial enable waits for the first row of vertical blank before
arming prefetch; host scheduling during active video cannot create a startup
underflow. Once armed, missing pixels still count as underflows. It switches a pending slot only
at a frame boundary, and acknowledges its sequence only once its initial
pixels are available. One submission may be pending. A frame that cannot
start safely is black and its submission remains pending for a later frame.
During an active frame, missing pixels are black and counted; late pixels
are discarded by raster index rather than shifting following pixels.

The video simulation checks complete frame timing and pixel data, blocked
submissions, deliberate underflow, delayed-data recovery, reset/drain, pending
switch cancellation and counter saturation. It writes `slot0.ppm` and
`slot1.ppm` under `build/sim/fes-menu-video/` for inspection. These modeled
results do not establish kit DDR bandwidth or routed clock closure.

Build the standalone diagnostic with authenticated, pinned tools (GPU 0):

```sh
make build-fes-menu-pattern FES_TOOLCHAIN_CACHE_ROOT=/absolute/cache/path
```

The output is `build/oss/fes-menu-pattern/core.rbf`; its closed input record and
build summary are retained beside it. The source must be committed and clean.
Use the existing kit lease and development-RBF load path; this diagnostic does
not replace the appliance boot or idle image.

The local pattern memory responds to real reader bursts without instantiating
DDR or GP hardware. Eight color bars, a one-pixel white border and a lower
checkerboard check geometry and color order. The upper-left marker alternates
green/magenta every 60 frames through normal sequence submissions. Any reader
underflow turns white pixels red. The pattern mode ignores external memory
inputs and emits no external memory commands.

`sim_fes_menu.py --case pattern` verifies full frames, switching and quiesce.
`--case board` verifies PLL-unlock output gating and automatic switching across
120 frames. These digital models do not validate the analog PLL, HDMI link,
DDR bandwidth, host framebuffer uploads or runtime presentation behavior.

The FIFO uses an unconditional synchronous M10K look-ahead with recent-write
forwarding. An earlier asynchronous mapping passed RTL simulation and timing
but produced corrupted vertical bands on the kit; the synchronous mapping
produced clean captures. The diagnostic producer rejects asynchronous M10Ks.
The layer responsible for the earlier hardware corruption remains under
investigation.

The contained raw development load deliberately powers HDMI down during
replacement and does not initialize video afterward. The recorded kit check
restored only the retained fixed-720p transmitter power register under its
lease before capture. This is a diagnostic procedure, separate from the
described-package runtime presentation path. Stop restored idle and the
session released its lease.

Build the read-only DDR diagnostic separately:

```sh
make build-fes-menu-ddr FES_TOOLCHAIN_CACHE_ROOT=/absolute/cache/path
python3 scripts/sim_fes_menu.py --case ddr
```

Its output is `build/oss/fes-menu/core.rbf`. It selects the qualified DDR
Yosys/nextpnr pins from `cores/fes-menu/toolchain.lock`, connects shared port 0 at
74.25 MHz, supplies the generated core-window base and reads slot 0 only.
The synthesized enable is fixed on after PLL lock and recorded in the
diagnostic contract. There is no GP identity or framebuffer upload API.

The shared guard holds the port during startup. A live hold must follow
completed local quiesce/drain. An unexpected hold while traffic remains
latches a fault, keeps DDR held and requires reprogramming; it cannot resume
a reader whose responses were hidden by the guard. This diagnostic does not
claim automatic recovery or runtime admission. The DDR simulation uses the
real shared wrapper and guards with a modeled hard block; it checks complete
frames, slots, disabled commands, reserved-window bounds, response stalls,
ordered hold/restart and unexpected-hold containment.

The producer requires matching DDR layout constants in both synthesized and
routed graphs and rejects all writes and unused-port commands. Building does
not release physical DDR ports. Raw contained loading is insufficient for
DDR use; the described-menu/runtime integration performs admission,
port release and video setup through the runtime. The diagnostic alone is not
hardware-qualified by the earlier test-pattern captures.

The shared wrapper's compile-time specialization disables port-0 writes and
ports 1/2 at the hard-block command pins for the diagnostic; the described
package enables port 1 and port-2 reads for the blitter and keeps port-2
writes disabled. Its default read/write behavior and
boot layout remain unchanged for the RAM tester and other consumers. The
producer recognizes routed constant-zero drivers as well as synthesized zeros.
The DDR recipe defaults to seed 4; `--seed 1..8` selects a bounded alternate
and records it in the closed build inputs. Failed routes publish no RBF.

## Described menu firmware

`make build-fes-menu-package` builds separate format-2 `fes.menu` 1.0.0
firmware under `build/oss/fes-menu-package`, exporting the closed package to
`build/packages`. It requires application fixed video, HPS DDR, menu-display
1.0 and menu-blitter 1.1. It has no playable system identity and does not
change factory selection.
The qualified RAM-test lock now includes the congestion fix from FES #266.
GPU 0 and bounded explicit seeds retain the normal provenance/evidence gates.
The described package tries seed 5 first, then 1, 2, 3, 4, 6, 7, 8, and keeps
the first passing route. The DDR diagnostic above retains seed 4.

`menu_top.v` uses the shared application GP mailbox with an optional menu
hook; all existing consumers leave it disabled. `fes_menu_control` implements
fixed-layout configuration, coherent counters, ordered sequence staging and
frame-boundary completion. Quiesce waits for the reader to drain before ACK;
execution hold during enabled scanout rejects without abandoning responses.
The GP and reader use the same pixel clock.

`fes_menu_blitter` runs `fes.video.menu-blitter` command lists into a menu
slot (`sources/mister-packages/docs/application-io.md`). MenuBlit goes to
`fes_menu_blit_control`; the blitter reads destinations and writes results on
port 1, so a blend reads what earlier commands wrote, and reads commands and
sources on port 2. Each row is read, then goes through a three-clock alpha
stage (EXPAND's `A·m/255`), a four-clock blend and a sixteen-beat FIFO into
the write burst: 2 pixels a clock for fills, 1 for the other ops. Every op
takes the same pipeline, so beats stay in order. EXPAND's coverage can start
at any byte, so a second copy of the source row buffer reads the beat before.
The menu builds with DSPs: the blend's and the alpha stage's unsigned 8x8
products each map to one 9x9 multiplier, eight in all. Their operands are
zero-extended, so the multiplier's signedness cannot change them. The package
producer's evidence requires exactly those eight unsigned `MISTRAL_MUL9X9`
cells inside the blitter, in both netlists and the timing report, and still
forbids every other DSP; the diagnostics, which have no blitter, forbid DSPs
entirely. A run
reports idle and counts only once port 1's guard has drained its last write
beats, so a submitted slot never has writes in flight. Quiesce stops a
run at the next row and waits for both ports to drain; the drained proof is
registered, because the f2sdram responses are too far from the GP control
for one clock.
The diagnostic DDR wrapper leaves the blitter and ports 1/2 out. The package
producer's netlist evidence allows exactly that: port 0 and port 2 commands
must have their write bit tied low, write FIFOs 0, 1 and 3 and ports 3 to 5
tied low, so only port 1 can write. The diagnostic still requires every write
and ports 1 to 5 tied low.

`python3 scripts/sim_fes_menu.py --case gp` replays the shared golden blitter
wire fixture through actual GP/control/DDR/blitter RTL on a memory-backed
hard-block model. It verifies stalls, drain, restart, sequence exhaustion and
coherent snapshots; the fixture's run against the gfx reference byte for byte
and a whole scanned-out frame of the result; and quiesce during a blit and
fault containment. `--case blitter` checks every op, width, alignment, fault,
halt and full-frame throughput against the reference with randomly stalling
ports, and `--case blend` every source, destination and alpha byte.
`--case gp-board` checks the production board top, modeled GP/DDR atoms, PLL
gating and explicit enable. `--case all` runs every case.

Runtime presentation is implemented and has exact-artifact kit diagnostic
evidence in `docs/validation/2026-09-28-native-menu-kit-presentation.md`.
Factory-image selection and release acceptance remain separate integration
gates; building this firmware alone does not authorize releasing physical DDR
ports.
