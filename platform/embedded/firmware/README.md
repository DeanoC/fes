# Contained firmware and FPGA control prototype

This directory builds and verifies the EL3 TF-A (BL31) firmware modifications,
owned SiP services and FPGA control/drain helpers for the Linux-free DE25-Nano
experiment. The shared
[embedded-control contract](../../../sources/mister-packages/docs/embedded-control.md)
and [embedded-bridges contract](../../../sources/mister-packages/docs/embedded-bridges.md)
own register offsets and wire formats; regenerate consumers with `make
generate` rather than editing `generated/` by hand.

Historical bring-up diaries and hardware run records live under
`docs/validation/*de25*`.

## Firmware source

`prepare_tfa.py` transforms only two hash-pinned versions of
`plat/intel/soc/common/socfpga_sip_svc.c`:
- Upstream TF-A v2.12 blob `3c223c1498e3a412e075e4f921cd78acd3d0fcd6`
- Terasic DE25-Nano v2.12 blob `288baffb283787dbd49729983800ce764243adda`
  (commit `580dcb5c4e10aa7daff2bf69d493c076a6d3e665`)

It emits a fresh private directory under `out/` without mutating a shared
checkout. The contained SiP variant:
- Rejects partial/authentication flags (full configuration uses flag `0`).
- Disables bridges and checks readback before `CANCEL`/`RECONFIG`.
- Propagates asynchronous submission and completion errors, treating
  `MBOX_NO_RESPONSE` without a header as pending and retiring timed-out
  transfers without flushing, replaying or releasing buffers.
- Keeps data bridges closed across `ISDONE` and repeated status polls.
- Preserves CPU-to-SDM relative-address conversion and exposes the local
  experimental capability query.

In the matched `fpgaload` cold profile, `nano_configuration.c` and
`sdm_input_vendor_service.c` / `sdm_input_vendor_mailbox.c` gate preparation
and warm rearm against the boot-pinned uncached DDR arena, verifying whole and
chunk SHA-256/SHA-384 digests, live cold ownership, SMMU/source retirement and
fresh all-master closures before advancing generations.

The BL31 and U-Boot builders accept `--nano-mac-handoff` alongside
`--nano-sdm-source-rearm` for the retained PIO profile. At each SDM retirement
and before the next rearm, EL3 checks the original controller, its queue cursor,
the reset state of MAC0, and the Secure and Non-secure SMMU controls. It reads
the retained Non-secure entries for MAC SID 1 and SDM SID 10. A mismatch
blocks continuation. EL3 also checks the prospective five-page XGMAC layout
at `0x95c00000`. It contains one read/write descriptor page, two transmit pages readable
by the MAC and two receive pages writable by the MAC. The build receipt
records `mac_grant=false` and `translation_published=false`. Physical launcher
qualification requires matching MENU, computer and return observations.

