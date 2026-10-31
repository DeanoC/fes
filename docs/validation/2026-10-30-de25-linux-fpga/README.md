# DE25 Linux FPGA-manager bring-up

This is a physical hardware diagnostic on bottom board TRWU8J8G, using the
approved FES Linux image from source `24fc445a2`. It does not qualify normal
FES package loading or admit the production runtime.

The headless image contains no kernel modules. Four unmodified modules were
extracted from the exact vendor BSP image locked in `image/de25-nano/vendor.lock.toml`:
`stratix10-soc`, `fpga-bridge`, `fpga-region` and `of-fpga-region`.
All are AArch64 ELF modules with the matching vendor kernel release.
They were uploaded to a private temporary directory and loaded explicitly.
The manager registered as `fpga0` and the region as `region0`, with Linux and
SSH remaining responsive in the same boot epoch.

The retained PFG logs show two different operations on
`zx81-jtag-with-spl.sof`. `zx81-core-conversion-full.log` records a successful
`hps_core_only=on` conversion to `out/zx81-with-spl.rbf`; it does not record a
rejected core-only conversion. `hps-split-rejection.log` records a separate
request for `out/split-comparison/zx81.jic` with `hps=on`, which Quartus
rejects with error 19921 because the design is not HPS first. `artifacts.json`
records one SOF hash and one RBF hash without filenames. The retained DTS asks
Linux for `fes-c134-zx81.core.rbf`, a different filename from the successful
PFG output. These records do not establish that the RBF hash matches the bytes
Linux attempted to load.

A minimal target-path overlay requested that core image through the Linux
FPGA manager. The vendor driver reported `timeout waiting for svc layer buffers`.
The overlay close remained blocked in `stratix10_svc_done` -> `kthread_stop`;
the manager state remained `write`. `after-load.log` retains the boot UUID,
manager state and kernel failure. The claimed 55-second duration and follow-up
SSH/UART observations are not present in the retained logs, so this record does
not rely on them. No CPU fabric register access followed this failed load.

This does not yet identify whether the transfer failure comes from the factory
FPGA boot configuration, its firmware pairing, the converted artifact, or the
vendor driver. The service's data-claim polling loop also needs investigation:
its busy-result path does not observe a stop request or an elapsed deadline.
Do not infer successful fabric loading from successful module probing.

The retained `pre-shutdown-cleanup.log` is empty, so it does not verify removal
of uploaded files or restoration of the temporary firmware search path. The
first test's restoration is recorded separately below. Restoration after the
matching-configuration comparison remains pending.

The procedure follows Altera's
[Linux fabric configuration example](https://altera-fpga.github.io/rel-26.1.1/embedded-designs/agilex-5/e-series/premium-065a/fabric-config-linux/ug-fabric-config-linux-agx5e-premium-065a/).
That example uses a prepared HPS-first QSPI image; this experiment used the
existing factory configuration. Those starting states are not equivalent.

## Matching-configuration comparison

The paired programmer log records Quartus reporting success with zero
errors/warnings when loading the known complete ZX81 SOF through the claimed
cable. The retained logs do not include a UART transcript of graceful
power-down or prove that the later boot began with no modules or firmware
search-path override.

The paired state and stack logs record the manager/region registration and a
repeated buffer timeout with blocked cleanup. They do not bind the loaded RBF
bytes to the recorded RBF hash or prove an identical artifact across attempts.
These logs show a repeated failure, but do not isolate boot-state or exact-RBF
equivalence. No CPU fabric registers are accessed. Both failures are tracked
in [#1298](https://github.com/DeanoC/fes/issues/1298).

`shutdown-check.log` contains only an SSH connection-timeout message; it does
not retain timing details or a UART transcript. The reported identity-guard
retry is not evidenced in this directory and is not treated as a configuration
result.

`paired-cleanup.log` and `paired-final-poweroff.log` are empty. They do not
verify file cleanup, firmware-path restoration, a passive `Power down` UART
line, or full physical power removal. The running FPGA configuration still
needs restoration verification.

Altera's [posted polling fix](https://lists.openwall.net/linux-kernel/2026/10/23/612)
addresses the same cleanup failure on a newer driver base. It is an investigation
lead rather than a tested fix for the Terasic kernel. A separate
[Altera firmware erratum](https://community.altera.com/kb/knowledge-base/why-does-linux-fail-to-configure-the-fpga-with-error-arm-smmu-v3-16000000-iommu-/351547)
reports a similar transfer timeout with explicit SMMU faults; those fault lines
were not observed here, so that root cause is not established.

## Supported HPS split rejects the attempted input

A later off-board PFG comparison requests the documented HPS split of the
same complete SOF, using `hps=on`, MT25QU128 and the configured SCS flash loader.
PFG refuses it with error 19921: this design's HPS configuration order is not
HPS first. No JIC is programmed and no hardware is accessed by that comparison.
The successful earlier `hps_core_only=on` conversion therefore did not establish
an input pair supported by the documented Linux flow.

This is a concrete configuration gate for the next build: use the documented
[HPS_INITIALIZATION](https://docs.altera.com/r/docs/683296/25.3/quartus-prime-pro-edition-settings-file-reference-manual/hps_initialization)
assignment with `HPS FIRST`, compile it and require the supported split to pass
before another Linux load. Actual HPS-first boot/transfer still needs physical
verification. Keep the independently reproduced service cleanup hang and its
[recovery candidate](https://github.com/DeanoC/fes/pull/1300) separate
from a claim that the transfer cause is fixed.

## Post-shutdown recovery inspection

A UART transcript for this inspection was not retained. Read-only
`CONFIG_STATUS` on the exact claimed bottom-board cable succeeds without a
helper image. Quartus decodes `STATE=IDLE`, `MSEL=AS_FAST_POR`, `nSTATUS=1`,
`nCONFIG=1`, `CONF_DONE=1` and `INIT_DONE=1`; the raw response and tool decoding
are retained. This proves the SDM responds and reports user mode. It does not
prove Linux restarted, identify a new boot epoch, or verify the factory FPGA
baseline. No fabric CPU registers are accessed.

The installed programmer help confirms `R` means erase. Its `--initcfg` option
applies after programming configuration devices; it does not document a
standalone flash-restart operation. Neither is used for recovery. The
[Agilex 5 RSU operation requirements](https://docs.altera.com/r/docs/813773/25.1/device-configuration-user-guide-agilextm-5-fpgas-and-socs/operation-commands)
restrict RSU commands to an RSU image loaded from QSPI in AS mode. The temporary
JTAG image and factory flash layout have not established that prerequisite.
No RSU command or guessed flash offset is sent. A documented nCONFIG or verified
board-power transition remains the restoration boundary for this state.

The retained evidence does not record USB permission changes/restoration,
UART closure or release of the board claim. The physical power-cycle request
stays pending, and restoration of the running FPGA configuration remains
unverified.
