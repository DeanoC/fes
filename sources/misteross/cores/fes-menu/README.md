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
Yosys/nextpnr pins in `toolchains/ramtest.lock`, connects shared port 0 at
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
ports 1/2 at the hard-block command pins. Its default read/write behavior and
boot layout remain unchanged for the RAM tester and other consumers. The
producer recognizes routed constant-zero drivers as well as synthesized zeros.
The DDR recipe defaults to seed 4; `--seed 1..8` selects a bounded alternate
and records it in the closed build inputs. Failed routes publish no RBF.

## Described menu firmware

`make build-fes-menu-package` builds separate format-2 `fes.menu` 1.0.0
firmware under `build/oss/fes-menu-package`, exporting the closed package to
`build/packages`. It requires application fixed video, HPS DDR and menu-display
1.0. It has no playable system identity and does not change factory selection.
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

`python3 scripts/sim_fes_menu.py --case gp` replays the shared golden wire
fixture through actual GP/control/DDR RTL and verifies stalls, drain, restart,
sequence exhaustion and coherent snapshots. `--case gp-board` checks the
production board top, modeled GP/DDR atoms, PLL gating and explicit enable.
These and the seven existing cases are included in `--case all`.

Runtime presentation is implemented and has exact-artifact kit diagnostic
evidence in `docs/validation/2026-09-28-native-menu-kit-presentation.md`.
Factory-image selection and release acceptance remain separate integration
gates; building this firmware alone does not authorize releasing physical DDR
ports.
