# 620 dedicated DDR clock forwarding

`make sim EXP=620_ddr_clock` tests the fabric GPI protocol and a digital
stand-in that copies the 50 MHz reference onto `DDR_OUT`. Simulation does
not model analog DDR registers or pin delay. `make oss EXP=620_ddr_clock`
produces compressed `build/oss/620_ddr_clock/top.rbf`. It requires one
width-one `altddio_out`, one HPS GP, no memory, DSP or PLL, and passing
50 MHz timing. Quartus comparison is not implemented.

This experiment forwards PIN_V11 50 MHz through complementary constant
`datain_h=1` / `datain_l=0` onto PIN_W15 (`DDR_OUT`). nextpnr packs that
cell into `MISTRAL_DDROUT` on the existing GPIO BEL. Fabric DDR data,
widths above one, inverted polarity, PLL sources and dynamic controls
remain rejected. The probe observes fabric beat counters, not the pin
waveform, duty or external timing.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xDD01` |
| GPI [15:8] | 50 MHz beat |
| GPI [7:0] | Copy of the same beat |
| GPO | Unused |

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
