# Native USB ownership client (`usbcontrol/`)

`usbcontrol` wraps DE25-Nano USB0 driver access behind the EL3
[embedded-usb lease contract](../../../../sources/mister-packages/docs/embedded-usb.md)
(`0xc2000303`), ensuring firmware releases USB0 resets, validates PIO-only
ownership, and verifies clean channel drain on close.

- **Called by:** [board/de25nano](../board/README.md), `cmd/fpgaload`
  (`shared_usb.go`), [nativefabric](../nativefabric/README.md)
  (`ProgramInputPlatform` / `ProgramGamepadPlatform`), and `cmd/usbcheck`.
- **Delegates to:** [dwc2](../dwc2/README.md) (single-device keyboard/gamepad
  or 16-channel `MultiHost` via `usb.BusHost`) and EL3 `nano_usb_service.c` via
  SMC `0xc2000303`.

## Lease modes and startup

| Constructor | Firmware scope | Use case |
| --- | --- | --- |
| `New(cpu, owner)` | `ClosedOnly` | Standalone USB diagnostics (`cmd/usbcheck`) while FPGA bridges stay closed. |
| `NewSharedPIO(cpu, owner, ...)` | `SharedPio` | Paired native Tenfoot (`cmd/fpgaload`) keeping USB PIO active across SD and MENU ↔ core transitions while data bridges are held. |

1. **Acquire → driver factory → confirm:** Both constructors require a fresh
   EL3 query showing generation 0 and a contained controller (never adopting an
   existing lease). `Open*` reserves the prepared lease, runs one synchronous
   driver factory, and confirms live firmware readiness before exposing input.
2. **Startup failures (`StartupFailure`):** Errors during acquisition, driver
   startup, or confirmation wrap the original cause in `StartupFailure`
   (recording `Stage`, `StopAttempted`, and `StopReceipt`). A known prepared
   lease receives one bounded EL3 `Stop`; a lost/malformed reply retires
   without a speculative stop or retry. Partial drivers stay retained until
   reboot.

## Typed sources, pause/resume, and verified close

- **Three typed openers** (sharing the single-attempt board guard with no
  protocol fallback):
  - `Open` / `de25nano.OpenKeyboardWithControl` → `*Source` (`PollKeyboard`)
  - `OpenGamepad` / `de25nano.OpenGamepadWithControl` → `*GamepadSource`
    (`PollGamepad`)
  - `OpenBus` / `de25nano.OpenMultiWithControl` → multi-device `usb.BusHost`
    (binds `dwc2.MultiHost` with guarded GICv3 IRQ wiring and hotplug)
- **Polling pause across core loads (`input.PollingControl`):**
  `PausePolling` and `ResumePolling` serialize with polling/closure under the
  same confirmed owner/generation and drain pending DWC2 packets without an EL3
  mutation, preventing packets from timing out across long SDM reconfigurations
  or MENU closures.
- **Verified `Close`:** Revokes polling, drains pending driver packets/channels
  under the operation lock (unless already paused), and issues one bounded EL3
  `Stop` (where firmware independently verifies PIO ownership, channel
  retirement, AHB idle, and reset readback). Stopping USB preserves an already
  active SD lease.

## Verification

Host tests compile and run the real C EL3 USB dispatcher/service alongside
`usbcontrol` and `dwc2`:

```sh
GOWORK=off go -C platform/embedded/tamago test -race -count=1 ./usbcontrol ./dwc2
```
