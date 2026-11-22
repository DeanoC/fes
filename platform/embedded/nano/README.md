# DE25-Nano native boot, flash packaging and recovery

This directory contains the host-side build, packaging, recovery, JTAG-gate
and serial-observer tools for the opt-in Linux-free DE25-Nano stack
(`FES_EXPERIMENTAL_DE25_EMBEDDED=1`).

- For end-to-end native SD launcher setup, keyboard controls and clean-reboot
  preferences, see [launcher.md](launcher.md).
- For overall embedded architecture and boundaries, see
  [../README.md](../README.md).
- Historical hardware run records live under `docs/validation/*de25*`. Open
  whole-system qualification (repeated/injected failures, DMA drain, factory
  rollback round-trip and appliance readiness) is tracked in
  [Issue #1867](https://github.com/DeanoC/fes/issues/1867).

## Tooling map

| Area | Primary files | Purpose |
| --- | --- | --- |
| Cold boot & SD/QSPI images | `launcher_image.py`, `boot_flash_image.py`, `flash_trial.py`, `power_on_trace.py` | Assemble verified autoboot FAT32 SD images, R1 native + factory-rollback QSPI JICs, and passive POR serial capture verification. |
| Matched boot-chain builders | `hps_boot.py`, `hps_context.py`, `hps_io.py`, `cold_profile.py`, `prepare_uboot.py`, `fit_layout.py`, `sof_hps.py`, `package.py`, `native_admission.py`, `spectrum48.py` | Derive HPS/DDR/pinmux/control/ROM/tape/Spectrum contracts, compose HPS-first SPL+SOF images, and validate matched chains before hardware use. |
| Recovery, upload & restoration | `recovery.py`, `recovery_package.py`, `recovery_return_package.py`, `recovery_reset_package.py`, `upload.py`, `reset_recovery.py`, `reset_restoration.py`, `preferences_recovery.py`, `installer_recovery.py`, `installer_boot_refresh.py` | Volatile Ethernet TFTP recovery images, verified SD file uploads/readbacks, nonce-bound reset witnesses and single-file boot FIT restoration. |
| JTAG programming gate | `jtag_scan.cpp`, `jtag_clock.py`, `jtag_probe.py`, `jtag_programming.py`, `native_program_dispatch.py`, `pinned_files.py` | Fixed 3 MHz (`auto=0`) fresh forced-scan admission (`4362C0DD`) and one-use claim-bound programming dispatch. |
| Serial & firmware observers | `native_trace.py`, `paired_tenfoot_trace.py`, `tenfoot_trace.py`, `installer_client.py`, `installer_cycle.py`, `configuration_trace.py`, `sdm_trace/*` | Read-only trace decoders and bounded board-test controllers for paired Tenfoot, installer and SDM configuration sequences. |

## Package an opt-in R1 cold boot

On `TRWU8CBL`, Quartus Pro 26.3 maps `A5EB013BB23BR1` to JTAG ID `0x4362C0DD`
(the generic loader part selects `0xC362C0DD` and fails pre-erase). Use
`launcher_image.py` to build the checked autoboot SD image and
`boot_flash_image.py` to produce matching native and factory-rollback JICs
from an independently verified factory 16 MiB main-array backup:

```sh
FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/nano/boot_flash_image.py \
  --composition "$matched_composition" --launcher-image "$launcher_image_directory" \
  --factory-backup "$factory_main_array" --factory-sha256 "$factory_sha256" \
  --quartus-pfg "$quartus_pfg" --quartus-lock "$shared_quartus_lock" \
  --output "$fresh_output_directory"
```

- Runs from a clean checkout, checks the Quartus part table, reads every FAT32
  payload back against pinned staging/FIT/recovery bytes, verifies protected
  raw prefix and boot sectors, rejects extra/hidden/aliased entries, and
  rehashes the full file closure before publishing the plan and result.
- `power_on_trace.observe` frames passive serial captures around the expected
  16-digit SPL build ID and delegates the matched segment to a checked native
  decoder callback.
- `--native-autoboot` on U-Boot selects the guarded `fesboot` command after a
  two-second UART abort window (omitting it retains the manual diagnostic
  prompt).

## Shared core shell and Spectrum 48K

`--shared-core-shell` on the native SDK pairs the existing MENU and 16 KiB
upload/scanout shell without linking a per-Spectrum admission record into BL31
(see `spectrum48.py` and
[2026-11-22 native Spectrum 48K record](../../../docs/validation/2026-11-22-de25-native-spectrum48.md)):

- Matched BL31 admits fresh owned configuration publications for the fixed
  shell role while retaining full source, digest, range, bridge and retirement
  checks.
- TamaGo validates retained FAT selections against the fixed shell maps,
  device, ABI and ROM upload contract (assumes a trusted local producer; does
  not cryptographically authenticate the SD catalogue).
- The native Spectrum scanout reader guards registered Avalon bursts against
  the fixed DDR window, latches DMA fault status without adding return credit,
  drains accepted commands and routes normal Tenfoot Stop through BL31's
  drain/closure checks.

## Compose the HPS-first FPGA diagnostic with native SPL

`hps_boot.py` pairs a sealed HPS-first FPGA `.sof` with the matched native SPL
hex (`u-boot-spl-dtb.hex`) under the shared Quartus Pro lock (`quartus_pfg`),
checks the embedded SPL and unchanged canonical core payload, and emits the
phase-one/phase-two `.sof`/`.rbf` receipts without touching hardware.

## Source context readback

`hps_context.py` and `hps_io.py` derive the immutable boot/HPS/EMIF/pinmux,
control, configuration, ROM and tape maps from the sealed project and compiler
handoff. `cold_profile.py` compiles the matched EL3 cold profile header and
verifies agreement across the native SDK, entry stub, BL31 and U-Boot/SPL
receipts. `native_admission.py` records and re-verifies source-bound file
admission before any board run.

## CPU FPGA identity diagnostic

`package.py` and `prepare_uboot.py` assemble the volatile native boot FIT
(`FES/UPLOAD.BIN`, bounded by the 16 MiB layout in `fit_layout.py`). The
dedicated SPL (`spl_cold.c`) verifies boot kind, reset state and FIT digest
before parsing and hands off to the contained EL3 BL31 (`FES_NANO_OWNED_BOOT`).
`uboot.c` enforces owned bridge snapshots around `fesboot` before entering the
EL2→EL1 stub ([../entry/README.md](../entry/README.md)).

## Contained phase-two configuration

At runtime, EL1 TamaGo (`nativefabric`) stages verified `.fcore` payloads in
the pinned uncached DDR arena, publishes a 640-byte per-load descriptor, and
invokes contained EL3 configuration (`sdm_input_vendor_service.c`). Both
configuration owners latch the original rejection without masking deadlines;
source retirement verifies queue consumption and final SMMU/reader drain before
returning to Tenfoot or parking cleanly.

## Recovery, restoration and JTAG gate

- **Recovery & uploads:** `recovery.py` / `recovery_package.py` /
  `recovery_return_package.py` build volatile Ethernet TFTP recovery loaders.
  `upload.py` performs claim-bound, LMB-checked uploads and fresh SD readbacks
  (`--purpose` selects fixed destinations; `preferences_recovery.py` snapshots
  `FES/PREFS.JSON` read-only).
- **Reset recovery & FIT restoration:** `recovery_reset_package.py` embeds a
  nonce-bound pre-DDR SPL witness; `reset_recovery.py` and
  `reset_restoration.py` separate full JTAG programming audits from live serial
  ownership during single-file FIT restoration (see
  [de25-restoration-phase-ownership.md](../../../docs/design/de25-restoration-phase-ownership.md)).
- **3 MHz JTAG scan gate:** `jtag_scan.cpp` (built against pinned Intel AJI
  headers), `jtag_clock.FixedClock` (`3 MHz`, `auto=0`), `jtag_probe.checked`
  and `jtag_programming.ProgramAttempt` enforce fresh forced-scan identity
  (`4362C0DD`) and one-use durable reservations. Adapter discovery uses
  `jtagconfig --listhardware` (never preliminary `--enum`).

## Paired Tenfoot and installer observers

`paired_tenfoot_trace.PairedTenfootClient`, `tenfoot_trace.py`,
`installer_client.py` and `installer_cycle.py` validate ordered serial
lifecycle/firmware/USB/SD records against independently admitted source fields
under strict per-phase deadlines (`20 s`/`25 s` configuration, `100 s` launch,
`5 s` close/park). These observers hold no device or programming authority.
