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
