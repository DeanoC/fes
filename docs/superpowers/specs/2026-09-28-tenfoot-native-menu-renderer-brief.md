# Tenfoot renderer on the native HDMI menu — brief

Status: DRAFT — for Deano to lock. No code yet. Base: FES fb069f29.

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

## 3. Decisions to lock

1. **Renderer process and language.**
   - Proposed default: tenfoot (Go) gains a menu-display `gfx.Device` backend built on
     the existing `ui/menudisplay` client, without importing kitlauncher. It runs as the
     on-kit renderer and replaces the `fbgrid` shell. No SDL on the kit.
   - Alternative: keep `fogcast-kit` as the process and move shared tenfoot drawing into
     a common package. The spec allows a "common rendering/application service" too.
2. **Frame pipeline and fps budget.**
   - Proposed default: present only when the frame changes, as full 1280x720 frames, with
     no continuous animation until #270 lands and presents are measured again.
   - Alternative: set a fixed frame-rate target now (for example 30 fps) and require
     #270 to meet it before tenfoot ships.
3. **Input source.**
   - Proposed default: the renderer reads the kit-local pad directly through evdev,
     following the standing rule in fes#172. Input goes to the core once a game owns the display.
   - Alternative: route menu input through the agent's virtual pad, as the sofa launcher
     does today (`docs/sofa-launcher-design.md`).
4. **Lease and agent coordination (#268 review nit-4).**
   - Proposed default: the agent stays the single coordinator and lease owner. The
     renderer is a display client of the agent and holds no lease (ties to FogCast#151).
   - Alternative: the renderer holds a kit-local lease for menu sessions, which risks
     the "second coordinator/lease" the menu-core-library draft rejects.
5. **Menu → game handoff.**
   - Proposed default: both mechanisms. Clients pause presenting before Launch, as
     `fogcast-kit` already does, and #270 gives the runtime lifecycle priority as the
     backstop. The renderer keeps browse state across Stop and redraws on a new generation.
   - Alternative: rely on runtime priority only and drop the client pause contract.
6. **Error and unsafe states.**
   - Proposed default: on a present failure or splash fallback, back off with a bounded
     retry and show a plain status. Never loop reprograms; recovery stays with the runtime.
   - Alternative: stop presenting after the first failure until an explicit user action or
     a new generation.
7. **UX states needing copy.** These are placement_unresolved, placement_fail_closed,
   offline cached browse, connecting/retry, busy/launching/stopping, kit leased by
   someone else, and menu unavailable/splash fallback. See §4.

If Deano changes the §3 defaults for input (#3) or error handling (#6), re-check §4 with Foggy.

## 4. UX copy table (Foggy)

Copy supplied by Foggy 2026-09-28.

General rules for every state: one headline plus one plain sentence. Use the game's
name, never the internal state name. Keep IPs, `user@host` lease owners and error
codes off the main line; a small details line is fine. Name the actions (Retry,
Back), not the button glyphs.

| State | When | Headline / line | Actions | Launch allowed |
| --- | --- | --- | --- | --- |
| placement_unresolved | Mesh cannot resolve a source/executor for the title | "Can't find a machine to play this on" / "Nothing on your network can run {title} right now. Check the machine is switched on, then try again." | Retry, Back | No |
| placement_fail_closed | Placement policy refuses the title (fail closed) | "Not available on this TV" / "Your settings don't allow {title} to play here." | Back | No |
| offline cached browse | Configured host unreachable; cached rows only | Banner "Offline, showing your saved list" / "Can't reach {host name}. You can browse, but games won't start until it's back." | Retry connection | No |
| connecting/retry | Renderer connecting to agent/host or backing off | "Connecting…"; backoff: "Can't connect yet. Trying again in {n}s." | Retry now | No |
| busy/launching/stopping | Lifecycle operation in flight | "Starting {title}…" / "Stopping {title}…" / "One moment…" (operation unknown) | None | No |
| kit leased by someone else | Another session holds the kit lease | "In use" / "Someone else is playing on this machine. You can play when they're done." | Back | No |
| menu unavailable/splash fallback | Menu present failed or runtime fell back to splash | Retrying: "The menu is restarting…"; retries used up: "The menu couldn't start. Restart the machine, or check it from FogCast on your computer." | None on the kit | No |

- placement_fail_closed shows Back only, because retrying won't change a policy
  refusal. Keep it separate from placement_unresolved and from the rooms states
  (Needs a choice, version skew, in use). Don't merge them into one "Unavailable".
- Offline is a banner over the browse grid, not a blocking screen. The Play button is
  disabled and labelled "Offline". Whether kit-local titles may launch while offline is
  a possible later Deano decision and is out of scope here.
- In busy/launching/stopping, ignore all input except Back, and Back only closes overlays.
- In use: with a friendly name, show "{name} is playing {title}", never the raw lease
  owner. There is no take-over or steal option.
- The renderer can't draw while the kit is on splash. The menu-unavailable copy
  therefore appears after recovery, or on the host if status is mirrored there.

## 5. Out of scope

- Hardware-in-the-loop acceptance. That is a separate gate after the code slices.
- Mesh defaults: `[mesh] ensure` and `[mesh] placement` stay off and unchanged.
- Attract video and continuous animation on the native menu.

## 6. Next step

Once this is locked, split it into small PRs:

1. Tenfoot menu-display `gfx.Device` backend (change-driven presents), host tests only.
2. On-kit tenfoot entry point with evdev input and agent status client, replacing `fbgrid` behind config.
3. Handoff and error states: pause before Launch, redraw on new generation, bounded retry, copy table wired.
4. Image switch to the tenfoot renderer, plus a separate HIL validation record.
