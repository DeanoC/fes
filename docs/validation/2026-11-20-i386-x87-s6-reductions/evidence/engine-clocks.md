# The engine's clocks per instruction, before and after the one-stage fast lane

The engine bench's own vectors (`test_fes_x87_trans_engine.py`, seed 1958, 2,000 a class) split by
instruction and run on two builds of `x87_trans_tb_top`: before is `b4213b4d6` (FBLD on the engine, the fast
lane pipelined), after is `d74d8d419` (the fast lane one registered stage). Clocks are from `start` to
`done`, the engine alone; both builds pass every vector against the model.

| Instruction | Vectors | Before: mean | Before: most | After: mean | After: most | Mean change |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| FSIN | 2,000 | 283.6 | 552 | 199.7 | 402 | -84.0 |
| FCOS | 2,000 | 281.2 | 552 | 198.1 | 402 | -83.1 |
| FSINCOS | 2,000 | 390.3 | 668 | 267.7 | 476 | -122.6 |
| FPTAN | 2,000 | 435.1 | 735 | 312.8 | 543 | -122.3 |
| F2XM1 | 2,000 | 211.7 | 416 | 136.8 | 260 | -75.0 |
| FYL2X | 1,995 | 515.5 | 694 | 334.1 | 448 | -181.4 |
| FYL2XP1 | 2,000 | 552.1 | 755 | 358.0 | 491 | -194.2 |
| FPATAN | 2,000 | 323.6 | 463 | 245.9 | 361 | -77.8 |
| FPREM | 1,200 | 69.4 | 147 | 69.4 | 147 | +0.0 |
| FPREM1 | 1,200 | 71.4 | 144 | 71.4 | 144 | +0.0 |
| FRNDINT | 3,200 | 42.0 | 60 | 33.0 | 48 | -9.0 |
| FSCALE | 800 | 14.0 | 14 | 14.0 | 14 | +0.0 |
| FXTRACT | 600 | 22.4 | 29 | 22.4 | 29 | +0.0 |
| FBLD | 800 | 265.0 | 265 | 163.0 | 163 | -102.0 |
