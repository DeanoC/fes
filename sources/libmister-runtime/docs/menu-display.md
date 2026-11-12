# Local menu frame presentation

This is an explicit diagnostic/library presentation path, with host software
coverage and a [Kit A diagnostic](https://github.com/DeanoC/fes/pull/959#issuecomment-6316246790)
of granted-slot presentation; the earlier
[exact-artifact kit diagnostic pass](../../../docs/validation/2026-09-28-native-menu-kit-presentation.md)
covered the retired copy path. Product-image acceptance remains pending. The FES image now installs and selects
a described menu package for daemon startup. The sealed splash remains the boot
and failure fallback. A daemon started without a menu selection stays on splash.

Only `fes.menu` format 2 with required fixed video, HPS DDR and menu-display
1.0 interfaces, and optionally menu-blitter 1.0 or 1.1, can be configured. It is idle
firmware, not a game. A minor 1 or later declaration receives the larger
blitter grant; a minor 0 declaration receives only the ordinary two-slot CPU
grant. Normal package programming, build identity and boot-latched DDR
admission still apply.
The blitter command-list path has host software coverage; exact-artifact kit
acceptance, including the 80 MiB target mapping, remains pending.
`src/native/menu_admission.hpp` lists the interfaces `CheckMenu` admits. The
binary carries that list in its `.fes_menu_interfaces` section, and kit
software packing publishes it in the bundle manifest, so an image can refuse a
runtime that would reject its menu
([image compatibility](../../../docs/kit-software-releases.md#image-compatibility)).
Admit and list a new menu interface in the same change.
The window is mapped through the [fes-window driver](../../../image/kernel/fes-window/README.md),
which the runtime loads from its software release and which refuses a window
overlapping System RAM (see [architecture](../ARCHITECTURE.md)).

Protocol 2 uses the existing local Unix socket. Configure while idle:

```json
{"protocol":2,"operation":"configure_menu","package_path":"/absolute/package/path","package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
```

The example identity is syntax only. Status adds `menu_display` when a menu
has been configured or has unavailable/error evidence. Its generation is
independent of gameplay `generation`; absent means no menu configuration.
Geometry is fixed: 1280×720, stride 5120, `byte_count` 3,686,400, two (`slots`)
4 MiB (`slot_bytes`) slots in `format` `bgrx8888`: XRGB8888 words, bytes
B, G, R, X. X is the pixel's alpha: idle MENU ignores it, and a session plane
blends with it as premultiplied alpha, so clients write 255 where a pixel is
opaque. The client draws straight into the slots; the runtime copies no
frame.

### Mapping lifetime

An allocation-backed adapter can report `menu_display.frame_mapping: true`.
That generation requires a `menu_frame_grant` request with `frame_mapping: true`;
the grant reply repeats the flag. The runtime refuses a missing agreement
before duplicating a grant descriptor. This mode does not support the blitter.
An absent flag keeps the existing persistent-window contract and request.

For a frame mapping, the client closes its received descriptor and unmaps its
view before sending `menu_frame_commit`, including before waiting for a failure
reply. The runtime's own reference retains the allocation and submitted pixels.
The next frame obtains a fresh descriptor for the same generation; slot and
damage history remain valid across that remap. Merely inspecting a grant leaves
no client mapping. A failed unmap refuses the commit and retains the mapping for
explicit cleanup. The client must also release a mapping on preparation failure.

This matters because frame failure can recover idle before replying. A peer VMA
must not prevent the kernel from retiring allocation references during that
recovery. Kernel exclusion remains authoritative; this agreement never permits
discarding another peer's references. DE10 keeps its persistent cached window.
The protocol and client implementation exist; the DE25 adapter and production
factory remain unfinished, with no new hardware acceptance.

### Grant

Each menu or session-plane activation grants both slots through fes-window and
fills them black. A client asks for that grant once per generation:

```json
{"protocol":2,"operation":"menu_frame_grant","expected_generation":1}
```

The response carries one `SCM_RIGHTS` descriptor, the grant file, and
`menu_frame` with the generation, `format`, `slots` and `slot_bytes`. Map it
shared and read/write: slot *n* starts at *n* × `slot_bytes`. The mapping is
cached. Every process's mapping of the grant ends when the runtime revokes it
(see Lifecycle); a later access raises SIGBUS. A new generation needs a new
grant.

A menu package with `fes.video.menu-blitter` gets a larger grant, and its
`menu_frame` adds the layout of the
[blitter](../../mister-packages/docs/application-io.md#menu-blitter-11):

```json
"blitter":{"minor":1,"list_offset":8388608,"commands":32768,"command_bytes":32,"heap_offset":16777216,"heap_bytes":67108864}
```

`minor` is the menu-blitter minor the firmware declares, which names the ops
it runs: minor 1 adds EXPAND. A client must not send an op its firmware's
minor lacks; the blitter faults it.

Map `heap_offset` + `heap_bytes` (80 MiB) from offset 0 to use the blitter.
The client writes command lists and BGRA textures there, and the blitter
writes X as 0, which idle MENU ignores. A client that draws only CPU frames
can ignore `blitter` and map the slots as on any grant. Any socket peer may take
the grant, as any peer may present, launch or Stop. The mapping covers both
slots, so the slot rules below are the client's contract: the runtime checks a
slot only when a commit names it, and refuses the displayed one.

### Frames

A frame uses two JSON lines on **one connection**:

```json
{"protocol":2,"operation":"menu_frame_begin","expected_generation":1}
{"protocol":2,"operation":"menu_frame_commit","generation":1,"slot":1,"written":[[96,50,32,10],[600,500,8,8]]}
```

Begin returns `menu_frame` with the generation, `slot`, the one scanout is
not showing, and `preparation`, which counts the generation's begins from 1.
The client draws into that slot only between this response and its commit;
the first frame after a grant goes to slot 1 and slots alternate after each
presented frame. Commit names the same slot and, optionally, the
1–16 `[x,y,w,h]` rectangles inside 1280×720 it wrote. Without `written` the
whole frame counts as written. The client tracks what each slot holds: a slot
two frames old needs this frame's changes and the previous frame's.

On a blitter grant the commit can name a command list instead, and the heap
bytes it uploaded since its last frame as 1–16 `[offset,bytes]` window ranges
inside the heap:

```json
{"protocol":2,"operation":"menu_frame_commit","generation":1,"slot":1,"list":{"first":0,"count":819},"uploaded":[[16777216,65536]]}
```

The client writes the list and uploads between begin and commit. Every peer
shares the heap, so a client can keep textures there across frames only while
no other peer has begun one: when a begin's `preparation` is not one more than
the client's own last, it must upload again everything it draws. The runtime
writes back the list's commands and the uploads, checks that the blitter is
idle, runs it into the slot, waits for its completed count, then submits the
slot as below. The completed count and busy state use separate GP reads, so a
completion between those reads is confirmed with another sample before it can
be reported as a mismatch. Nothing reads the list or the heap once commit
returns.

A blitter grant takes CPU frames until its first list commit. That list is
the client's opt-in: from then on the grant takes only lists, and the blitter
alone draws its slots. A CPU frame's write-back leaves its lines valid in the
PL310, so a CPU write over what the blitter drew could carry those stale bytes
back over it. The rule ends with the grant: a later activation's grant takes
CPU frames again. A list commit with `written`, a list on a grant without the
blitter, and a CPU commit after a list on the same grant are refused without
touching the display.

fpga2sdram reads DDR past the CPU caches, so the runtime writes back the
written regions (`FES_WINDOW_CLEAN`) before it submits the slot. Commit
returns its own `menu_frame` generation, slot, displayed sequence and underflow
count. Those completion fields remain bound to this frame even if current
status changes concurrently. `underflows` is the per-present delta, not the
cumulative hardware counter. Commit also returns `menu_frame.timing` with
`clean_us` (writing back the client's lines), `blit_us` (running the blitter,
0 for a CPU frame), `submit_us` (submitting the slot) and `scanout_wait_us`
(waiting until scanout shows it). These are
diagnostics for frame-time measurement; clients must not depend on them.
A commit naming the displayed slot is refused before any display access.

A healthy present, including one whose counter was already nonzero before
it, reports 0. A tolerated glitch reports the pixels missed during
that present, at most one 720p scanline (1280). The counter increments once
per missing active pixel. Three consecutive presents with any positive
delta, or one delta above 1280, fails that present. The runtime then
reprograms the configured menu, at most twice in ten minutes. The next
failure in that window loads the splash and sets `menu_display.error`.
A later successful `configure_menu` clears the streak and that budget.
Programming and port reset clear the hardware counter, so activation still
requires zero. There is no GP clear. FogCast clients accept a delta at or
below 1280 and refuse a larger one. The runtime and the agent decode this
nested status strictly and must ship in the same image; capability-gated
emission and tolerant decoding are not implemented.
A GP submit ACK is not completion: the runtime waits for displayed sequence
before reusing the previous slot. Only one preparation/submission is outstanding.
EOF or the fixed five-second preparation deadline releases an uncommitted
frame. Accepted frames remain owned through completion or verified containment,
even if the response recipient disconnects.

Grant is one request with one descriptor in its response. All other operations
remain one request/response and permit no FDs, commit included. Extra queued
bytes after a request or commit newline are rejected rather than silently
consumed. Split JSON and ancillary delivery remain in their request phase.
Multiple, misplaced or truncated descriptors are rejected and closed.

### Lifecycle

Game launch, contained diagnostics and Stop revoke old menu generations.
Launch, core-data access, Stop, contained development load, idle recovery,
session-plane hiding and `configure_menu` wait up to two seconds
(`kMenuFrameMutationWait`) while a frame is being drawn or presented, from
begin to the end of its commit. New frames are rejected while that wait holds.
The operation then uses its normal busy checks. Before it quiesces menu
firmware or programs another core it revokes the grant: every mapping ends and
the grant's lines are written back, so no process writes the window, and no
dirty line is evicted into it, once a core owns it. A client still drawing past
the wait faults on its next access and fails that frame. A failed present
revokes the grant too. Stop restores an explicitly configured menu with a new
generation, a new grant and black slots. On a menu kit, Stop reprograms the
FPGA, blips HDMI, and can end in `reboot_required` if the menu reload and the
splash reload both fail. Stop from splash idle remains idempotent. A present
that misses completion, trips the underflow limit or faults the blitter
reactivates the menu within the budget above before using the splash path. A
blitter fault holds until execution reset, so reprogramming is its recovery.
Failed containment requires reboot.

## In-session display

An active simple-computer or computer package with required
`fes.video.session-display` 1.0, its shared plane
`fes.fabric.video.display-plane` 1.0 and `fes.memory.hps-ddr` 1.0 exposes the
same frame transport. The plane blends the slot pixels over the machine
picture, with X as premultiplied alpha; an opaque frame replaces it. Open/return uses the active
core identity, not the frame generation. Each request names whether the plane
is shown and who receives input:

```json
{"protocol":2,"operation":"session_display","expected_package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expected_generation":7,"visible":true,"focus":"launcher"}
{"protocol":2,"operation":"session_display","expected_package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expected_generation":7,"visible":true,"focus":"machine"}
{"protocol":2,"operation":"session_display","expected_package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expected_generation":7,"visible":false,"focus":"machine"}
```

A shown plane with `"focus":"machine"` composites while the machine keeps its
keyboard and controllers, as a surround does. A closed plane with launcher
focus is invalid.

Status carries `session: true`, `core_generation: 7`, `focus` and the machine picture
rectangle `picture: [x, y, width, height]` in `menu_display`, alongside the
active package ID and fixed geometry, while the plane is closed. Admission
refuses a session display that does not declare the shared plane, such as ZX81
1.5.0, before programming. Load then requires the plane to report pixel format
2 and a picture inside the frame; idle MENU must report format 1.
`available` becomes true only after successful open, which grants the plane's
slots afresh, keeping their pixels, under a fresh nonzero
`menu_display.generation`. A repeated open with the same focus is
idempotent. Moving focus on a shown plane keeps that generation, its grant and
any preparation. Close sets
availability false and generation zero, revokes the grant and the preparation,
and requires a new generation and grant after the next open. Stop and replacement also revoke outstanding display ownership. The
frame generation never follows a replacement core automatically.

Taking launcher focus neutralizes held ZX81 keys and suppresses navigation
keys while the launcher holds it. A single-gamepad session also mutes button
snapshots; controls observed under launcher focus stay suppressed until they
are released, so later snapshots cannot restore them. The native adapter owns
that gate and status reports it. The first complete submitted frame becomes
visible at a frame boundary. Close and frame failure disable and drain the
plane without holding execution or resetting the computer. A sticky reader fault does not
prevent proof that the disabled plane is drained. On an ambiguous GP exchange,
the adapter realigns from the live ACK and verifies the same package identity
before the distinct quiesce operation. It never repeats a submission, programs
idle or retires the active machine to recover the display. The error remains
in `menu_display.error`. If close or a failed frame cannot prove drained state,
the runtime keeps launcher focus and continues suppressing machine keys until a
later request succeeds or the machine is replaced/stopped. Media mutations and display frames share the runtime busy
fence; close and Stop use the same bounded frame wait.

The combined ZX81 display has a
[frozen image/HDMI diagnostic](../../../docs/validation/2026-10-02-zx81-session-display.md)
with a ROM-backed BASIC program preserved during visible tape arm/eject.
Keyboard and controller events used Linux evdev injection. Physical operator
input, controlled clock/service faults and expansion/audio acceptance remain
separate; existing idle-menu evidence does not qualify a later ZX81 artifact.

## Pattern client

Build with `make menu-pattern-client`. It uses the daemon protocol only:

```sh
build/tools/menu-pattern-client --socket /run/mister-runtime.sock \
  --package /absolute/package/path --package-id ACTUAL_PACKAGE_ID --frames 120
build/tools/menu-pattern-client --socket /run/mister-runtime.sock --seconds 600
```

The default is 120 frames at about 30 submissions/second. `--seconds` explicitly
selects a sustained, bounded run (maximum one hour); it cannot combine with
`--frames`. `--interval-ms 0` removes pacing for an explicit stress run.
Patterns alternate full-color bars, fine pixel/row boundaries, a top-row
sequence marker and a final-column row/sequence marker. It maps the grant once
and draws every frame whole into its slot. On a blitter grant it writes the
same pattern as 819 FILL commands and commits that list, and it reports
`blitter=1` and the average `blit_avg_ms`. Its first list switches that grant
to lists, so a client that draws only CPU frames on the same generation has
its commits refused until the next activation. tenfoot draws lists on a
blitter grant, so it keeps drawing after the pattern client. The client checks generation,
geometry, format, the grant, completion sequence and an underflow delta of at
most one scanline, and prints frame count, elapsed time,
average/maximum commit latency and its own CPU/RSS. Measure daemon CPU separately
for hardware acceptance. The diagnostic does not restore idle configuration;
the kit operator owns restoration and lease release.
