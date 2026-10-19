# Kit launcher

The kit UI is `fogcast-tenfoot` (`ui/tenfoot`). The base-image supervisor
`fes-kit-software` starts it after the runtime and agent:

```sh
fogcast-tenfoot -config /media/fat/fogcast/launcher.json \
  -catalog-config <release>/share/fogcast/config.toml \
  -gfx menu-display -menu-socket /run/mister-runtime.sock -input auto -home rooms
```

Home, Library and rooms are described in [rooms](rooms.md) and
[the native tenfoot launcher](native-tenfoot-launcher/README.md).

`/media/fat/fogcast/launcher.json` is the provisioned host connection. FES
generates and embeds it through its launcher setup and media workflow.
`ui/launcherconfig` reads `api`, `token` and `target_id`. Other keys, such as
`kit_ui`, `theme` or `shelf` written for the removed grid launcher, are
ignored, so older cards still load. Tenfoot preferences and per-room state live
under `/media/fat/fogcast/state/FogCast/` (`tenfoot.json` and
`rooms/<id>.state.json`).

`cmd/fogcast-kit` is a stub. Base images whose supervisor runs
`fogcast-kit --print-kit-ui` before starting the launcher get `tenfoot`; any
other invocation fails. Kit software bundles still ship it for those bases.
Delete it once every kit runs a base whose supervisor starts tenfoot directly.

## Menu display

With `-gfx menu-display`, tenfoot sends complete sealed RGBA frames to the local
runtime socket. The runtime gates every submission by the current menu
generation and alone owns DDR and FPGA access. When menu firmware is
unavailable, the current FPGA picture is left alone. The FES native image
selects the sealed `fes.menu` package; the boot splash remains the fallback
when menu activation fails.

## Kit-local catalog and play

