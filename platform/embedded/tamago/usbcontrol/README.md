# Native USB ownership client

`New` requires a CPU caller and an independently obtained trusted boot owner.
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
The Nano BSP exposes `OpenKeyboardWithControl`; existing cold SD boot profiles
keep USB disabled. The explicit `usbcheck` program selects the matched secure
provider while entering with USB held, and obtains its owner through the actual
boot-context/pin verifier. It polls a direct boot keyboard for five seconds and
closes the lease; hub support and mixed storage/core permission remain open.
