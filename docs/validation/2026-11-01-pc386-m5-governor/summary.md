# pc386 M5: the i386 speed governor, 1 November 2026

[#1357](https://github.com/DeanoC/fes/issues/1357), part of
[#1267](https://github.com/DeanoC/fes/issues/1267) (M5 = CPU Phase 3). The
CPU now has the governor that
[`i386-cpu-design.md`, "Speed control"](../../../sources/misteross/docs/i386-cpu-design.md#speed-control)
describes. `pc386_system` sets it from the `fes.pc386.machine` record, which
FogCast already writes:

- word 0: the preset;
- word 1: the frequency override, in 100 kHz units;
- word 3: turbo at launch.

Ultima VI's 286-class preset and The Black Gate's 386-class one now have
something to set.

**On Kit B, the sealed package ran at the speed each preset set through the
record, and matched the simulation to within one pass in 27,000.** Set to
286-12, the Turbo Pascal–style calibration counts 27,454 delay-loop passes
in a timer tick (DelayCnt 499); at `max` it counts 210,892 (DelayCnt 3,834).
The table time is 27,463 at 286-12. Every governed preset ran within 0.04% of
its table time, on the kit and in `pc386_system`'s simulation. The lockstep
runs against the Bochs oracle under 286-12 match `max` in every architectural
result.

This record has three kinds of evidence:
- host-only checks: simulation, lockstep against the Bochs oracle, and the
  Yosys netlist;
- a sealed package;
- exact-artifact hardware evidence of the speed change on Kit B.

It is not acceptance of any game, or of the presets' tuning by play. The
games slice does both.

## Inputs

- **Branch.** FES `deano/pc386-m5-governor-35a3f1`, from main `58edb6b3b`.
  [#1335](https://github.com/DeanoC/fes/pull/1335), the keyboard and mouse
  slice, was not on main yet.
- **Main merge.** `3cf44fc41` merges main `81662e4ad`, with
  [#1363](https://github.com/DeanoC/fes/pull/1363), in a plain merge. #1335
  is still not on main. The merge changes no sealed input and no RTL.
  After it, the governor suite gives the same counts on every preset. These
  also pass:
  - the pc386 device suite;
  - main's FESENV boot of the games image;
  - the 1,605 misteross unit tests;
  - `test-changed`'s always and parent lanes.

  The other cores' RTL simulations, which the merge doesn't touch, were
  left to CI. Main's `boot_suite` test now covers governed names
  (`57940e00e`).
- **RTL.** `ab30f6c11`: sealed, and run on the kit. Its CPU RTL equals
  `b10c40664`'s, which the lockstep and governor bench ran.
- **Governor suite.** Its five presets ran on `pc386_system` at
  `4a6826570`. That commit differs from `ab30f6c11` only in
  `.init_gpr('0)`, which `ab30f6c11` writes as `256'd0` (the Yosys
  workaround below). The 6 MHz override run and the FreeDOS system boot
  under 386DX-25 ran at the head's RTL.
- **Tools.**
  - Verilator 5.032 on Powerboat, and NASM 2.16.03, the test386 runner's pin.
  - Quartus Prime Lite 17.0.2 for the seal, with the netlist from the pc386
    lock's Yosys `0a7927bf`, which also wrote the netlist the governor bench
    simulated.

## What it does

- **Charges.** `fes_i386_governor` adds each instruction's reference cost,
  in clocks of the preset's machine, to a debt as WB retires it.
  - A branch WB takes charges its taken cost.
  - A REP string charges each element as it retires, and its base as it
    ends.
  - Every memory bus cycle a committed uop makes adds the preset's wait
    states. On the 16-bit bus of the 286 and SX presets, a dword access adds
    its second cycle.
  - An address of base, index and displacement adds one clock.
  - An exception or interrupt delivery charges as INT.
- **The table.** [`governor.toml`](../../../sources/misteross/cores/fes-common/rtl/i386/governor.toml)
  holds seven presets and 128 cost classes (111 distinct rows). The counts
  are the 80286, Intel386 and i486 real-mode figures, and the file names its
  sources. `scripts/fes_i386_governor.py` checks the table and generates
  the classifier and an M10K cost ROM. It refuses an encoding in two classes,
  and requires every encoding the lockstep's scope builds to have one.
- **Pay-down and hold.** Each clock, a step at the preset's frequency pays
  the debt down, never below zero. The step is `GOV_N`/`GOV_D` of the clock:
  5/3072 at 61.44 MHz, from the producer's guest rates. While the debt is
  over 64 reference clocks, D starts no instruction and no REP element. A
  sequence's later uops are never held.
- **Off the critical paths.** Each step from WB to the hold is a register:
  the retirement, the class and ROM address, the ROM word, the charge, the
  debt, and the hold compare. That is five clocks from a retirement to the
  hold, and one more to D, where `issue` gains one registered term. At `max`
  (or with turbo) it charges nothing and never holds.

| ID | Preset | Table | Frequency | Wait states | Bus |
| ---: | --- | --- | ---: | ---: | ---: |
| 1 | 286-12 | 80286 | 12 MHz | 1 | 16 |
| 2 | 386SX-16 | Intel386 | 16 MHz | 1 | 16 |
| 3 | 386DX-25 | Intel386 | 25 MHz | 0 | 32 |
| 4 | 386DX-33 | Intel386 | 33.3 MHz | 0 | 32 |
| 5 | 386DX-40 | Intel386 | 40 MHz | 0 | 32 |
| 6 | 486DX-33 | i486 | 33.3 MHz | 0 | 32 |
| 7 | 486DX2-66 | i486 | 66.7 MHz | 0 | 32 |

## Checks

- **Architectural invisibility.**
  - At `max`, the branch's full lockstep equals main's in every reported
    field, cycle counts included: corpus 1,758,699 states and 174,952,748
    cycles, random 32,000 programs and 154,243,909 cycles
    ([`lockstep.json`](lockstep.json)).
  - Under 286-12, `test_fes_i386_lockstep.py --preset 1 --same-as <max
    report>` passes, and every architectural result equals `max`'s: the
    1,758,699 corpus states, the 32,000 random programs, faults, interrupts,
    divergences, and every corpus file's and random seed's trace hash; the
    directed, memory and unbuilt modes too. The governor's timing shows only
    where it should: the random programs took 1,053,497,554 cycles against
    154,243,909, and the corpus 195,084,746 against 174,952,748. Replays and
    restarts for self-modifying code moved with the timing (146,273 and
    17,241 against 148,203 and 17,243), and the latency-free holds are not
    checked under a preset.
  - The governor bench's runs all halt with the same registers as `max`'s.
- **FreeDOS under a preset.**
  - The RTL machine sim (`machine_tb`, at 100 MHz) boots under 386DX-40
    (`--suite boot --cpu rtl --preset 5`,
    [`boot-rtl-386dx40-report.json`](boot-rtl-386dx40-report.json)) and
    passes. FreeDOS reaches `C:\>` and lists the directory at 5.95 simulated
    seconds, against 4.1 at `max` on main. The RTL ran alone (no oracle
    steps, no stops), and its VGA trace replays against Bochs's VGA with no
    mismatch. It retired 52,708,504 instructions against 113,546,092 at
    `max`. Under 286-12 (`--preset 1`,
    [`boot-rtl-286-12-report.json`](boot-rtl-286-12-report.json)) it passes
    too: `dir` at the prompt at 14.0 simulated seconds, the RTL alone, the
    VGA replay matching, 30,826,678 instructions retired. Its longest stretch
    without screen output (FreeDOS loading its shell) is the 386DX-40 run's
    1.73 s grown with the slower preset.
  - `system_tb` boots through the record under 386DX-25 (`--suite system
    --preset 3`,
    [`system-freedos-386dx25-report.json`](system-freedos-386dx25-report.json))
    and passes. FreeDOS reaches `C:\>` at 3.646 simulated seconds, against
    2.746 at `max` on main, with no stop and every underflow counter at zero.
    The governor held the CPU, and the record's 12 checks pass, its word 0
    holding the preset. The boot retired 19,276,848 instructions against
    45,544,393 at `max`: its time-paced loops run fewer passes on a slower
    CPU.
- **Charges and pacing.** `scripts/test_fes_i386_governor.py` runs fifteen
  loops under `max` and every preset at the sealed clock's
  `GOV_N`/`GOV_D` (table below).
  - Every loop's charge equals the table's exactly, on every preset.
  - Every loop at presets 1-5 is within 0.1% of its table time (budget 5%).
  - The same bench passes on the locked Yosys's netlist with numbers
    identical to the RTL's at the same clock ratio
    ([`governor-bench-netlist-report.json`](governor-bench-netlist-report.json)).
- **The record's other words.**
  - Word 1: 286-12 overridden to 6 MHz paces every loop within 0.05% of 6 MHz.
  - Word 3: 286-12 with turbo runs all 15 loops in exactly `max`'s cycles and
    charges nothing.
- **Interrupt latency.** INTR is raised while a loop of REP MOVSB, DIV, PUSH
  and POP runs. The bound is the window, the loop's largest single charge
  and the delivery's charge.

  | Preset | INTRs | Max latency | Mean | Bound | Debt max |
  | --- | ---: | ---: | ---: | ---: | ---: |
  | 286-12 | 114 | 84.8 | 60.3 | 115 | 132 |
  | 386SX-16 | 85 | 95.1 | 86.9 | 128 | 124 |
  | 386DX-25 | 39 | 71.2 | 61.9 | 123 | 114 |
  | 386DX-33 | 29 | 70.5 | 62.1 | 123 | 112 |
  | 386DX-40 | 24 | 66.4 | 58.4 | 123 | 111 |
  | 486DX-33 | 23 | 63.4 | 55.4 | 118 | 116 |
  | 486DX2-66 | 12 | 77.1 | 51.3 | 118 | 101 |

  Latencies and the debt are in reference clocks.
- **Turbo Pascal CRT delay calibration.** This is an equivalent loop, not
  Borland's code: `cores/fes-pc386/sim/governor_test.asm`. It boots from a
  1 MiB disk on `pc386_system`, with the preset set in the restored record as
  FogCast writes it (`sim_fes_pc386.py --suite governor`,
  [`system-governor-report.json`](system-governor-report.json)). The "table"
  column is the reference machine's figure: one tick (65,536 PIT clocks) at
  the preset's frequency, over the loop's data-sheet cost. That cost is SUB,
  SBB, JC falling through, CMP with ES:[DI] and its bus cycle, and JE taken:
  24 clocks on 286-12, 22 on 386SX-16, 21 on the 386DX presets.

  | Preset | Passes in a tick | Table | DelayCnt | Passes in 18 ticks | Table |
  | --- | ---: | ---: | ---: | ---: | ---: |
  | `max` | 210,892 | — | 3,834 | 3,796,053 | — |
  | 286-12 | 27,453 | 27,463 (−0.03%) | 499 | 494,129 | 494,329 (−0.04%) |
  | 386SX-16 | 39,933 | 39,946 (−0.03%) | 726 | 718,747 | 719,024 (−0.04%) |
  | 386DX-25 | 65,375 | 65,387 (−0.02%) | 1,188 | 1,176,721 | 1,176,973 (−0.02%) |
  | 386DX-40 | 104,609 | 104,620 (−0.01%) | 1,901 | 1,882,935 | 1,883,157 (−0.01%) |

  Each run's record, captured at Stop, holds the same figures in CMOS
  70h-79h with A5h in 7Bh, and its record checks pass. Those runs were at
  `4a6826570`. After Codex's review, at `1b3a348ba`, the suite passes
  `--frequency` and `--turbo` into the record with `--preset`. With 286-12
  overridden to 6 MHz (`--preset 1 --frequency 60`), it counts 13,722 passes
  in a tick and 246,964 in 18, against the table's 13,731 (−0.07%) and
  247,164 (−0.08%)
  ([`system-governor-6mhz-report.json`](system-governor-6mhz-report.json)).
- **Unit tests.** `tests/test_fes_i386_governor.py` (8),
  `tests/test_build_fes_pc386_quartus.py` (21) and
  `tests/test_fes_i386_ucode.py` pass, and `fes_i386_governor.py --check`.

## Pacing

These are cycles between each loop's marks at 61.44 MHz, against the table
time: the loop's charged reference clocks at the preset's frequency
([`governor-bench-report.json`](governor-bench-report.json)). "RTL slower"
marks a loop the RTL at `max` runs slower than the preset. Such a loop
takes the CPU's own time.

| Loop | max cycles | 286-12 | 386SX-16 | 386DX-25 | 386DX-33 | 386DX-40 | 486DX-33 | 486DX2-66 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| alu | 63,999 | -0.05% | -0.06% | -0.08% | -0.09% | -0.09% | +8.34% (RTL slower) | +117.01% (RTL slower) |
| memory | 60,100 | -0.02% | -0.02% | -0.02% | -0.02% | -0.02% | -0.06% | +55.29% (RTL slower) |
| dword | 48,001 | -0.03% | -0.02% | -0.02% | -0.02% | -0.02% | -0.01% | +73.61% (RTL slower) |
| branch | 147,115 | -0.00% | +0.00% | -0.00% | -0.00% | +0.01% | +56.35% (RTL slower) | +213.06% (RTL slower) |
| stack | 60,000 | +0.01% | +0.02% | -0.01% | -0.01% | -0.01% | -0.09% | +3.36% (RTL slower) |
| muldiv | 54,999 | +0.00% | +0.01% | +0.00% | +0.01% | +0.00% | +0.00% | -0.08% |
| string | 19,059 | -0.00% | +0.01% | +0.01% | -0.00% | -0.00% | +0.02% | -0.01% |
| shift | 54,002 | -0.02% | -0.02% | -0.02% | -0.02% | -0.02% | +0.01% | +0.01% (RTL slower) |
| crt | 75,008 | -0.00% | -0.00% | -0.01% | -0.02% | -0.03% | +1.59% (RTL slower) | +103.49% (RTL slower) |
| io | 55,998 | -0.00% | +0.01% | +0.01% | +0.01% | +0.00% | -0.07% | -0.08% |
| int | 28,028 | +0.01% | +0.03% | +0.03% | +0.02% | +0.02% | +0.05% | +24.11% (RTL slower) |
| misc | 39,999 | -0.04% | -0.04% | -0.00% | -0.01% | -0.01% | +0.01% | +14.22% (RTL slower) |
| loop | 35,999 | -0.02% | -0.03% | -0.03% | -0.03% | -0.03% | -0.03% | +62.72% (RTL slower) |
| pusha | 22,499 | -0.02% | +0.06% | +0.07% | -0.03% | -0.02% | +1.75% (RTL slower) | +103.27% (RTL slower) |
| i386 | 29,999 | -0.04% | -0.04% | -0.00% | -0.03% | -0.03% | -0.16% | +16.25% (RTL slower) |

The 486 presets are beyond the RTL on loops with taken branches, because
a redirect costs about 11 cycles until Phase 4's prediction. They are kept
outside the accepted set (presets 1-5) until then.

## Seal

Quartus 17.0.2 sealed `ab30f6c11` from a clean checkout (seed 1,
[`seal-summary.json`](seal-summary.json)):

- **Identity.** Build `c210ae8b575df9c5f36b405d7b09ea0f`, package
  `dcaea9ce12763cae10badebbff300d3683b535d25e598a22aad7be63bb9051fb`, RBF
  `13893f80…`.
- **Timing.** Every analysis passes on every clock:
  - setup +1.478 ns;
  - hold +0.092 ns;
  - recovery +7.631 ns;
  - removal +0.435 ns;
  - minimum pulse width +1.122 ns.

  The system clock is 61.44 MHz, with Fmax 70.37 MHz at the slow corner
  (the last seals were 67.6-72.9 MHz).
- **Coverage.** The build record names `ab30f6c11`. All 108 pinned inputs
  hash the same at this record's head, after the main merge. Of the
  branch's later commits, none touches an input.
- **Resources.** 24,318 ALMs, 22,315 registers, 257 RAM blocks and 31 DSP
  blocks, against #1320's seal of the same shell without the governor:
  24,246 ALMs, 254 RAM blocks and 29 DSP blocks.

## Kit

Run gov1 was on Kit B, 10:31-10:34 EET, core-only on the kit's baseline:
software `86cc27b6`, image `f904961e`, shared host `58edb6b3`. Nothing was
installed, and there was no image change, reboot or host change. It was
claimed and released in `kitB-STATUS.md`, with the baseline verified
unchanged (no reboot, same keys) and the agent idle with its lease free
([`kit-gov1-result.json`](kit-gov1-result.json)).

Under the agent's kit lease, the run did what FogCast's host does for a pc386
library game:
- It staged the governor test disk (`0cc048d2…`) by SHA-256.
- For each preset, it wrote game `pc386-m5-governor`'s machine settings
  (`/v1/library/core/settings`, `P,0,0,0,544,7,1`).
- It launched the sealed package with the disk (`/v1/library/core/load`),
  waited 30 s, grabbed HDMI and stopped.
- It then read CMOS 70h-7Bh from the saved record on the kit.

| Preset | Record word 0 | Passes in a tick (sim) | DelayCnt | Passes in 18 ticks (sim) | Boot count |
| --- | ---: | ---: | ---: | ---: | ---: |
| `max` | 0 | 210,892 (210,892) | 3,834 | 3,796,054 (3,796,053) | 1 |
| 286-12 | 1 | 27,454 (27,453) | 499 | 494,128 (494,129) | 2 |
| 386DX-25 | 3 | 65,375 (65,375) | 1,188 | 1,176,721 (1,176,721) | 3 |
| 386DX-40 | 5 | 104,610 (104,609) | 1,902 | 1,882,934 (1,882,935) | 4 |

The kit and `pc386_system`'s simulation agree to within two passes. The
speed change from `max` to 286-12 is 7.7 times. `kit-gov1-286-12.png` and
`kit-gov1-max.png` are the screens: `GOV TP=00006b3e DC=01f3 C18=00078a30`
at 286-12. The run left the staged disk and the game's record and media
directory in FogCast's data directories.

## Not done here, and design notes

- **Runtime turbo** ([#1358](https://github.com/DeanoC/fes/issues/1358)).
  The design says turbo switches while running, but not how it is triggered.
  This slice reads turbo at launch only.
- **ISA bus time** ([#1362](https://github.com/DeanoC/fes/issues/1362)). I/O instructions and VGA memory are charged
  their data-sheet cost, and the machine keeps its own `isa8` device time.
  While the governed CPU is ahead of the reference, device time spends that
  lead, so a port loop takes the larger of the two, not a period AT's ISA
  time. Ultima VI's speaker effects may come out fast at 286-12. The
  follow-up proposes a per-preset ISA charge, if play shows it is needed.
- **The hold also applies between REP elements**, not only between
  instructions as the design first said, so a long REP string cannot run
  unthrottled and then stall interrupts. The design doc now says so.
- **Counts the bytes do not give** take a typical value: the middle of an
  early-out multiply's range, a 286 shift by 4, a 386 bit scan of 8, and
  ENTER at level 0. Prefixes, and the 386's and 486's queue and cache
  effects, are not charged.
- **Yosys.** Once `fes_i386_cpu` takes parameters it is a parameterised
  cell, and `pc386_system`'s `.init_gpr('0)` hits the assert of
  [DeanoC/yosys#71](https://github.com/DeanoC/yosys/issues/71) under
  `hierarchy -chparam`. It is a second trigger of that issue, an unsized
  literal; the repro is posted there. FES sizes the literal.
