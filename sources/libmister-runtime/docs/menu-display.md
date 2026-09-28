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
Stop restores an explicitly configured menu with a new generation and requires
a fresh complete frame; Stop from splash idle remains idempotent. Missing
completion or uncertain drain disables the menu configuration and uses the
existing physical splash containment path. Failed containment requires reboot.

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
completion sequence and zero underflows, and prints frame count, elapsed time,
average/maximum commit latency and its own CPU/RSS. Measure daemon CPU separately
for hardware acceptance. The diagnostic does not restore idle configuration;
the kit operator owns restoration and lease release.
