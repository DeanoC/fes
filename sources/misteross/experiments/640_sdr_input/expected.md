# 640 dedicated SDR input register

`make sim EXP=640_sdr_input` tests the fabric GPI protocol and the
registered `SDR_IN` capture flop. Simulation does not model analog GPIO
registers or pin delay. `make oss EXP=640_sdr_input` produces compressed
`build/oss/640_sdr_input/top.rbf`. It requires `FAST_INPUT_REGISTER ON`
on `SDR_IN`, one HPS GP, no memory, DSP or PLL, and passing 50 MHz fabric
timing. Quartus comparison is not implemented.

This experiment captures PIN_Y15 through a dedicated flop whose only data
source is that input buffer. nextpnr absorbs the flop into `MISTRAL_SDRIN`
on the existing GPIO BEL, with the result on GPIO `DATAIN.3` and the clock
on GPIO `CLKIN.0`. The flop must have constant `ENA=1`, inactive `ACLR=1`,
`SCLR=0`, `SLOAD=0`, a non-inverted clock and no other consumers of the
input-buffer output. Dynamic controls, inverted clocks, data fanout and
`FAST_INPUT_REGISTER OFF` remain outside this experiment. The probe
observes fabric beat counters, not the pin waveform or input-register
setup/hold. The Mistral timing database has no characterized GPIO
input-register model, so reported fabric Fmax is not input-interface
closure.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0x5E01` |
| GPI [15:9] | 50 MHz beat |
| GPI [8] | Captured `SDR_IN` |
| GPI [7:1] | Copy of the same beat |
| GPI [0] | Copy of the captured bit |
| GPO | Unused |

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
