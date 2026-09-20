# 750 native 18x19 DSP

`make sim EXP=750_dsp18x19` checks two unsigned 18x19 products through a
digital stand-in. `make oss EXP=750_dsp18x19` maps `dsp18x19` to
`MISTRAL_MUL18X19`. Quartus comparison is not implemented.

GPO bits [7:0] are A, [15:8] are B, [23:16] are C, and bit 24 selects the
second product. D is the constant 19'd3. GPI signature `0xD619`. The low
16 bits of GPI are the selected product. The first product is A*B. The
second is C*3.

The kit probe checks both products. It does not measure DSP setup/hold.

| Register bits | Meaning |
| --- | --- |
| GPI [31:16] | Signature `0xD619` |
| GPI [15:0] | Selected 18x19 product |
| GPO [7:0] | A |
| GPO [15:8] | B |
| GPO [23:16] | C |
| GPO [24] | Select C*3 |

Claim the designated kit with `scripts/kit.py session`, load the exact OSS
RBF, and run `hardware/probe.sh`. Never take over another owner.
