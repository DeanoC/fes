# Native USB ownership client

`New` requires a CPU caller and an independently obtained trusted boot owner.
It accepts only ClosedOnly firmware. `NewSharedPIO` explicitly requires SharedPio
backed by the compiled cold/master binding. Both initially require held data.
A fresh query must describe generation zero and a contained controller. It
cannot adopt a previous active or returned lease. `Open` reserves the prepared
lease, invokes one synchronous driver factory, then confirms live firmware
readiness before exposing reports.

Retain the client, source and partial driver until reboot, including constructor
errors. Factories return partially initialized resources on failure, start no
background polling and do not re-enter the client. A known prepared lease gets
one bounded stop on startup failure. A lost or malformed reply retires the
client without a speculative stop or retry.

Polling and closing serialize with lease mutations. Cached receipts authorize
only the source's own generation; they are not fresh hardware observations.
Stale sources cannot poll or stop replacements. Driver errors latch, drop partial
reports and prohibit further driver access; closure still attempts one known
stop. `Close` revokes polling before invoking firmware.

Tests use the actual C dispatcher/service/platform/board/cold policy through a
register-model process. DWC2 tests separately exercise the real host driver with
a modeled device and firmware replies. Neither is physical USB acceptance.
`OpenGamepad` uses the same lease lifecycle through a separate typed source;
it supplies `PollGamepad`, never `PollKeyboard`. Copies of either public source
share the private lease, so closing a copy revokes the original too. The Nano
BSP exposes `OpenGamepadWithControl`, returning a copied HID report descriptor
and retaining partial sources/hosts on failure. Keyboard and gamepad share the
single controller attempt guard; there is no protocol fallback.

The Nano BSP exposes `OpenKeyboardWithControl`; existing cold SD boot profiles
keep USB disabled. The explicit `usbcheck` program selects the matched secure
provider while entering with USB held, and obtains its owner through the actual
boot-context/pin verifier. It polls a direct boot keyboard for five seconds and
closes the lease. `--usb-input gamepad` explicitly selects descriptor-driven HID
input instead. The linker assignment, SDK/entry receipts and cold chain bind that
selection; the observer must receive the same checked protocol. It captures the
descriptor and normalized state without guessing this device's menu buttons.
An unsupported schema retains its descriptor but cannot produce Ready/Pass.
Physical gamepad input and mixed storage/core permission remain open.

The shared-mode RPC fixture runs the actual owned dispatcher, USB/SD services,
boot/master and bridge code against one register model. It covers retained USB
reports through SD activity and control/DDR transitions, dynamic DataHeld,
explicit scope selection, malformed modes, independent USB/SD reservations and
fault retirement. It does not select a composed board image or establish
physical coexistence. Firmware changes to USB ownership require a closed,
qualified core return; stopping USB can preserve an already active SD lease.

Both owned typed sources expose `input.PollingControl`. Pause/resume serialize
with polling and closure and require the same confirmed owner/generation.
They delegate to a capable report driver without a firmware mutation; paused
polls return no report or hardware access. An unsupported driver is rejected.
An attempted driver transition failure retires local polling while retaining
the active reservation for the existing checked `Close` path. No resume can
repair a failed, closed or replaced source. The native backend uses this to
drain pending DWC2 transactions before long core loads and returns.
