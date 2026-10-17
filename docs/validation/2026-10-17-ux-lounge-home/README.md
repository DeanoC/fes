# Native Home Lounge: host render record, 17 October 2026

**Evidence class: host-only.** These are captures from the native tenfoot
software renderer and focused host tests. They are not hardware diagnostics
or exact-artifact acceptance. No kit, lease or HDMI output was used.

- Issue: [#770](https://github.com/DeanoC/fes/issues/770), first slice.
- Base: FES `bf972350f62ad335d3f7b8665e661c3660212225`; branch
  `claude/ux-lounge-home`. The PR records the result commit.
- Path: `fogcast-tenfoot` with the kit Home room `example.fes-cores`, as the
  kit starts it on `-gfx menu-display`.
- Renderer: `gfx.NewSoftware` through the production `presentFrame` and
  room display-list replay. The production Lua and launcher chrome are used.
- Data: fake local-control client returning the factory set's names and
  local blocks (`homeFixtureCores` in `ui/tenfoot/home_lounge_test.go`). The
  `inputs` values follow the packages' required interfaces. The installed
  list on a real kit was not read.

Regenerate the captures from `sources/FogCast`:

```sh
FES_ROOM_SCREENSHOT_DIR=/tmp/home go test ./ui/tenfoot/ -run TestHomeLoungeNativeRender -count=1
```

## Baseline (before)

| Capture | Observation |
| --- | --- |
| [before-ready](before-ready-1280x720.png) | Acronym tiles ("FP", "FZS"), no destination, debug-font prompts, lease line in the footer |
| [before-checking](before-checking-1280x720.png) | "UNRESOLVED" while listing; the prompt says "A open the title list" and Confirm does nothing visible |

## Lounge Home (after)

| Capture | What it shows |
| --- | --- |
| [ready 1280×720](after-ready-1280x720.png) | Pong selected: Ready, "Plays on this TV", the stop hold stated before Play, A Play / Y Details |
| [ready 1024×600](after-ready-1024x600.png) | Same at the smaller layout; three shelf cards |
| [blocked](after-blocked-1280x720.png) | ColecoVision: Unavailable, "Display setup incomplete" with the remedy; A opens Details |
| [details](after-details-1280x720.png) | A on ColecoVision: core id, package id and the kit's verbatim block in the hero; B closes |
| [long 1280×720](after-long-1280x720.png) / [1024×600](after-long-1024x600.png) | A 60-character name: the title shortens to one line and nothing reaches the buttons |
| [checking + Confirm](after-checking-confirm-1280x720.png) | Checking with the acknowledgement after A; no launch |
| [settings](after-settings-1280x720.png) | Up from the shelf: Settings reached with Direction and Confirm |
| [launching](after-launching-1280x720.png) | Existing FES launch overlay over the hero; title stays visible |
| [launch failed](after-launch-failed-1280x720.png) | Failure reason in the footer; selection kept |
| [in use](after-in-use-1280x720.png) | Foreign kit lease: Unavailable, "In use", no Play |
| [empty](after-empty-1280x720.png) | Nothing installed: explanation, Library focused |
| [no kit](after-no-kit-1280x720.png) | Host launcher: Library focused, no core claims |

## Focused checks

- `go test -race ./ui/rooms/ ./ui/tenfoot/ ./internal/localcores/`: pass.
- An independent agent review found that core Details and the hero buttons
  did nothing visible, that Ready could show during launch reconciliation,
  and that long titles overlapped the buttons. These were fixed with tests
  before the PR.
- New tests: Confirm during Checking is acknowledged and never queued;
  core Details opens in the hero and Back closes only Details; pointer
  clicks on the hero's Play and Details buttons; Checking during launch
  reconciliation; long titles fit above the buttons at both sizes;
  launch → Select+Start stop → return keeps the room and selected package;
  hero copy for each `needs` class and a foreign lease; hero geometry stays
  inside the room; later visit restores the selected package; required
  keyboard/mouse interfaces become `inputs`.

## Not covered

H1–H9 on a kit: boot entry, physical controller and keyboard runs, the real
HDMI picture and viewing distance, a real launch and Stop, host loss,
occupied hardware and save failure. The gameplay exit is still only the
Select+Start hold, so H2 does not pass.
