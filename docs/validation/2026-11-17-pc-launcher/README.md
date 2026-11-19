# PC families in Home and first-run setup

This is host-only UI evidence, using the production Lua rooms, Tenfoot model
and software renderer with fixture inventories. No kit was claimed or changed.
The images do not establish core, package or hardware acceptance.

Base: `84318b52dbfadcd51b8cd92065756ea50fd5ef91` (main).
Source: the commit containing this record.

Home and first-run setup include ZX81, Spectrum, Atari ST, C64, PC/XT and
PC 386. Home retains four visible tiles and scrolls to both PC families;
Open enters the shared workbench and Back preserves the family selection.
Welcome labels PC 386 **In development**. Release scopes in the support matrix
and PC 386's pending quality tier are unchanged. Readiness continues to come
from package and hardware admission.

The next-start and installed-setup pickers use the package's actual media role.
PC/XT uses a 360 KiB floppy; PC 386 uses the existing hard-disk path, including
images above the ordinary 32 MiB file limit. Ordinary media limits remain in
force.

## Host renders

- [Welcome at 1280 x 720](welcome-720.png): all six families and the development caption.
- [Home with PC 386 selected](home-pc386-720.png): fixture readiness and scrolling.
- [Setup completion at 768 x 432](ready-432.png): compact rows and buttons.

Reproduce the images with:

```sh
cd sources/FogCast
FES_ROOM_SCREENSHOT_DIR=/absolute/output go test ./ui/tenfoot \
  -run 'TestFirstRunIncludesPCFamiliesAtSupportedSizes|TestHomeWorkbenchReturnsToTheSameFamily' \
  -count=1
```

The first-run renderer checks 720, 648, 600, 480 and 432 px heights, including
label bounds and button clearance. Existing maximum-safe-area checks cover the
smaller 684 x 432 content box. The six-family full-page test retains the
existing per-frame conversion and reduction bounds.

## Completed checks

- Full `ui/rooms` and `ui/tenfoot` suites pass; release, naming and host-client
  tests pass.
- Focused hard-disk tests pass normally and under the race detector, including
  streamed 64 MiB imports and unchanged ordinary-file bounds.
- The asynchronous family batching checks pass 20 consecutive runs after their
  wait accounts for six family responses. No callback budget was changed.
- Documentation links and support-matrix consistency pass.

## Remaining integration

Build an exact-head kit-software bundle against a verified baseline, install it
on a claimed DE10 kit, and inspect first-run Welcome/Finish and Home navigation
to both PCs. Verify installed-package setup and disk selection, then perform
the applicable launch/stop checks with an admitted package. Restore and verify
the designated baseline before releasing the kit. This source-only change
requires no new bitstream or shared protocol definitions.
