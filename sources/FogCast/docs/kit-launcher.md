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
`rooms/<id>.state.json`). `tenfoot.json` `firmware_info_seen` lists the families
whose first-use firmware card was continued past.

Home, the rooms and the core catalog view list only the v1 release cores
(`internal/release`, recorded in `docs/support-matrix.md` as v1, v2 or held
back). Settings › Show all cores, `tenfoot.json` `all_cores`, or
`FOGCAST_TENFOOT_ALL_CORES=1` lists every core for development and HIL.
Changing it in Settings keeps Settings open until it closes, then returns Home
with the new list. Tenfoot reuses a Home room only while its release scope
matches the current setting. A setup room opened for a running machine always
keeps that machine.

`cmd/fogcast-kit` is a stub. Base images whose supervisor runs
`fogcast-kit --print-kit-ui` before starting the launcher get `tenfoot`; any
other invocation fails. Kit software bundles still ship it for those bases.
Delete it once every kit runs a base whose supervisor starts tenfoot directly.

## Menu display

With `-gfx menu-display`, tenfoot maps the runtime's grant of both menu slots
and writes each frame, in the scanout's BGRX order with each pixel's alpha as
X, straight into the slot
scanout is not showing; the runtime writes those lines back and submits the
slot. The runtime gates every frame by the current menu generation, revokes
the grant before a core owns the window, and alone controls the FPGA. When menu firmware is
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
the runtime is called. Select+Start held for one second on one pad stops a
kit-local core instead of posting the host session stop. Keyboard F12 or a pad's
Guide button also stops it with one press; neither is forwarded to the core.
Guide is the pad's GameControllerDB `guide` binding, or `BTN_MODE` when that
mapping leaves the code free. The first
loading frame shows phase and elapsed time. Tenfoot waits until that frame
has been presented, pauses menu display, then dispatches the launch request.
A failed or unpresented frame aborts dispatch. Late adoption of a core that
already owns HDMI submits no loading frame, drains outstanding menu work, and
pauses; an older menu failure does not block that pause. Local status
exposes only `launching`/`running`, so no percentage is invented.

While a kit-local core runs, `ui/localfeed` writes pad and USB keyboard events
as raw input frames on `/run/fogcast/local-input.sock`. mister-agent delivers
them into the running core. Play input does not cross the LAN and does not fall
back to the host.

Tenfoot gives the first two gamepads it opens controller ports 1 and 2 and
logs each assignment: pads present at startup in `/dev/input` order, then pads
as they are plugged in, each taking the lowest free port. A pad keeps its port
until it is unplugged. The exception is the kits' fixture pad, the generic
`081f:e401` USB pad: it gives its port to any other pad that arrives, even
one plugged in later, and moves to the port that pad would have taken, or
goes menu only. Its held buttons come up first. So with the fixture and a
DualSense attached, the DualSense is port 1, the pad a Coleco reads for its
options keypad. A pad that finds both ports taken never reaches a machine and does
not take a port that frees up later. It drives the menu wherever a pad with a
port would, including the room shown during play, and its Guide button still
stops a kit-local core like any pad's. Every pad event
carries its port as `Player`, so a `fes.gamepad.ports` core sees two players
and the kit's Select+Start stop needs both buttons on the same pad. Use two pads
with the current `fes.coleco` package for an independent-player hardware check.
SMS, SG-1000 and older Coleco packages that declare `fes.keyboard` take their
joystick through the keyboard matrix: mister-agent turns both pads' directions
and A (and Coleco's B) into that one joystick, so both pads steer the same
player. A core with only the single-pad `fes.gamepad` interface (Catch, Pong)
also sees both pads as one, and one pad letting go never releases what the
other still holds. Closing the connection
releases only that local source. Unplugging one pad sends the releases for its
held buttons and then a local departure event, freeing only that port's claim;
the other pad keeps playing. The local listener serves one writer at a time.
The host input stream remains the path for a pad that is not plugged into this
kit.

## Physical controls

Tenfoot's native input (`ui/tenfoot/linuxfb_linux.go`, `-input auto`) opens
evdev devices that `ui/controller` admits: `controller.Eligible` for gamepads
and `controller.EligibleMouse` for mice. The virtual FogCast device
(`BUS_VIRTUAL` / `FogCast Virtual Gamepad`), other virtual-bus nodes and
devices without gamepad buttons are excluded. `controller.NewMapper`
identifies each pad from its EVIOCGID bus, vendor, product and version and
its EVIOCGNAME name CRC. It reads the pad through the matching
[SDL GameControllerDB](https://github.com/mdqinc/SDL_GameControllerDB) Linux
entry, embedded as `ui/controller/gamecontrollerdb.txt`, and numbers the
pad's EVIOCGBIT keys, hats and axes the way SDL's Linux driver does. A pad
the DB does not list uses a fallback layout: Linux gamepad-API codes
(`BTN_SOUTH`…), plus the generic HID order for `BTN_TRIGGER`-range keys
(1 West, 2 South, 3 East, 4 North, 5/6 shoulders, 9 Select, 10 Start).
Tenfoot logs each pad's identity and the mapping it got. Axis ranges come
from EVIOCGABS, including unsigned 0–255 axes. Left stick and D-pad share
`left-x`/`left-y`; a D-pad on a hat or a full axis owns that axis until it
returns to centre. Keyboard events carry the key's USB HID usage;
mister-agent keeps it for `fes.keyboard.hid` cores and maps it to the ZX81
matrix or native gamepad keys for the other cores.

A pad can have a saved profile in `tenfoot.json`, under `controllers` and the
pad's GUID. Tenfoot logs the GUID when the pad attaches:

```text
tenfoot: gamepad "USB Gamepad " 081f:e401 v0110 bus 3 guid 0300e9f51f08000001e4000010010000: Super Famicom Controller
```

A profile remaps the pad's controls after its mapping. The `swap-ab` preset
trades A and B, for players who confirm with Nintendo A or a Japanese
PlayStation's Circle. `bindings` then remap single controls, in one lookup
rather than a chain, so `"a": "b"` with `"b": "a"` swaps the two. The names
are `a`, `b`, `c`, `x`, `y`, `l`, `r`, `start`, `select` and `dpad-up`,
`dpad-down`, `dpad-left`, `dpad-right`, plus `keypad-0` to `keypad-9`,
`keypad-star` and `keypad-hash` for a core that reads a keypad
(`fes.keypad.ports`):

```json
{
  "controllers": {
    "0300e9f51f08000001e4000010010000": {
      "preset": "swap-ab",
      "bindings": { "x": "keypad-1", "y": "keypad-2" }
    }
  }
}
```

Pads with the same GUID, such as two of the same model, share a profile.
Tenfoot reads it as a pad attaches, so after an edit unplug the pad and plug
it back in. The pad's log line then ends `, saved profile`, or says why the
profile was ignored. Check it: a `tenfoot.json` that does not parse is
rewritten from defaults the next time a setting is saved from the menu.
Guide is never remapped. There is no on-screen editor yet.

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
