# ZX81 hardware-room verification

Scope: FogCast host catalogue/API/client, sandboxed room services, the embedded
ZX81 workbench and native tenfoot navigation. Base FES commit:
`aaf32d3f290ba67929e751a2436ebedf658fbf40`. The PR's commits identify the result.
No runtime, FPGA, shared capability definition, image recipe or toolchain
selection changes are included.

## Behavioral coverage

- Exact installed shell admission for available expansions; wrong shell bytes
  and another package's cards are not offered as compatible.
- Compare-and-swap selection rejects stale package and expansion edits.
- Catalogue descriptions survive reopen and do not change immutable identity.
- A staged synthetic ROM/expansion launch captures the running composition;
  later draft selection leaves that composition and target operation count
  unchanged.
- The production Lua room inspects before saving, saves/removes via the typed
  host method, keeps draft and running hardware separate, and issues ordinary
  launch/Stop/tape actions.
- Missing artwork, missing firmware, unavailable session/host, failed edits
  and room resume preserve usable navigation and block unavailable mutations.
- The native room retains focus through play and tape controls; Home,
  controller Select/View and the visible pointer button return during play.
- Tape busy/retry, unavailable and hard failure keep session/package/generation
  and avoid Stop/relaunch. Failed save stays in the ordinary failure lifecycle.
- Displayed play identity is retained across room actions and tape retries;
  stale actions cannot control a replacement foreground play. Queued keyboard
  input is invalidated on Stop, play identity change and shutdown.

## Reproduce the software checks

Use a short physical temporary directory for Unix socket paths on macOS,
then the focused module tests (`mkdir -p /private/tmp/fhr-go`):

```sh
cd sources/FogCast
TMPDIR=/private/tmp/fhr-go \
CGO_LDFLAGS_ALLOW='-Wl,-weak_framework,ScreenCaptureKit' \
  go test -race ./catalog ./hostclient ./fogcast ./internal/hostapi ./ui/rooms ./ui/tenfoot
```

The allowlist is for existing macOS capture link flags. CGO-free host tests
cannot exercise the native stream-adapter assertion. The native tenfoot build
is `make -C sources/FogCast build-fogcast-tenfoot`; it includes SDL3.

From the FES root, use the documented schema-test Python environment and a
physical temporary directory outside the checkout on macOS (the system
`/var` alias otherwise trips existing symlink/path checks):

```sh
mkdir -p /private/tmp/fes-hardware-room-tests
TMPDIR=/private/tmp/fes-hardware-room-tests \
CGO_LDFLAGS_ALLOW='-Wl,-weak_framework,ScreenCaptureKit' \
  out/test-venv/bin/python scripts/test_changed.py --base aaf32d3f
make check PYTHON="$PWD/out/test-venv/bin/python"
```

The existing parent fsync-publication test reads Linux `/proc/self/fd` and
fails on macOS; its test and implementation are unchanged from the base.
The affected runner stops at this parent failure, so its subsequent planned
host, appliance, shared-linker, browser UI and generated-consistency commands
are run directly. This is a platform limitation, not a passing affected run.

Native display-list rendering uses the actual embedded Lua and `drawRoom`
through `gfx.Software`, with a synthetic host catalogue. To export idle,
running/draft-different and missing-artwork views at 1280×720 and 1024×600:

```sh
cd sources/FogCast
FES_ROOM_SCREENSHOT_DIR="$PWD/../../out/hardware-room/screenshots" \
  go test ./ui/tenfoot -run TestHardwareRoomNativeRender -count=1
```

The screenshots' card labels are test fixtures, not an inventory or hardware
acceptance result. They establish rendering of this implementation, separate
from the older browser concept. A rendering review caught and corrected native
footer overlap.

## Results on 27 September 2026

- Native SDL tenfoot build: passed on macOS arm64 with Go 1.27.1 and SDL3
  3.4.16. Linker warnings about the existing minimum macOS version remain;
  this run does not establish support for older macOS versions.
- Production-room software rendering: passed at 1280×720 and 1024×600 for
  idle, running/different draft and missing artwork.
- Appliance and shared expansion-linker race suites: passed. Browser/UI
  tests: 331 unit tests and 57 Chrome/CDP tests passed, none skipped.
- Parent Python suite: 571 tests, one unchanged macOS fsync-observation
  failure, 40 skipped. The bootstrap outside-checkout case passed with the
  external physical temporary directory.

## Remaining acceptance gates

No target was programmed, stopped, reset, deployed or otherwise controlled.
No full image assembly or FPGA build was needed for this software slice.
Physical keyboard, mouse and controller use at TV distance, an SDL interaction
run, and an exact-package mid-session tape replace/eject on a designated leased
kit remain unrun. The kit check must verify unchanged running program/RAM as
well as host session/package/generation, including busy and unavailable eject.
Software fixtures and renderer screenshots cannot establish those results.
