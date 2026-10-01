# Local menu frame presentation

This is an explicit diagnostic/library presentation path, with host software
coverage and an [exact-artifact kit diagnostic pass](../../../docs/validation/2026-09-28-native-menu-kit-presentation.md).
Product-image acceptance remains pending. The FES image now installs and selects
a described menu package for daemon startup. The sealed splash remains the boot
and failure fallback. A daemon started without a menu selection stays on splash.

Only `fes.menu` format 2 with required fixed video, HPS DDR and menu-display
1.0 interfaces can be configured. It is idle firmware, not a game. Normal
package programming, build identity and boot-latched DDR admission still apply.
Linux must exclude the shared window from System RAM and use the qualified
ARM mapping semantics described in [architecture](../ARCHITECTURE.md).

Protocol 2 uses the existing local Unix socket. Configure while idle:

```json
{"protocol":2,"operation":"configure_menu","package_path":"/absolute/package/path","package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
```

The example identity is syntax only. Status adds `menu_display` when a menu
has been configured or has unavailable/error evidence. Its generation is
independent of gameplay `generation`; absent means no menu configuration.
Geometry is fixed: 1280×720, stride 5120, 3,686,400-byte RGBA8888 staging,
two 4 MiB XRGB8888 DDR slots. Clients never receive a DDR descriptor/address.

A frame uses two JSON lines on **one connection**:

```json
{"protocol":2,"operation":"menu_frame_begin","expected_generation":1,"byte_count":3686400}
{"protocol":2,"operation":"menu_frame_commit","generation":1,"byte_count":3686400}
```

A successful begin response has one `SCM_RIGHTS` FD and `menu_frame` metadata
with the request's generation, byte count and `staging_format: "rgba8888"`.
Fill that exact runtime-created file. Remove all writable mappings, then apply
`F_SEAL_WRITE`, `F_SEAL_GROW`, `F_SEAL_SHRINK` and `F_SEAL_SEAL`. Send that same
file descriptor with commit; aliases of the same file are accepted, substitutions
are rejected. Size, identity, immutable seals, preparation ownership and current
generation are checked before copying. Pixels are not JSON fields.

Commit returns its own `menu_frame` generation, displayed sequence and
underflow count. Those completion fields remain bound to this frame even if
current status changes concurrently. Begin's displayed sequence is null.
`underflows` is the per-present delta, not the cumulative hardware counter.
A healthy present, including one whose counter was already nonzero before
the copy, reports 0. A tolerated glitch reports the pixels missed during
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
even if the response recipient disconnects. A revoked preparation retains its
allocation quota until its connection finishes; replacement never waits for it.

All other operations remain one request/response and permit no FDs. Extra
queued bytes after a request or commit newline are rejected rather than
silently consumed. Split JSON and ancillary delivery remain in their request
phase. Multiple, misplaced or truncated descriptors are rejected and closed.

Game launch, contained diagnostics and Stop revoke old menu generations.
Launch, core-data access, Stop, contained development load, idle recovery
and `configure_menu` wait up to two seconds (`kMenuFrameMutationWait`) when
a menu frame is the in-flight work. New frames are rejected while that wait
holds. The operation then uses its normal busy checks. It does not quiesce
or program until the frame fence is clear, and the copy stays inside that
fence. The copy converts each row in a cached buffer and writes aligned
words; it checks cancel every four rows. There is no client-visible cancel:
a lifecycle operation does not abort the frame, it waits for it. Stop
restores an explicitly configured menu with a new generation and requires
a fresh complete frame. On a menu kit, Stop reprograms the FPGA, blips
HDMI, and can end in `reboot_required` if the menu reload and the splash
reload both fail. Stop from splash idle remains idempotent. A present that
misses completion or trips the underflow limit reactivates the menu within
the budget above before using the splash path. Failed containment requires reboot.

## In-session simple-computer display

An active package with required `fes.video.session-display` and
`fes.memory.hps-ddr` 1.0 exposes the same frame transport. Open/return uses the
active core identity, not the frame generation:

```json
{"protocol":2,"operation":"session_display","expected_package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expected_generation":7,"visible":true}
{"protocol":2,"operation":"session_display","expected_package_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","expected_generation":7,"visible":false}
```

Status carries `session: true` and `core_generation: 7` in `menu_display`,
alongside the active package ID and fixed geometry, while the plane is closed.
`available` becomes true only after successful open, which grants a fresh
nonzero `menu_display.generation` for immutable frame submissions. A repeated
open is idempotent. Close sets availability false and generation zero, revokes
the preparation immediately, and requires a new generation after the next
open. Stop and replacement also revoke outstanding display ownership. The
frame generation never follows a replacement core automatically.

Open neutralizes held ZX81 keys and suppresses navigation keys while the
launcher owns focus. The first complete submitted frame becomes visible at a
frame boundary. Close and frame failure disable and drain the plane without
holding execution or resetting the computer. A sticky reader fault does not
prevent proof that the disabled plane is drained. On an ambiguous GP exchange,
the adapter realigns from the live ACK and verifies the same package identity
before the distinct quiesce operation. It never repeats a submission, programs
idle or retires the active machine to recover the display. The error remains
in `menu_display.error`. If close cannot prove drained state, the runtime
continues suppressing machine keys until close succeeds or the machine is
replaced/stopped. Media mutations and display frames share the runtime busy
fence; close and Stop use the same bounded frame wait.

The combined ZX81 display has host tests only. Existing idle-menu hardware
diagnostics do not accept its FPGA artifact or prove preservation of a live
BASIC program during cassette selection.

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
sequence marker and a final-column row/sequence marker. Every frame covers the
exact full staging area. The client checks generation, geometry, file size,
completion sequence and an underflow delta of at most one scanline, and prints frame count, elapsed time,
average/maximum commit latency and its own CPU/RSS. Measure daemon CPU separately
for hardware acceptance. The diagnostic does not restore idle configuration;
the kit operator owns restoration and lease release.
