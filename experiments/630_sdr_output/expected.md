# 630 dedicated SDR output register

`make sim EXP=630_sdr_output` tests the fabric GPI protocol and the
registered `SDR_OUT` flop. Simulation does not model analog GPIO
registers or pin delay. `make oss EXP=630_sdr_output` produces compressed
`build/oss/630_sdr_output/top.rbf`. It requires `FAST_OUTPUT_REGISTER ON`
on `SDR_OUT`, one HPS GP, no memory, DSP or PLL, and passing 50 MHz fabric
timing. Quartus comparison is not implemented.

This experiment registers `beat[7]` through a dedicated flop onto PIN_W15.
nextpnr absorbs that flop into `MISTRAL_SDROUT` on the existing GPIO BEL.
The flop must have constant `ENA=1`, inactive `ACLR=1`, `SCLR=0`,
`SLOAD=0`, one Q consumer and a non-inverted clock. Dynamic controls,
inverted clocks, Q fanout and `FAST_OUTPUT_REGISTER OFF` remain outside
this experiment. The probe observes fabric beat counters, not the pin
waveform or clock-to-pad delay. The Mistral timing database has no
characterized GPIO-register model, so reported fabric Fmax is not
external-interface closure.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0x5D01` |
| GPI [15:8] | 50 MHz beat |
| GPI [7:0] | Copy of the same beat |
| GPO | Unused |

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
