# Embedded Low-Latency Bluetooth Input Stack (`bt`)

Lean, input-only Classic Bluetooth (BR/EDR) stack for bare-metal DE25-Nano
TamaGo over `usb.BusHost` (`dwc2.MultiHost`), with zero Linux/BlueZ/audio/SCO
dependencies and zero steady-state heap allocations on the gameplay hot path.

## Packages

- **`bt/hci`**: USB Bluetooth HCI transport (EP0 Control OUT commands, EP1
  Interrupt-IN events, EP2 Bulk-IN/OUT ACL data with `NumCompletedPackets`
  flow-control credits), curated dongle allowlist (including Realtek
  **`0bda:a728`** BT 5.4 / RTL8761BU, CSR8510, Barrot BR8041A02, Broadcom
  BCM20702), pure-Go Realtek EPATCH v1 firmware uploader (`0xFC6D` + `0xFC20`),
  and gaming latency link policies (Role Switch Master ON, Hold/Sniff/Park OFF,
  `SetGameplayRadioMode` disabling RF scan windows during gameplay).
- **`bt/l2cap`**: Fixed-buffer ACL->L2CAP reassembly, L2CAP signaling
  (`PSM 0x0011` HID Control, `PSM 0x0013` HID Interrupt), fail-closed SSP Just
  Works pairing (unknown connections and user confirmations rejected outside
  explicit pairing mode), and bounded JSON `BondStore` (`FES/BTBONDS.JSON`, max
  16 devices / 16 KiB).
- **`bt/hidp`**: Classic BT HIDP session for up to 4 concurrent players:
  - **Sony DualSense (`054c:0ce6` / Edge `054c:0df2`) & DualShock 4**: unlocks
    78-byte high-rate `0x31` / `0x11` reports via `GET_REPORT` Feature `0x05` /
    `0x02` on `PSM 0x0011`, sets Player 1–4 LEDs/lightbar/rumble via CRC-32
    seeded output reports on `PSM 0x0013`.
  - **Nintendo Switch Pro (`057e:2009`)**: reads 18-byte factory stick
    calibration from SPI `0x603D` (`0x10`), enables 49-byte Standard Full Input
    Report `0x30` (`0x03 0x30`), and sets Player 1–4 LEDs (`0x30`).
  - **Classic BT HID Keyboard**: routes `0xA1` boot keyboard reports cleanly.
- **`bt` (`Manager`)**: Supervises USB dongle hotplug attach/detach on a
  `usb.BusHost`, refuses pairing while gameplay is active, and neutralizes
  active player slots on dongle unplug.
