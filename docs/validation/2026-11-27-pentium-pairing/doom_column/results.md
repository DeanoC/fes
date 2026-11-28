# DOOM v1.9 renderer loops

The restricted second integer slot accepts **one pair in a full pass through the 15-instruction column loop** and **zero pairs in the 16-instruction span loop**. Each path includes its conditional loop-back branch. Both use the executable's actual 32-bit register operands and instruction encodings. No adjacent pair crosses either branch.

| Loop start in the H2 profile | Instructions | Eligible pairs inside one pass | Historical 486DX H2 site counts |
| --- | ---: | ---: | --- |
| Column, `0018bdf8` | 15 | 1 | All 15 starts appear. They account for 7,874,428 of 28,333,925 retirements (27.79%). |
| Span, `0018c018` | 16 | 0 | Three starts appear. Those three account for 864,975 retirements. |

The one pair is `mov al,[esi+ecx]` followed by `add edi,160` at `0018be13` and `0018be16`. The historical H2 counters give these addresses 524,961 and 524,960 retirements. **At most 524,960 opportunities could come from this edge** in that profile if its runtime instruction forms match. At one clock saved per opportunity, that would recover at most 1.05% of the 50,000,000 H2 clocks from this edge alone. Interrupts, faults, interlocks and memory delays can reduce the gain. H2 records site marginals, so the number that actually retired consecutively is unknown. Entry and exit pairs, other code, and whole-demo speed require a retirement stream. [486DX H2 profile](https://github.com/DeanoC/fes/blob/ebe9d522eee31beb1ddc25be5ea9a6662e08e060/docs/validation/2026-11-24-i486dx-s1/cpi/dx-doom-h2.json)

## Inputs and reproduction

The binary is the original shareware DOS v1.9 executable in Phil's DOS Benchmark Pack. Its SHA-256 is `b8020523561a5ad9706e009a52d61c578f37faafd85ac471962308406292ce27`. That matches the prefix FES recorded for the shareware run and the independently published full hash. The separate, registered-game 486DX H2 run identifies v1.9 and the census disk. All 15 column starts and the three reported span starts match the decoded layout and recorded instruction classes at a common relocation. That comparison corroborates the blocks. The published H2 profile supplies neither the entire registered executable's hash nor runtime bytes. The classifier ignores immediate and displacement values, including the column loop's patched texture-step immediates. [Benchmark executable](https://github.com/SaxonRah/blitzBUS/blob/58dc068b0e76aac47c3bb6cef2c877f6473b446c/dos_bench/MARKS/DOOMS/DOOM.EXE) · [FES shareware provenance](https://github.com/DeanoC/fes/blob/ebe9d522eee31beb1ddc25be5ea9a6662e08e060/docs/validation/2026-11-04-i386-phase4-profile/summary.md) · [Independent file hashes](https://github.com/SkiltonUSA/ZZDosBox-ARM-68K/blob/c4afb45cbf66d7c47f949428aef4585814d7b7f2/amiga/records/2026-10-13/dosbox-doom/installed-files.json)

Run from the validation directory with Python 3, GNU objdump, g++, and local copies of the two linked inputs.

```sh
python doom_column/measure.py /path/to/DOOM.EXE /path/to/dx-doom-h2.json
```

The script verifies both file hashes, disassembles the 51 bytes at file offset `5de0c` and the 64 bytes at `5e02c`, checks both backward branches, and sends each ordered block through the same greedy classifier used for the Quake path. The profile SHA-256 is `c2c889cde72e78c2843d88a58a957967b11dcf6c82df717a796725b337d8c34e`. The classifier header used here has SHA-256 `5b699fb100b472f97d9bb4cad600d5762bcd23202f77dc1872519a6e67e5b676`.
