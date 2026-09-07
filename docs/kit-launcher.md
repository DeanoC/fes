# Native kit launcher adapter

`cmd/fogcast-kit` is the CGO-free ARMv7 controller/session shell. It uses
`kitlauncher.Run` with a renderer callback and physical controller factory.
The callback receives `kitlauncher.Model`: games, selected index, connection and
session status, controller presence and a readable message. This boundary lets
the renderer use the shared `host/tenfoot/fbgrid` primitive without owning
network, input leases, or FPGA transitions. The kit view pages the live catalog
as a 4×3 grid; the standalone `tenfoot-linuxfb-grid` command remains a hardcoded
paint/input fixture for framebuffer tests. Existing SDL rendering files are
unchanged.

Build with `make build-fogcast-kit`. The native image installs the command and
supervises it after runtime and agent startup. Its default configuration is
`/media/fat/fogcast/launcher.json`; FES generates and embeds that file through
its launcher setup/media workflow. Missing configuration causes a readable
service error and retry; late host/network startup stays on the connecting screen.
See [the host connection contract](launcher-host.md) for listener configuration
and exact HTTP/input-stream schemas. The host remains required for browsing and
launch. Host endpoint configuration is explicit; target discovery is separate.

## Physical controls

One physical evdev gamepad is selected; the virtual FogCast device and duplicate
joystick interface are excluded. Standard Linux gamepad buttons and the kit's
081f:e401 USB pad are normalized. Axis range comes from EVIOCGABS, including
unsigned 0–255 fixture axes. Buttons held on opening are suppressed until release.
Unplug/replug reopens a physical device and closes the old input stream.

The grid uses D-pad/vertical axis to select and A to launch. During native play,
events flow through the authenticated host stream into the existing leased
virtual pad. Hold Select + Start together for one second to request ordinary
Stop; both must release before rearming. Individual Start and Select remain game
controls; B does not stop gameplay. Stop/save errors retain the retry operation.

The host source stream releases controls on close/timeout, and an attachment ID
prevents old input affecting a new session. Its source is exclusive; the launcher
cannot steal desktop input. Local USB input currently travels via the host and
back to the kit, so its responsiveness depends on the LAN. Report measured
transport values separately from end-to-end button-to-photon latency.

## Display and verification

The native runtime enables the MiSTer HPS framebuffer on Menu bring-up and every
successful return to idle. The launcher only renders memory; it never issues SPI,
programs the FPGA, or claims a kit lease. Rendering pauses while a game is active.
The connecting/library screen uses the existing pure-Go linuxfb backend and the
shared `fbgrid` paint path. Tiles are a bounded page of live catalog rows with
system-specific colors and ASCII-safe labels; no artwork or extra host route is
introduced in this slice.

Run `go test -race ./kitlauncher/... ./cmd/fogcast-kit` for adapter tests. They
exercise real HTTP transports with isolated servers and never open real input or
framebuffer devices. Runtime and host tests cover their respective boundaries.
Use FES for selected image assembly and exact-artifact evidence. A diagnostic
binary or modified image does not establish reproducible-image acceptance.
