# i386 Phase 4 slice 6: the early ALU, 6 November 2026

This record covers CPU Phase 4's slice 6
([`i386-cpu-design.md`, "Phase 4: speed"](../../../sources/misteross/docs/i386-cpu-design.md#phase-4-speed),
tracker [#1488](https://github.com/DeanoC/fes/issues/1488)). The plan's
first step splits the profile's AGI wait by the uop that produced the
address register. The split cleared the plan's stop line, so the slice built
the early ALU: AG's address adder gives the result of a one-uop ADD, INC,
DEC or MOV
([design](../../../sources/misteross/docs/i386-cpu-design.md#registers-address-generation-and-the-early-alu)).

**Tree.** Main `5e201aa3b`, which has slices 2 to 4 (#1511, #1512, #1515),
plus this branch. The split was measured at `e4eb91ea4` (bench-only
signals, nothing the CPU does changes); the early ALU at `561271c3b`, which
is what the seal, lockstep and kit runs used.

**Result.**
- **Kit A, max** (against slice 4's run `s2` on main's seal `254b2bf5`):
  - DOOM v1.9 `-timedemo demo3`: 39.5 fps, up from 39.2 (+0.7%).
  - The DOS Benchmark Pack's DOOM: 35.6 fps at maximum detail, up from 35.4,
    and 98.4 at minimum, up from 97.5.
  - 3DBench 76.9, PCPBench 13.2 and TopBench 309, all unchanged.
- **Microbenchmark.** The AGI chain (ADD SI, 2; MOV AX, [SI]) costs 2.50
  clocks a case, down from 5.13. No other loop changes.
- **Bench windows.**
  - 3DBench: AGI falls from 5.0% of clocks to 2.2%. The window retires 3.0%
    more instructions; CPI 2.156, from 2.221. Its kit score shows no change:
    it reads 76.9 before and after.
  - DOOM: AGI falls by 0.71M clocks in the first half-second after the
    handover, but more than half of that comes back as a front end that is
    empty ([below](#where-dooms-clocks-went)). It retires 0.44% more.
- **Lockstep.** It passes as main does, on the RTL and on the Quartus
  build's Yosys netlist. The one failure is
  [#1510](https://github.com/DeanoC/fes/issues/1510), as on main. The 64
  directed cases include 7 new groups for the early ALU.
- **Seal.** Quartus 17.0.2 meets every analysis
  ([seal-summary.json](seal-summary.json)): worst setup +0.802 ns, hold
  +0.104 ns, 32,629 ALMs. D and AG lose no slack against main's seal
  ([below](#timing)).
- **Step 1.** The split is the decision's basis:
  - DOOM: producers an early ALU would run cause 2.0% of its kit-equivalent
    clocks, nearly all 32-bit ADD, INC, DEC, SUB with an immediate, and MOV.
  - 3DBench: 3.1%, almost all 16-bit ADD, INC and DEC.
  - Loads: a load's value into an address costs DOOM 4.0%. Half of that is
    a wait that `fes_i386_dfwd`'s `LOAD` kind was designed to remove
    ([below](#loads)).

## The split

`machine_tb --cpu-profile` puts each clock that D waits on an address
register (AGI) against the producer that holds it:
- **Stage.** `fes_i386_cpu` gives the stage, AG to EX, of the youngest late
  producer of the base or index (`prof_agi`). With none, it gives the
  youngest early one: a late scaled index, or two late operands.
- **Class.** A shadow of AG to EX in `cpu_profile.h` moves with the
  pipeline and keeps each uop's class from when it was in D (`prof_dcls`).
- **Ready.** The shadow also keeps whether the producer's register sources
  were in a register, or would be by the time it reached AG. Only then could
  an early ALU run it. A 32-bit MOV does not read its destination.
- **Report.** `scripts/pc386_cpu_profile.py` prints the split. The `early`
  total counts the ready producers in the classes an early ALU would run:
  MOV, ADD, SUB, ADC, the logic ops, a 16-bit LEA, MOVZX and MOVSX, and
  zeroing.

Columns:
- `ready`: the producer's sources were ready for AG.
- `imm`: the producer had no second register source. That is an immediate,
  a one-operand op, or a memory operand, so every load counts.
- `w32`: ready, and the producer wrote 32 bits.

The `ready` test has two biases:
- **Too low for chains.** It is measured against today's producers. In
  ADD, ADD, then an address, the second ADD is not ready, because the first
  is late; with an early ALU both would be early.
- **Too high for M2.** It counts a producer whose source is in M2 as not
  ready, though AG takes one late operand from EX's register for addresses.

The true figure lies between `early` and the classes' whole AGI.

## DOOM `demo3`

The bench window is the one slices 1 to 4 used:
- the profile's disk `7d98bffd…`;
- the oracle to page flip 40, then the RTL from cycle 2,222,604,910;
- the window 22.726–23.226 s, 50,000,000 clocks
  ([doom-window.json](doom-window.json)).

This tree has the return stack, which slice 4's window did not. The window
retires 29.2M instructions at CPI 1.710 (slice 4's pre-merge tree: 28.1M at
1.781), and AGI is 4,535,063 clocks (4,331,317).

The kit-equivalent clocks charge each L1 miss 7 clocks fewer, as the
plan's estimates did: 47,180,001.

| Producer | AGI clocks | Ready | Ready, 32-bit | Share of kit-equivalent clocks |
| --- | ---: | ---: | ---: | ---: |
| Load (MOV, MOVZX, MOVSX from memory) | 1,901,795 | 1,810,852 | 1,250,103 | 4.03% |
| AND, OR, XOR, NOT | 680,851 | 10,317 | 10,053 | 1.44% |
| Shift | 614,712 | 9,344 | 9,113 | 1.30% |
| ADD, INC, DEC | 580,994 | 454,480 | 454,373 | 1.23% |
| MOV, register or immediate | 309,855 | 304,338 | 302,312 | 0.66% |
| ALU op with a memory source | 212,022 | 83,724 | 83,724 | 0.45% |
| SUB | 192,455 | 183,201 | 183,201 | 0.41% |
| Multiply and divide | 18,186 | 3,202 | 3,202 | 0.04% |
| POP | 13,388 | 13,083 | 12,483 | 0.03% |
| ADC, SBB, NEG | 8,566 | 8,443 | 8,443 | 0.02% |
| MOVZX, MOVSX | 1,198 | 1,198 | 1,198 | 0.00% |
| XOR or SUB of a register with itself | 672 | 498 | 498 | 0.00% |
| LEA, 16-bit | 369 | 369 | 0 | 0.00% |
| **All** | **4,535,063** | | | **9.61%** |

- **The early ALU's share.** The ready producers in its classes cause
  962,844 clocks, 2.04%. Of those, 940,384 (1.99%) are 32-bit ADD, INC,
  DEC, SUB, MOV and zeroing, every one an add: a register plus an immediate
  or a register, or one of them alone. The classes' whole AGI is 1,083,976
  (2.30%).
- **What it cannot reach.** The logic ops and shifts are almost never
  ready: their sources come from loads. Neither are loads, POP or an ALU op
  with a memory source.
- **Stage.** A ready ADD waits most often while it is in M2 (327,509 of
  580,994 clocks), so it is two uops ahead of the address that needs it.
  An early result removes the wait at every distance.

## 3DBench

The window is the profile record's: the bench disk `5c585ebc…`, 3DBench
started at the prompt, Enter at 3.8 s, the RTL from 4 s, and the window
4.5–8.0 s, 350,000,000 clocks, its fly-through
([3dbench-window.json](3dbench-window.json)). The run ends at its 8 s limit.
It retires 157.6M instructions at CPI 2.221, and AGI is 16,679,028 clocks:
5.04% of 331,154,782 kit-equivalent clocks.

| Producer | AGI clocks | Ready | Ready, 32-bit |
| --- | ---: | ---: | ---: |
| ADD, INC, DEC | 11,228,581 | 9,048,579 | 0 |
| MOV, register or immediate | 1,747,652 | 1,031,875 | 0 |
| Load | 1,134,269 | 1,133,583 | 0 |
| Shift | 961,367 | 302,436 | 0 |
| POP | 824,972 | 697,031 | 0 |
| SUB | 540,938 | 242,602 | 0 |
| XOR or SUB of a register with itself | 179,790 | 4,320 | 0 |
| AND, OR, XOR, NOT | 61,459 | 6 | 0 |

3DBench runs in real mode with 16-bit operands. Ready producers in the
early ALU's classes are 3.12% of its clocks, and ready 16-bit ADD, INC and
DEC alone 2.73%. A 16-bit result writes only the low half, so the early
value must take its high half from the old register, as AG already does
for a 16-bit stack's ESP.

## Loads

`fes_i386_dfwd` has three kinds of producer: early (the value is ready at
the end of AG), load (at the end of M2) and late (at the end of EX). D
issues a uop whose address register is a load's when the load is in M2, and
AG takes the value from EX's register as the load moves there.
`fes_i386_cpu` never drives the load kind: every producer that is not early
is late. So a load into an address waits one clock more than the design
intended. The clocks the split charges to a load in M2 are exactly that
clock:
- DOOM: 1,023,380, 2.17% of its kit-equivalent clocks, more than the early
  ALU's;
- 3DBench: 525,564, 0.16%.

The value is in EX's `ld` field there, not its `result`, which is what
`stage_value` and AG's late operand take. Driving the load kind needs that
value on AG's late path for loads whose value is the whole register (32-bit
MOV loads). It is not part of this slice.

## The early ALU

As built:
- **Forms.** At a 32-bit operand size: ADD r, imm and ADD r, r2, INC, DEC,
  MOV r, imm and MOV r, r2. At 16 bits: ADD r, imm, INC and DEC. Only
  one-uop instructions with no memory operand that write a GPR.
- **Left out.** SUB (its 0.4% would need a negated immediate on the Q to DC
  path), the logic ops and shifts (their sources are loads), and 16-bit MOV
  and ADD r, r2 (two registers' halves).
- **Fallback.** A uop is early only when D's address forwarding has its
  operands. Otherwise it runs at EX as before, so the early ALU never adds a
  wait.

### Where DOOM's clocks went

Both runs hand over from Bochs at the same page flip, so the first
half-second after it (22.226–22.726 s) runs the same code:

| Bucket | Before | After | Change |
| --- | ---: | ---: | ---: |
| AGI | 2,728,722 | 2,019,117 | −709,605 |
| Front end empty | 280,012 | 673,536 | +393,524 |
| Uops issued | 37,102,562 | 37,312,973 | +210,411 |
| L1D miss | 6,331,752 | 6,391,927 | +60,175 |
| Instructions retired | 33,461,218 | 33,607,759 | +146,541 (+0.44%) |

- **Front end.** D now issues faster than the front end decodes, and 55%
  of the AGI clocks saved become clocks with nothing in DC. The front end's
  bandwidth is now this code's limit.
- **Kit.** That matches the kit's +0.7%, against the split's 2.0%.
- **3DBench.** Its window has no such loss: front end empty rises by 0.1M
  against AGI's fall of 8.9M.

The second half-second, the one slices 1 to 4 used, is in
[doom-window-after.json](doom-window-after.json). A faster CPU reaches later
demo code in it, so its comparison is weaker: AGI 4,535,063 → 3,790,994
clocks, CPI 1.710 → 1.701.

### Latency

`scripts/test_fes_i386_latency.py` ([latency-report.json](latency-report.json)):
`agi_alu` takes 22,029 clocks, from 43,029, 2.50 a case from 5.13. Every
other loop takes exactly the clocks it did.

### Lockstep

`scripts/test_fes_i386_lockstep.py`, on the RTL and with `--yosys` on the
locked Yosys's netlist, matches main's results:
- the corpus: 1,758,699 tests pass;
- 32,000 random programs: one failure, `random#7#1936`, which is #1510, as
  on main;
- 8,000 protected-mode programs, the unbuilt encodings, the unreal-mode,
  protected-mode, no-387 and memory cases: all pass;
- 64 directed cases pass, 57 before this slice.

The new groups (`lockstep_tb.cpp`, `run_ealu_cases`) run each form at
distances 0 to 3 from an address or operand that uses it:
- `ealu-16` and `ealu-16-wrap`: ADD, INC and DEC of SI, with ESI's high half
  set and SI wrapping at FFFFh;
- `ealu-32`: the 32-bit forms, with [ESI], [ESI + EBX] and a scaled index;
- `ealu-late`: sources a load gives, so the ADD runs at EX;
- `ealu-esp`: ADD ESP and SP before a push and a load, MOV EBP, ESP;
- `ealu-chain`: one early result into another;
- `ealu-other`: the forms that stay at EX, and the flags.

## Timing

`make build-fes-pc386-quartus SEED=1` ran from a clean checkout of
`561271c3b`. Package `c214820c…` passes with every analysis met. The
system clock is 61.44 MHz.

| Analysis | Main `254b2bf5` | This slice `c214820c` |
| --- | ---: | ---: |
| Setup, worst | +0.410 ns | +0.802 ns |
| Hold, worst | +0.102 ns | +0.104 ns |
| ALMs | — | 32,629 of 41,910 |

[stage-slack.json](stage-slack.json) has the worst setup slack into each
register group at the −40 °C corner. D's and AG's groups have as much slack
as on main or more:
- D's EIP: +2.61 ns (main +1.13);
- AG: +2.85 ns (+2.61);
- the forwarding tables: +2.46 ns (+1.84);
- DC: +2.15 ns (+2.45).

The worst path into both D and AG is still D's issue decision. EX's worst
fell from +6.80 to +3.70 ns, on the divider's flags, which this slice does
not touch: that is placement.

## Kit

Kit A ran core-only at max on baseline software `a8cd30c4` and image
`f904961e`, with no install, no image change and no reboot
([kit-scores.json](kit-scores.json)):
- **Run.** `t1`, 6 November 20:50–21:03, with package `c214820c…`.
- **After.** The kit was checked against its pre-run snapshot and logged
  FREE. The run's five FogCast data trees were removed at 21:05, and it was
  logged FREE again.
- **Disk and commands.** As slice 4's run.

| Benchmark | Before (slice 4, `s2`) | After (`t1`) | Change |
| --- | ---: | ---: | ---: |
| DOOM v1.9 `-timedemo demo3` | 3,863 gametics in 3,448 realtics, 39.2 fps | 3,863 in 3,425, 39.5 fps | +0.7% |
| 3DBench | 76.9 | 76.9 | 0 |
| PCPBench | 13.2 | 13.2 | 0 |
| TopBench | 309 | 309 | 0 |
| Pack DOOM, maximum detail | 2,134 in 2,111, 35.4 fps | 2,134 in 2,096, 35.6 fps | +0.7% |
| Pack DOOM, minimum detail | 2,134 in 766, 97.5 fps | 2,134 in 759, 98.4 fps | +0.9% |

## Commands

```bash
python3 scripts/sim_fes_pc386.py --suite cpu-profile
machine_tb --bios pc386-bios.bin --vgabios pc386-vgabios.bin --disk doom.img --cpu rtl --cpu-profile cpu.prof \
  --limit-seconds 300 --type 'cd doom|doom -timedemo demo3|' --rtl-at-flip 40 --rtl-for 1.0 --cpu-profile-every 0.5
machine_tb --bios pc386-bios.bin --vgabios pc386-vgabios.bin --disk bench.img --cpu rtl --cpu-profile cpu.prof \
  --limit-seconds 8.0 --type 'b3d|' --keys '3.8:|' --rtl-from 4 --cpu-profile-every 0.5
python3 scripts/pc386_cpu_profile.py cpu.prof --from 22.726049 --to 23.226049 --json doom-window.json
python3 scripts/pc386_cpu_profile.py cpu.prof --from 4.5 --to 8.0 --json 3dbench-window.json
```

The disks hold commercial software and are not committed.
