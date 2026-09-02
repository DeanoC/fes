# FogCast architecture

This is the canonical description of the working system.

## Normal FPGA game launch

```text
Browser UI
  -> POST /api/v1/session/launch
  -> host session service
  -> target /v2/cache and /v2/launch
  -> mister-agent
  -> transient MGL
  -> /dev/MiSTer_cmd: load_core <mgl>
  -> MiSTer/Main-compatible process
  -> FPGA core and game content
```

The important source entry points are:

- `internal/hostapi/server.go`: browser-facing session endpoints.
- `internal/mediasession/`: host selection and session lifecycle.
- `internal/systems/table.go`: platform, core, file-index, and library mapping.
- `internal/httpapi/content.go`: target cache and cached-launch endpoints.
- `internal/agent/content.go`: target-side cached content launch.
- `internal/mister/runtime.go`: MGL creation, command dispatch, core
  observation, and stop.

The browser sends a game ID. The host resolves it through the catalog and
system table, uploads a cache miss, and calls the target agent. The agent
writes the MGL atomically and sends `load_core <mgl>` to `/dev/MiSTer_cmd`.
FogCast waits for the expected value in `/tmp/CORENAME`. Stop uses the same
command path with `menu.rbf` and waits for `MENU`.

## Process ownership

The host owns the catalog, UI, user intent, content selection, and host-side
media. The target agent owns its HTTP API, cache, transient MGLs, and launch
requests. The MiSTer/Main-compatible process owns FPGA programming and the
MiSTer core services.

These are simple process boundaries on a local, disposable development kit;
they are not a distributed ownership, failover, or recovery protocol.

## Agent runtime backends

The target agent defaults to the existing Main runtime. Passing
`--runtime native` explicitly selects the separate native adapter, which uses
only `/run/mister-runtime.sock` and does not inspect the Main process,
`/dev/MiSTer_cmd`, or `/tmp/CORENAME`. There is no backend detection or
fallback.

The native adapter reports ready only when `mister-runtime` reports `idle`.
An idle Stop confirms that state without calling the runtime Stop operation.
Native game support has zero systems, and game requests return an
unsupported-system error; development loading and recovery return an
unsupported-operation error. The separate `native-dev` candidate packages
this composition, but it has only software and QEMU root/init packaging
evidence. Physical idle-path acceptance remains Task 7 work.

## Other modes

Host-emulator execution, remote input, capture, and host-to-target media are
existing optional modes. They share the host session UI but do not replace or
precede the direct FPGA launch path.

## Native 10-foot launcher

`cmd/fogcast-tenfoot` is an SDL3 host-side cover grid. It is another client of
the public host API, not a second launch path:

```text
Native SDL3 UI
  -> GET /api/v1/platforms
  -> GET /api/v1/library/collections
  -> GET /api/v1/games (grouped=1, availability=ready, optional collection/platform/sort/q)
  -> PUT or DELETE /api/v1/library/favorites/{id} for the focused title
  -> GET /api/v1/presentation/artwork/{handle} from catalog cover handles
  -> GET /api/v1/presentation/games/{id} for the focused title
  -> POST /api/v1/session/launch
  -> host session service
  -> existing FPGA launch path
```

Source entry points are `host/tenfoot/` and `cmd/fogcast-tenfoot`. The browser
shell remains the default UI. Build and run notes are in
[native-tenfoot-launcher/README.md](native-tenfoot-launcher/README.md).

## Target image

The active image toolchain is under `buildroot/`, `containers/target-image/`,
`internal/targetimage/`, and `scripts/*target-image*`. It produces:

- `build/output/target-image/dev/linux.img`: the fast development image.
- `build/output/target-image/prod/linux.img`: the reproducible production image.
- `build/output/target-image/native-dev/linux.img`: the reproducible native
  runtime candidate image.
- `build/output/target-image/kernel/`: the reproducible kernel artifact.

The working `dev` and `prod` targets boot `/media/fat/linux/linux.img`, start
the MiSTer/Main process, and then start the FAT-side FogCast agent from
`/media/fat/fogcast`. Before Main starts, the image-owned legacy boot service
atomically enforces `osd_timeout=0` and `video_off=0` in the persistent
`/media/fat/MiSTer.ini`, preserving unrelated settings and one copy of the
pre-FogCast file. This keeps the Menu HDMI output visible during unattended
capture and is idempotent across reboots. They remain the game and
development-RBF path.

The `native-dev` candidate instead starts image-owned `mister-runtime` and
then image-owned `mister-agent --runtime native`. It contains exactly one
locked idle RBF under `/usr/share/mister-runtime`, has no Main startup or
legacy Menu-configuration helper, has no `/dev/MiSTer_cmd` wait, and retains
the same read-only root with volatile `/run`, `/tmp`, and `/var/log`. Its QEMU
smoke proves only root filesystem and init packaging; it does not emulate FPGA
programming, prove target readiness, or establish game or development-RBF
support.

## Development RBF extension

```text
Host tool
  -> POST /api/v1/session/development-rbf (raw RBF)
  -> host session service
  -> POST /v1/development/rbf (raw RBF)
  -> mister-agent atomically installs /tmp/fogcast-development/core.rbf
  -> /dev/MiSTer_cmd: load_core /tmp/fogcast-development/core.rbf
  -> development FPGA image
```

This path intentionally has no catalog entry, MGL, game identity, manifest,
rollback store, or second programmer. The public session reports
`execution: fpga_development` with no game or system.

Non-MiSTer development images, including the `misteross` blinky and mailbox
experiments, do not implement the GPI signature expected by the current
MiSTer/Main process. Main exits after loading them and cannot reload Menu from
that FPGA state. Development Stop therefore uses an explicit recovery
handshake:

1. `POST /v1/stop` returns `recovery: reboot_required` without rebooting.
2. The host records the target's Linux boot ID, then sends
   `POST /v1/development/reboot`.
3. The host waits for target health to report a different boot ID and an idle
   status after boot. This proves the reboot even when polling does not sample
   the brief disconnect.
4. Only then does the public Stop return an idle session.

Normal game Stop is unchanged and continues to load `menu.rbf` through
`/dev/MiSTer_cmd` without rebooting. The relevant source entry points are
`internal/hostapi/session.go`, `fogcast/service.go`,
`host/development_client.go`, `internal/httpapi/development.go`,
`internal/agent/coordinator.go`, and `internal/mister/runtime.go`.
