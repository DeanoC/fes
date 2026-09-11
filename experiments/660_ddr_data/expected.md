# 660 fabric-data DDR output register

`make sim EXP=660_ddr_data` tests the fabric GPI protocol and a digital
`altddio_out` stand-in. Simulation does not model analog GPIO registers or
pin delay. `make oss EXP=660_ddr_data` produces compressed
`build/oss/660_ddr_data/top.rbf`. It requires one width-one `altddio_out`
with two nonconstant data nets, one HPS GP, no memory, DSP or PLL, and
passing 50 MHz fabric timing. Quartus comparison is not implemented.

This experiment drives PIN_W15 from changing fabric `datain_h` /
`datain_l` on a non-inverted 50 MHz clock. nextpnr packs that cell into
`MISTRAL_DDROUT` on the existing GPIO BEL, with high/low data on GPIO
`DATAOUT.1`/`DATAOUT.0` and the clock on GPIO `CLKOUT.0`. Complementary
constant data remains the separate clock-forwarding experiment. Widths
above one, mixed or missing data, inverted output, dynamic controls and
used `oe_out` remain rejected. The probe observes fabric beat counters,
not the pin waveform or output-register setup/hold. The Mistral timing
database has no characterized GPIO output-register model, so reported
fabric Fmax is not output-interface closure.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xDD03` |
| GPI [15:10] | 50 MHz beat |
| GPI [9] | Fabric high data |
| GPI [8] | Fabric low data |
| GPI [7:2] | Copy of the same beat |
| GPI [1] | Copy of the high data |
| GPI [0] | Copy of the low data |
| GPO | Unused |

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
