# 670 direct altiobuf primitives

`make sim EXP=670_altiobuf` tests the fabric GPI protocol and digital
`altiobuf_in` / `altiobuf_out` / `altiobuf_bidir` stand-ins. Simulation does
not model analog pad delay. `make oss EXP=670_altiobuf` produces compressed
`build/oss/670_altiobuf/top.rbf`. It requires one of each width-one
primitive, one HPS GP, no memory, DSP or PLL, and passing 50 MHz fabric
timing. Quartus comparison is not implemented.

This experiment captures PIN_Y15 through `altiobuf_in`, drives PIN_W15
through `altiobuf_out`, and drives PIN_V16 through `altiobuf_bidir` with
constant OE high. nextpnr folds those cells into the existing
`MISTRAL_IB`, `MISTRAL_OB` and `MISTRAL_IO` BELs. Wider channels, bus
hold, differential mode, output OE and malformed pad connections remain
rejected. The probe observes fabric beat counters, not pad waveforms.
GPIO electrical timing is outside the current Mistral timing model.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xAB01` |
| GPI [15:8] | 50 MHz beat |
| GPI [7:0] | Copy of the same beat |
| GPO | Unused |

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
