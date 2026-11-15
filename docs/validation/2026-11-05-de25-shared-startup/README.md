# Select trusted shared USB startup for native Tenfoot

Source `48868ab57` wires the existing shared USB service and paused input path
into an explicitly selected Linux-free boot image. The builder accepts
`--shared-usb-input boot-keyboard|gamepad` only for the selected-program serial
Tenfoot computer-control/reload/SD-return path. A common source-bound policy
checks scope and protocol at the native, entry and cold-profile boundaries.
Entry packaging requires exact linker assignments, the complete sealed HPS pin
map and all selected control/memory maps. Default USB-only and SD-only images
retain their existing scope; no non-secure initializer or direct-access fallback
is introduced.

The secure startup lock covers SD initialization, physical USB board/platform
initialization, cold device composition, independent FPGA stop callbacks and
binding to the accepted private boot object. Only then is the shared USB service
published. The selected boot test includes the actual boot setup and PHY
readiness code with independent register models. It verifies three SD cycles,
bad clocks, missing SD initialization, denied private binding, failed PHY
confirmation and duplicate setup. These cycles do not configure an actual FPGA.

The native program verifies cold owner and pins and opens the explicitly shared
keyboard/gamepad source before acquiring SD. Tenfoot attaches that exact source
to the existing pause/resume backend. The diagnostic gamepad policy assigns
menu Confirm/Back to buttons 1/2, and computer arrows plus S/start and Space to
its first two buttons. These fixed assignments assert no compatibility for other
packages. A healthy idle exit stops USB through its exact receipt. Failed core
states retain resources. Output remains serial state; no HDMI grant is added.

The frozen source passes the native Go race suite, root generated-consumer check
and full selected integration plan. The SDK, entry, BL31 and U-Boot/FIT rebuild
share the same cold chain identity and source hashes. The installed firmware
header enables SD, USB and shared mode, and the secure ELF links the shared
initializer. Exact commands, receipts, hashes and coverage are in `result.json`.

Two early drafts are retained separately: a test-only compile failure mixed
incompatible register fixtures, and the first firmware build was rejected by
an unchanged serial-only HPS pin-observation guard. The fixture now shares the
PHY model with the combined USB/SD model; the pin exception requires the explicit
shared profile and retains its complete map/source checks. The repaired selected
startup and final matched builds pass.

Artifacts, source and logs are retained at
`/home/deano/fes-retained/de25-shared-startup-20261105-r1/`. Retention verifies
file-by-file archive readback and independent source restoration with prerequisite
bundles and strict Git integrity checks. Internal build symlinks are retained
with portable relative targets. External toolchains remain dependencies and no
working artifacts are removed.

This result contains no hardware programming or shared-core acceptance. Before
the next bounded TRWU8CBL test, the serial decoder/operator must recognize the
shared startup and input/stop markers and bind them to this matched chain.
Ordinary package/ROM/media admission, physical playable input/video and sustained
load/return operation remain open before DE10-style readiness can be claimed.
