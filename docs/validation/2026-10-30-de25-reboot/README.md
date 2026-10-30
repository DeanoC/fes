# DE25 cold-reset investigation, 2026-10-30

Issue [#1291](https://github.com/DeanoC/fes/issues/1291) is a boot-source
limitation: FPGA-first JTAG configuration cannot redeliver the HPS first-stage
bootloader after a cold reset. Reloading the complete SOF is the supported
recovery; a physical power cycle is not required to resume the diagnostic.
No reset acknowledgement, bridge fence, constraint or firmware binary is changed.

The bottom board is USB serial `TRWU8J8G`, UID `79B9468337C3A50B`, JTAG ID
`4362c0dd`. Its printed SCS/R1 suffix remains hidden/unconfirmed. Tests preserve
the original factory QSPI and Ubuntu SD, kernel `6.12.11-gd7d192a9ddd9`, root
`/dev/mmcblk0p2` ext3, and strict UART-derived SSH identity. Private network
addresses and credentials remain outside Git.

## Controls and interpretation

1. Factory configuration from QSPI: the ordinary Linux reboot succeeds, prints
   `Reset state: Cold`, boots SPL/ATF/Linux and returns to UART/pinned SSH with
   a changed boot UUID. `/sys/kernel/reboot/mode` is `cold`; the kernel command
   line has no warm-reset override.
2. Unmodified Terasic `golden_top_hps.sof` from the locked resource ZIP, loaded
   over JTAG: initial boot succeeds; the same ordinary Linux cold reboot ends
   at `reboot: Restarting system` with no new SPL/login in the capture window.
   This control contains no FES RAM engine or FES warm-reset acknowledgement
   patch. Loading the identical SOF again restores Ubuntu over UART and SSH,
   without a power cycle or persistent change.
3. The exact RAM-test SOF from [the physical memory record](../2026-10-30-de25-physical-memory/README.md)
   is tested with both local engines idle/calibrated and HPS writes disabled.
   Cold reboot repeats the stall. The same full SOF is reloaded for recovery;
   the resulting UART/SSH and endpoint identities are retained with this record.

The `watchdog0: watchdog did not stop!` line appears in both the successful
factory reboot and stalled JTAG reboot. It is not evidence of a watchdog-specific
root cause. The unmodified vendor control rules out the FES RAM engine and
its deliberately inactive warm-reset acknowledgement as necessary causes of
this observed cold-reset behavior. It does not qualify warm reset or the FES
global drain contract.

Altera explicitly documents the JTAG boot-information limitation and SOF
redelivery in [Agilex 5 configuration debugging, section 10.3.3](https://docs.altera.com/r/docs/813773/26.1/device-configuration-user-guide-agilextm-5-fpgas-and-socs/debugging-guidelines-for-the-jtag-configuration-scheme).
The captured `quartus_pfg -i` output reports
`HPS/FPGA configuration order: HPS_AFTER_INIT_DONE` for the vendor SOF. The
[reset-manager driver guide](https://altera-fpga.github.io/rel-26.1.1/linux-embedded/drivers/reset_manager/reset_manager/)
describes the default Linux cold reset and firmware/SDM path.

The published Terasic ATF source at
[`580dcb5c4e10aa7daff2bf69d493c076a6d3e665`](https://github.com/terasic/arm-trusted-firmware/tree/580dcb5c4e10aa7daff2bf69d493c076a6d3e665)
corroborates that path: `socfpga_system_reset()` calls `mailbox_reset_cold()`,
which sends `REBOOT_HPS` with no argument. Source review is supporting evidence,
not a claim that this checkout reproduces the shipped ATF binary. Factory boot
prints BL31 v2.12.0 and the DE25 revision banner.

## Operator recovery

Keep the normal kit claim and the exact selected image/bootloader identities.
Before intentionally resetting an active diagnostic, complete its memory work
and close all JTAG master claims; never infer global DMA drain from a local
IDLE bit. Normal Linux shutdown/sync remains necessary for a running writable
root filesystem. After the HPS cold reset has stopped the OS, reload the same
verified, bootloader-complete SOF on the explicitly identified cable/device:

```sh
quartus_pgm -c 'DE25-Nano [explicit-claimed-cable]' -m jtag \
  -o 'p;/absolute/verified/diagnostic-with-vendor-spl.sof@1'
```

Capture serial, reacquire any changed Ethernet identity from UART, pin the
public SSH key and verify the new boot UUID plus exact endpoint/build identity.
Reconfiguration starts a new FPGA epoch; discard all old control clients,
and in-memory protocol expectations. It is not a continuation of the old core,
and reconfiguration does not certify global drain or release HPS allocations.
Do not use a raw SOF missing its HPS loader, write flash, change boot settings,
force a reset ACK, or select warm reset as a shortcut. Production HPS DMA,
reset/drain admission and the FES rootfs remain outside this diagnostic.

## Evidence and handoff

[proof.json](proof.json) binds the six observed boot UUIDs, three reset controls,
exact stock/RAM SOF hashes, unchanged reset/DMA policy and no-power-cycle
recovery. [stock-image.json](stock-image.json) binds the unmodified vendor
member to the locked ZIP. [firmware-review.json](firmware-review.json) pins the
supporting published ATF source. The compressed console/SSH/programmer logs
retain the observations; private network tokens are redacted.

Affected planner/metadata/whitespace checks and documentation links pass.
No production module, FPGA RTL, constraints, firmware, shared definition or
persistent boot state changes. Source base: `7634cb82dcbd`. The same owner
retains the board claim for the immediately following authorized ZX81 test;
all reboot-control UART, SSH and JTAG master sessions are closed. This phase
finishes with the recovered RAM diagnostic running; the following phase must
restore the designated factory baseline before final release. The top board
is untouched. No harness, GPU or other owner's process is used.
