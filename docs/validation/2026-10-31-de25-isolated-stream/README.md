# DE25 Linux stream with one generated memory master

Producer `68e325201d22952f0eecef65bf335a59f069c560`, based on the contiguous
DMA work at `f63990136`. The explicit `--linux-dma --hps-first` build now removes
the DDR probe instance, control window and memory export, the memory JTAG master
and export, and its external address window. Other JTAG/OCM/F2H paths remain.
The default maintenance build still requires and verifies its diagnostic paths.
There is no production binding, target operation, SD write or QSPI change.

## Actual generated topology

Generation requires exactly one input into the F2SDRAM adapter, from
`subsys_periph_fes_mem_cdc.m0`, and one adapter route into HPS F2SDRAM. It rejects
retained probe/debug instances/windows, duplicate/extra masters, nonzero address
translations or additional hard-HPS memory routes. The HPS's similarly named
clock/reset interfaces are checked separately against their exact 100 MHz/reset
sources. The complete lightweight map remains nonoverlapping: mailbox plus the
256-byte scanout guard, with no probe window. The generated read-only endpoint,
bridge widths/responses, domain resets and fixed SMMU USER/SID path also pass.
[generation.json](generation.json) retains `scanout_memory.master_count = 1` and
the complete map. This is generated topology evidence, not physical isolation
or a Linux IOMMU/domain qualification.

The sole master still uses nonsecure stream 13 with the selected 25-bit USER
path and unchanged lower attributes. Only the recorded HPS `f2sdram_SMMU`
false-to-true parameter change is allowed. All other original HPS/EMIF parameters
and vendor boot data remain checked unchanged. The inactive warm-reset ACK is
preserved: local scanout isolation/drain cannot fence unrelated HPS-origin or
other retained debug paths for global reset.

Input, generation, compilation and phase receipts bind `scanout-only`. The
phase receipt now additionally binds input/generation hashes and the complete
captured Linux-DMA selection, so source identity alone cannot mix Linux and
maintenance artifacts. [linux-programming.json](linux-programming.json) and
[artifact-verification.json](artifact-verification.json) verify every phase hash
and size, all 19 original report hashes and the profile/hash bindings.

## Compilation and generated transport

Quartus Pro 26.3 Build 114 compiles and converts the frozen source successfully
in 494.9 seconds, with a 30-second heartbeat and 7,200-second cap. Compilation
reports zero errors and 310 warnings. It produces the complete SOF, candidate
HPS JIC and Linux core RBF; no programmer is invoked. [build.json](build.json)
and [build-job.json](build-job.json) retain exact inputs and result.

All eight actual generated-interconnect cases pass in 27.4 seconds, including
complete frames/slot reuse, STOP/drain, clock stalls, response errors and the
bounded CPU read with pixel clock stopped while DMA remains outstanding.
The [receipt](interconnect-receipt.json) binds the generated QIP-selected HDL,
bench/helper, wrapper, executable, inputs and generation; the
[case log](interconnect-simulation.log) records every case.
Storage is explicitly **MODELED M20K STORAGE**; the HPS hard block, physical
memory, SMMU translation, firewall and physical CDC are not modeled/qualified.
Post-compile revalidation confirms all 172 simulated source files and 17 QIP
files remain byte-identical and rechecks graph, port, clock/reset and USER/SID
properties. [post-compile-verification.json](post-compile-verification.json)
retains that separate check.

All 74 reported timing rows have nonnegative slack and zero reported TNS;
hold includes rounded 0.000 ns. Original reports still state incomplete setup
and hold constraints. Five of 85 signoff rules fail and no violations are waived.
These are compilation results; timing and hardware acceptance remain unqualified.
Raw timing summary/signoff reports are retained here and all originals in the
independent artifact set.

## Software checks and retained failures

Twenty-three focused HPS cases and 18 scanout cases pass, covering default
requirements, exclusive-profile selection, source drift, extra/missing/direct
masters, stale probe windows and bad/missing clock/reset routes. Generated
consistency passes with 37 consumers and 40 fixture copies.

The broader affected run passes on a separate, unchanged committed snapshot
`523eb5cd24cafd9e56484d8c6b8fb33930209894`: 1,010 parent cases (1 skipped),
1,681 FPGA cases (75 skipped), plus affected harness, whitespace and consumer
checks. [affected-result.json](affected-result.json) and
[affected-job.json](affected-job.json) retain that exact snapshot and command
results; [affected.log.gz](affected.log.gz) retains the raw log. This is not a
claim that the broader suite ran at the final producer head. The subsequent
Tcl/checker corrections are covered by focused cases and the actual complete
68e325201 generation, compilation and transport simulation above.

The first 523eb5cd2 integration attempt fails because Quartus's Tcl interpreter
rejects the string-comparison form used in the export guard. Its failed job and
integration log are retained. The corrected 2e15e148a attempt generates the
isolated graph but fails validation because the new checker also counts the
HPS clock/reset ports as memory routes. Its failed job/output are retained.
The final producer checks data versus timing ports explicitly and rebuilds a
fresh project; neither failed attempt is relabelled as a success.

The complete binaries, all original reports and receipts are retained separately,
read-only, at:

```
/home/deano/fes/out/artifacts/de25-isolated-stream/68e325201d22952f0eecef65bf335a59f069c560/
```

The corresponding remote project remains at
`fes-build-3090:/data/fes-de25-isolated-68e325201/output/`.

## Remaining normal-kit work

The kernel production probe remains closed. The isolated source path is ready
for real device/domain binding work, including ordered guard identity/epoch/fault
checks and retaining MMIO/context/domain/allocation resources during uncertainty.
Unsafe detach, reconfiguration and failed-load/global reset cleanup must be
handled. Firmware/DT pairing, physical translation, frame/drain, clock and boot
acceptance are still required. The prepared custom kernel is not yet booted.
Managed AArch64 runtime/agent/bundle, MENU, and normal ZX81 catalog/session,
persistence, update and recovery acceptance remain incomplete. This record does
not make the DE25 a DE10-equivalent development kit yet.