A second option, `--nano-mac-table-trial`, reserves 28 KiB directly above the
SDM table at `0x94010000`. At the same contained retirement boundary EL3
builds and reads back a separate MAC table. It rechecks the CPU, device
owners, queues and both SMMU banks, selects the table base, and restores the
SDM base and verifies its original tree before allowing the native return.
Each successful generation emits `FES_NANO_MAC_TABLE` with its retained
cursor. `FES_NANO_MAC_TABLE_STOP` records the stage of a failed check.
MAC0 stays in reset and translation stays disabled throughout this trial.
[Arm SMMUv3 §3.11](https://documentation-service.arm.com/static/6813b2bbefb0f21122c144c2)

## Controlled Nano bridge service

`nano_bridges.c`, `nano_bridge_sip.c`, `nano_cold.c` and `nano_cold_platform.c`
provide the owned EL3 bridge, reset and device-lease channel:

- **Master stop & drain:** `nano_fabric_stop.c` and `nano_probe_stop.c` verify
  the compiled DDR master inventory, maintenance-held registers and active
  reader/probe receipts. Closing control issues a single `DISABLE` / `CANCEL`
  when active, waits with bounded polls for matching generation/cookie drain
  with zero pending traffic, and rechecks both receipts before completion.
- **Closed-bridge SD lease:** `nano_sd.c` and `nano_sd_platform.c` gate SDHCI
  ownership so storage is opened only while bridges/FPGA configuration are
  contained and must be cleanly stopped before any SDM reconfiguration.
- **Computer, ROM & tape control:** `nano_program.c`, `nano_rom.c` and
  `nano_tape.c` enforce owned control apertures, held-ROM bindings and the
  separate tape arena across MENU ↔ core transitions.
- **Trusted USB PIO handoff:** `nano_usb_platform.c` and `nano_usb_board.c`
  bind USB0 module and packet-RAM/ECC resets (PER0 bits 3 and 11, mask `0x808`)
  and pause/resume/stop generations to the cold boot owner.
- **Optional XGMAC lease:** `nano_mac.c` models the owned MAC/PHY lease;
  Ethernet remains disabled in the active Nano boot profile.

## Board-specific off-board candidate

Requires clean checkouts of the pinned Terasic commits, an AArch64 GNU cross
compiler, and U-Boot host build tools (`setuptools`, `pyelftools`, `swig`,
`dtc`, `bison`, `flex`, OpenSSL headers):

```sh
git clone --depth 1 --branch de25_nano_revA_v1.0 \
  https://github.com/terasic/arm-trusted-firmware.git out/vendor-tfa
git clone --depth 1 --branch de25_nano_revA_v1.0 \
  https://github.com/terasic/u-boot-socfpga.git out/vendor-uboot

FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/firmware/build_terasic.py \
  --reference-repo out/vendor-tfa --cross-prefix /absolute/path/aarch64-linux-gnu- \
  --output out/vendor-bl31

FES_EXPERIMENTAL_DE25_EMBEDDED=1 python3 platform/embedded/firmware/build_terasic_uboot.py \
  --reference-repo out/vendor-uboot --bl31-build out/vendor-bl31 \
  --cross-prefix /absolute/path/aarch64-linux-gnu- --output out/vendor-uboot-build
```

`build_terasic.py` verifies BL31 ELF load segments against vendor bounds.
`build_terasic_uboot.py` verifies the FIT embeds the exact BL31 digest with no
load-address overlap against BL31/BSS. For the full native launcher chain
(`--native-entry-build`, `--native-autoboot`, `../nano/hps_boot.py` and
`../nano/launcher_image.py`), see [../nano/launcher.md](../nano/launcher.md)
and [../entry/README.md](../entry/README.md).

The paired native build may add `--nano-sdm-trng` to a matched
`--nano-sdm-input-transfer` BL31. Before handing control to U-Boot, TF-A
collects four 32-byte responses to the fixed Agilex SDM random-number command
into its reserved private DDR. The standard ARM TRNG service returns those
bytes once and reports exhaustion. The native network profile reserves its
per-boot UUID before the Go runtime takes its separate 32-byte seed.
A busy mailbox or a rejected, short, zero or repeated response leaves the
entropy service unavailable. A dispatched transaction with uncertain completion
halts EL3 before subsequent mailbox users can run.

## Adapter and endpoint

`control.hpp` / `control.cpp` provide an allocation-free, exception/RTTI-free
C++14 CSR control adapter (cross-compilable for AArch64) enforcing two-snapshot
comparison, full 128-bit build-ID checks and generation/cookie-bound
disable/drain. Failed bridge, MMIO or SMC operations retire ownership without
automatic retry or resource reclamation.

## Verification

```sh
python3 platform/embedded/firmware/verify.py \
  --reference out/reference/socfpga_sip_svc.c \
  --cross-cxx /path/to/aarch64-elf-g++ \
  --output out/embedded-control-check
```

Omitting `--reference` fetches and hashes the pinned public TF-A blob; omitting
`--cross-cxx` skips the AArch64 adapter compile while running the full host C
+ Verilator RTL suite. Additional focused verifiers in this directory include
`verify_fabric_stop.py`, `verify_nano_bridges.py`, `verify_owned_sip.py`,
`verify_probe_stop.py` and the C unit tests under `tests/`.
