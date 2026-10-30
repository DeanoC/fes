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

Quartus 26.3 rejected core-only RBF conversion of the raw SOF. Conversion of
the previously qualified bootloader-complete SOF succeeded, without warnings,
using `quartus_pfg -c INPUT.sof OUTPUT.rbf -o hps_core_only=on`.
The retained logs and artifact hashes identify both inputs and output.

A minimal target-path overlay requested that core image through the Linux
FPGA manager. The vendor driver reported `timeout waiting for svc layer buffers`.
The overlay close remained blocked in `stratix10_svc_done` -> `kthread_stop`;
the manager state remained `write`. The owned SSH client timed out after 55s.
A fresh SSH connection and UART login prompt confirmed Linux remained alive.
No CPU fabric register access followed this failed load.

This does not yet identify whether the transfer failure comes from the factory
FPGA boot configuration, its firmware pairing, the converted artifact, or the
vendor driver. The service's data-claim polling loop also needs investigation:
its busy-result path does not observe a stop request or an elapsed deadline.
Do not infer successful fabric loading from successful module probing.

Before shutdown, the temporary firmware search path was cleared and all uploaded
modules, overlay and bitstream files removed. No boot configuration, SD image,
QSPI, managed software, or other board was modified. A physical baseline
restoration passed after the first test. A second restoration is pending after
the matching-configuration comparison below.

The procedure follows Altera's
[Linux fabric configuration example](https://altera-fpga.github.io/rel-26.1.1/embedded-designs/agilex-5/e-series/premium-065a/fabric-config-linux/ug-fabric-config-linux-agx5e-premium-065a/).
That example uses a prepared HPS-first QSPI image; this experiment used the
existing factory configuration. Those starting states are not equivalent.

## Matching-configuration comparison

The first physical power cycle restored the approved FES Linux image and
factory FPGA SLD identity. Linux had no loaded modules or firmware search-path
override. After observed graceful power-down, the known complete ZX81 SOF was
loaded through the exact claimed cable. Quartus reports success with zero
errors/warnings; the same FES card boots with the matching configuration.

The same four modules register the manager and region. The same core RBF and
overlay produce the same buffer timeout and blocked cleanup. Linux and SSH
remain responsive with the new boot UUID. This rules out a simple distinction
between these two starting configurations; it does not qualify either for
Linux core loading. No CPU fabric registers are accessed. Both failures are
tracked in [#1298](https://github.com/DeanoC/fes/issues/1298).

The initial SSH connection after the second boot timed out; a later connection
passed the physical-UART-derived identity guard before modules were loaded.
This startup failure is retained privately; it is not a configuration result.

Temporary uploaded files and the firmware search override were removed again.
The final passive UART capture observes `Power down` before requesting a full
physical power removal. Factory QSPI and the approved SD image remain intact;
restoration of the running FPGA configuration must still be verified.

Altera's [posted polling fix](https://lists.openwall.net/linux-kernel/2026/10/23/612)
addresses the same cleanup failure on a newer driver base. It is an investigation
lead rather than a tested fix for the Terasic kernel. A separate
[Altera firmware erratum](https://community.altera.com/kb/knowledge-base/why-does-linux-fail-to-configure-the-fpga-with-error-arm-smmu-v3-16000000-iommu-/351547)
reports a similar transfer timeout with explicit SMMU faults; those fault lines
were not observed here, so that root cause is not established.
