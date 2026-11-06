# i386 Phase 4 slice 2: the BTB, 6 November 2026

This record covers CPU Phase 4's slice 2
([`i386-cpu-design.md`, "Phase 4: speed"](../../../sources/misteross/docs/i386-cpu-design.md#phase-4-speed),
tracker [#1488](https://github.com/DeanoC/fes/issues/1488)). It adds a branch target buffer for Jcc,
LOOP, LOOPE, LOOPNE, JCXZ, JMP rel and CALL rel to `fes_i386_cpu`. Every
"before" figure comes from the
[slice 1 baseline](../2026-11-05-i386-phase4-baseline/summary.md).

**Result:**
- **Kit A, max.**
  - DOOM v1.9 `-timedemo demo3`: 37.5 fps, up from 26.7 (+40%). The plan
    projected 36–38.
  - 3DBench: 71.4, up from 50.0 (+43%).
  - PCPBench: 12.8, up from 10.7 (+20%).
  - TopBench: 303, up from 297. It stays halt- and VGA-bound.
  - The DOS Benchmark Pack's DOOM: 33.7 fps at maximum detail, up from
    24.3, and 92.8 at minimum, up from 69.7.
- **Microbenchmarks.** A DEC and taken JNZ loop takes 2.0 clocks an
  iteration, down from 12.0. A LOOP iteration takes 5.0 clocks, down from
  9.9.
- **Profile.** In the DOOM `demo3` bench window, CPI falls from 2.54 to
  1.81. The BTB predicts direct branches with 94.2% accuracy.
- **Lockstep.** It passes as main does, on the RTL and on the seal's Yosys
  netlist. The one failure also fails on main's RTL and is filed as
  [#1510](https://github.com/DeanoC/fes/issues/1510).
- **Seal.** Quartus 17.0.2 meets every analysis. Setup slack is +0.800 ns,
  where main's seal had +0.712. Hold slack is +0.109 ns.

## Design as built

[Prediction](../../../sources/misteross/docs/i386-cpu-design.md#prediction)
has the full description. In short:
- **Entries.** 512 entries, two ways of 256 sets, in M10K (`fes_i386_btb`).
  An entry is keyed by the block that holds the branch's last byte, and
  holds that byte's offset, the target and a 2-bit counter.
- **Prediction.** F2 redirects F1 one clock later, to an entry whose branch
  ends at or past the point where fetch entered the block. The fetch buffer
  fills its halves alternately. Q jumps to the target after the instruction
  that ends where the entry says, which adds one select bit and a 2:1 to
  its loop.
- **EIP.** D gives the next instruction the branch's own target EIP.
- **Resolution.** WB redirects only on a wrong direction.

**Checks.** The plan had DC check the branch type, and WB check the target
and type. One fetch check replaces both:
- AG compares each instruction's fetched linear address with CS.base plus
  its EIP.
- Q flags an instruction whose bytes, or a prefix consumed at a half's last
  byte, run into a half that does not follow (`xbad`).

Either case marks the instruction `FAULT_REPLAY` before M1, so it touches no
memory. WB then replays it and clears the BTB sets that sent fetch there.
This makes every entry only a hint: a wrong target, code changed under an
entry, a branch the entry places where none ends, and an entry left by the
other code size each cost a replay, never a result.

**Other changes.**
- **queue16.** The profile predicts nothing.
- **Self-modifying code.** The check compares lines with what each part of
  the front end holds, since the bytes after WB's instruction are no longer
  one run.
- **Fetch-wait trace.** The CPU has a new `trace_fe_wait`. The lockstep's
  hold checks put the retirements a front-end wait delayed in a new `fetch`
  bucket.

**Found in testing.** A stale entry made Q consume a 66h prefix at a half's
last byte and step into the target's half as if it were sequential. The
instruction kept the earlier half's address, so it passed the fetch check
with the target's bytes. `acc_xbad` now flags it.

## Latency microbenchmarks

`scripts/test_fes_i386_latency.py` runs on the RTL alone at max
([latency-report.json](latency-report.json)). The table compares main with
this slice:

| Loop | Clocks before | Clocks after | Per iteration before | Per iteration after |
| --- | ---: | ---: | ---: | ---: |
| DEC BP and JNZ, taken (`jcc_loop`) | 47,999 | 8,039 | 12.00 | 2.01 |
| 8 taken JE, each over a NOP (`jcc_taken`) | 103,999 | 77,045 | 104.00 | 77.05 |
| 8 CALL rel and RET (`call_ret`) | 199,118 | 180,137 | 199.12 | 180.14 |
| 8 CALL BX and RET (`call_reg`) | 245,999 | 236,029 | 246.00 | 236.03 |
| 8 CALL [mem] and RET (`call_mem`) | 246,029 | 236,059 | 246.03 | 236.06 |
| 8 real-mode far CALL and RETF (`far_call`) | 269,029 | 259,060 | 269.03 | 259.06 |
| MOV CX, 8 and LOOP (`loop`) | 90,999 | 42,270 | 91.00 | 42.27 |
| 8 MOV BX, [BX] (`agi_load`) | 44,999 | 35,039 | 45.00 | 35.04 |
| 8 ADD SI, 2 and MOV AX, [SI] (`agi_alu`) | 52,999 | 43,029 | 53.00 | 43.03 |
| 8 stores, each with a load from the same address | 36,029 | 26,059 | 36.03 | 26.06 |
| 8 stores, each with a load from another line | 36,029 | 26,059 | 36.03 | 26.06 |

Every loop saves its back branch's ten clocks.
- **Taken Jcc.** A predicted taken JNZ costs about one clock, where it cost
  11.5.
- **jcc_taken.** `jcc_taken` puts five taken JEs in each 16-byte block.
  The two ways keep two of a block's branches, so it gains less: 9.4
  clocks a JE, from 11.5.
- **CALL rel.** CALL rel now costs about one clock. Its RET, which slice 3's
  return stack predicts, still redirects.
- **Other cases.** The cases that slice 2 does not predict keep their
  per-case cost.

## Lockstep

`scripts/test_fes_i386_lockstep.py` ran with every default (16 seeds of 2,000
random programs, 16 seeds of 500 protected-mode programs, the full corpus):

| Part | BTB RTL | BTB netlist (Yosys `0a7927bf`) |
| --- | --- | --- |
| SingleStepTests corpus | 1,758,699 of 1,758,699 | same |
| Random programs | 31,999 of 32,000 | 31,999 of 32,000 |
| Protected-mode programs | 8,000 of 8,000 | 8,000 of 8,000 |
| Directed cases | 43 of 43 | 43 of 43 |
| Unreal, protected-mode, no-387 and memory cases | 7, 92, 1,933 and 26, all pass | same |
| Holds without latency | pass | pass |

**New cases.** The directed cases add three BTB cases:
- `btb-store-disp`: a store rewrites a learned JMP's displacement.
- `btb-cs-size-16` and `btb-cs-size-32`: the same bytes run as 16-bit and
  then as 32-bit code. The 32-bit run's CLI ends where the 16-bit loop's
  JNZ did.

**Random programs.** A quarter of their LOOPs now count 6 to 20, so the
counters saturate. This changes the programs every seed draws.

**The one failure.** `random#7#1936` is a protected-mode CALL rel past CS's
limit. Bochs writes the push before its #GP, and the RTL does not.
- **Before the slice.** Main's RTL fails it the same way with this bench.
- **The old generator.** Main's run with the old generator also has one
  failure, `random#1#1473` (likely #1425).

[#1510](https://github.com/DeanoC/fes/issues/1510) tracks the divergence.

**Coverage.**
- **Interrupts and replays.** The random programs inject INTR and NMI into
  their loops and replay uncached loads.
- **DMA and FLUSH.** The DMA sweeps write into the code of loops whose
  JMPs the BTB predicts.
- **Stale entries.** The BTB is never cleared between runs, so every case
  also runs over the entries of the runs before it.

**Without latency.** With prediction, 76.2% of the retirements after a taken
branch are one clock apart. On main, 60.1% were 11 clocks apart.

## Seal

The final seal is Quartus 17.0.2, seed 1, from clean checkout `2213bd72c`
([seal-summary.json](seal-summary.json)). The commits after it change no
sealed input.
- **Package.** `57a78420…`, with `core.rbf` `15e015cc…`.
- **Analyses.** Every one is met:
  - setup +0.800 ns;
  - hold +0.109 ns;
  - recovery +7.074 ns;
  - removal +0.278 ns;
  - minimum pulse width +1.122 ns.
- **Clocks.** The system clock is 61.44 MHz.
- **Resources.**

  | Resource | This seal | Main's `01e1eebb` |
  | --- | ---: | ---: |
  | ALMs | 31,502 | 29,485 |
  | M10K blocks | 264 | 259 |
  | DSP blocks | 38 | 36 |

[stage-slack.json](stage-slack.json) gives each register group's worst setup
slack at the slow −40 °C corner. Before the slice, the figures came from the
profile record's rebuild:

| Path | Before | After |
| --- | ---: | ---: |
| Governor ISA charge, the system clock's worst | +1.22 | +0.80 |
| Q's start and prefix accumulator | +3.20 | +2.09 |
| Q to DC decode | +2.40 | +2.15 |
| Fetch buffer, jump bits included | — | +3.01 |
| WB redirect target (`wb_rlin`) | — | +3.61 |
| BTB address registers | — | +4.65 |

The governor's path does not change in RTL: its slack moves with
placement.

## Kit

Run `k1` used Kit A, core-only, on 6 November from 09:32 to 09:48
([kit-scores.json](kit-scores.json)).
- **Package.** `57a78420…`, the seal above.
- **Kit.** Baseline software `a8cd30c4` on image `f904961e`, with no
  install, no image change and no reboot. Kit A was restored, checked
  against its snapshot and logged FREE. Its run data was removed after.
- **Disk.** The baseline's bench disk, `fe81c9c4…`, was staged from Powerboat
  and is not committed.
- **Commands.** Each was the baseline's, at max (preset 0), after a SCRUBX
  launch.

| Benchmark | Before (a1) | After (k1) | Change |
| --- | ---: | ---: | ---: |
| DOOM v1.9 `-timedemo demo3` | 3,863 gametics in 5,064 realtics, 26.7 fps | 3,863 in 3,608, 37.5 fps | +40% |
| 3DBench | 50.0 | 71.4 | +43% |
| PCPBench | 10.7 | 12.8 | +20% |
| TopBench | 297 | 303 | +2% |
| Pack DOOM, maximum detail | 2,134 in 3,072, 24.3 fps | 2,134 in 2,218, 33.7 fps | +39% |
| Pack DOOM, minimum detail | 2,134 in 1,072, 69.7 fps | 2,134 in 805, 92.8 fps | +33% |

## Profile

The machine bench reran DOOM v1.9's `-timedemo demo3` as the baseline did:
- the profile's disk `7d98bffd…`;
- the oracle to page flip 40, then the RTL from cycle 2,222,604,910;
- the window 0.5–1.0 s after the handover, 50,000,000 clocks
  ([doom-window.json](doom-window.json)).

The bench was built before this branch merged main's rename of the
`flushes` counter to `flush_cycles`, which counts the same thing. The
profile was read with that key renamed.

| | Baseline (same window) | BTB |
| --- | ---: | ---: |
| Instructions retired | 19,694,266 | 27,605,211 (+40%) |
| CPI | 2.539 | 1.811 |
| Redirect refill | 18.6% of clocks | 5.3% |
| Wrong-path uops | 15.6% | 3.9% |
| Clocks from a taken Jcc to its target's retirement | 11.3–12.2 | 1.79 |
| Clocks from a CALL rel to its target | about 12 | 2.39 |

**Direct branches.** The window retires 2,961,312 of them, and 2,197,261 are
taken. The BTB predicts 2,206,200 taken and gets 171,075 wrong: **94.2%
accuracy**. That is above the plan's 85%, which Phase 7's gshare waits for.
No fetch check replays in the window.

**What is left.** RET now takes the largest share of redirect clocks:
209,305 RETs cost 12.15 clocks each, 5.1% of the window. Slice 3's return
stack is for these. Next come L1D and port 0 at 14.8% and the AGI interlock
at 7.5%.
