# i386 interrupts between a REP string's elements

Dated record, 2026-10-28. Host simulation and routed diagnostics on Powerboat.
No package, RBF or kit.

This slice takes INTR and NMI between a REP string's elements, the follow-up
[the interrupts record](2026-10-27-i386-interrupts.md) left: there, a REP
string ran to its end before an interrupt was taken. It is stacked on the
interrupts slice.

Run data: [`2026-10-28-i386-rep-interrupts.jsonl`](2026-10-28-i386-rep-interrupts.jsonl).
- The checks are of `3b30021d39`. The routes are of `eeb56de165`, whose RTL
  is the head's.
- The before figures are the interrupts record's routes of `6df0ef6d7d`,
  whose RTL is this branch's base, `3fb5ddce36`'s, with the same tools.
- Three more nextpnr sets are in the run data: a variant of `eeb56de165`
  with the boundary registered (`variant-registered-boundary`), and the
  base and the head with a neutral edit (`control-*-neutral-edit`). See
  Result.
- Eight nextpnr seeds. A Quartus row's `fingerprint` is the first 12 hex
  digits of the SHA-256 of its `sources` map, as JSON with sorted keys.

## How it runs

- **The boundary.** An element's last uop, the one that steps ECX
  (`str_cx`), is an interrupt boundary, as an instruction's last uop is.
  That is one term in WB's `at_boundary`. The rest is the interrupts
  slice's: the same IF, shadow and NMI block, the INTA wait, then WB
  flushes as for a fault.
- **The string runs on.** WB moves EIP only as an instruction ends, so the
  routine pushes the string's own CS:IP, its prefixes included. The
  elements done have stepped ESI, EDI and ECX at WB, so IRET returns to the
  string, which starts again from there. The uop in WB, the next element's,
  neither retires, stores nor makes an I/O access, as before.
- **Bochs** looks for an event after each iteration but the last, after
  stepping the count, and for REPE and REPNE after their ZF test. So an
  element that ends the string, by its count or by ZF, ends the instruction
  first, as here.
- **The shadow before a string** ends with its first element. Bochs counts
  each iteration as an instruction, so an STI, MOV SS or POP SS shadow is
  over by then. The RTL agrees without a change: the shadow is only ever
  the boundary of the instruction that arms it.
- **RF.** Bochs sets RF as it leaves a REP between iterations. A real-mode
  delivery pushes a 16-bit FLAGS image and then clears RF, so nothing sees
  it.

## The oracle hook

`Oracle::set_iteration_hook()` names a function the oracle calls after each
iteration of a REP string a step runs, with the iterations the step has run.
The Bochs adapter calls it from `BX_INSTR_REPEAT_ITERATION`, which Bochs
calls before it steps the count. A line the hook raises is taken after that
iteration unless it is the last. The hook needs a rebuild of Bochs's CPU
library, since `bochs/instrument.h` changes; the build script keys that on
the header already.

## The testbench

- **The RTL steps first,** then the oracle, so the oracle can raise what the
  RTL raised part way through a string at the same element.
- **Elements.** The testbench counts the RTL's elements by ECX, which each
  steps at WB, before each cycle. A line raised as element m has retired is
  sampled as element m + 1 retires.
- **The oracle follows.** Its hook raises each line the RTL raised after m
  elements after iteration m + 1. A line raised before the RTL's step is
  m = 0: after the first iteration if the step is a REP string, which is
  where the RTL takes it now. A line the step never reaches (not a string,
  a count of 0 or 1, an element that faults first) the oracle raises
  before its next step, as before.
- **What is raised.** A REP string of more than one element, by the count
  CX or ECX holds as it starts, raises INTR after one of its first 16
  elements one time in two, and NMI one in eight (never on two steps in a
  row), besides what every step raises.
- **The generator** puts CLI and STI, MOV SS, or POP SS just before a REP
  string now and then, so a line raised at the first meets the string.
- **The runs report** the interrupts taken between elements
  (`rep_interrupts`).

## Checks

| Check | RTL | Yosys `238eac9a49` netlist |
| --- | --- | --- |
| Lockstep corpus, every in-scope state, to its HLT | 1,634,099 pass (889 files) | the same |
| Lockstep random, 16 seeds × 2,000 programs, latency on | 32,000 pass; 1,164,086 INTRs and 462,433 NMIs taken, 12,665 of them between a REP string's elements; 373 stop at an interrupt through a vector they rewrote | the same |
| Directed cases | 16 of 16 | the same |
| Latency-free reruns (seeds 1, 2) | trace unchanged, interrupts included | the same |
| Unbuilt encodings stop | all 57,568 | the same |

**No cycle changed when no interrupt comes.** The new RTL, built with the
I/O slice's testbench, which raises none, gives the same latency-free
summaries as this branch's base on every file and program: cycles, every
gap histogram, the trace hash and the retirements.

