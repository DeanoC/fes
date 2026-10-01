# Running simple-computer display

`fes.simple-computer` 1.0 packages can declare required `fes.memory.hps-ddr`
1.0 (capability bit 8) and `fes.video.session-display` 1.0 (bit 9). The
display requires DDR and the ABI's existing fixed video and keyboard
interfaces. These capabilities do not change the ABI version, tag, transport,
keyboard or cassette contracts of packages that omit them. A declared
operational capability must be required at exactly version 1.0. Live identity
must match the declared bits before the runtime releases DDR ports.

The DDR window and boot-latched port layout are identical to the application
ABI. The display uses the same GP opcodes 18 through 21, `Menu*` indices and
state bits, fixed 1280×720 layout, and two 4 MiB XRGB8888 slots. Constants have
the `FesSimpleComputer` prefix. The application `fes.video.menu-display`
interface remains an idle-firmware contract; this interface belongs to the
active machine.

Configuration initializes the slots while the plane is disabled. Execution
release starts the computer with machine video visible. Enabling starts the
launcher reader; the first submitted frame remains hidden until the entire
scanout frame completes without missing pixels. Visibility changes at a
720p frame boundary. Displayed sequence acknowledges frame completion, never
just acceptance of a GP submission. Slot ownership transfers only on that
completion. At most one submission is pending. Closing drains the DDR reader
and returns machine pixels without changing execution reset, CPU, RAM, ROM,
cassette state or expansion state.

Display faults return machine pixels and permit drained quiescence even when
the reader's fault bit is sticky. The runtime retains the error and disables
frame admission; it never recovers a session display by reprogramming the
machine or activating idle firmware. Input focus belongs to FogCast; the
runtime additionally neutralizes the matrix on open and suppresses navigation
keys while the plane owns focus. Stop and replacement drain the display before
their ordinary execution hold and programming. A sticky display fault does not
prove drain: the guard must report no queued commands or outstanding/returned
DDR responses before Close or execution Hold acknowledges. If the pixel clock
stops permanently, GP completion and this drain proof are unavailable; the
runtime reports failure and retains focus rather than replacing the core.

The runtime's local `session_display` operation binds the active package and
core generation. Each opening grants a separate nonzero display generation;
close, Stop, replacement and display failure revoke old preparations. Existing
immutable `menu_frame_begin` / `menu_frame_commit` descriptors carry that display
generation. Closed active-core status retains its package and core generation,
with `available: false`, so legacy idle clients cannot acquire the plane.

The shared oracle and host tests establish software contract coverage. The
combined ZX81 shell requires fresh synthesis, timing and exact-artifact kit
acceptance; an idle-menu diagnostic does not establish session-display hardware
support.
