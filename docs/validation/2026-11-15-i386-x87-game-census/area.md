# What a reduced x87 would still have to carry, 15 November 2026

The area half of the [x87 game census](summary.md). It asks one question:
**of what slices 2 and 3 cost, how much is proportional to the significand's
width?** A reduced-precision unit saves only that part. Everything else it
pays in full.

Every figure here is measured, from the two slices' own per-entity fits.
Nothing is estimated, and where the granularity cannot separate
width-proportional logic from case-count logic, this record says so instead
of apportioning it.

## Sources

| Figure | From | Flow |
| --- | --- | --- |
| Slice 2, identity on against off, per entity | [slice 2 record](../2026-11-14-i386-x87-s2/summary.md), "Where the ALMs go" | the #1584 method: the sealer's Yosys program without `flatten`, Quartus 17.0.2 seed 1, head `bf581e303` |
| Slice 2, inside the unit, by ablation | the same record's ablation table | outputs forced constant so the driver chain trims; standalone `unit-noregs` |
| Slice 3, this branch against its merge base, per entity | PR [#1879](https://github.com/DeanoC/fes/pull/1879) at `3dd62f1e2203d4b627310be0826189100ff205cb`, "Where the ALMs go" | the same method, same CUDA slot and seed for both sides |

Slice 3's record is on an unmerged branch and is cited at that SHA. It is
not merged into this one.

## The two slices' cost

| | ALMs | What it is |
| --- | ---: | --- |
| Slice 2, `fes_i386_x87` own logic | 1,414 | the state, the interface, the conversions |
| Slice 2, `fes_i386_x87_mul` | 178 | the nine-DSP multiply, used as the shifter |
| Slice 2, CPU side (`fes_i386_cpu` own) | 143 | issue and hold conditions, the words in the environment and save layouts, the `arch_x87_*` readout |
| **Slice 2 total** | **1,735** | (the flattened in-machine delta is +1,608, this flow's +1,641; the three differ by the flow's spread) |
| Slice 3, inside `fes_i386_x87` | **+2,250** | add, subtract, multiply, compare, round: the alignment and normalisation paths, the PC-aware rounder, operand classification and the NaN rules, the exception sequencing and the sequencer |
| Slice 3, `fes_i386_x87_mul` | +2 | unchanged: the arithmetic bought its multiplies on slice 2's multiplier |
| Slice 3, the decoder | ±15, sign not established | the slice 3 record retracts its own direction: a matched re-run flips it |

So the unit is about **3,850 ALMs** of its own logic after slice 3, plus
the 178-ALM multiplier and the 143 ALMs on the CPU side.

## What is measured not to shrink

Slice 2's ablation forced the register stack's write data and the value the
CPU stores to constants, so their driver chains trimmed. What survived both
is the floor:

| Fit | `fes_i386_x87` own | registers | M10K | `_mul` | DSP |
| --- | ---: | ---: | ---: | ---: | ---: |
| as built | 1,491.7 | 1,248 | 4 | 179.4 | 9 |
| register-stack writes forced constant | 1,062.8 | 1,051 | 2 | 179.1 | 9 |
| the stored value forced constant | 1,405.5 | 1,262 | 4 | 187.4 | 9 |
| both | **791.1** | 958 | 2 | **176.1** | 9 |

**The floor is 967 ALMs** (791 of the unit and 176 of the multiplier) and
slice 2's record names what it is: the eight registers' control, the tag,
class, control and status words, TOP, the BUSY# latch and the board's error
path, the issue, hold and no-wait interlocks, the sequencer — **and the
conversion machinery itself**, because the exception flags IE, DE, OE, UE
and PE are derived from it and they drive ES, ERROR# and IRQ 13. A unit that
reports exceptions cannot gate its arithmetic away.

None of that is width-proportional:

- The **words** are architectural. The control word is 16 bits whatever the
  datapath rounds to; so are the status word, the tag word and the opcode
  register. The pointers are 32 and 16 bits because the 386's addresses are.
- The **exception path** is case-count: six flags, their masks, the summary
  bit, ES, the IRQ 13 latch and the F0h and F1h decode. A narrower
  significand changes none of them, and it does not remove one of the six.
- The **interlocks and the sequencer** count forms and states, not bits.
- The **register stack's control** is eight entries and three pointers
  regardless. The stack's *storage* narrows — see below — but the storage is
  M10K, not ALMs.
- The **exponent path does not narrow at all.** This is the point most
  easily missed and it is worth stating on its own: precision control
  narrows the **significand** to 24 or 53 bits and leaves the exponent
  15-bit extended at every setting. A 24-bit-significand x87 is therefore
  **not** an fp32 unit: it still carries the 15-bit exponent arithmetic, the
  ±16383 bias, the overflow and underflow thresholds at extended range and
  the denormal handling that goes with them. The only way to shrink the
  exponent path is to build something that is **not** an x87 at any
  precision control, and that diverges from a 387 on operands it is
  supposed to handle.

Against slice 2's 1,735, the measured non-width-proportional part is
**967 + 143 = 1,110 ALMs, or 64%**, taking the CPU side as architectural
too, which it is: issue, hold, the save and environment layouts and the
readout are the same whatever the datapath's width.

## What might shrink, and the bound on it

The rest of slice 2 — **about 700 ALMs** (1,491.7 − 791.1, which the record
breaks down as the load result path 429, the store-only logic 86 and 186
shared between the two directions) — is the conversion machinery's result
paths. Some of that is width: a 24-bit rounder is narrower than a 64-bit
one, and the alignment shift's control is smaller. Some of it is not:
unpacking and classifying an operand (zero, denormal, pseudo-denormal,
unnormal, infinity, quiet and signalling NaN) is case-count, and the
conversions to and from m32, m64, m80, int16, int32, int64 and BCD are the
**same seven formats** whatever the internal significand, because they are
the formats memory holds.

Slice 3's +2,250 is the same mixture and the record names it as such:
alignment and normalisation (width), the PC-aware rounder (width), operand
classification and the NaN rules (case-count), exception sequencing and the
sequencer (case-count).

**Neither can be split further from the figures that exist.** The
per-entity flow attributes area to `fes_i386_x87` as one entity, and the
ablation method cuts at an output, not at a datapath width. Separating the
width-proportional part would need its own ablation fits — a unit built with
a 24-bit significand, fitted the same way — and this record runs no fits.

So the honest statement, and it is the one the record makes:

- **Measured: at least 1,110 of slice 2's 1,735 ALMs (64%) do not shrink
  with the significand**, and the exponent path inside the remainder does
  not shrink either.
- **Bounded: at most about 700 ALMs of slice 2 and some part of slice 3's
  2,250 are width-related**, and "some part" is as far as the evidence goes.
- **Not established: the actual saving a 24-bit-significand unit would
  make.** It cannot be separated from these figures without a fit of such a
  unit. That is a measurement nobody should take before the coverage
  question is settled, because if the corpus needs 53 and 64 bits the saving
  is irrelevant.

The M10K side is the one place a narrower significand clearly pays: the
register stack is four M10K blocks because two copies of eight 80-bit
registers need two blocks each at the 40-bit port limit. A 32-bit register
would fit two copies in two blocks. M10K is not the binding constraint —
the slice 3 seal leaves 2,228 ALMs and far more M10K — so this saves the
resource that is not short.