| Run, latency-free | Retirements | Cycles | Summaries that differ |
| --- | --- | --- | --- |
| Corpus, 889 files, 1,634,099 states | 3,268,201 | 35,973,754 | 0 |
| Random, 16 seeds × 2,000 programs | 10,969,144 | 60,280,916 | 0 |

**Mutations**, each on 20 MOVS corpus states and 2 × 400 random programs,
of `eeb56de165`:

| Mutation | Programs that fail |
| --- | --- |
| No interrupt between elements (the interrupts slice's boundary) | 192 |
| Every uop's commit is a boundary, inside any instruction | 596 |
| NMI is not taken between elements (INTR is) | 50 |
| The element an interrupt follows does not step ESI and EDI | 192 |
| ... or its store is lost | 69 |
| The shadow after STI, MOV SS or POP SS lasts the whole string | 82 |

The first smoke run found one testbench bug, fixed before these runs: a
plan of 0 elements, meaning none, raised a line before the first element
(the RTL then took a stray NMI the oracle did not).

**`scripts/test_changed.py`** passes on `3b30021d39`: 7 commands, the parent
and FPGA lanes. The planner finds no core that consumes the i386 RTL yet.

## Result

| nextpnr `bd7a1c0d` (HIP, gpu-run) | Before (`6df0ef6d7d`) | Mid-REP (`eeb56de165`) | Boundary registered |
| --- | --- | --- | --- |
| Seeds 1-8 | 63.21, 65.26, 64.50, 62.70, 63.97, 67.07, 63.84, 64.06 | 61.73, 62.37, 61.57, 62.04, 59.48, 59.71, 58.28, 59.00 | 62.79, 62.34, 60.49, 62.53, 62.76, 59.90, 58.58, 59.82 |
| Average | 64.3 | 60.5 | 61.2 |
| Limited by | EX ×5, D ×3 | D ×4, EX ×3, DC | EX ×6, D, DC |
| Q, DC, D, AG averages | 69.8, 69.6, 65.4, 69.7 | 68.8, 64.1, 61.2, 65.9 | 67.2, 63.4, 63.1, 65.4 |
| M2, EX, WB averages | 69.8, 64.6, 72.6 | 69.4, 63.9, 70.5 | 69.6, 61.7, 68.4 |

| Quartus 17.0.2, DSPs in logic (oracle) | Before | Mid-REP |
| --- | --- | --- |
| Seeds 1-4 | 91.63, 90.22, 89.66, 88.07 | 86.11, 85.42, 89.14, 91.22 |
| Average | 89.9 | 88.0 |
| Limited by | EX ×4 | EX ×4 |
| Q, DC, D, EX averages | 95.6, 98.3, 96.0, 89.9 | 95.4, 96.4, 97.7, 88.0 |

**nextpnr is 3.8 MHz lower, and this record does not show why.** What is
known:
- The change is one input to WB's `at_boundary`, which feeds only WB's
  interrupt registers (`int_take`, `int_wait`, `io_wait`'s load, the NMI and
  shadow state). No flip-flop is added; the netlist is 110 ALUTs smaller.
- Every stage is lower, not one. The D paths that limit run D's issue loop
  (`dc` → `dfwd.base_value` → `mkind` → `rom_ok` → `dfwd.advance` → the
  string engine's copies), about 11 of their 16 ns routing, and none passes
  through `at_boundary`. Each stage's netlist depth is the base's (D 11
  LUTs, WB 8).
- Quartus shows no such loss: D 97.7 against 96.0, DC 96.4 against 98.3,
  and its limit is still the multiplier in logic, as in the I/O and
  interrupts records.
- Registering the boundary as the uop enters WB (`wb_bound`, end of
  instruction or `str_cx`), so that `at_boundary` has the base's inputs,
  routes at 61.2: no better.
- A neutral edit (a comment line, `at_boundary` renamed) gives Yosys the
  same netlist structure, so it is no control: the head's placements come
  back identical, and the base's but its seed 1 (64.09 against 63.21).

So two netlists of this slice, built two ways, route 3-4 MHz below the
base's on nextpnr, while Quartus does not move. That points at nextpnr's
placement of the new netlists rather than at a path the change adds, but
nothing here proves it. The interrupts record's first two routes (61.3 and
61.4) were D-limited through terms on the freeze and on D's issue; this
change adds neither.

## Size

| | Before | Mid-REP |
| --- | --- | --- |
| Yosys ALUTs | 18,532 | 18,422 |
| Yosys flip-flops | 9,826 | 9,826 |
| Quartus ALMs | 11,269-11,314 | 11,243-11,307 |

## What the design doc should say

This is reported for the doc's owner to write; the doc is unchanged here.

- **The sampling point** includes the edge a REP string element's last uop
  retires, with the elements done kept and the string's own CS:IP pushed.
- **The shadow before a REP string** ends with its first element, as Bochs
  keeps it (an iteration counts as an instruction). No check has compared
  that with a 386EX.
