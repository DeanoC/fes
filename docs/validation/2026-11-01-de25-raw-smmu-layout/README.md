# DE25 raw F2SDRAM SMMU layout correction

The installed Quartus Pro 26.3 HPS parameter definition establishes the raw
F2SDRAM address USER layout: bits 7:0 preserve USER attributes, bit 8 carries
AxMMUSECSID, and bits 24:9 carry the 16-bit AxMMUSID. This is different from
the ACE5-Lite translator CSR layout used by the earlier implementation.

The primary source is `ip/altera/intel_hps/sm/common/parameters_data.tcl`,
parameter `f2sdram_SMMU`, line 858, SHA-256
`7b7b65f95ab1a70f1bbd1a985893c6425d0366896070ff6160d1ed9eded5591c`
in the installed `/data/altera_pro/26.3` tree on `fes-build-3090`.
The accompanying `common/interface_ports.tcl` declares the raw SMMU extension
as 25 bits; the generated MPFE wrapper forwards all 25 bits to the hard block.
No proprietary vendor file is copied into the repository.

The old word `0x0000de0` decodes as secure stream 6 on that raw port, rather
than the intended nonsecure stream 13. The corrected word is `0x0001ae0`.
The low attributes remain `0xe0`, and the software owner remains SID 13.
Regression checks independently decode both address channels using the raw
port bit positions and reject either old layout constant. The generated
shared constants are regenerated from their canonical YAML source.

This proves a functional stream-identity wiring error. It does not yet prove
that this error was the only cause of the two physical frame failures, or
establish physical translation, DDR access or display acceptance. The old
failed image is not retried. A new source-frozen compiler run, generated
transport validation and artifact verification are required before a bounded
physical frame trial. No SMMU bypass, firewall widening, TCU register probe or
boot-flash update is part of this correction.

The corrected source `41d508610` passed 22 builder tests and the 39 generated /
41 fixture consistency checks. Quartus completed in 483.6 seconds. All 19
recorded report hashes match; all 74 timing-summary slack rows are nonnegative.
Control/video setup are +2.920/+7.323 ns. Four nonzero signoff-summary rules
remain (missing input/output delays, multiple asynchronous reset synchronizers and low-severity reset false path), so this
is still a timing-unqualified diagnostic. The actual generated transport
passed nine modeled-memory cases, including the observed high IOVA. The
rebuilt plain-C client passed 21 native/AArch64 modeled-kernel pairs.

The [physical trial](hardware-result.json) loaded the new RBF through Linux
and received a complete-frame acknowledgement for sequence 1, slot 1. It then
failed the second publication with one pixel underflow (`errors=8`), rather
than the previous AXI-response error (`errors=4`). This is partial progress,
not a sustained scanout pass. The HDMI capture ended before the client began;
its black frames establish no picture acceptance. No second trial was made.

After retaining the failure, a separate single supported owner recovery
verified STOP/drain with the unchanged allocation identity before CLEAR and
release. The [kernel trace](recovery.trace) records the full 8 MiB unmap.
Owned diagnostic files, overlay and trace mounts were cleaned up, and the same
Linux boot, MAC and protected owner device were verified. No power cycle,
QSPI update, SMMU bypass or firewall change was required. The board remains
claimed on its candidate Linux card, with the stopped diagnostic in SRAM.
Next: investigate sustained-reader latency/throughput under concurrent CPU
back-buffer writes, and synchronize the next capture with the client.

Timing-analysis correction: the original short summary filtered rule families
and omitted medium-severity RDC-50003 (two violations). The unchanged compiler
report declares **4 of 85 rules failed**. The corrected analysis checks every
rule family and both declared totals. No waiver or admission changed.
