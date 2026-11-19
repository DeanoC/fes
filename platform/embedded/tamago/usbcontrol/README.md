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

An attempted `Open` failure now wraps its original cause in `StartupFailure`.
Its stage distinguishes acquisition, driver startup and confirmation. The
read-only `StopAttempted` and copied `StopReceipt` preserve the outcome of the
existing cleanup before local retirement. Only a complete successful Stop
response supplies that historical inactive receipt; missing, malformed, lost or
failed replies remain unverified. `errors.Is` still finds the original cause
and terminal retirement. Inspecting the record performs no firmware call and
does not revive the source/client, release partial resources or authorize a
retry. A verified Stop is controller containment evidence under the firmware
contract, not a diagnosis of the missing device or electrical VBUS proof.
Ordinary no-device boot and hotplug still require a separate input lifecycle;
this record does not make startup failure optional in the native application.

Polling and closing serialize with lease mutations. Cached receipts authorize
only the source's own generation; they are not fresh hardware observations.
Stale sources cannot poll or stop replacements. Driver errors latch, drop partial
reports and prohibit further driver access; closure still attempts one known
stop. `Close` revokes polling and, for a driver exposing polling control, drains
its pending report/channel work before invoking firmware Stop. An already
paused driver is not drained twice. The same lease and operation locks cover
both steps, so another poll cannot arm a packet between drain and Stop.
Firmware still independently validates PIO ownership, channel retirement,
AHB idle and reset readback. A drain error remains latched even if the one known
firmware cleanup succeeds; it never supplies a usable input source. Previously
failed drivers are not entered again. Cancellation before a firmware mutation
keeps the existing known-lease cleanup rule, including the drained state.

The final native Quit previously resumed input after core return and went
directly to firmware Stop. Register/device tests now require the actual DWC2
host's pending packet, RX FIFO and channel to be retired before that call, for
both keyboard and gamepad. The exact previous source fails those unpaused-close
checks. Actual C shared-firmware tests separately exercise cancellation, stale
ownership, drain failure, foreign DMA and one-stop/no-replay behavior. These
checks do not identify the cause of the physical USB error or qualify reset
recovery of an already panicked runtime.

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

The standalone `usbcheck` selector can inspect USB while a current held-ROM
FPGA shell remains contained. It selects no FPGA control/configuration, ROM,
computer, SD, menu or reader resources. The entry receipt binds the actual USB
program/link assignments, and the cold profile derives an explicit `usb_probe`
selection. Missing or mixed resource selections reject; ordinary core loading
continues to require the complete ROM/computer contract. This prepares USB-only
diagnostics with current sealed shells; it supplies no physical acceptance.

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

Final paired Tenfoot Quit drains through the owning backend before synchronous
MENU firmware closure, then uses this same checked Close/Stop path. A pending
packet cannot be left to age across that closure. The regression retains the
former ordering as a failure case: an unserviced packet still times out on its
first poll, invalidates the client and retains resources. Neither the packet
clock nor failed-source ownership has been relaxed.
