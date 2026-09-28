# Tenfoot renderer on the native HDMI menu — brief

Status: LOCKED 2026-09-28 (Deano). No code yet. Base: FES fb069f29.

## 1. Why and where we are

The native menu transport has landed: menu firmware, reserved-DDR scanout, sealed
memfd `menu_frame_begin`/`menu_frame_commit` (#268), and the image selects `fes.menu`
at boot (#273). Today the kit draws the menu with `fogcast-kit` (kitlauncher plus the
`fbgrid` painter) through `gfx.MenuDisplay`, not with tenfoot. The approved display spec
defers the tenfoot wiring to "the subsequent UI plan"
(`docs/superpowers/specs/2026-09-27-native-menu-display-design.md:70-73`). A present
costs 76.2 ms average and 95.6 ms maximum, about 13 fps
(`docs/validation/2026-09-28-native-menu-kit-presentation.md:41`). #270 hardening
(underflow policy, present cost and busy fence) is in progress.

## 2. Existing inputs

- `docs/superpowers/specs/2026-09-27-native-menu-display-design.md` — approved transport/lifecycle; UI wiring deferred; no kitlauncher import into tenfoot.
- `docs/superpowers/specs/2026-09-27-menu-core-library-design.md` — draft; Launch yields the display before programming; rendering owns no FPGA, leases or recovery; no second coordinator.
- `docs/superpowers/plans/2026-09-28-native-menu-shell.md` — landed kit shell: kitlauncher + `fbgrid` via `gfx.MenuDisplay` and `ui/menudisplay`.
- `docs/superpowers/plans/2026-09-28-native-menu-image.md` — image selects the menu; kit UI starts in menu-display mode.
- `docs/validation/2026-09-28-native-menu-kit-presentation.md` — present timing; `:96` names the tenfoot renderer as the next gate.
- `sources/libmister-runtime/docs/menu-display.md` — runtime presentation contract.
- `docs/idle-menu-rooms.md`, `docs/idle-menu-rooms-phase2.md` — idle design lock (pre-native-menu); linuxfb overlay is optional.
- `docs/sofa-launcher-design.md` — on-kit launcher; host owns session and kit lease.
- `sources/FogCast/docs/native-tenfoot-launcher/README.md`, `LINUX.md` — tenfoot SDL3 and linuxfb `gfx.Device` backends; no menu-display backend.
- `sources/FogCast/docs/ARCHITECTURE.md:804-808` — `menu_display` uses kitlauncher/fbgrid; `:1314`, `:1332` — `placement_unresolved` / `placement_fail_closed` map to unavailable.
- `sources/FogCast/docs/kit-launcher.md:16` — `menu_display` launcher config switch.
- fes#268, fes#273 — transport and image; #268 says the ten-foot renderer is follow-on work.
- fes#270 — P2 hardening before the tenfoot renderer builds on the menu.
- fes#172 — kit-plugged pad should drive the kit directly; fes#140 — firmware import while the kit is busy.
- FogCast#151 — tenfoot authenticated kit lease / host status proxy; FogCast#138 — Linux GUI display proof.
- FogCast#273 — rooms destination panel and five availability states (no renderer content).

## 3. Locked decisions

Deano locked the proposed defaults on 2026-09-28. "Considered" lines record the
alternatives that were set aside.

1. **Renderer process and language.** Locked: proposed default. Tenfoot (Go) gains a
   menu-display `gfx.Device` backend built on the existing `ui/menudisplay` client, without
   importing kitlauncher. It runs as the on-kit renderer and replaces the `fbgrid` shell.
   No SDL on the kit. Considered: keep `fogcast-kit` with shared drawing in a common package.
2. **Frame pipeline and fps budget.** Locked: proposed default. Present only when the
   frame changes, as full 1280x720 frames. No continuous animation until #270 lands and
   presents are measured again. Considered: a fixed fps target (e.g. 30) gating #270.
3. **Input source.** Locked: proposed default. The renderer reads the kit-local pad
   directly through evdev (fes#172). Input goes to the core once a game owns the display.
   Considered: the agent virtual pad, as `docs/sofa-launcher-design.md` does today.
4. **Lease and agent coordination (#268 review nit-4).** Locked: proposed default. The
   agent stays the single coordinator and lease owner. The renderer is a display client
   of the agent and holds no lease (FogCast#151). Considered: a kit-local renderer lease,
   rejected as the "second coordinator/lease" the menu-core-library draft rules out.
5. **Menu → game handoff.** Locked: proposed default. Clients pause presenting before
   Launch, as `fogcast-kit` already does, and #270 gives the runtime lifecycle priority as
   the backstop. The renderer keeps browse state across Stop and redraws on a new
   generation. Considered: runtime priority only.
6. **Error and unsafe states.** Locked: proposed default. On a present failure or splash
   fallback, back off with a bounded retry and show a plain status. Never loop reprograms;
   recovery stays with the runtime. Considered: stop presenting until user action.
7. **UX states needing copy.** Locked: proposed default. See §4.
8. **The main menu is a room.** Locked 2026-09-28 (Deano). The main menu is composed
   through the existing rooms model and API, not a hardcoded screen. It includes
   favourites and similar content. Its initial entries are a Utils room (tools such as the
   RAM tester, `sources/misteross/cores/fes-ramtest/README.md`), a Settings room and a Room
   selector room. The main menu changes by updating room data (adding API entries where
   needed), never by redesigning the renderer. This follows the rooms-driven idle lock:
   "The living-room product is rooms on the FogCast host/tenfoot renderer"
   (`docs/idle-menu-rooms.md:42`), and the on-kit grid "retires toward rooms"
   (`docs/idle-menu-rooms.md:124-125`). Phase 2 did not deliver "rooms on kit HDMI"
   (`docs/idle-menu-rooms-phase2.md:23`).

If Deano changes the §3 defaults for input (#3) or error handling (#6), re-check §4 with Foggy.

### Main-menu room vs the current rooms model

Rooms are sandboxed Lua packs (`room.toml` + `main.lua`) that tenfoot replays through
`gfx.Device` (`sources/FogCast/docs/rooms.md:19-24`, `:82-96`). Launcher bindings come
from FogCast#272 (`sources/FogCast/docs/rooms-controller-bindings.md`). Availability
states come from FogCast#273 (`rooms.md:189-195`). Attract is off in rooms unless a room
opts in (#271, `sources/FogCast/docs/rooms-experience.md:162-164`).

| Need | Status |
| --- | --- |
| (a) Nested rooms (Utils, Settings, Room selector) | Already supported: `rooms.open(id)` / `rooms.back()` with parent stack and `on_resume` (`rooms.md:112`, `:240-243`; `ui/rooms/api_room.go:64-95`). Room selector: `rooms.list()` plus the `lobby` example ("Every installed room on one wall", `ui/rooms/examples/lobby/room.toml`). |
| (b) Non-game entries | Partly supported. Destination kinds are `game`, `room`, `library` and `unresolved` (`rooms.md:189-191`). The RAM tester works as a `game` destination only if `fes.ramtest` is a catalog core entry. Rooms cannot open launcher Settings (`settings`/`home` are never delivered, `rooms.md:110`). Needs: an allowlisted launcher-action entry (e.g. `rooms.action("settings")` or destination `kind="action"`), plus a decision on publishing utility cores as entries. |
| (c) Favourites | Already supported: `library.query{collection="favorites"}` and the game `favorite` field (`rooms.md:163`, `:168-169`). The list is dynamic, from the stored household library-user store (`rooms-experience.md:106`). |
| (d) Designated default/main room | Needs: the start preference is only `library` or `rooms` (Go-drawn Home picker) (`rooms.md:44-46`; `ui/tenfoot/run.go:60-62`). Add a `home_room` preference (room id) used at start and for Home, falling back to the picker. Ship the main-menu pack as embedded or kit-installed data; a user pack with the same id replaces it (`rooms.md:37-38`). There is no remote room-install API today. |

## 4. UX copy table (Foggy)

Copy supplied by Foggy 2026-09-28.

General rules for every state: one headline plus one plain sentence. Use the game's
name, never the internal state name. Keep IPs, `user@host` lease owners and error
codes off the main line; a small details line is fine. Never show pack IDs or paths on
screen. Name the actions (Retry, Back), not the button glyphs.

| State | When | Headline / line | Actions | Launch allowed |
| --- | --- | --- | --- | --- |
| placement_unresolved | Mesh cannot resolve a source/executor for the title | "Can't find a machine to play this on" / "Nothing on your network can run {title} right now. Check the machine is switched on, then try again." | Retry, Back | No |
| placement_fail_closed | Placement policy refuses the title (fail closed) | "Not available on this TV" / "Your settings don't allow {title} to play here." | Back | No |
| offline cached browse | Configured host unreachable; cached rows only | Banner "Offline, showing your saved list" / "Can't reach {host name}. You can browse, but games won't start until it's back." | Retry connection | No |
| connecting/retry | Renderer connecting to agent/host or backing off | "Connecting…"; backoff: "Can't connect yet. Trying again in {n}s." | Retry now | No |
| busy/launching/stopping | Lifecycle operation in flight | "Starting {title}…" / "Stopping {title}…" / "One moment…" (operation unknown) | None | No |
| kit leased by someone else | Another session holds the kit lease | "In use" / "Someone else is playing on this machine. You can play when they're done." | Back | No |
| menu unavailable/splash fallback | Menu present failed or runtime fell back to splash | Retrying: "The menu is restarting…"; retries used up: "The menu couldn't start. Restart the machine, or check it from FogCast on your computer." | None on the kit | No |
| main-menu room empty | A room has no entries to show | Favourites: "No favourites yet" / "Mark a game as a favourite and it'll show up here." Other rooms: "Nothing here yet" / "This room is empty. Pick another room from the Room selector." | Room selector, Back | n/a (nothing to launch) |
| main-menu room missing | `home_room` pack absent or invalid; falls back to the Home picker | Small notice on the picker: "Showing the basic menu" / "Your home menu couldn't load, so here's the simple list instead. Your games still work." | None (dismissible or fades) | Yes, from the fallback picker if otherwise healthy |

- placement_fail_closed shows Back only, because retrying won't change a policy
  refusal. Keep it separate from placement_unresolved and from the rooms states
  (Needs a choice, version skew, in use). Don't merge them into one "Unavailable".
- Offline is a banner over the browse grid, not a blocking screen. The Play button is
  disabled and labelled "Offline". Whether kit-local titles may launch while offline is
  a possible later Deano decision and is out of scope here.
- An empty room leaves the other rooms usable, including Utils and Settings.
- The main-menu-missing notice never takes focus or blocks input. The fallback exists so
  play isn't blocked. The pack id and reason go only to host / FogCast-on-computer
  diagnostics, never to the TV.
- In busy/launching/stopping, ignore all input except Back, and Back only closes overlays.
- In use: with a friendly name, show "{name} is playing {title}", never the raw lease
  owner. There is no take-over or steal option.
- A room that fails to compile or errors already gets a Go-drawn error panel, and Back returns
  Home (`sources/FogCast/docs/rooms.md:67-70`). Its kit copy follows the general rules above.
- The renderer can't draw while the kit is on splash. The menu-unavailable copy
  therefore appears after recovery, or on the host if status is mirrored there.

## 5. Out of scope

- Hardware-in-the-loop acceptance. That is a separate gate after the code slices.
- Mesh defaults: `[mesh] ensure` and `[mesh] placement` stay off and unchanged.
- Attract video and continuous animation on the native menu.

## 6. Next step

Split into small PRs. The renderer draws any room generically through the room API; the
main menu is seeded room data.

1. Room API: an allowlisted launcher-action entry (Settings, etc.) and a `home_room` preference with picker fallback; tests only.
2. Tenfoot menu-display `gfx.Device` backend with change-driven presents; host tests.
3. On-kit tenfoot entry point: the generic room renderer and navigation, including nested rooms and back stack, with evdev input and agent status client, replacing `fbgrid` behind config.
4. Seed the main-menu room pack (favourites, Utils with the RAM tester entry, Settings, Room selector) and set it as `home_room` on the kit.
5. Status and error states from §4: pause before Launch, redraw on new generation, bounded retry, copy wired; then the image switch plus a separate HIL record.
   This includes replacing the in-use string "This executor is in use." in FogCast
   code and tests with the §4 copy (`ui/rooms/destination.go:276`, `ui/tenfoot/app.go:1949`, and tests).
