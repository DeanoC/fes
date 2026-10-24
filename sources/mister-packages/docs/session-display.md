# Running-machine display plane

`fes.simple-computer` 1.0 packages can declare required `fes.memory.hps-ddr`
1.0 (capability bit 8) and `fes.video.session-display` 1.0 (bit 9);
`fes.computer` 1.0 packages use bits 12 and 13, and `fes.application` 1.0
machines, such as the Coleco native shell, use bits 8 and 11. The display
requires DDR and the ABI's existing fixed video interface. These capabilities do not change the
ABI version, tag, transport, keyboard or media contracts of packages that omit
them. A declared operational capability must be required at exactly version
1.0. Live identity must match the declared bits before the runtime releases
DDR ports.

The DDR window and boot-latched port layout are identical to the application
ABI. The display uses the same GP opcodes 18 through 21, `Menu*` indices and
state bits, fixed 1280×720 layout and two 4 MiB slots. The application
`fes.video.menu-display` interface remains an idle-firmware contract; this
interface belongs to the active machine, and no package declares both.

When the launcher takes focus on a shown plane, the runtime neutralizes the
machine's keyboard and releases every controller port. For a declared single
`fes.gamepad`, it sends a zero button snapshot and gates the input worker's
nonzero snapshots until focus returns. While the launcher has focus, FogCast
drops controller presses and consumes controller releases into the retained
source snapshot without publishing them. A release therefore cannot replay as
a held button after focus returns.

## Blending and picture rectangle

[`fes.fabric.video.display-plane`](../packages/fabric/fes_fabric_video_display_plane.yaml)
1.0 defines the shared plane behind this interface. It generates
`fes_display_plane.vh` for misteross and `fes_display_plane.hpp` for the
runtime. Keeping these constants out of the ABI files leaves every other
core's generated ABI header unchanged. The runtime consumer revision is
recorded in `testdata/oracles/fes-display-plane.json`.

A package with the session display also declares `fes.fabric.video.display-plane`
1.0 as a required interface. It grants no capability bit. Software with the
plane advertises it beside the session display and refuses either declaration
without the other, so it refuses a package with the earlier opaque plane at
compatibility. Software without the plane does not know the interface, so it
refuses the package at compatibility too. Neither refusal programs the FPGA.

Each 32-bit slot word is B, G, R, A in byte order. The plane reports pixel
format 2 (`FesDisplayPlanePixelFormatPremultipliedRgba`) at MENU_INFO index
7; idle menu display stays format 1 and ignores the top byte. After
programming, the runtime accepts a session plane only at format 2 and idle
firmware only at format 1, so a bitstream that does not match its declaration
fails closed. For machine pixel `M` and plane pixel `P`
with premultiplied alpha `a`, every channel of the visible plane is

```
out = min(255, P + ((M × (256 − a − (a >> 7))) >> 8))
```

Alpha 0 returns `M` exactly and alpha 255 returns `P` exactly. The plane
takes the shell's finished 720p word in the video-part response layout,
after any Direct or Scanlines part, so a part changes only machine pixels. It
delays that whole word by `FesDisplayPlaneLatencyCycles` (3) pixel clocks.
MENU_INFO indices 14 through 17 (`FesDisplayPlaneInfoPictureLeftIndex`, top,
width, height) report the machine picture rectangle inside the 1280×720
frame; idle menu display rejects them. The launcher uses the rectangle to fit
a surround around the machine picture.

Configuration initializes the slots while the plane is disabled. Execution
release starts the computer with machine video visible. Enabling starts the
launcher reader; the first submitted frame remains hidden until the entire
scanout frame completes without missing pixels. Visibility changes at a 720p
frame boundary. The plane locks to the shell's 720p sync before it can become
visible; losing that lock while visible is a display fault. Displayed sequence
acknowledges frame completion, never just acceptance of a GP submission. Slot
ownership transfers only on that completion. At most one submission is
pending. Closing drains the DDR reader and returns machine pixels without
changing execution reset, CPU, RAM, ROM, cassette state or expansion state.

Display faults return machine pixels and permit drained quiescence even when
the reader's fault bit is sticky. The runtime retains the error and disables
frame admission; it never recovers a session display by reprogramming the
machine or activating idle firmware. Input focus belongs to FogCast and is
separate from visibility: a shown plane can leave input with the machine, as a
surround does. The runtime additionally neutralizes the matrix when the
launcher takes focus and suppresses navigation keys while the launcher holds
it. Stop and replacement drain the display before
their ordinary execution hold and programming. A sticky display fault does not
prove drain: the guard must report no queued commands or outstanding/returned
DDR responses before Close or execution Hold acknowledges. If the pixel clock
stops permanently, GP completion and this drain proof are unavailable; the
runtime reports failure and retains focus rather than replacing the core.

The runtime's local `session_display` operation binds the active package and
core generation, and names both `visible` and `focus` (`machine` or
`launcher`). Each opening grants a separate nonzero display generation, which
focus changes on the shown plane keep;
close, Stop, replacement and display failure revoke old preparations. Existing
immutable `menu_frame_begin` / `menu_frame_commit` descriptors carry that display
generation. Closed active-core status retains its package and core generation,
with `available: false`, so legacy idle clients cannot acquire the plane.

The shared oracle and host tests establish software contract coverage. The
combined ZX81 shell requires fresh synthesis, timing and exact-artifact kit
acceptance; an idle-menu diagnostic does not establish session-display hardware
support.
