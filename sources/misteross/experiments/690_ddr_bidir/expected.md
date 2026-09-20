# 690 DDR bidirectional I/O register

`make sim EXP=690_ddr_bidir` tests the fabric GPI protocol and a digital
`altddio_bidir` stand-in. Simulation does not model analog GPIO registers or
pin delay. `make oss EXP=690_ddr_bidir` produces compressed
`build/oss/690_ddr_bidir/top.rbf`. It requires one width-one `altddio_bidir`
with two nonconstant data nets, connected `dataout_h`/`dataout_l`/`combout`,
one HPS GP, no memory, DSP or PLL, and passing 50 MHz fabric timing.
Quartus comparison is not implemented.

This experiment drives PIN_W15 through a width-one `altddio_bidir` with
changing fabric `datain_h` / `datain_l` and a constant-high output enable
on a shared non-inverted 50 MHz clock. nextpnr packs that cell into
`MISTRAL_DDRBIDIR` on the existing GPIO BEL. High/low output data use GPIO
`DATAOUT.1`/`DATAOUT.0`, registered inputs use `DATAIN.3`/`DATAIN.2`,
`combout` uses `DATAIN.0`, and the clock uses `CLKOUT.0` and `CLKIN.0`.
Widths above one, mixed or missing data, inverted clocks, dynamic set/clear
and used `oe_out` remain rejected. The probe observes fabric beat counters,
not the pin waveform or bidirectional-register setup/hold. The Mistral
timing database has no characterized bidirectional GPIO model, so reported
fabric Fmax is not interface-timing closure.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xDD04` |
| GPI [15:8] | 50 MHz beat |
| GPI [7:0] | Copy of the same beat |
| GPO | Unused |

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
