# i386 Phase 4 slice 3: the return stack, 6 November 2026

This record covers CPU Phase 4's slice 3
([`i386-cpu-design.md`, "Phase 4: speed"](../../../sources/misteross/docs/i386-cpu-design.md#phase-4-speed),
tracker [#1488](https://github.com/DeanoC/fes/issues/1488)). It adds a
sixteen-entry return stack to `fes_i386_cpu`, so a near RET (C3h, C2h) no
longer pays the full redirect. Every "before" figure comes from
[slice 2's BTB record](../2026-11-06-i386-phase4-btb/summary.md).

**Result:**
- **Kit A, max.**
  - DOOM v1.9 `-timedemo demo3`: 38.7 fps, up from 37.5 (+3.3%). The plan
    projected about 3%.
  - 3DBench: 76.9, up from 71.4 (+7.7%).
  - PCPBench: 13.0, up from 12.8.
  - TopBench: 307, up from 303.
  - The DOS Benchmark Pack's DOOM: 34.9 fps at maximum detail, up from
    33.7, and 96.9 at minimum, up from 92.8.
- **Microbenchmarks.** A CALL rel and RET pair takes 13.0 clocks, down from
  22.3. A CALL r/m and RET pair takes 20.3, down from 29.3.
- **Profile.** In the DOOM `demo3` bench window, CPI falls from 1.81 to
  1.74. A near RET costs 2.55 clocks to its target's retirement, down from
  12.15, and 94.1% of near RETs retire without a redirect.
- **Lockstep.** It passes as main does, on the RTL and on the Quartus
  build's Yosys netlist. The one failure is
  [#1510](https://github.com/DeanoC/fes/issues/1510), as on main.
- **Seal.** Quartus 17.0.2 meets every analysis. Setup slack is +1.020 ns,
  where slice 2's seal had +0.800. Hold slack is +0.104 ns.

## Design as built

[Prediction](../../../sources/misteross/docs/i386-cpu-design.md#prediction)
has the full description. In short:
- **Stack.** 16 linear return addresses in flip-flops and a top pointer, in
  the front end (`fes_i386_ras`).
- **BTB entries.** The BTB keeps an entry for every near RET and CALL rel,
  with a bit for each kind. An entry grows from 60 to 62 bits.
- **Fetch.** When F2 passes on a block whose chosen entry is a CALL rel's,
  the stack pushes the address after the call's last byte. When it is a
  RET's, F1 goes to the top, which the stack pops. Q goes on at the
  target's offset as for any predicted branch.
- **The check.** D gives the instruction after a predicted RET the target's
  linear address less CS.base as its EIP. EX compares the EIP the RET pops
  with it. A RET that was not predicted, or whose target differs,
  redirects to the popped target as a mispredicted branch does.
  - Slice 2's fetch check cannot catch a wrong return target. The next
    instruction's EIP comes from the address fetch went to, so it passes
    by construction. EX's compare is the check for the target.
  - The fetch check still covers a RET entry where no RET ends
    (`ras-store` below).
- **Recovery.** WB keeps the committed pointer: near CALLs committed (CALL
  rel and CALL r/m) less near RETs, modulo 16.
  - Every redirect sets the front end's pointer to that count.
  - A near CALL the front end did not push always redirects at WB, and WB
    writes its return address then. So a RET after a CALL r/m predicts,
    though slice 5 has yet to predict the CALL.
  - Entries are not repaired. A wrong path's push over a live entry, more
    than 16 calls deep, and a dropped or rewritten return address each cost
    a later redirect, never a result.
- **Interrupts and far transfers.** INT, IRET, far CALL, RETF, task
  switches and mode changes neither push nor pop. Their flush sets the
  pointer back like any other.

## Latency microbenchmarks

`scripts/test_fes_i386_latency.py` runs on the RTL alone at max
([latency-report.json](latency-report.json)). Only these loops change:

| Loop | Clocks before | Clocks after | Per case before | Per case after |
| --- | ---: | ---: | ---: | ---: |
| 8 CALL rel and RET (`call_ret`) | 180,137 | 106,148 | 22.27 | 13.02 |
| 8 CALL BX and RET (`call_reg`) | 236,029 | 164,029 | 29.25 | 20.25 |
| 8 CALL [mem] and RET (`call_mem`) | 236,059 | 164,059 | 29.26 | 20.26 |
| MOV CX, 8 and LOOP (`loop`) | 42,270 | 42,250 | 5.03 | 5.03 |

- **RET.** Every RET in these loops is predicted, which saves about nine
  clocks a pair.
- **What is left.** Most of the 13 clocks is the CALL. `call_ret` runs
  eight CALLs back to back, five to a 16-byte block. The BTB's two ways keep
  two of each block's CALLs, so most CALLs still redirect. Real code spaces
  its CALLs further apart, which the DOOM window shows.
- **`loop`.** The 20 clocks come from the BTB's way choice. RET entries now
  take turns with the other branches' allocations.

## Lockstep

`scripts/test_fes_i386_lockstep.py` ran with every default (16 seeds of 2,000
random programs, 16 seeds of 500 protected-mode programs, the full corpus):

| Part | RTL | Netlist (Yosys `0a7927bf`) |
| --- | --- | --- |
| SingleStepTests corpus | 1,758,699 of 1,758,699 | same |
| Random programs | 31,999 of 32,000 | 31,999 of 32,000 |
| Protected-mode programs | 8,000 of 8,000 | 8,000 of 8,000 |
| Directed cases | 49 of 49 | 49 of 49 |
| Unreal, protected-mode, no-387 and memory cases | 7, 92, 1,933 and 26, all pass | same |
| Holds without latency | pass | pass |

**New cases.** Six directed cases run real-mode loops, so the BTB learns
their CALLs and RETs before each case breaks the prediction:
- `ras-deep`: recursion 20 deep, three times. The outer RETs predict entries
  the wrap overwrote.
- `ras-drop`: callees that drop their return address, by `POP AX; JMP AX`
  and by `ADD SP, 2; JMP`, between calls that RET.
- `ras-rewrite`: a MOV over the return address on the stack before RET, then
  a `PUSH imm16; RET` with no CALL.
- `ras-sizes`: `RET imm16` after a PUSH and CALL, and a 32-bit CALL and RET
  (66h E8h, 66h C3h) in 16-bit code.
- `ras-int`: an interrupt handler's frame, made by PUSHF and a far CALL and
  left by IRET, around a CALL and RET inside a called function. A directed
  case fails on any INT n, so the case builds the frame itself. The random
  programs inject real INTR and NMI between their CALLs and RETs.
- `ras-store`: a store turns a learned RET into `INC BX`, with a RET after
  it. The RET's entry now ends at the INC, and the fetch check replays the
  next instruction.

A RET whose 32-bit pop lands beyond CS's limit was already a directed case
(`#GP`). It still passes.

**The one failure.** `random#7#1936` is a protected-mode CALL rel past CS's
limit. Bochs writes the push before its #GP, and the RTL does not. It fails
the same way on main and in slice 2's record, and
[#1510](https://github.com/DeanoC/fes/issues/1510) tracks it.

**Without latency.** The numbers match slice 2's: 76.2% of the retirements
after a taken branch are one clock apart.

## Seal

The seal is Quartus 17.0.2, seed 1, from clean checkout `4ee70a831`
([seal-summary.json](seal-summary.json)).
- **Package.** `0a16a224…`, with `core.rbf` `29d7dd0f…`.
- **Base.** The checkout holds slice 2's tip `31b10651f` merged with main
  `eecc2151d`. That main includes #1504's VGA BIOS change, so this package's
  `firmware.lock` differs from slice 2's. #1504 changes INT 10h's scroll
  down from row 0, which none of the benchmarks use.
- **Analyses.** Every one is met:
  - setup +1.020 ns;
  - hold +0.104 ns;
  - recovery +6.147 ns;
  - removal +0.478 ns;
  - minimum pulse width +1.122 ns.
- **Clocks.** The system clock is 61.44 MHz.
- **Resources.**

  | Resource | This seal | Slice 2's `57a78420` |
  | --- | ---: | ---: |
  | ALMs | 32,544 | 31,502 |
  | M10K blocks | 265 | 264 |
  | DSP blocks | 38 | 38 |

[stage-slack.json](stage-slack.json) gives each register group's worst setup
slack at the slow −40 °C corner:

| Path | Slice 2 | This seal |
| --- | ---: | ---: |
| Governor ISA charge, the system clock's worst | +0.80 | +1.02 |
| Q's start and prefix accumulator | +2.09 | +1.28 |
| Q to DC decode | +2.15 | +2.09 |
| Fetch buffer | +3.01 | +3.36 |
| WB redirect target (`wb_rlin`) | +3.61 | +4.10 |
| Return stack | — | +5.18 |
| D's EIP | — | +5.57 |
| WB's committed pointer (`ras_c`) | — | +9.99 |

**Q's loop.** Q's start loop lost 0.8 ns, though its logic does not change.
Q's record gains the 32-bit predicted target (`ptgt`), loaded with the
loop's own select, and placement moved. It is now the front end's tightest
path. A later slice that needs Q's margin can move `ptgt` out of
Q's record: D can read it from the half's BTB information instead.

### Changes after the seal

`2be3ca091` changes the `ras-int` directed case's code in `lockstep_tb.cpp`,
which is not a sealed input. The RTL is the sealed RTL. While pc386 is in
progress, a post-seal change needs no reseal before merge, and this one
changes no sealed input. The merge of main after #1511 landed (`78830ddf1`)
changes no file of this branch's tree outside this record.

## Kit

Run `r1` used Kit A, core-only, on 6 November from 12:07 to 12:21
([kit-scores.json](kit-scores.json)).
- **Package.** `0a16a224…`, the seal above, built from `4ee70a831`. That
  commit has the return stack's RTL and not the `ras-int` test change.
- **Kit.** Baseline software `a8cd30c4` on image `f904961e`, with no
  install, no image change and no reboot. Kit A was restored, checked
  against its snapshot and logged FREE. The run's five FogCast data trees
  were removed after, and Kit A was logged FREE again at 12:21.
- **Disk.** The baseline's bench disk, `fe81c9c4…`, was staged from Powerboat
  and is not committed.
- **Commands.** Each was the baseline's and slice 2's, at max (preset 0),
  after a SCRUBX launch.

| Benchmark | Before (k1) | After (r1) | Change |
| --- | ---: | ---: | ---: |
| DOOM v1.9 `-timedemo demo3` | 3,863 gametics in 3,608 realtics, 37.5 fps | 3,863 in 3,492, 38.7 fps | +3.3% |
| 3DBench | 71.4 | 76.9 | +7.7% |
| PCPBench | 12.8 | 13.0 | +1.6% |
| TopBench | 303 | 307 | +1.3% |
| Pack DOOM, maximum detail | 2,134 in 2,218, 33.7 fps | 2,134 in 2,141, 34.9 fps | +3.6% |
| Pack DOOM, minimum detail | 2,134 in 805, 92.8 fps | 2,134 in 771, 96.9 fps | +4.4% |

3DBench gains more than the plan's 2.8%. Its bench window was not rerun, so
this record does not break the gain down.

## Profile

The machine bench reran DOOM v1.9's `-timedemo demo3` as slices 1 and 2 did:
- the profile's disk `7d98bffd…`;
- the oracle to page flip 40, then the RTL from cycle 2,222,604,910;
- the window 0.5–1.0 s after the handover, 50,000,000 clocks
  ([doom-window.json](doom-window.json)).

The bench was built from `4ee70a831`, the sealed RTL.

| | BTB (slice 2) | Return stack |
| --- | ---: | ---: |
| Instructions retired | 27,605,211 | 28,699,732 (+4.0%) |
| CPI | 1.811 | 1.742 |
| Redirect refill | 5.3% of clocks | 3.3% |
| Redirects | 449,717 | 267,873 |
| Clocks from a near RET to its target's retirement | 12.15 | 2.55 |

**Near RETs.** The window retires 218,410 of them:
- 210,482 were predicted by the return stack;
- 12,936 redirected, which is 5.9%: the 7,928 the BTB did not predict,
  and about 5,000 whose predicted target was wrong.

So 94.1% of near RETs go to their target without a redirect. The plan
assumed 95–99%. Unlearned RETs are those the BTB has not yet seen or has
lost to its two ways. Wrong targets come from a stack out of step with the
program, after a dropped return address, a wrap or a wrong path's push. The
profile does not split the wrong targets by cause.

**Direct branches.** 3,086,698 retire, and the BTB predicts them with 94.1%
accuracy, as in slice 2. One fetch check replays in the window.

**What is left.** CALL r/m now costs the most per branch: 41,733 of them
take 22.97 clocks each, 1.9% of the window. Slice 5's indirect prediction
is for these. Next come L1D and port 0 at 15.4% and the AGI interlock at
7.8%.

## What this record does not cover

- Wolfenstein 3D's page flips on the bench were not rerun. The plan gives
  the return stack 0.6% of its clocks, and its kit capture is held at 30 fps.
- 3DBench's and PCPBench's bench windows were not rerun. The kit scores
  above stand for them.
