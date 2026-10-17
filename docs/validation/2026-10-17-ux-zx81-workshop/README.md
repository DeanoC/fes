# ZX81 Workshop: host render record, 17 October 2026

**Evidence class: host-only.** These are native software-renderer captures of
the production `example.hardware` Lua for
[#797](https://github.com/DeanoC/fes/issues/797) (the third #770 slice). No kit
was used.

- Base: FES `290fa08b`; branch `claude/ux-zx81-workshop`. The PR records the
  result commit.
- Data: the fixture hardware snapshot in `TestHardwareRoomNativeRender`
  (`ui/tenfoot/hardware_render_test.go`): one ZX81 setup with a saved 16K RAM
  pack, a ready sound card and an unavailable card. "Running" uses a fixture
  session with an empty connector.

Regenerate from `sources/FogCast`:

```sh
FES_ROOM_SCREENSHOT_DIR=/tmp/ws go test ./ui/tenfoot/ -run TestHardwareRoomNativeRender -count=1
```

| Scene | Before | After |
| --- | --- | --- |
| Idle | [before](before-idle-1280x720.png) | [after](after-idle-1280x720.png): Running and Next start together, consequence box beside Fit & save |
| Running, restart needed | [before](before-running-1280x720.png): states in a footer line | [after](after-running-1280x720.png): "Running: Empty socket", "Next start: 16K RAM pack", "Restart needed: Stop, then Start" |
| Previewing an unsaved card | not shown before | [after 1280×720](after-preview-1280x720.png), [1024×600](after-preview-1024x600.png): outlined "Preview" card, "Previewing: Sound expansion (not saved)" |
| Missing artwork | [before](before-no-artwork-1280x720.png) | [after](after-no-artwork-1280x720.png): labelled machine silhouette |

Both sizes are captured for every scene.

## Checks

- The existing hardware-room tests pass. One Direction route changed with the
  regrouped controls; that test still proves Direction alone reaches Library.
- New: every control is reachable by Direction from Start, and the setup tools
  form a grid (Import under More cards, Library under Refresh).
- New: without a session reading the room says "Running: unknown", never
  "not running".
- An independent agent review of the first commit found:
  - the unknown-session claim
  - Import expansion reachable only from Back
  - a clipped tag at 1024 wide
  - narrowed info and description text
  - a stale doc label
  - an unavailable card drawn as a preview

  All are fixed.

## Not covered

Z1–Z3 on a kit: inspect without mutation, save and read back, the running
receipt and Restart needed, two-client conflict, and the live return to a
running ZX81 with BASIC continuity.
