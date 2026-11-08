# i386 Phase 4 slice 5: indirect JMP and CALL prediction, 8 November 2026

This record covers CPU Phase 4's slice 5
([`i386-cpu-design.md`, "Phase 4: speed"](../../../sources/misteross/docs/i386-cpu-design.md#phase-4-speed),
tracker [#1488](https://github.com/DeanoC/fes/issues/1488)). `fes_i386_cpu`
now predicts a near indirect JMP or CALL (JMP r/m, CALL r/m) to the target
it last went to. Every "before" figure comes from this branch's first commit,
which is main `66af9a662` plus the bench-only measurement below, or from
main's last kit run, `r9` in [#1552's record](../2026-11-07-i386-phase2-ring3/summary.md).

**Result:**
- **Last target is enough for DOOM.** In DOOM's bench window, 98.9% of
  indirect CALLs and every indirect JMP go to the target their site went
  to last. The six hot sites repeat 99.3–100% of the time. The front-end
  record assumed 70–90%. 3DBench's one indirect site repeats 4.3% of the
  time across 67 targets, so last target gains it nothing.
- **Kit A, max.**
  - DOOM v1.9 `-timedemo demo3`: 40.05 fps, up from 39.74 (+0.8%).
  - The DOS Benchmark Pack's DOOM: 36.2 fps at maximum detail, up from
    35.9, and 99.9 at minimum, up from 98.8.
  - 3DBench 76.9 and TopBench 309, unchanged. PCPBench 13.3, from 13.2.
- **Microbenchmarks.** One CALL BX and RET take 9.0 clocks, down from 20.0.
  One JMP BX takes 2.0, down from 11.0. A CALL whose target changes every
  time costs what it did.
- **Profile.** DOOM's second half-second retires 0.95% more, and CPI falls
  from 1.683 to 1.667. A CALL r/m's target now retires 13.29 clocks after it,
  down from 23.04. 96.9% of indirect JMPs and CALLs retire without a
  redirect.
- **Lockstep.** Everything passes, on the RTL and on the Quartus build's
  Yosys netlist, with nine new directed cases.
- **Seal.** Quartus 17.0.2 meets every analysis. Setup slack is +1.180 ns,
  where main's last seal had +1.012. Hold slack is +0.088 ns.

## Design as built

[Prediction](../../../sources/misteross/docs/i386-cpu-design.md#prediction)
has the full description. In short:
- **No new structure.** The BTB, the return stack and the fetch buffer do
  not change. An indirect JMP's or CALL's entry is an always-taken entry
  whose target is the one it last went to. A CALL's entry has the call bit,
  so F2 pushes its return address as for a CALL rel.
- **D.** A predicted indirect JMP, or a predicted CALL r/m's last uop, its
  jump, is handled as a predicted RET is: the next instruction's EIP is the
  predicted target's linear address less CS.base. A new pipe bit, `nind`,
  marks the jump.
- **EX.** RET's compare (`ex_ind_eip` against `peip`) now also covers
  `nind`. A JMP or CALL that was not predicted, or whose target differs,
  redirects to the computed target.
- **WB.** Every indirect JMP's or CALL's retirement writes its entry, with
  the target EX computed, as a RET's does. So a changed target is written
  back at once.
- **Checks.** The fetch check passes the target by construction, as after a
  RET, so EX's compare is the target's check. The fetch check still covers
  an entry where no branch ends (`ind-store` below). A target past CS's
  limit faults at EX and never redirects through the compare
  (`ind-limit`).
- **The return stack.** A predicted CALL r/m is pushed by F2. One predicted
  to a wrong target is pushed too, and its redirect writes the same
  address again at WB, as for any near CALL that redirects.

## Measuring the last target first

`machine_tb --cpu-profile` now keeps, for every indirect near CALL and JMP
site, the target it went to last and the distinct targets it has gone to,
and counts the retirements that go to the site's last target again
(`cpu_profile.h`, "Indirect targets"). That is what a predictor holding
every site's last target would get right. A BTB entry can only do worse,
since a set has two ways. The bench run was made from the branch's first
commit, `384023cfe`, whose RTL behaves as main's: it adds only
the profile's `prof_ind` and the pipe bit `nind`, which nothing else reads.

| Window | Indirect CALLs | To their last target | Indirect JMPs | To their last target |
| --- | ---: | ---: | ---: | ---: |
| DOOM, first half-second | 23,545 | 23,120 (98.2%) | 336 | 327 |
| DOOM, second half-second | 42,822 | 42,341 (98.9%) | 582 | 582 |
| 3DBench | 73,655 | 3,144 (4.3%) | 0 | — |

DOOM's sites in the second half-second
([doom-window-h2-before.json](doom-window-h2-before.json),
`indirect_sites`):

| Site | CALLs | To the last target | Targets |
| --- | ---: | ---: | ---: |
| `001a65f8` | 10,436 | 100.0% | 1 |
| `001a6b0f` | 8,407 | 100.0% | 1 |
| `001a2f46` | 8,339 | 99.3% | 2 |
| `001a61a3` | 5,685 | 100.0% | 1 |
| `001a7b49` | 4,301 | 100.0% | 1 |
| `001a6ba3` | 4,200 | 100.0% | 1 |
| `00185266` | 231 | 0.0% | 5 |

So the slice keeps the plan's policy: last target, no history. 3DBench's
calls all come from one site, `0004d3b2`, which goes to 67 targets in the
window. No last-target scheme helps it; a predictor keyed on history would
be a separate slice, and 3DBench's indirect calls are 0.3% of its window.

## Latency microbenchmarks

`scripts/test_fes_i386_latency.py` runs on the RTL alone at max
([latency-report.json](latency-report.json)). The bench gains three loops,
each running its case once an iteration so that the case's branch is alone
in its 16-byte block but for the loop's DEC and JNZ:
- `call_ind1`: CALL BX and RET;
- `jmp_ind1`: JMP BX, to a block that counts down and jumps back;
- `call_alt`: CALL BX whose target swaps between two RETs every iteration.

Their "before" ran this bench on the first commit's RTL. Only these loops
change:

| Loop | Clocks before | Clocks after | Per case before | Per case after |
| --- | ---: | ---: | ---: | ---: |
| 8 CALL BX and RET (`call_reg`) | 164,029 | 173,999 | 20.25 | 21.50 |
| 8 CALL [mem] and RET (`call_mem`) | 164,059 | 153,070 | 20.26 | 18.88 |
| MOV CX, 8 and LOOP (`loop`) | 42,250 | 42,270 | 5.03 | 5.03 |
| CALL BX and RET (`call_ind1`) | 176,046 | 88,068 | 20.00 | 9.00 |
| JMP BX (`jmp_ind1`) | 104,053 | 32,062 | 11.00 | 2.00 |
| CALL BX, target swapping (`call_alt`) | 184,081 | 184,081 | 21.00 | 21.00 |

- **The spaced loops** show the slice's gain: a predicted CALL BX and RET
  save 11 clocks, and a predicted JMP BX saves 9.
- **A changing target** costs what an unpredicted one did: the redirect
  comes from WB either way.
- **`call_reg` loses 1.25 clocks a case.** Its eight 2-byte CALL BX share
  two 16-byte blocks with the loop's DEC and JNZ. Each CALL now writes an
  entry, and with two ways a set the CALLs' entries evict the JNZ's: a
  traced run shows the JNZ redirecting on 999 of 1,000 iterations, about
  10 clocks each. The CALLs' own entries never predict, since each RET
  re-enters the block past them. `call_ret`, eight CALL rel, has had the
  same contention since slice 3. `call_mem`'s 4-byte CALLs leave the JNZ
  its own block, and it gains. Real code spaces its CALLs further apart,
  as the DOOM window shows.
- **`loop`'s 20 clocks** come from the way choice, as in slice 3.
- **What is left.** Most of `call_ind1`'s 9 clocks is the CALL's jump
  waiting at D for T1, which the profile below finds in DOOM too.

## Lockstep

`scripts/test_fes_i386_lockstep.py` ran with every default (16 seeds of 2,000
random programs, 16 seeds of 500 protected-mode programs, the full corpus),
on the RTL and on the netlist at `3321e416e`, the sealed checkout:

| Part | RTL | Netlist (Yosys `0a7927bf`) |
| --- | --- | --- |
| SingleStepTests corpus | 1,758,699 of 1,758,699 | same |
| Random programs | 32,000 of 32,000 | same |
| Protected-mode programs | 8,000 of 8,000 | same |
| Directed cases | 77 of 77 | same |
| Unreal, protected-mode, no-387 and memory cases | 7, 147, 1,933 and 27, all pass | same |
| Holds without latency | pass | pass |

[#1510](https://github.com/DeanoC/fes/issues/1510)'s random program no
longer fails on main's tree, so nothing fails.

**New cases.** Nine directed cases run real-mode loops, so the BTB learns
their indirect sites before each case breaks the prediction:
- `ind-change`: a CALL BX whose target changes once it is learned.
- `ind-alt`: a CALL BX and a JMP CX whose targets alternate every time.
- `ind-call-ret`: CALL [mem] to a function that CALLs BX. Both RET, so each
  RET's prediction comes from an indirect CALL's push.
- `ind-table`: JMP [mem] through a slot that a store rewrites halfway.
- `ind-sizes`: CALL EBX with a 32-bit RET (66h C3h), CALL [BX+SI], and
  JMP BX with EBX's upper half set, which JMP r/m16 ignores.
- `ind-int`: an interrupt handler's frame, made by PUSHF and a far CALL
  and left by IRET, around a CALL BX in the handler. A directed case fails
  on any INT n, so the case builds the frame itself.
- `ind-store`: a store turns a learned CALL BX into INC SI; INC SI. Its
  entry now ends where no CALL ends, and the fetch check keeps the next
  instruction right.
- `ind-limit`: a learned JMP EBX whose target moves past CS's limit on the
  last iteration: #GP(0) at the JMP, though the BTB predicts it.
- `ind-fault`: a learned CALL [SI] whose operand load faults (a word at
  offset FFFFh): #GP(0) at the CALL, before its push.

The last two are fault cases. `run_fault_case` gains an optional faulting
IP and retirement limit for them, since their fault is not at the code's
first instruction.

**The cases catch a broken compare.** With EX's compare limited to RET
again, as before the slice, `ind-change`, `ind-alt`, `ind-call-ret` and
`ind-table` fail.

## Seal

The seal is Quartus 17.0.2, seed 1, from clean checkout `3321e416e`
([seal-summary.json](seal-summary.json)).
- **Package.** `54c89405…`, with `core.rbf` `22d7461e…`.
- **Analyses.** Every one is met:
  - setup +1.180 ns;
  - hold +0.088 ns;
  - recovery +8.232 ns;
  - removal +0.325 ns;
  - minimum pulse width +1.122 ns.
- **Clocks.** The system clock is 61.44 MHz.
- **Resources.**

  | Resource | This seal | Main's last seal (#1552, `dc42f624`) |
  | --- | ---: | ---: |
  | ALMs | 33,455 | 33,436 |
  | M10K blocks | 265 | 265 |
  | DSP blocks | 38 | 38 |

[stage-slack.json](stage-slack.json) gives each register group's worst setup
slack at the slow −40 °C corner. The paths the earlier slices watched:

| Path | Main (#1552) | This seal |
| --- | ---: | ---: |
| D's issue register (`dc0`), the system clock's worst | +1.50 | +1.18 |
| Governor ISA charge | +1.20 | +1.20 |
| Q's start and prefix accumulator | +1.46 | +1.57 |
| D's EIP | +1.38 | +1.84 |
| Q to DC decode | +1.71 | +2.03 |
| EX | +6.29 | +5.90 |
| Return stack | +4.28 | +4.78 |
| BTB write | +6.25 | +7.80 |

The slice adds a term to D's EIP select's condition and to EX's compare,
neither on a tight path. `dc0`'s worst path starts at the load-forwarding
select (`dfwd_ld_ex`), which the slice does not change; it moved with
placement.

## Kit

Run `r1` used Kit A, core-only, on 8 November from 14:19 to 14:31
([kit-scores.json](kit-scores.json)).
- **Package.** `54c89405…`, the seal above.
- **Kit.** Baseline software `4efd34bf` on image `31b39063`, with no
  install, no image change and no reboot. Kit A was restored, checked
  against its snapshot and logged FREE. The run's five FogCast data trees
  were removed after, and Kit A was logged FREE again at 14:32.
- **Disk.** The baseline's bench disk, `fe81c9c4…`, was staged from Powerboat
  and is not committed.
- **Commands.** Each was the baseline's, at max (preset 0), after a SCRUBX
  launch.

| Benchmark | Main (#1552's r9) | This slice (r1) | Change |
| --- | ---: | ---: | ---: |
| DOOM v1.9 `-timedemo demo3` | 3,863 gametics in 3,402 realtics, 39.7 fps | 3,863 in 3,376, 40.05 fps | +0.8% |
| Pack DOOM, maximum detail | 2,134 in 2,079, 35.9 fps | 2,134 in 2,062, 36.2 fps | +0.8% |
| Pack DOOM, minimum detail | 2,134 in 756, 98.8 fps | 2,134 in 748, 99.9 fps | +1.1% |
| 3DBench | 76.9 | 76.9 | 0 |
| PCPBench | 13.2 | 13.3 | +0.8% |
| TopBench | 309 | 309 | 0 |

The plan projected about 1% for DOOM. Main's slice 6b run (`u1`) had 3,399
realtics for `demo3`, so against it the gain is +0.7%.

## Profile

The machine bench reran DOOM v1.9's `-timedemo demo3` as the earlier slices
did, with the oracle to page flip 40 and then the RTL from cycle
2,222,604,910, and 3DBench from 4 s. The before bench was built from
`384023cfe`, the after bench from `3321e416e`.

| | Before | After |
| --- | ---: | ---: |
| DOOM, second half-second: instructions retired | 29,715,179 | 29,997,850 (+0.95%) |
| CPI | 1.683 | 1.667 |
| Redirect refill | 3.44% of clocks | 3.04% |
| Clocks from a CALL r/m to its target's retirement | 23.04 | 13.29 |
| Clocks from a JMP r/m to its target's retirement | 29.54 | 21.38 |
| DOOM, first half-second: instructions retired | 33,746,836 | 33,878,869 (+0.39%) |
| 3DBench, 4.5–8.0 s: instructions retired | 162,363,138 | 162,372,939 (+0.006%) |

([doom-window-h2.json](doom-window-h2.json),
[doom-window-h2-before.json](doom-window-h2-before.json),
[doom-window-h1.json](doom-window-h1.json),
[doom-window-h1-before.json](doom-window-h1-before.json),
[3dbench-window.json](3dbench-window.json),
[3dbench-window-before.json](3dbench-window-before.json).)

**Indirect branches.** In the second half-second, 43,956 retire. The BTB
predicts 42,946 of them and 1,341 redirect, so 96.9% go to their target
without a redirect, against the 98.9% the last-target count allows. The
difference is entries lost to the two ways, as for RETs. Near RETs keep
their 93.9–94.2% accuracy, and direct branches their 94.1%.

**3DBench.** The BTB predicts 72,813 of its 73,655 indirect CALLs, and
70,511 redirect: 4.3% accuracy, the site's last-target rate. A wrong
target costs what no prediction did, so 3DBench neither gains nor loses.

**What is left of a CALL r/m.** Its target still retires 13.29 clocks
after it, against 2.44 for a CALL rel. Most of that is the CALL's jump
waiting at D for T1's load to retire (`b_t1`, SEQ_CALLI). The TEMP bucket
holds 387,005 clocks of the window, about 9 per CALL r/m. A predicted jump
needs T1 only for EX's compare, so it could issue without that wait.

## What this record does not cover

- Wolfenstein 3D's page flips on the bench were not rerun. Its kit capture
  is held at 30 fps.
- PCPBench's and TopBench's bench windows were not rerun. The kit scores
  above stand for them.

## Commands

```bash
python3 scripts/pc386_cpu_profile.py cpu.prof --from 22.726049 --to 23.226049 --json doom-window-h2.json
python3 scripts/pc386_cpu_profile.py cpu.prof --from 22.226049 --to 22.726049 --json doom-window-h1.json
python3 scripts/pc386_cpu_profile.py cpu.prof --from 4.5 --to 8.0 --json 3dbench-window.json
```
