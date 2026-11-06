# i386 Phase 4 slice 6: the early ALU, 6 November 2026

This record covers CPU Phase 4's slice 6
([`i386-cpu-design.md`, "Phase 4: speed"](../../../sources/misteross/docs/i386-cpu-design.md#phase-4-speed),
tracker [#1488](https://github.com/DeanoC/fes/issues/1488)). The plan's
first step splits the profile's AGI wait by the uop that produced the
address register. This record has that split and the estimate it gives.

**Tree.** Main `5e201aa3b`, which has slices 2 to 4 (#1511, #1512, #1515),
plus the profile split on this branch. The split adds bench-only signals
and changes nothing the CPU does.

**Result.**
- **DOOM `demo3`.** AGI waits are 9.1% of the window's clocks. Producers
  an early ALU at AG would run cause 0.96M of them, 2.0% of the window's
  kit-equivalent clocks. Nearly all are 32-bit ADD, INC, DEC, SUB with an
  immediate, and MOV, which AG's address adder can run as it runs a 32-bit
  LEA today. The bracket is 2.0–2.3%: about 39.2 to 40.0 fps.
- **3DBench.** AGI waits are 5.0%. Producers an early ALU would run cause
  3.1%, and 2.7% of that is 16-bit ADD, INC and DEC in real mode. A 32-bit
  early ALU gains nothing here; a 16-bit one needs the destination's high
  half at AG.
- **Loads.** A load's value into an address costs DOOM 4.0%, more than any
  ALU class. Half of it, 2.2%, is a wait that `fes_i386_dfwd`'s `LOAD` kind
  was designed to remove ([below](#loads)).
- **Decision.** The DOOM figure is over the plan's 1.5% stop line, so
  step 2 (the build) goes ahead.

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