When `-catalog-config` (or `FOGCAST_CONFIG` / `FES_HOST_CONFIG`, or a
`config.toml` beside `launcher.json`) resolves, tenfoot boots
`fogcast.BootLocalCatalog` and serves it on loopback. It replaces the
`launcher.json` host library only when the kit has playable local files and the
config does not declare only disabled targets (#616). Otherwise the host
library stays in place.

A present Master System row from that catalog is Play on the tenfoot room path:
Confirm posts the library root and the catalog-relative cartridge path to the
local-control launch route and does not post the host session or fall back to
the network. The agent pins that root and opens the relative path from the
root directory handle. A symlink in any component under the root is refused,
so a parent directory replaced with a symlink cannot be read from outside the
library. Symlinks above the configured root stay followed. The agent then
calls LoadCartridge with the bytes. LoadCartridge rebuilds the canonical
package archive from the extracted install, wraps the cartridge with that
archive, and programs the core through the same ROM-link load the host uses. A
missing install, a core that cannot be read or take a cartridge, or a cartridge
that does not fit is reported, and the core is not programmed.

Installed cores that need no cartridge or firmware are listed and launched on
the kit through mister-agent's root-only `/run/fogcast/local-control.sock`
(`GET /v1/local/cores`, `POST /v1/local/cores/{package_id}/launch`,
`POST /v1/local/stop`, `GET /v1/local/status`). A list or launch while the
agent is still warming its catalog caches returns HTTP 503
`{"error":"warming"}`. Tenfoot reports `warming`, keeps checking and retries;
it does not treat that response as an empty library or an unavailable agent.
That path claims owner `kit-hostless` with purpose `kit-local-core` and fails
closed, without taking over, when the lease is already held (fes#172). See
[the host connection contract](launcher-host.md) for listener configuration and
the session HTTP schema.

A successful local launch returns its exact execution-lease generation, which
tenfoot keeps for Stop and status reconciliation. `POST /v1/local/stop`
requires `expected_generation`; missing or stale generations are refused before
the runtime is called. Select+Start held for one second stops a kit-local core
instead of posting the host session stop. Keyboard F12 or a pad's Guide button
also stops it with one press; neither is forwarded to the core. The first
loading frame shows phase and elapsed time. Tenfoot waits until that frame
has been presented, pauses menu display, then dispatches the launch request.
A failed or unpresented frame aborts dispatch. Late adoption of a core that
already owns HDMI submits no loading frame, drains outstanding menu work, and
pauses; an older menu failure does not block that pause. Local status
exposes only `launching`/`running`, so no percentage is invented.

While a kit-local core runs, `ui/localfeed` writes pad and USB keyboard events
as raw input frames on `/run/fogcast/local-input.sock`. mister-agent delivers
them into the running core. Play input does not cross the LAN and does not fall
back to the host. For Coleco, SMS and SG-1000, mister-agent turns local pad
directions and A into the core's keyboard joystick matrix; SMS and SG-1000
ignore B. Closing the connection releases only that local source. Unplugging one
pad sends its releases, centered axes and a local departure event, freeing only
that player's claim. The local listener serves one writer at a time. The host
input stream remains the path for a pad that is not plugged into this kit.

## Physical controls

Tenfoot's native input (`ui/tenfoot/linuxfb_linux.go`, `-input auto`) opens
evdev devices that `ui/controller` admits: `controller.Eligible` for gamepads
and `controller.EligibleMouse` for mice. The virtual FogCast device
(`BUS_VIRTUAL` / `FogCast Virtual Gamepad`), other virtual-bus nodes and
devices without gamepad buttons are excluded. `controller.NewMapper`
normalizes standard Linux gamepad buttons and the kit's 081f:e401 USB pad, with
axis ranges from EVIOCGABS, including unsigned 0–255 fixture axes. Left stick
and D-pad hat share `left-x`/`left-y`; a held hat owns that axis until it
returns to centre. Keyboard events carry the key's USB HID usage;
mister-agent keeps it for `fes.keyboard.hid` cores and maps it to the ZX81
matrix or native gamepad keys for the other cores.

Remapping lives in `ui/inputmap`. The default **identity** profile
leaves codes unchanged, so A still launches and Select+Start still stops.
`-input-profile` (then `FOGCAST_INPUT_PROFILE`) selects a built-in name (`identity`, `swap-ab`) or a
JSON file:

```json
{
  "name": "swap-ab",
  "description": "Swap A and B after device normalization.",
  "bindings": { "a": "b", "b": "a" }
}
```

Bindings are a single lookup of logical names (`a`, `b`, `start`, `select`,
`dpad-up`, `left-x`, …) or raw `evdev:<code>` / `js:<n>` keys onto those logical
names. The tenfoot `CommandFromLogical` adapter applies the remapper. There is
no on-screen editor.

## HOW_TO_RUN: LaunchBox covers on the kit host

The paired kit host (`fogcast-api` with `--launcher-config`) needs the same
opt-in `[metadata]` section the sofa host already uses. LaunchBox does not take
credentials. On the kit host:

1. Place official `Metadata.zip` at a private path (copy the sofa archive, or
   download `https://gamesdb.launchbox-app.com/Metadata.zip`).
2. Add to the host `config.toml` (mode `0600`; no `client_id` / `client_secret`):

```toml
[metadata]
enabled = true
provider = "launchbox"
archive = "/absolute/path/to/Metadata.zip"
```

3. Restart the kit-host API so it reloads metadata. Cover files land under
   `~/.cache/fogcast/metadata/launchbox-covers`.
4. Tenfoot fetches presentation and artwork for visible titles; titles without
   a match keep the existing placeholder. Clear logos use the same artwork GET via `logo_id`
   and fall back to text labels when the handle is missing. Arcade-Marquee and
Banner files use the same artwork GET via `marquee_id` and hide when absent.
   Box-3D, Cart-3D, and Box-Spine files use the same artwork GET via `box3d_id`
   when present; local `library_media` RoleBox3D (including a `Box - 3D`
   folder or `*_3d` still) wins when present. Missing 3D handles hide rather
   than invent geometry.

Do not put provider secrets in launcher JSON, logs, or pull requests. Local
`library_media` covers still win when `Game.Cover` is set.
