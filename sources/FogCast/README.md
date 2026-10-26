# FogCast

Development lives only in the [FES repository](https://github.com/DeanoC/fes),
under `sources/FogCast/`. The former standalone repository is archived.
Use a FES worktree and open PRs against FES; do not clone or update the old repository.

FogCast is a host application and MiSTer target agent for browsing and
launching a large multi-system game library. The host owns the UI, catalog,
and content selection; the MiSTer is a small, directly controlled target.

## What works now

- **Settings → BIOS & media drives** remembers USB volumes and SMB shares on
  either the kit/launcher device or the FogCast host, with separate BIOS and
  media folders. Saved drives appear in BIOS, core setup and live-media
  pickers and reconnect when opened. Browser Settings manages host drives;
  **From host drive** imports their files in FPGA library setup. See
  [BIOS and media drives](docs/ARCHITECTURE.md#bios-and-media-drives). USB
  discovery and mounting require Linux; non-Linux Tenfoot builds can still use
  SMB shares.

- Household video preference selects **Direct** or **Scanlines** for the next
  library launch. Browser Settings saves the preference; **Manage FPGA library**
  imports video parts and shows the resolved choice for the exact package.
  `fogcast video-part-install direct|scanlines PATH` snapshots a raster
  (`fes.fabric.video.raster-rgb888`) or native-pixel
  (`fes.fabric.video.native-pixels`) `.fexp` and posts it to that same host
  import; the host composes the archive against the installed sealed shell
  before storage.
  A missing Scanlines part falls back to a matching Direct part. Native pixel
  shells require a linked video part; raster shells retain built-in Direct as
  their final fallback. An installed selected part that fails admission
  requires repair. Marked Coleco shells, including a format-3 16 KiB cartridge, link
  one video part with an optional CPU expansion, retaining the shell identity
  and fixed 720p output. The target independently links the selected bytes.
  The separate `go run ./cmd/fes-parts` developer path remains volatile. See
  [library video preferences](docs/ARCHITECTURE.md#library-video-preferences) and
  [developer parts](docs/ARCHITECTURE.md#developer-video-parts).

- The tenfoot **Settings → Systems** overlay browses the configured published
  core catalog, installs packages and creates normal library games from their
  manifest-declared ROM requirements. Gamepad, keyboard and pointer navigation
  use the existing file picker and on-screen keyboard. BIOS replacement is an
  explicit household action. Cached systems remain browsable when the host is
  unavailable; setup requires an online catalog. See
  [guided core setup](docs/core-package-library.md#tenfoot-guided-core-setup).

- Library `fes.computer` 1.0 packages (`fes.apple2`, `fes.c64`,
  `fes.spectrum` and `fes.atari-st`) launch as ordinary `fpga_native` sessions. A format-3
  package links its selected `firmware` ROM at download. An Apple II shell
  with optional `fes.expansion.apple2-bus` selects one card per physical slot;
  a Commodore 64 shell with optional `fes.expansion.c64-bus` selects socket 1
  (ROM) and socket 2 (I/O); a ZX Spectrum 48K shell with optional
  `fes.expansion.spectrum-bus` selects one card per edge socket. Atari ST
  uses optional `fes.expansion.atari-st-bus` with one card in socket 1. Selection is
  `GET`/`PUT /api/v1/library/core-entries/{game_id}/expansions[/{slot}]`;
  launch links the ROM and cards on the host, the target relinks them
  independently and calls the runtime's multi-slot load. See
  [Apple II slot cards](docs/core-package-library.md#apple-ii-slot-cards).
  The entry's selected medium is delivered to media unit 0; writable ST
  library disks are restored and inserted before CPU release, and other media
  is inserted after Start:
  Apple II is an exact 143,360-byte `.dsk`/`.do` image, Commodore 64 is an
  exact 174,848-byte `.d64`, Atari ST uses raw `.st` disks (legacy 737,280 bytes;
  packages requiring `fes.media.atari-st-floppy-geometry` also admit
  80..82 tracks, 1..2 heads and 9..10 sectors of 512 bytes), and
  the ZX Spectrum models take a `.tzx`, `.tap`, `.pzx` or `.csw` tape of up to
  32 MiB, which the host converts to the unit's PZX stream.
  `POST /api/v1/session/live-media` swaps or ejects that unit while the
  machine runs. Apple II, Commodore 64 and Atari ST use `fogcast change-disk` and
  `fogcast eject-disk`. The ZX Spectrum models use `fogcast change-cassette` and
  `fogcast eject-cassette`. A stored media ID follows the active unit's
  size. See
  [removable disks](docs/ARCHITECTURE.md#removable-disks-fescomputer-media-units).
  Its keyboard is `fes.keyboard.hid`: the browser (**Capture keyboard**),
  tenfoot and the kit forward physical keys as USB HID usages through
  `set_keyboard_hid`, including Esc and Backspace; controllers use
  `set_controller` ports 0/1. See
  [keyboard HID](docs/ARCHITECTURE.md#keyboard-hid-for-home-computers).
  Atari ST media and socket paths have host tests; hardware acceptance is pending.

- Described `fes.application` packages may negotiate `fes.gamepad.ports` 1.0
  for two independent digital controllers and optional `fes.keypad.ports` 1.0
  for two twelve-key keypads. The existing session input event accepts
  zero-based `Player` (default 0). On the kit, tenfoot gives the first two
  pads it opens ports 1 and 2, releases only an unplugged pad, and keeps the
  surviving pad's port. Stop releases both. These interfaces use full snapshots
  bound to the active package generation through the native runtime; legacy
  single-pad cores retain their existing uinput path. See
  [shared controller ports](docs/ARCHITECTURE.md#shared-controller-ports).

- [Installed FPGA core packages](docs/core-package-library.md) with explicit
  version and immutable media selection in the normal library, with multiple
  titles per core. The browser's **Manage FPGA library** panel imports packages
  and media, creates titles, and explicitly changes their selections through
  the same host API as the CLI. Package/media changes need no rebuild or host restart. Import and
  selection do not replace a running core. Library media imports stream into
  bounded storage up to the host's 32 MiB policy; package capability queries
  report the separately enforced declared core limit: 16 KiB for legacy blob
  1.0, or a safe 32 KiB guarantee for required `fes.media.blob-stream` 1.0
  alongside required blob 1.0. Stream launch also validates the runtime's
  observed endpoint limits and package generation. Importing a larger asset
  does not make it runnable; these software paths do not establish SMS hardware
  or mapper compatibility.
  Library FES Pong packages with the
  persistence interfaces retain paddle speed and best rally on the target;
  [settings/progress APIs and CLI](docs/core-package-library.md#persistent-settings-and-progress)
  expose durable data. Library FES ZX81 is a volatile `fes.computer` 1.0
  entry (`fes.keyboard.hid`, `fes.media.zx81-tape`). A selected `.p` uses the
  role that package declares: `cassette` on the tape unit, inserted into unit 0
  after Start, or `blob` on an installed `fes.simple-computer` package. On a
  menu-display kit, Home or Select/View opens **Change media** over the running
  machine through `fes.video.session-display`. The selector inserts or ejects
  the active core's supported tape or disk format and returns directly to play.
  The emulator picture remains visible outside the picker. A setup
  saved earlier as `blob` still launches on the tape unit when the current
  package declares only cassette. `POST /api/v1/session/launch` with its
  `game_id` programs the package and attaches keyboard input. Library FES
  cores with a recognized play ABI (including ZX81 `fes.computer` 1.0
  and Coleco `fes.application` 1.0) are ordinary `fpga_native` play sessions, not
  Diagnostic `fpga_development`. That development label is the no-ABI
  fallback and the explicit LoadDevelopmentRBF path. Development package
  loads remain volatile.

- Development media upload for an active described `fes.simple-computer` package
  with `fes.media.blob`: `fogcast --api http://127.0.0.1:8797 core-media PATH`
  sends 1..16384 raw bytes through that running host's existing session and kit
  lease. See [development media](docs/ARCHITECTURE.md#development-media-upload)
  for admission, identity binding and failure handling. This is a development
  operation; Coleco hardware acceptance is separate. A ZX81 upload resets
  execution, so wait for the BASIC prompt before entering `LOAD`.


- Thousands of catalogued games across the systems in
  `internal/systems/table.go`.
- Real FPGA game launches on the designated MiSTer Pi.
- Target `GET /v1/health` reports sealed `artifacts` from the installed
  `build-inputs` record (and appliance boot ticket when present). Host
  `GET /api/v1/health` includes `host` OS/arch/version identity and forwards
  those target artifacts as provenance. Connection compatibility uses the
  advertised target API contract (`v1`), not Git revision equality. Missing or
  unsupported API versions refuse launch; package ABI, media, input and
  persistence support are checked by the corresponding operation. Different
  host, agent or runtime revisions alone do not require an image update.
- Target-side content caching, input, stop, and active-core observation.
- Native targets advertise installed legacy-core availability. The shared host
  marks unavailable legacy platforms browse-only and rejects direct launch
  requests before dispatch; described FPGA `core_package` entries stay
  launchable (including Coleco titles with `firmware_ready`) and retain their
  own compatibility checks. Raw Coleco library carts remain browse-only. Missing core files are also rejected by the agent
  before runtime mutation. A new valid package-library launch can recover a
  narrowly recognized retained idle launch error with one leased Stop, then
  activate once. Save failures, foreign ownership and reboot-required states
  remain explicit recovery failures; launches are never silently replayed.
  Kit footer text wraps recovery instructions and the selected title instead
  of cutting off the action the operator needs.
- Stable target identity and local DNS-SD reconnection after reboot/address
  changes. `GET /api/v1/session` reports the host session `id` and the bound
  FPGA `target`. `POST /api/v1/session/launch` may set `target` to bind that
  session without rewriting `selected_target`. A second configured target may
  play at the same time; `GET /api/v1/sessions` lists live plays. `GET
  /api/v1/session` is the foreground session. `selected_target` is the default
  session binding, not a process identity: it can change while idle even when
  remote input or media is enabled. In browser target
  settings, choose **Prepare identity**
  before assembling new media; the existing settings API accepts
  `PATCH /api/v1/library/settings` with `{"prepare_target":"dev"}`. An already
  running agent can bind its identity through authenticated health at the
  configured address. The browser and tenfoot distinguish connection state
  from game state. See [target reconnection](docs/ARCHITECTURE.md#target-identity-and-reconnection).
- Opt-in mesh placement (`[mesh] placement = true`) can claim another
  configured FPGA kit with the existing kit lease and rebind the session to
  it. Once that launch starts execution, the host releases the lease it held
  on the kit it left with `POST /v1/kit/release` (bounded at 5 seconds before
  the launch returns; never an idle Stop). A failed release is logged and does
  not undo the rebind; the grant is kept for a later explicit Stop, or ends by
  the kit's lease TTL when that kit is unreachable. See
  [placement](docs/ARCHITECTURE.md#normal-fpga-game-launch) in the architecture.
- Explicit contained development-RBF diagnostics. Recovery programs idle
  again (`recover_idle`) before any board reboot. `/sbin/reboot` runs when
  that idle program fails, and when an older runtime rejects `recover_idle`
  as an unknown operation. Stop returns a recovery error immediately when
  no reboot was started. Managed software updates wait for a released kit lease
  and restart through the base supervisor; successful releases remain installed.
  See [kit software releases](../../docs/kit-software-releases.md).
- Browser UI, local media previews, and host-emulator/remote-media modes.
- Linux hosts can provide the optional local session preview from a V4L2
  capture device through FFmpeg; configure the absolute device path in the
  private `media.capture_device` setting.
- Native SDL3 10-foot launcher (`cmd/fogcast-tenfoot`) with cover-grid, shelf,
  and list layouts plus scriptable [rooms](docs/rooms.md) (sandboxed Lua
  menu screens: overworld maps, single-console rooms, tag-driven
  cross-system views), that calls the same public host API, including a
  DIAGNOSTIC development-RBF path OSK (local file path, no browser picker).
  A kit-only host is `fogcast-api --headless --launcher-config`: catalog and
  session stay up without local capture or an SDL window, and kit tenfoot
  reconnects to the launcher listener. That listener can pair several kits
  (`pairings` in `launcher-host.json`), each with its own bearer; a bearer
  shared by two kits, or equal to any target's agent token, is refused at
  startup. Every paired kit browses the catalogue and launches on itself; the
  host keeps per-kit play sessions. Launcher requests are scoped to the paired
  kit; unscoped browser Stop, status, and input follow the active kit, then the
  selected kit. Launches without a target preserve FogCast placement selection.
  USB keyboard is first-class browse/nav (arrows/Enter/Esc/Tab; no gamepad
  required); USB mouse/pointer hover moves focus and primary click activates
  (select/launch/confirm) without a controller; on-screen hints and focus
  follow the last-used keyboard, mouse, or gamepad, a newly plugged device
  claims affinity without restart, and unplug returns to a remaining device;
  an attached play session forwards USB keyboard HID to the core/session path
  instead of the sofa graph (ZX81 still uses the matrix; native SNES/MD encode
  as gamepad buttons; Esc/Backspace still stop, except in `fes.keyboard.hid`
  sessions, which receive every key as a USB HID usage); pointer browse does
  not steal that session, and a foreign or recovery-required kit lease fails closed. Mac is the primary sofa target;
  Linux uses the same Makefile target with
  system SDL3 (`pkg-config sdl3`). Draw goes through `gfx.Device`: SDL3 is
  the production backend; Software is a CGO-free rasterizer with portable Go and an optional ARM NEON alpha-fill kernel;
  FPGA records a versioned FC2D command stream and rasters through Software
  (`-gfx fpga` / `TENFOOT_GFX=fpga`; `IsStub` true until a programmed 2D
  core exists — not HDMI FPGA UI); FPGA stub remains the thin Software
  wrapper without a stream (`fpga-stub`). `fogcast-tenfoot -gfx linuxfb`
  runs the full shared app without SDL on a 32bpp Linux framebuffer,
  with native evdev keyboard and digital gamepad input. Button, hat and
  mapped stick direction edges are applied as they arrive, including when a
  press and return to centre share one poll; d-pad input reaches a running
  local core while the menu is suppressed. Automatic gamepad discovery requires
  a readable physical bus and device name; it rejects unknown and virtual
  gamepad identities. A paired Bluetooth HID or HoG pad is an evdev node on
  that bus: hold the controller pair button and the kit initiates pairing,
  while its adapter stays non-pairable and non-discoverable to nearby devices.
  BlueZ link keys live on the FESDATA3 ext4 partition at
  `/run/fogcast/fesdata3/bluetooth`, not on FAT. Build using
  `CGO_ENABLED=0 go build ./cmd/fogcast-tenfoot`; see the
  [Linux framebuffer guide](docs/native-tenfoot-launcher/LINUX.md#framebuffer-without-sdl).
  `fogcast-tenfoot -gfx menu-display` runs that same app and evdev input through
  the runtime menu socket (`-menu-socket`, default `/run/mister-runtime.sock`).
  The direct CPU shells cache complete draw commands and textures, skipping identical raster work while updates and input continue at 30 Hz. Opaque sprites use row copies and bounded cached resizing; menu submissions use rendered revisions and reusable immutable buffers. Profiling flags and benchmark recipes are in [CPU rendering](docs/native-tenfoot-launcher/CPU.md). Presents are change-driven: an unchanged rendered revision that has not failed or been dropped is skipped. Callers without revisions compare framebuffer bytes. Once a
  second after a successful present, that skip still reads menu status. A new
  menu generation is submitted in full; the same generation presents nothing.
  The first frame is submitted because nothing has been queued yet. Status
  errors, scanout underflow, and present failures wait 250ms, doubling up to
  5s. An unavailable menu uses that schedule but never waits longer than the
  1s probe, so a Stop redraws within about a second. A generation mismatch
  does not start a new wait. The next present after a wait is submitted. The menu-display backend takes no kit lease and only
  talks to the runtime menu socket; the tenfoot app keeps its existing host
  session client and existing status reads (for example the kit-lease status
  read). With a running ZX81 that advertises the session display and HPS DDR
  interfaces, Home or Select opens the hardware room on the same HDMI output.
  An unchanged workshop setup reuses the last presented frame. Its first
  picture is built over several frames so a cold open stays inside the draw
  budget. Live cassette
  replace/eject preserves the core generation and RAM. Other
  packages keep prelaunch controls. Opening/closing is a leased host session
  operation; frame submission stays on the local runtime socket. Host tests cover the backend. This renderer is the only kit UI: the
  base-image supervisor starts it with `-config` naming `launcher.json`, from
  which tenfoot reads the kit API URL, launcher bearer, and target ID.
  Explicit `-api`, `-token`, and `-target-id` flags override matching
  environment variables, which override launcher configuration. On this
  backend tenfoot opens the kit-local control socket, shows "Starting
  {title}…", and pauses presents after that loading frame is presented so the
  core owns HDMI. A failed or unpresented frame aborts the launch. Late
  adoption drains outstanding menu work and pauses without a loading frame;
  an older menu failure does not block that pause. Presents resume after
  Select+Start stop, or when that kit-local session is no longer running.
  See
  [docs/native-tenfoot-launcher/README.md](docs/native-tenfoot-launcher/README.md).
- A reproducible target image toolchain with a development image containing
  SSH and curl.
- A separate reproducible `native-dev` image that packages the native runtime,
  native agent backend, one locked idle RBF and a selected set of validated
  format-2/3 packages (`fes.menu`, `fes.pong`,
  `fes.zx81`, `fes.coleco`, `fes.sms`, `fes.sg1000`, `fes.spectrum`, and
  `fes.ramtest`, `fes.atari-st`) through the closed package selection described in [the
  development guide](docs/DEVELOPMENT.md). The earlier Mega Drive image lane
  is retired; its dated hardware record is the
  [native Mega Drive baseline](docs/hardware/native-megadrive-baseline.md).

The native FES appliance also has a release/update client and a fixed bootstrap
with watchdog-bounded trial boots. These require FES bootstrap media; ordinary
direct-root images do not expose the update API. A card with leftover capacity
already formatted as `FESDATA3` bind-mounts cache, saves, core-data,
launcher-cache and evidence from that partition at agent startup unless
`GET /v1/update` shows trial, pending, or corrupt. Releases stay on the 1 GiB
FAT. Confirmation promotes a trial to known-good, atomically publishes that
image's sealed idle core to FAT `/idle.rbf`, and only then lets the watchdog
disarm. A non-trial boot, including factory fallback, republishes the running
image's splash when FAT does not already match. The assembler still ships the
fixed 1 GiB image; expanding leftover space is a live-card mutation. Build the operator client with
`make build-fes-update`, then use `bin/fes-update --action status` with the existing
private host configuration. See [appliance updates](docs/appliance-updates.md).
Hardware acceptance of this new boot path is tracked separately from game tests.

The normal FPGA launch path is:

1. The browser sends a game ID to `POST /api/v1/session/launch`.
2. The host resolves an installed described FES package and its library context.
3. The target agent coordinates transfer and the active session.
4. libmister-runtime validates and programs the package through local protocol 2.
5. Stop captures declared persistent data and restores the locked idle artifact.

## Source boundaries

FogCast keeps its host applications and target agent in one Go module, with
the ownership visible in the source tree. The ten-foot sofa app is under
`ui/tenfoot` and it is also the kit UI. Shared drawing, input, theme, and
library helpers live under `ui/shared`, `ui/gfx`, `ui/controller`,
`ui/inputmap`, `ui/localfeed`, `ui/menudisplay` and `ui/theme`;
`ui/launcherconfig` reads the kit's `launcher.json`. Host catalog/config/library and
host-owned input bridges live in `host` and `internal/hostapi`;
`targetclient` owns host-to-target HTTP/cache/core/development transport,
endpoint reconciliation, and kit leases; target-side HTTP/cache coordination
lives in `internal/agent`; and local MiSTer integration lives in
`internal/mister`. The UI consumes host and target-client contracts but does
not own target handlers, runtime lifecycle, image assembly, or FPGA builds. The
FES parent selects this component revision and owns image integration and
release evidence.

## Target diagnostic evidence

The target agent keeps a bounded, in-memory diagnostic ring in the same process
that owns `/v1/kit/*`; it does not add a second daemon or a second ownership
control plane. An authenticated read of
`GET http://mister.lan:8182/v1/kit/debug/events?limit=10000` returns
`{"events":[...]}` using the event shape `{ts_utc, mono_ms, flight_id?,
lease_gen?, run_id?, layer, kind, severity, detail}`. `flight_id`, when present,
is the canonical host UUID v4 from #205; lease generations and run IDs remain
opaque join strings. The ring includes lease lifecycle events and the target/runtime
hooks that can be observed locally (FIFO dispatch, descriptor open,
runtime transitions and ownership/program/recovery fences). The native
adapter also drains `/run/mister-runtime.events.json` from mister-runtime into
the same ring when that dump is present. Unavailable runtime or `fpga_manager`
observations are left absent rather than fabricated.

Before an intentional reboot, while holding the current kit lease, persist the
window with the authenticated target-agent call:

```sh
curl -H "Authorization: Bearer $KIT_TOKEN" \
  -H 'X-FogCast-Kit-Lease: <lease-token>' \
  -H 'Content-Type: application/json' \
  -d '{"run_id":"<run-id>","lease_gen":"<current-generation>","flight_id":"<host-flight-id-if-present>"}' \
  http://mister.lan:8182/v1/kit/debug/snapshot-before-reboot
```

The response points at a durable `/media/fat/fogcast/evidence/` directory with
`evidence_class: "diagnostic"`, the ring window, and bounded copies/metadata
for the journal, owner, `/tmp/CORENAME`, `fpga_manager`, and FAT-note paths.
Only after that snapshot should the existing leased development-reboot path be
called. The snapshot request requires the current opaque `lease_gen` and never
claims or mutates the kit by itself.

The fog-flight page polls the authenticated kit dump beside its
existing `/v1/health`, `/v1/kit/lease`, and `/v1/status` polls, render the
events by `layer`/`severity`, and join to host session events only when a
`flight_id` is actually present. It should preserve `run_id` and `lease_gen`
as opaque join fields, label vault results diagnostic, and continue to use the
host events endpoint at `:8787` when available; launcher `:8789` is not a
target-event join source. Tenfoot and the sofa browser stamp launch, stop,
focus, and nav with client wall + monotonic clocks. Host session events repeat
those client clocks next to host `ts_utc`/`mono_ms`; focus/nav, a rejected
launcher action, and a home-room fallback also land on
`GET /api/v1/debug/ui-events`. fog-flight builds a per-flight latency
waterfall from client → host → target when both ends are present and leaves
missing layers labelled missing.

## On-kit launcher

The kit UI is `fogcast-tenfoot -gfx menu-display`, started by the base-image
supervisor. It uses an explicitly paired host listener and the existing
session/input ownership path, or the kit-local catalog and local-control
socket when the kit has its own playable shelf. Select + Start held for one
second requests Stop. Each kit pad applies the profile `tenfoot.json` saves
under its GUID (`ui/inputmap`: the `swap-ab` preset and single-control
bindings). `cmd/fogcast-kit` is only a stub that answers
`--print-kit-ui` with `tenfoot` for bases whose supervisor still probes it.
See [kit launcher](docs/kit-launcher.md). Exact image and hardware evidence
belong to FES.

## FPGA packages and diagnostics

FPGA products require installed described FES packages. Bare Pong, SNES, NES
and Mega Drive game records no longer launch raw cores. Existing library rows,
ROM caches and saves remain on disk. Non-FPGA host execution remains available.

`POST /api/v1/session/development-rbf` is an explicit contained hardware
diagnostic. It stages a bounded upload in `/tmp/fogcast-development/core.rbf`
and programs it once through runtime protocol 2. It has no package ABI or
media/video/input guarantee. Stop restores the locked idle artifact; actual
recovery failure requires a separately requested reboot. Raw uploads remain
volatile and are not image products.

Format-2 `.fcore` development packages use
`POST /api/v1/session/development-core` with a bounded
`application/octet-stream` body. FogCast negotiates runtime protocol 2,
validates package compatibility before mutation, and reports the active package
identity, ABI, build ID, interfaces, capability flags, and generation in the
session response. Input is enabled only when the active verified package
provides `fes.gamepad`; raw development RBF input stays disabled. The command
line provides `fogcast core-inspect PATH` for local inspection without a kit
connection and `fogcast core-load PATH` for a mutation owned by the running
host session. Ordinary `fogcast launch`, `status`, and `stop` use the same
persistent `/api/v1/session/*` API and origin precedence; they no longer open a
short-lived service or emit the old `{status,content}` launch envelope.
`core-load` calls the host API selected by `--api`, then
`FOGCAST_API`, then `http://127.0.0.1:8787`; it does not open an independent
target service. The archive inspection and upload use the same bounded byte
snapshot. Success requires the returned package ID, ABI, build ID, positive
generation, and required active interfaces to match that inspected package.
There is no browser file picker. Tenfoot types or pastes a local path with
the gamepad OSK and POSTs the file bytes.

## Repository boundaries

These are modules in the FES repository. Quartus Lite 17.0.2 is an oracle
check, not the product FPGA route. The product route is HIP/nextpnr.

| Module | Owns |
| --- | --- |
| FES | Component selection, compatibility, orchestration, image assembly (`image/`), and integration evidence |
| FogCast | UI, game library, host services, and the network-facing target agent |
| libmister-runtime | FPGA programming, physical lifecycle, media and input delivery, and hardware recovery |
| misteross | FPGA source builds, RBF provenance, simulations, and compiler recipes |
| mister-packages | Shared board, protocol, and source definitions, and their schema and emitter |
| `Main_MiSTer` | Comparison reference; not a production dependency |

Native image assembly lives in the FES `image/` recipe. FogCast keeps the
agent, kit, package selector and native-runtime lock as inputs. Git
history is the archive for superseded experiments.

The `native-dev` image installs only the sealed FES core packages selected by
the FES profile; [core status](../../docs/core-status.md) records which packages
have exact-artifact kit acceptance. Its MiSTer-compatible development-RBF path
is hardware-tested for the narrow load/Stop lifecycle. The earlier Mega Drive
image slice and its one-player hardware results are dated evidence for a
retired lane ([baseline](docs/hardware/native-megadrive-baseline.md)), not a
current image capability.

## Build and test

The independent `appliance/` Go module owns release manifests and image storage.
The root module uses its local source through `go.mod`; no extra checkout is
required. Use `make test` and `make vet` to check both modules. Direct root
`go test ./...` excludes the nested module; check it with
`(cd appliance && go test -race -timeout 30m ./...)` when running Go commands manually
(the explicit per-package limit matches CI; see #482).

```sh
make build
make test
make vet
git diff --check
```

For the target image, fixture details, deployment, and live launch checks,
read [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md). The current process
boundaries and source entry points are in
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).
Phase 1 Coleco firmware (household BIOS slot, Ready gate, sofa import picker)
is in [docs/launch-composition.md](docs/launch-composition.md), with the
ZX81/Coleco expansions, Apple II slot cards and the Apple II removable disk.

The [Sinclair ZX81 workbench](../../docs/hardware-rooms.md) adds local installed-package
setup for ZX81, ZX80, TS1000 and TS1500, compatible expansion import with progress labels, and cassettes selected
before Start. Its three starter programs retain their source and licences in
[`internal/zx81tapes`](internal/zx81tapes/README.md). The kit's single HDMI menu
uses MENU while idle. A display-capable ZX81 shell also offers visible live
replace/eject on that HDMI output; older packages keep prelaunch selection.
CPU execution and RAM continue while those controls are open.

Native image assembly lives in the FES `image/` recipe. FogCast supplies the
agent, kit launcher, package selector (`cmd/target-image-lock`) while FES owns external-artifact policy in
`image/build/native-inputs.toml`. FES alone
selects the runtime source revision and generates its assembly lock. The legacy
smoke runner requires that concrete lock via `NATIVE_RUNTIME_INPUT_LOCK`.
`TARGET_IMAGE_LOCK_BIN` remains an
explicit verifier override.

The Atari ST interaction path supports relative mouse from host browser,
SDL and local evdev, with per-source button merging and no motion replay.
Writable disks use explicit library bindings and runtime-owned complete-image
saves. Format-3 Play can select firmware, an ST Direct/Scanlines video archive
and the optional slot-1 CPU expansion together; every part binds the exact
shell package. These extensions do not add Atari ST to the factory image.

### Independent room updates

First-party room directories use their public `example.*` identities directly.
The existing `fogcast-tenfoot room-install PACK_DIRECTORY` command installs Lua,
metadata and artwork under the same identity as the built-in room. Reopen Rooms
and select it to trial the update in the running launcher, without rebuilding
Go or restarting the agent. `room-rollback example.fes-cores` restores the
previous selection; a first-install rollback reveals the built-in default.
See [room delivery](docs/rooms.md#installing-and-opening-rooms). The binary's
embedded snapshots remain offline recovery defaults, rather than a required
route for delivering ordinary room updates.

Portable room files use the same installer and rollback path:
`fogcast-tenfoot room-export --output home.room.zip PACK_DIRECTORY`, then
`fogcast-tenfoot room-install home.room.zip` on the launcher's machine. Export
validates before publishing; import checks the bounded archive privately before
changing the selected snapshot. No source checkout is needed to install it.
See [room installation](docs/rooms.md#installing-and-opening-rooms).

Tenfoot synchronizes held-direction repeat state between frame-loop input and
asynchronous session-display handoffs. A failed surround transition can clear
pending repeats without racing the frame tick; repeat delay and cadence are
unchanged. The input helper releases its lock before command dispatch.
