# Cold SD and DWC2 keyboard prototype

FES-owned exploratory native-base work on `codex/de25-embedded-os`, based on
`2e732d2d7a12621c06f6b5fab6ad107df72a83a7`. The result is the commit containing
this record. Evidence is host software, register/device models, native AArch64
QEMU execution and Nano cross-compilation. No physical board, kit, FPGA, GPU or
Powerboat process was used. Shared schemas, generated consumers and production
Linux components are unchanged.

## Implemented behavior

The SD transport replaces adoption of firmware's selected card with host/card
initialization. Firmware establishes input clocks, reset/pinmux and ComboPHY;
Go requests SDHCI reset and bus-power sequencing, sets a divided identification
clock, sends CMD0/CMD8 and bounded CMD55/ACMD41, obtains CID/RCA/CSD and selects
the card. Capacity comes from CSD, not configuration. The initial path stays
at <=400 kHz, one-bit, 3.3V and read-only. SDSC, eMMC and non-v2 CSD reject.
Actual VDD switching depends on unverified board wiring.

A failed read attempts bounded command/data reset, retires the device and
refuses further reads/flush. No partial failed sector is returned as successful.
There is no automatic retry into an existing FAT mount after possible removal.
A fresh Open and mount is required; the Nano wrapper permits another attempt
only after reboot. This is controlled containment and cold recovery, not hotplug.

The Nano USB provider now uses the DWC2 PIO host and existing shared USB/HID
input decoder. It admits firmware's idle 60 MHz ULPI controller, resets/configures
host FIFOs and the root port, assigns a device address, validates descriptors,
selects configuration/boot protocol and polls interrupt reports. Input polling
has fixed work bounds, preserves the data toggle on NAK and publishes only
completed, retired packets. Fatal errors attempt bounded channel halt and
permanently retire that host. No DMA arena, interrupt driver or Linux is used.

USB is limited to one direct low/full-speed boot keyboard. Hubs, splits,
high-speed endpoints, gamepads and HID output reports remain unsupported.
SoC PHY clocks, board VBUS and Type-C role remain firmware obligations. Firmware
profiles move to schema 2: actual SD input frequency replaces capacity/RCA;
USB readiness must match a 60 MHz clock or explicit disabled clock 0. Schema 1
and its superseded fields reject. There is no compatibility path.

`cmd/keyboard` prints normalized keys over serial independently of SD or FPGA
display. The board provider wires DWC2 into both this probe and tenfoot. Missing
Nano Ethernet, FPGA display and physical activation still prevent a complete
physical tenfoot launch. QEMU retains its original VirtIO/xHCI/RAMFB providers.

## Validation

- Entire native module: `GOWORK=off go -C platform/embedded/tamago test -race ./...`.
- Final SD/DWC2 additions: `test -race ./sdhci ./dwc2`.
- SD model: startup/reset/clock/voltage/APP_CMD/R6/CSD rejection, power-up timeout/delayed readiness,
  response types, divider rounding, discovered capacity including 2^32 sectors,
  sector bounds/data integrity, CRC/timeout/removal/short-transfer containment,
  frozen-clock budget and fresh initialization after failure.
- Real mtools FAT32 image through the SD register/card model: cold initialize,
  discover capacity, mount, read exact BOOT.JSON bytes, flush readiness, unchanged
  image hash, and rejection of a later read after simulated removal.
- DWC2 model: complete address/configuration/HID enumeration for low/full speed;
  report press/release and shared normalized input; bounded NAK polling, control-IN retries, 64-byte full-speed EP0 and data
  toggles; STALL, disconnect, bad PID, overflow, missing/short data, stuck or failed
  halt; rejected DMA/active channels/capabilities/PHY/port/descriptor/interval;
  frozen-clock startup bound. No FIFO/channel is reused after failure.
- Python embedded/profile tests: schema/field retirement, bad RAM/reservations,
  input clock limits, mismatched USB readiness and explicit USB disable.
- Native QEMU `keyboard`: an actual emulated USB keypress reached normalized input.
- Native QEMU `play`: all eleven tenfoot launch/return/failure cases passed,
  including stale ownership and application cleanup before QMP shutdown; disk
  hash unchanged. This exercises xHCI and the software FPGA model, not DWC2.
- Nano `probe`, `storage`, `keyboard`, `tenfoot`: cross-built with a **synthetic**
  schema-2 profile, AArch64 ELF/layout checks and absence of QEMU controller
  implementations. The profile is not a deployable boot recipe.

The compact receipt binds final tested source/tool/ELF identities and full local
receipt/log hashes. Full run logs and QEMU captures remain under the recorded
worktree `out/` paths; no physical acceptance or portable release is attached.
`make test-changed` with the base above passed parent regressions and generated
consumer consistency; [parent results](parent-results.json) retain that outcome.
`make deadcode` and `git diff --check` passed.

First board gates: identify revision/console routing and matching firmware;
verify reservations and identification-capable PHY configuration; serial `probe`;
read disposable FAT32 media with `storage` and compare BOOT.JSON's hash; then
validate USB host power/ULPI and key press/release with `keyboard`. Keep writes
and physical FPGA activation out of that first bring-up.
