# 650 dedicated DDR input register

`make sim EXP=650_ddr_input` tests the fabric GPI protocol and a digital
`altddio_in` stand-in. Simulation does not model analog GPIO registers or
pin delay. `make oss EXP=650_ddr_input` produces compressed
`build/oss/650_ddr_input/top.rbf`. It requires one width-one `altddio_in`,
one HPS GP, no memory, DSP or PLL, and passing 50 MHz fabric timing.
Quartus comparison is not implemented.

This experiment captures PIN_Y15 through complementary `dataout_h` /
`dataout_l` on a non-inverted 50 MHz clock. nextpnr packs that cell into
`MISTRAL_DDRIN` on the existing GPIO BEL, with high/low results on GPIO
`DATAIN.3`/`DATAIN.2` and the clock on GPIO `CLKIN.0`. Widths above one,
inverted clocks, dynamic controls, indirect data paths and output aliasing
remain rejected. The probe observes fabric beat counters, not the pin
waveform or input-register setup/hold. The Mistral timing database has no
characterized GPIO input-register model, so reported fabric Fmax is not
input-interface closure.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xDD02` |
| GPI [15:10] | 50 MHz beat |
| GPI [9] | Captured high sample |
| GPI [8] | Captured low sample |
| GPI [7:2] | Copy of the same beat |
| GPI [1] | Copy of the high sample |
| GPI [0] | Copy of the low sample |
| GPO | Unused |

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
