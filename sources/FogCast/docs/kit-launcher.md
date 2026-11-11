# Kit launcher

The kit UI is `fogcast-tenfoot` (`ui/tenfoot`). The base-image supervisor
`fes-kit-software` starts it after the runtime and agent:

```sh
fogcast-tenfoot -config /media/fat/fogcast/launcher.json \
  -api http://127.0.0.1:8787 -kit-host \
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

On the kit (`-gfx menu-display` with a rooms Home), tenfoot opens first-run
setup instead of Home until `tenfoot.json` has `setup_done` (#1345). The
steps are rooms: Welcome (`example.welcome`), then Ready
(`example.setup-ready`); the controllers (#980) and games folders (#1346)
steps go between them. Finish and Skip setup both write `setup_done`; Skip
also records the steps it left in `setup_skipped`, and Home shows a Get
started banner whose Finish setup resumes at the first of them. Settings ›
About & help › Run setup again starts from Welcome without clearing anything.
Other launchers and backends start Home directly.

Settings (GUIDE or `o`) opens on its sections, in the v1 order (#1349). A
section is listed only when it has rows; Controllers waits for #980.

- **Games folders:** BIOS & media drives, and the host's library roots where
  the launcher edits them. #1346 replaces this with per-family folders.
- **Firmware:** one row per v1 family, Built in (with the firmware's name) or
  Needs your files, from the host's bundled firmware list. A opens that
  family's firmware card over Settings.
- **Display & sound:** Resolution (the menu display's fixed 1280×720, shown,
  not chosen), Safe area, Reduced motion and TV surround. The image has no
  volume control yet.
- **Network:** on the kit, the wired link (`eth0`; the image has no Wi-Fi).
  A host launcher lists its kit targets here instead.
- **Updates:** Cores (install cores and add games). #1350 adds the rest.
- **About & help:** Versions (the kit software bundle and base image that
  `fes-kit-software` started tenfoot with), Kit ID (`target_id`), Run setup
  again, Diagnostics (Show all cores, DIAGNOSTIC RBF, Release kit) and
  Factory reset. Factory reset asks for A twice, then deletes `tenfoot.json`
  and returns the menu settings to their defaults: safe area, layout, motion,
  surround, attract, Show all cores, pinned rooms, firmware cards seen, pad
  profiles and the first-run marker. On the kit it then opens Welcome.
  Images, software, cores, the library and the user's files are untouched. A
  pad keeps its remap until it is plugged in again.
- **Library & rooms** (listed with Show all cores): Layout, Home, Attract, and
  the host's Idle and Regions.

B goes up one level and closes Settings from the section list. A paired kit's
listener serves no library settings, so the host's rows (libraries, targets,
video preference, idle, regions) are left out there.

Home, the rooms and the core catalog view list only the v1 release cores
(`internal/release`, recorded in `docs/support-matrix.md` as v1, v2 or held
back). Settings › About & help › Diagnostics › Show all cores,
`tenfoot.json` `all_cores`, or
`FOGCAST_TENFOOT_ALL_CORES=1` lists every core for development and HIL.
In the release, Home has no Library or Systems route; Show all cores brings
them back. Back at a release-scoped Home root keeps Home open. If that Home
room cannot open, the fallback picker keeps its Library row as the way out.
Changing it in Settings keeps Settings open until it closes, then returns Home
with the new list. Tenfoot reuses a Home room only while its release scope
matches the current setting. A setup room opened for a running machine always
keeps that machine.

The bundle's `bin/fogcast-kit` is `cmd/fogcast-api` built under the kit's
name, a multi-call binary. `fogcast-kit host` runs the kit's FogCast host
(`fogcast-api -kit -headless`, see [kit mode](ARCHITECTURE.md#kit-mode)).
Base images whose supervisor runs `fogcast-kit --print-kit-ui` before starting
the launcher still get `tenfoot`; any other invocation fails.

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

## Kit host and play

Tenfoot reads the core inventory and catalog from the supervised kit host at
`127.0.0.1:8787`, and launches and stops through its normal session API. The
agent's local control socket provides read-only installed-core inventory. A
session already owning the display keeps menu presents parked.

While a host session runs on the kit, `ui/localfeed` writes pad and USB keyboard events
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
port would, including the room shown during play. Every pad event
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
