# i386 x87: a plan for a 387 in pc386, 12 November 2026

This record plans the x87 phase (the design's
[Phase 6](../../../sources/misteross/docs/i386-cpu-design.md#milestones)) for
`fes_i386_cpu` in `fes.pc386`. Today the CPU behaves as a 386 with an empty
387 socket ([#1430](https://github.com/DeanoC/fes/pull/1430),
[record](../2026-11-02-i386-no387/summary.md)). Quake and other titles stop at
their x87 instructions. The record measures which titles use a 387 and how,
on the Bochs oracle with a 387 fitted. It sets what a real 386 and 387 on an
AT board do that software can see, weighs the datapath options against the
area that is left, and orders the work into slices. It changes no RTL.
Tracker: [#1768](https://github.com/DeanoC/fes/issues/1768).

**Result.**
- **Two games need a 387: Quake and Screamer.** Quake's
  timedemo retires 45.8M x87 instructions in 25 simulated seconds, with 97%
  of its arithmetic at 24-bit precision and 87% of its x87 instructions
  under round-to-zero. Its mix is loads 23%, FXCH 20%, multiply 19%, add
  18%, stores 11%, compares 6%, divide 1.8%; FSIN and FCOS are 0.05%
  (21,137) and FSQRT 0.003%. Screamer retires 1.27M in 30 seconds, all at
  53-bit precision, with 61,006 FLDCWs (it switches rounding around its
  float-to-integer conversions) and 0.76% transcendentals.
- **Two benchmarks and one game use a 387 when there is one.** Chris's 3D
  Benchmark retires 15.2M in 20 seconds at 64-bit precision, a tenth of
  them FLDCW and FNSTCW; SpeedSys's FPU test retires 53.3M register-only
  instructions in real mode; Terminal Velocity retires 6,850 at its menus,
  7.5% of them FSIN and FCOS, and reached its main menu on #1533's
  empty socket. Norton SI 8 identifies the 387 ("Math Co-Processor: 80387")
  in 26 instructions.
- **The rest only probe, or use none.** DOOM, Rise of the Triad, Descent,
  Duke Nukem 3D, One Must Fall 2097, U.S. Navy Fighters, Frontier and
  Falcon 3.0 run FNINIT, FNSTSW, FNSTCW and at most the 287/387 infinity test
  (FLD1, FLDZ, FDIVP, FCHS, FCOMPP) in their runs. Duke Nukem 3D adds one
  FDIV of two 64-bit loads, the shape of a Pentium FDIV-bug check. Strike
  Commander, Serpent Isle and CHKCPU run no x87 instruction. Descent's runs
  reach its main menu, not its renderer; the perspective mapper in its
  released 1.5 source divides with IDIV, one divide per 16 pixels
  (`texmap/tmap_per.asm`, `NBITS equ 4`; the FDIV mappers in that tree were
  added by the 1998 d1x port).
- **The hot path is short.** Across the four that compute in volume, loads,
  stores, FXCH, add, multiply and compare are 86–96% of x87 instructions;
  divide is at most 1.8%, square root 0.44%, transcendentals 0.76%. The
  control-word and status-word forms are next: FLDCW, FNSTCW and FNSTSW AX
  are 10% of Chris's benchmark and 9% of Screamer. That decides where area
  goes: [Datapath options](#datapath-options).
- **No run raises an unmasked exception or touches F0h/F1h.** Every run
  computes with all six exceptions masked (control word low byte 3Fh),
  except 139 of Quake's instructions, which run with invalid and
  divide-by-zero unmasked (3Ah) and raise neither. No run sets ES, and none
  writes ports F0h or F1h. CWSDPMI (Quake, Chris's benchmark) and WIN386
  switch x87 context lazily, setting TS and taking #NM: 558, 164 and 1 of
  them.
- **The oracle hangs on an unmasked x87 exception,** and Linux 2.0.0 booted
  with its 387 hits one about 0.19 s into the kernel
  ([below](#the-oracles-error-path-is-not-the-ats)). Fixing the oracle is
  slice 0.
- **The design's x87 budget (7,000 ALMs) is more than the 5,107 ALMs left.**
  A lean, DSP-heavy unit is estimated at about 2,400 ALMs, 93.5% of the
  device, above the 92% working ceiling. A fit spike decides
  ([Area and timing budget](#area-and-timing-budget)).

## Method

- **Tree.** Main at `2d895034c` in a scratch tree on chip-30x0-ai-dev,
  with a census patch that is not committed (scratch:
  `~/fes-scratch/x87plan/x87-census-full.diff` on Powerboat). The patch:
  - adds `x87_words()` to the oracle interface, Bochs's control word and
    status word with TOP;
  - counts every ESC by form (opcode and ModRM reg for memory forms, the
    whole ModRM byte for the register forms that need it: D9 E0–FF, DB E0–E7,
    DE D9, DF E0, DA E9, DD E0–EF) and mode, faults by vector, the control
    word each ran under by class (add, multiply, divide, square root,
    transcendental), exception flags raised and unmasked, ES going high,
    WAIT with ES pending, #NM and #MF with CR0's x87 bits, CR0's MP/EM/TS/ET
    changes, INT 34h–3Eh (Microsoft and Borland emulator fixups) and I/O to
    F0h–FFh;
  - sets the CMOS equipment byte (14h) to 06h, so INT 11h reports a
    coprocessor as on a machine with one fitted. Main's is 04h.
- **Runs.** `machine_tb --cpu oracle --census` with Bochs's 387, each title's
  launch and keys from its drive record, 20 simulated seconds (25 for Quake,
  30 for the `fpu` bucket's titles, 45 for the longer Descent run). Drives:
  `real` a6977555, `xms` 5053c36b, `pmode` b4be6e29, `dpmi` 9e6ae3a3, `fpu`
  ff322ddc and `bench` e69a4cb6 (SHA-256 prefixes, checked on the host),
  Quake on the [Phase 5 plan](../2026-11-08-i386-phase5-plan/summary.md)'s
  `bench-q` copy (`Q.BAT`). WfW 3.11 on `win-msdos` (c5d77840); Linux 2.0.0
  on a copy of its disk with `no387` taken off the LOADLIN line.
- **Calibration.** Quake's 45,787,636 x87 instructions equal its D8–DF
  stem total in the same census and the Phase 5 record's 45.7M.
- **Title choice.** The sibling compatibility census was still running and
  had not landed, so this run took its own subset: every title in which
  [#1533](https://github.com/DeanoC/fes/pull/1533)'s census saw x87 escapes
  or put in the `fpu` bucket, the 32-bit titles most likely to compute in
  floating point, the DOSBench tools that report or time an FPU, Linux and
  WfW. Titles #1533 found x87-free were not rerun.
- **What a run can miss.** A 20- to 45-second launch window covers start-up
  and menus; a title that uses the 387 only in play is undercounted. Runs
  that stopped short of play for reasons outside the CPU: Grand Prix 2 needs
  its CD, U.S. Navy Fighters asks for its CD, Frontier needs EMS (rerun on
  the EMS boot entry, where it waits at its sound prompt), and Descent
  waits at its main menu without starting its demo.
- **Static scan.** Each drive's executables were searched for emulator and
  coprocessor strings (`emu387`, `WEMU387`, "coprocessor required" and
  similar). Packed executables hide strings, so this is weak evidence. Hits:
  NetHack's directory and the DOSBench pack's `MARKS` tree (three files,
  where Quake and Chris's benchmark live) carry DJGPP's `emu387` and a
  "requires coprocessor" message; no `WEMU387`.
- **Not committed.** Drives, frames and the patch stay on the hosts.
  [`census.jsonl`](census.jsonl) has one row per run: counts by class and
  form name, control-word precision and rounding, events and ports. It
  holds no game bytes, frames or instruction bytes.

## Who needs a 387

### By title

Percentages are of the run's x87 instructions; titles with fewer than 1,000
show counts. "Precision" is the control word's precision field over add,
multiply, divide and square root. FWAIT counts standalone WAIT (9Bh)
instructions.

| Title | Run (s) | x87 retired | load / store / FXCH | add / mul | div / sqrt | transc. | control | precision (arith.) | FWAIT |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: |
| SpeedSys 4.78 | 20.0 | 53,336,842 | 23.93% / 11.58% / 0.00% | 26.46% / 17.36% | 0.00% / 0.00% | 0.00% | 0.00% | 64-bit 100% | 3,087,243 |
| Quake timedemo (DOSBench) | 25.0 | 45,787,636 | 22.53% / 11.12% / 19.73% | 17.60% / 19.16% | 1.83% / 0.00% | 0.05% | 1.76% | 24-bit 97%, 64-bit 3% | 28,023 |
| Chris's 3D Benchmark | 20.0 | 15,236,311 | 27.90% / 22.66% / 0.61% | 10.16% / 8.17% | 1.48% / 0.44% | 0.02% | 10.23% | 64-bit 100% | 719 |
| Screamer (1995) | 30 | 1,266,208 | 26.14% / 12.94% / 7.66% | 22.29% / 10.74% | 0.46% / 0.05% | 0.76% | 8.78% | 53-bit 100% | 137,449 |
| Terminal Velocity (1995) | 30 | 6,850 | 15.74% / 15.58% / 7.52% | 7.96% / 15.05% | 0.06% / 0.01% | 7.50% | 22.98% | 53-bit 100% | 5,860 |
| Duke Nukem 3D (1996) | 20.0 | 40 | 10 / 0 / 1 | 1 / 1 | 2 / 0 | 0 | 20 | 53-bit 75%, 64-bit 25% | 3,154 |
| Descent (1995) | 20.0 | 31 | 7 / 0 / 0 | 0 / 0 | 1 / 0 | 0 | 20 | 64-bit 100% | 4,627 |
| Descent (1995) | 45 | 31 | 7 / 0 / 0 | 0 / 0 | 1 / 0 | 0 | 20 | 64-bit 100% | 5,186 |
| U.S. Navy Fighters (Gold) (1995) | 30 | 30 | 6 / 0 / 0 | 0 / 0 | 2 / 0 | 0 | 17 | 64-bit 100% | 10 |
| Norton System Information 8 | 20.0 | 26 | 4 / 1 / 0 | 0 / 0 | 1 / 0 | 1 | 17 | 64-bit 100% | 22 |
| One Must Fall 2097 (1994) | 20.0 | 23 | 3 / 0 / 0 | 0 / 0 | 1 / 0 | 0 | 16 | 64-bit 100% | 4,829 |
| Grand Prix 2 (1995) | 30 | 15 | 0 / 0 / 0 | 0 / 0 | 0 / 0 | 0 | 15 | - | 4,876 |
| DOOM (1993) | 20.0 | 11 | 0 / 0 / 0 | 0 / 0 | 0 / 0 | 0 | 11 | - | 6,167 |
| Rise of the Triad - The Hunt Begins (Deluxe Edition) (1995) | 20.0 | 11 | 0 / 0 / 0 | 0 / 0 | 0 / 0 | 0 | 11 | - | 3,522 |
| Frontier - Elite 2 (1993) | 20.0 | 10 | 3 / 0 / 0 | 0 / 0 | 1 / 0 | 0 | 4 | 64-bit 100% | 7 |
| Frontier - Elite 2 (1993) (`frontier-e3`) | 25 | 10 | 3 / 0 / 0 | 0 / 0 | 1 / 0 | 0 | 4 | 64-bit 100% | 7 |
| WfW 3.11, WIN (386 enhanced) | 20 | 8 | 0 / 0 / 0 | 0 / 0 | 0 / 0 | 0 | 8 | - | 3 |
| Falcon 3.0 (1991) (`falcon3-e2`) | 20 | 3 | 0 / 0 / 0 | 0 / 0 | 0 / 0 | 0 | 3 | - | 3 |
| CHKCPU | 12.0 | 0 | 0 / 0 / 0 | 0 / 0 | 0 / 0 | 0 | 0 | - | 0 |
| Strike Commander (1993) | 20.0 | 0 | 0 / 0 / 0 | 0 / 0 | 0 / 0 | 0 | 0 | - | 0 |
| Ultima VII Part 2 - Serpent Isle + The Silver Seed (1993) | 20.0 | 0 | 0 / 0 / 0 | 0 / 0 | 0 / 0 | 0 | 0 | - | 0 |

### Needs, probes, or emulates

| Title | On the oracle (387 fitted) | On an empty socket ([#1533](https://github.com/DeanoC/fes/pull/1533), Kit B) | Verdict |
| --- | --- | --- | --- |
| Quake timedemo | 45.8M x87 | stopped by Phase 5 before reaching x87 | **needs a 387** (its manual: "absolutely must have a Math Co-Processor") |
| Screamer | 1.27M x87 | black screen, last vector #NM | **needs a 387** |
| Chris's 3D Benchmark | 15.2M x87 | stopped by Phase 5 | uses a 387; its executables carry DJGPP's `emu387` for when there is none |
| SpeedSys | 53.3M x87 in its FPU test | stops at its ISA PnP probe (#1532) | times a 387 when there is one |
| Terminal Velocity | 6,850 x87 at its menus, 7.5% FSIN/FCOS | main menu | uses a 387 when there is one; menus run without |
| Norton SI 8 | identifies "80387" | system summary | reports it |
| DOOM | probe, 11 | plays | probes |
| Rise of the Triad | probe, 11 | sound setup | probes |
| One Must Fall 2097 | probe, 23 | plays | probes |
| Duke Nukem 3D | probe, 40 (with an FDIV-bug check) | stops at a 486 instruction | probes |
| Descent | probe, 31, to its main menu in 45 s | stopped by Phase 5 | probes; renderer is IDIV in its source |
| U.S. Navy Fighters | probe, 30, then asks for its CD | stopped by Phase 5 | probes so far; needs its CD |
| Frontier | probe, 10, to its sound prompt (EMS boot entry) | not enough base memory | probes so far |
| Falcon 3.0 | probe, 3 (boot entry 2) | not enough free RAM | probes so far |
| Grand Prix 2 | probe, 15, then "CD drive/MSCDEX not detected" | the same | blocked by CD-ROM |
| Strike Commander, Serpent Isle, CHKCPU | none | (see #1533) | no x87 |

**Emulators.** No run executed an INT 34h–3Eh emulator fixup, so none of
these titles was built with Borland's or Microsoft's fixup emulation (with
a 387 fitted, each fixup still runs once and is patched to its ESC).
DJGPP's `emu387` loads only when no 387 is found, so with one fitted it
never runs; the static scan finds it in NetHack's directory and the
DOSBench `MARKS` tree (Chris's benchmark's `BENCH.EXE` and `SVGABNCH.EXE`,
and `QUAKE.EXE`). The [Phase 5 plan](../2026-11-08-i386-phase5-plan/summary.md)
read Chris's benchmark's 164 #NM as x87 emulation; with CR0's bits in this
census they are CWSDPMI's lazy context switch (TS set, 387 fitted).

**Windows and Linux.** WfW 3.11 in 386 enhanced mode runs 8 x87
instructions in 20 seconds: FNINIT, FNCLEX, FLDCW, FNSTCW and FNSTSW. WIN386
sets MP and TS, a ring-3 FNINIT takes #NM, and CLTS follows: lazy context
switching between virtual machines, which the empty-socket design already
supports (#NM on TS). Linux 2.0.0 with its 387
hangs the oracle in boot (see the interface section), so its x87 use is
not measured; the [early-Linux record](../2026-11-11-pc386-linux-plan/summary.md)'s
kernels run with `no387` and the kernel's own math emulation.

## The interface: what a 386 and 387 on an AT board do

The sources are Intel's 1987 80386 Hardware Reference Manual (HRM), the
80387 Programmer's Reference Manual 231917-001 (387 PRM), the 386 DX
Programmer's Reference Manual 230985-003 (386 PRM), the Intel387 DX
datasheet 240448-005, and IBM's PC AT Technical Reference 6183355 (AT TR).
Links are in [Sources](#sources).

### The coprocessor bus

- The 386 talks to the 387 by I/O cycles at 800000F8h (command out, status
  in) and 800000FCh (operands both ways), chip-selected by A31 high and
  M/IO# low (HRM p. 5-1 and §5.1–5.2). No other address is used. These
  cycles ignore IOPL (HRM p. 5-1), and on pc386 they never reach the ISA
  decoder.
- BUSY# must be inactive before the 386 starts any ESC instruction except
  FNINIT and FNCLEX (HRM p. 5-1). PEREQ asks the 386 for operand transfers;
  it goes inactive before BUSY#, and ERROR# goes active before BUSY# goes
  inactive (HRM p. 5-1).
- The 386 samples ERROR# only at the start of WAIT and of the ESC forms that
  wait (386 PRM p. 11-5). The no-wait forms are FNINIT, FNCLEX, FNSTSW,
  FNSTCW, FNSTENV and FNSAVE (387 PRM pp. 5-14/5-15). The 387 PRM's
  interrupt-16 table leaves FNSTCW out of the forms that cannot raise it;
  the datasheet says only FNINIT, FNCLEX, FNSTENV and FNSAVE clear ERROR#
  (datasheet §3.1.7). The plan follows the 5-14 list and records FNSTCW as a
  case to settle on real hardware. "No-wait" means only that the form skips
  the ERROR# check: FNSTSW, FNSTCW, FNSTENV and FNSAVE still wait for BUSY#
  like every ESC but FNINIT and FNCLEX, so they never see a computation in
  flight.
- Exception 16 is the 386's coprocessor error: "Any 386 DX … system with a
  coprocessor must use interrupt 16" (386 PRM, item 14 of the 386/286
  differences), with the saved CS:EIP at the ESC's prefixes. Exception 9 is
  a coprocessor operand's middle part crossing into a protected or
  not-present page or segment, and that instruction cannot be restarted
  (386 PRM pp. 9-16, 11-5).
- CR0.ET: the 386 samples ERROR# after RESET and sets ET when it is low,
  which a 387 drives after reset (HRM §3.7.2; 387 PRM App. C.1). But an AT
  board wires the 387 so that ET may stay clear, and the HRM says
  initialisation software may need to set it (HRM §5.4.1.1). Bochs sets ET
  whenever its x87 is built, whatever the board.

### The AT error path

- On the AT, the coprocessor's error does not reach the CPU's ERROR# pin.
  For the 286 AT, "the ERROR# input to the 80286 is tied inactive (high)
  permanently", and a flip-flop latches BUSY# with ERROR# as its clock
  (387 PRM App. F.1). For a 386 AT, the latch extends BUSY# and holds PEREQ
  until software writes port F0h (387 PRM App. F.2). The latched error
  raises IRQ 13 on the slave PIC.
- Port F0h clears the latched busy; port F1h resets the coprocessor (AT TR,
  "NMI and Coprocessor Controls"). The AT BIOS's INT 75h handler writes F0h,
  sends EOI to both PICs and runs INT 02h, the NMI vector (AT TR, the
  `HARDWARE INT 75 H` listing). pc386's BIOS (Bochs rombios) has the same
  handler, `int75_handler` in `rombios.c`.
- So on a real 386+387 AT, an unmasked 387 exception freezes the next
  waiting ESC or WAIT on BUSY# until the IRQ 13 handler writes F0h. Exception
  16 never happens on that board.
- **pc386 today.** The CMOS equipment byte (14h) is 04h, "no x87", in
  `pc386_rtc.sv`, so INT 11h's bit 1 is clear. The BIOS installs the INT 75h
  handler, but nothing drives IRQ 13 and nothing decodes F0h or F1h.

### The oracle's error path is not the AT's

Bochs's level-3 build models a 486's FERR# and IGNNE# path
(`cpu/fpu/ferr.cc`): a pending unmasked exception at the next waiting ESC
calls `DEV_extfpuirq_set_fpu_error(true)` and puts the CPU to sleep in
`BX_ACTIVITY_WAIT_FOR_X87` until IGNNE#. The oracle adapter
(`bochs_oracle.cpp`) installs Bochs's no-op `stubExtFpuIRQ` for that device,
so on the machine simulation's oracle no IRQ 13 is raised.

It is worse than a missing IRQ: **an unmasked x87 exception hangs the
machine simulation.** The error path longjmps out of the instruction before
the adapter's `fes_bx_after_execution` hook runs, so nothing sets
`kill_bochs_request`, which `step()` clears on entry. Bochs's
`handleWaitForEvent` then loops on its own `BX_TICKN` until an interrupt is
pending. The harness raises interrupts only between steps, so the step
never returns. Linux 2.0.0, booted on the oracle without `no387`, stops
there: simulated time stops at 4.19 s, 0.19 s after "Now booting the
kernel", with the process still at full CPU. Linux of this age checks the
386/387 coupling early in boot by unmasking exceptions and dividing by
zero, so that is the likely exception; the run cannot say which
instruction it was. The [early-Linux record](../2026-11-11-pc386-linux-plan/summary.md)'s
oracle runs passed `no387` and never reached this. The first slice gives the
oracle the AT's path.

### What software sees: timing

387 clocks from the datasheet (pp. 37–39), and the 386's transfer overhead,
about 14–20 clocks per ESC (Juffa's COPRO16A; Intel gives no figure):

| Instruction | 387 clocks |
| --- | --- |
| FNINIT | 33 |
| FNSTENV / FLDENV | 103–104 / 71 |
| FNSAVE / FRSTOR | 375–376 / 308 |
| FLD m64 / FST m64 | 16–23 / 32–44 |
| FILD m32 / FIST m32 | 26–42 / 57–76 |
| FXCH | 10–17 |
| FADD ST(i) | 12–26 |
| FMUL ST(i) | 17–50 |
| FDIV ST(i) | 77–80 |
| FSQRT | 97–111 |
| FSIN, FCOS | 121–680, 122–680 |
| FPTAN | 162–430 |
| FPATAN | 250–420 |
| F2XM1 | 167–410 |
| FYL2X | 99–436 |

A 387 runs concurrently with the 386: integer instructions continue while
it computes, and only a waiting ESC, WAIT, or a read of its state waits.
Code that times FNINIT or FNSAVE, or counts loop iterations around FDIV, sees
these figures. The governor's presets already charge the coprocessor-fitted
counts for the ten no-387 ESC classes (no387 record, "Governor"); with a 387
the presets need the 387's own counts per class and the overlap.

### Integrated, not a bus

A separate coprocessor on an internal copy of the 800000F8h bus would cost
two transfers per operand and a protocol state machine on each side, for
nothing software can see. The plan builds the 387 inside the CPU and keeps
every visible behaviour of the bus design:

- ESC issues to the x87 unit from D, as today's no-387 forms do. Memory
  operands use the existing M-stage uops (today's microcode already loops
  over the 387's FNSAVE and FNSTENV layouts and reads 8- and 10-byte
  operands as dword or word uops).
- BUSY is a scoreboard: integer uops keep issuing; every ESC except FNINIT
  and FNCLEX, and WAIT, waits for it, the no-wait forms included (FNSTSW AX,
  FNSTENV and so on). Only the pending-error check is skipped by the no-wait
  forms.
- ERROR# is not wired to the CPU. An unmasked exception sets ES, raises the
  machine's IRQ 13 through a latch, and holds BUSY until a write to F0h,
  as on an AT. F1h resets the unit. NE does not exist on a 386.
- ET follows a machine identity bit, `i386dx+387`, which also sets CMOS
  14h bit 1. With the bit clear, today's empty socket stays exactly as it
  is, so the no-387 behaviour remains selectable per title.
- Exception 9 is raised where the 386 would raise it, for an operand whose
  middle crosses into a protected or not-present page or segment.

## Datapath options

Four ways to build the unit. The area figures are the estimates in
[Area and timing budget](#area-and-timing-budget); the latencies are CPU
clocks at the sealed 61.44 MHz system clock.

| | A. Microcoded, DSP-heavy | B. Small iterative | C. HPS offload of everything | D. Fabric core, HPS for the slow ops |
| --- | --- | --- | --- | --- |
| Fabric | 80-bit registers in M10K, 68-bit adder and rounder, 64×64 multiply on 9 DSP blocks, shifts as DSP multiplies, divide and square root by Goldschmidt or Newton on the multiplier, transcendentals in microcode | one 32-bit slice of the datapath, multiply on one 27×27 DSP block in nine passes, divide and square root one bit (or two) a clock, transcendentals in microcode | the CPU side only: ESC issue, operand uops, a mailbox to the HPS, the registers' shadow for FNSTSW and FNSTCW | A without transcendentals, FPREM, FBLD and FBSTP; those go to the HPS |
| ALMs (estimate) | about 2,400 | about 1,300–1,600 | about 700 | about 2,100 |
| DSP blocks | 9–12 | 1–2 | 0 | 9–12 |
| FADD / FMUL / FDIV | about 4 / 5 / 20 | about 8 / 20 / 70 | HPS round trip each | as A |
| FSIN | about 150–300 | about 400–800 | HPS round trip | HPS round trip |
| Bit-exact basic ops | yes | yes | yes (SoftFloat 3e, Bochs's library) | yes |
| Lockstep in simulation | as today | as today | needs an HPS model in the bench | needs an HPS model for the slow ops |
| Timing of a run | fixed | fixed | depends on the HPS: Linux scheduling, DDR traffic | fixed except the slow ops |

A real 387 at 33 MHz takes about 1–1.5 µs for FADD or FMUL and 3 µs for
FDIV, with the 386's transfer overhead. Option A or B at 61.44 MHz is faster
than that at every operation; the governor's presets bring the period
speeds back.

**HPS offload.** The HPS's ARM Cortex-A9 has VFP, which has no 80-bit
format, so offload means SoftFloat in software. Its round trip (a fabric
mailbox write, an HPS core noticing it, the operation, the reply) has not
been measured on a kit. FES's HPS runs Linux, so an operation's latency
depends on scheduling unless a core is isolated and polls. It has three
costs:
- **Determinism.** Lockstep compares architectural state per retirement,
  not timing, so results can be checked whatever the latency, but a kit
  run's timing would no longer be a function of the program alone. Code
  that counts loop iterations around an FPU operation (some speed tests and
  detection routines do) would see jitter.
- **A second implementation.** The simulation bench needs a model of the
  HPS side (Bochs's softfloat serves) and the kit needs the HPS program
  shipped and versioned with the core.
- **Licence.** Bochs's transcendentals are LGPL-2.1; SoftFloat 3e is BSD.
  An HPS program would use SoftFloat for the basic operations and its own
  transcendentals unless the LGPL is acceptable there.

It is worth it only for the slow, rare operations (option D), and only if
the census shows them rare enough that their latency does not matter.

**Recommendation.** Build the unit in the fabric, shaped by the census, and keep
the HPS as the fallback for the slow operations only if the fit spike says
so:
- **A fast path for what the titles run:** loads and stores with their
  conversions, FXCH, add, multiply and compare, which are 86–96% of the x87
  instructions in the four runs that compute in volume. FLDCW, FNSTCW and
  FNSTSW AX should be cheap register moves too: two titles spend about a
  tenth of their x87 instructions on them. Multiply uses the DSP blocks. FXCH can be a swap in a small table of physical
  register numbers rather than a data move, since it is a fifth of Quake's
  x87 instructions.
- **A slow path in microcode on the same datapath** for divide, square root,
  remainder, transcendentals and BCD: under 2% of the instructions. Divide
  and square root iterate on the multiplier (Goldschmidt or Newton with a
  final remainder check for exact rounding) and stop early at 24- or 53-bit
  precision, which is what Quake's 809k divides at 24 bits and Screamer's at
  53 use. The
  transcendentals reuse the adder and multiplier with constants in M10K.
- **Not the HPS by default.** Offload would save the slow path's few hundred
  ALMs at the cost of run-to-run timing, a second implementation and a
  bench model, for operations the titles barely use. It stays the answer
  if slice 1's fit shows the fabric slow path does not fit.

That is option A without the SRT divider and LUT shifters, about 2,400 ALMs
by the estimate above, and still above the 92% ceiling unless the audit's
wins land first. Deano's call on the open questions below decides the
order.

### Accuracy, and what lockstep can check

- **Basic operations are exact.** Add, subtract, multiply, divide and
  square root round as IEEE 754 under x87 precision and rounding control;
  FRNDINT, the integer and BCD stores, and stores to a 32- or 64-bit real
  round under rounding control only, ignoring precision control; loads,
  compares and FXTRACT are exact. The 387 implements these correctly
  rounded, and Bochs uses Berkeley SoftFloat 3e (BSD-3-Clause) for them.
  Lockstep compares them bit for bit, flags included.
- **FPREM is not the IEEE remainder.** FPREM1 is: its quotient rounds to
  nearest. FPREM truncates its quotient toward zero, the 8087/287 rule.
  Both results are exact and ignore rounding and precision control. When
  the operands' exponents differ by 64 or more, a step returns a partial
  remainder with C2 set for software to loop on; a complete one clears C2
  and reports the quotient's low three bits in C0, C3 and C1 (387 PRM,
  FPREM and FPREM1; Intel SDM vol. 2). Bochs implements both in one routine, `cpu/fpu/fprem.cc`, with
  round-to-zero and round-to-nearest quotients. TestFloat's `rem` checks
  only FPREM1's IEEE remainder; FPREM needs its own directed cases.
- **The 387's operand rules.** Pseudo-zero, pseudo-NaN, pseudo-infinity and
  unnormal operands raise invalid; pseudo-denormals are used with their
  exponent read as 1 and raise the denormal exception (387 PRM §3.1.7).
  FPTAN pushes 1.0. FSIN, FCOS, FSINCOS and FPTAN accept |x| < 2^63 and
  otherwise set C2 and leave the operand (datasheet Table 2.2). Bochs's
  handling of each of these needs a directed case; where Bochs differs, the
  RTL follows the 387 and the case enters the divergence list.
- **Transcendentals cannot be bit-exact to a 387 from public data.** The 387
  reduces by a 67-bit π/4 and computes with "a modified CORDIC" on a
  68-bit datapath, with relative error under 2^-62 (387 PRM; Juffa). Its
  microcode is not published, and the published die and microcode
  reverse engineering covers the 8087 only. Bochs instead evaluates
  polynomials (`cpu/fpu/poly.cc`) and claims no match with any FPU. So the
  RTL can match Bochs bit for bit (by computing Bochs's algorithm), or
  follow the 387's method within its error bound, but not both. Lockstep
  compares transcendental results within a bound in ulps and their flags
  exactly. Measured 387 results, from a real 386+387, would set that bound.
- **Precision control** reduces add, subtract, multiply, divide and square
  root results to 24 or 53 bits; the census above shows which titles run at
  which precision.

## Area and timing budget

**What is left.** The latest seal, [#1728](https://github.com/DeanoC/fes/pull/1728)'s at
`105a73942` (Quartus 17.0.2, seed 1), uses 36,803 of the 5CSEBA6's 41,910
ALMs (87.8%), 269 of 553 M10K and 38 of 112 DSP blocks
([seal summary](../2026-11-12-i386-ud-1701/seal-summary.json)). So 5,107
ALMs, 284 M10K and 74 DSP blocks are free. The
[area audit](../2026-11-08-i386-area-audit/summary.md) set a working
ceiling of 92% (38,557 ALMs) because Quartus has never closed this design
above 88%; that leaves **about 1,750 ALMs**. Fit noise is ±200 ALMs.

**The design's own budget does not fit.** The design document's x87 row is
7,000 ALMs, 8 M10K and 9 DSP blocks: more than all 5,107 free ALMs. An x87
built as the design describes (registers in flip-flops, a LUT normaliser,
radix-4 SRT divide) does not fit beside the CPU and machine as they are.

**Estimates for a leaner unit.** These are unmeasured, from the sizes of
comparable blocks in the [audit](../2026-11-08-i386-area-audit/entities.tsv)
(the integer `muldiv` is 925 ALMs, the shifter 277, the sequencer 121, the
BIU 1,188). A fit spike is the first RTL gate (slice 1) because they could be
off by a factor of two.

| Block | Lean (DSP-heavy) | As the design says |
| --- | ---: | ---: |
| Register stack, tags (M10K, two read ports, one write) | 100 | 700 (flip-flops) |
| Status, control, pointers, opcode, exception logic | 300 | 300 |
| Exponent path (17-bit add, compare, bias) | 150 | 150 |
| 68-bit add, round, sticky | 400 | 400 |
| Leading-zero count | 150 | 150 |
| Alignment and normalising shifts | 150 (DSP multiplies by 2^n) | 700 (LUT barrels) |
| 64×64 mantissa multiply | 150 glue, 9 DSP blocks | 150 glue, 9 DSP |
| Divide and square root | 200 (multiplicative, on the multiplier) | 700 (radix-4 SRT) |
| Microcode sequencer, ROM in M10K, constants | 400 | 400 |
| CPU side: issue, scoreboard, operand uops, IRQ 13 latch, F0h/F1h | 400 | 400 |
| Total | **about 2,400** | **about 4,050** |

Both are above the 1,750 under the ceiling. The lean column would put the
design at about 39,200 ALMs (93.5%), the other at about 40,850 (97.5%).
Transcendentals in microcode add ROM words and M10K, not ALMs, if they
reuse the multiplier and adder. Timing: the multiplier, adder and
normaliser can be pipelined behind BUSY freely, since nothing waits on them
cycle by cycle, so the x87 should not touch the CPU's critical paths (D,
Q and the L1D's wait). The risk is routing congestion at 93% and above, not
logic depth.

**If it does not fit.** In order of preference:
1. **Wins elsewhere first.** The audit's list: the HID keyboard's four
   144-bit vectors (1,313 ALMs, up to about −650 if redesigned), the return
   stack in MLAB (about −250), OPL2's register memories in MLAB (about
   −150). Together up to about −1,050, which brings the lean x87 to about
   38,150 (91%).
2. **A second core variant,** `fes.pc386` without the x87 and a
   `fes.pc386-387` with it, from one RTL with a parameter, each sealed. The
   387 variant can also drop what its titles do not need. It doubles the
   seals and kit checks for every pc386 change.
3. **HPS offload** of the slow operations (below), which takes the
   multiplier's glue and the divider out of the fabric.

Comparisons follow the Quartus best-vs-best rule: Quartus 17.0.2 at its
best settings, with DSP inference on. No nextpnr routes (#1235).

## Verification

- **Lockstep against Bochs with its 387.** Today the lockstep forces
  Bochs's 387 to an empty socket per step (`EscStep` in `lockstep_tb.cpp`).
  The x87 slices compare the eight registers, tags, status, control,
  opcode and pointers at every retirement, exactly for the basic operations
  and within the bound above for transcendentals, on the RTL and on the
  locked-Yosys netlist.
- **The oracle's error path first.** Before any lockstep of the error path,
  the oracle adapter needs the AT's: FERR into the machine's IRQ 13, F0h
  and F1h, and no IGNNE sleep (see above).
- **Random FP programs.** Constrained-random x87 streams over every
  precision and rounding control, every operand class (zero, denormal,
  pseudo-denormal, unnormal, infinity, quiet and signalling NaN), masked
  and unmasked exceptions with an IRQ 13 handler, and stack overflow and
  underflow.
- **TestFloat.** Berkeley TestFloat 3e (BSD-3-Clause) generates extF80
  vectors with `-precision32`, `-precision64` and `-precision80` for add,
  subtract, multiply, divide, square root, remainder (FPREM1's, not
  FPREM's), round to integer,
  conversions and compares, in every rounding mode. They run as guest
  programs on the RTL and the oracle. TestFloat has no transcendentals.
- **QEMU's x87 tests** in `tests/tcg/i386` (fprem, fpatan, f2xm1, fyl2x,
  fyl2xp1, fscale, fxtract, fxam, fbstp, fldcst, pseudo-denormal and
  fp-exceptions; GPL-2.0-or-later) are test inputs like test386.asm: pinned,
  run outside any package, never shipped.
- **test386.asm has no x87 instructions** (checked in the pinned source), so
  it gives no FPU coverage; its run stays the integer regression.
- **Directed cases.** Every form's 16- and 32-bit FNSAVE, FNSTENV, FRSTOR and
  FLDENV layouts; FPTAN's 1.0; the 2^63 range; the IRQ 13 path with F0h and
  F1h; the no-wait forms with an error pending; exception 9; the common
  FPU detection routines (Intel's FNSTSW and FNSTCW checks, the 287/387
  affine/projective infinity test, BIOS POST); and the switch between the
  two identities.
- **Kit acceptance,** on the slice's seal: Quake's timedemo, DOOM's
  `-timedemo demo3` (must not slow), the titles the census unlocks, the
  DOSBench pack's FPU benchmarks (SpeedSys's FPU test, Norton SI 8), Linux
  2.0.0 booting with the 387 (its boot message names the error reporting it
  found), and WfW 3.11.

## Slices

One PR per slice. As in Phase 5, a slice that changes the CPU merges only
with:
- lockstep against Bochs on the RTL and on the locked-Yosys netlist, with
  its directed cases and random programs;
- test386.asm on the RTL to POST FFh, unchanged;
- a Quartus 17.0.2 seal with every analysis met;
- a kit run on that seal: the titles it unlocks, and DOOM's
  `-timedemo demo3` and the acceptance games rechecked.

A timing miss stops the slice; it never relaxes a check. The
`i386dx+387` identity is a machine setting, and it defaults off in every
package until slice 5 lands: with it on, titles that probe would find a 387
that cannot yet compute everything they use. Quake, Screamer, Terminal
Velocity and Chris's benchmark all run FSIN and FCOS, and Screamer and
Chris's benchmark also F2XM1 and FYL2X (x^y); Screamer runs FPATAN too. A slice's own kit run
switches the identity on for its checks only.

0. **Census and oracle.** Land this record's census keys in
   `machine_tb --census`. Give the oracle adapter the AT's error path:
   Bochs's FERR into the machine's IRQ 13, F0h clearing it, F1h resetting
   the 387, and no IGNNE# sleep. Add an x87 state accessor to the oracle
   interface and the lockstep's 387-present rule (compare the registers,
   tags, status, control, opcode and pointers). Machine: the identity bit
   for CMOS 14h bit 1. No CPU change. **Gate:** lockstep's no-387 results
   unchanged; a directed IRQ 13 case on the oracle; the census rerun gives
   this record's counts.
1. **Fit spike.** The lean datapath (option A's register stack, adder,
   rounder, normaliser, multiplier and Goldschmidt divider) as a standalone
   Quartus 17.0.2 analysis fit at its best settings, then inside pc386 with
   the CPU's ESC path tied to it, analysis fits only, no seal. **Gate:**
   measured ALMs, DSP blocks and slack. It decides between A, B and D
   against the budget, and whether the audit's wins (keyboard, return
   stack, OPL2) or a second core variant come first. Deano picks with the
   numbers.
2. **Interface, state, loads, stores and control.** The integrated unit's
   scoreboard (BUSY), the AT error latch on IRQ 13 with F0h and F1h, ET and
   the identity, exception 9; FNINIT, FNCLEX, FLDCW, FNSTCW, FNSTSW, the real
   FNSTENV, FLDENV, FNSAVE and FRSTOR layouts (today's microcode loops with
   the 387's values in place of the all-ones fields); FLD, FST(P), FILD,
   FIST(P), FXCH, FFREE, the constants, FCHS, FABS, FTST, FXAM, stack
   overflow and underflow. **Gate:** lockstep with the 387-present rule;
   TestFloat's conversions; FPU-detection cases report a 387 with the
   identity on and none with it off.
3. **Add, subtract, multiply, compare and round.** FADD/FSUB/FSUBR and their
   integer and popping forms, FMUL, FCOM/FCOMP/FCOMPP/FUCOM*, FICOM,
   FRNDINT, FSCALE, FXTRACT, precision and rounding control, every
   exception and its flag. **Gate:** TestFloat add, sub, mul, compare and
   roundToInt in every precision and rounding mode; random programs.
4. **Divide, square root and remainder.** FDIV/FDIVR and their forms, FSQRT,
   FPREM, FPREM1. **Gate:** TestFloat div and sqrt, and rem for FPREM1;
   directed FPREM and FPREM1 cases over large exponent differences, checking
   each partial step's C2 and the final C0, C3 and C1 quotient bits; QEMU's
   fprem test.
   The identity stays off by default: every title the census found computing,
   except SpeedSys's FPU test, also runs a transcendental.
5. **Transcendentals.** FSIN, FCOS, FSINCOS, FPTAN, FPATAN, F2XM1, FYL2X,
   FYL2XP1, FBLD and FBSTP, in microcode on the slice 3–4 datapath or on the
   HPS (option D). **Gate:** results within the agreed ulp bound of the
   oracle and, where available, of a real 387's; QEMU's transcendental tests;
   the 2^63 range cases.
6. **Timing and acceptance.** The governor's ESC classes with the 387's
   clocks and the overlap for the presets; Quake's timedemo, the FPU titles
   and benchmarks on the kit, Linux 2.0.0 with its 387, WfW 3.11; the
   census rerun on the RTL with the identity on. From here the identity's
   default is Deano's call.

## Open questions for Deano

These are Deano's decisions; the plan does not settle them.

1. **All-fabric or HPS for the slow operations** (options A and D). The fit
   spike gives the fabric's cost; the HPS round trip still needs measuring
   on a kit.
2. **What to give up if the lean unit does not fit under 92%:** the audit's
   wins (keyboard redesign first), a second core variant, or running Quartus
   above 92% and accepting the risk.
3. **Transcendental results: Bochs's or the 387's.** Matching Bochs bit for
   bit makes lockstep exact; following the 387's method follows real
   hardware but leaves a tolerance in lockstep. Either needs a bound.
4. **Real 387 measurements.** The design's empty-socket facts came from a
   tested 386DX-33. If that machine, or any 386 with a 387, can run a
   capture program, it settles FNSTCW's wait behaviour, transcendental
   results, and the ulp bound.
5. **The identity's default** once slice 5 lands: on for every title, or
   per title, as the presets are.

## Sources

- Intel 80386 Hardware Reference Manual (1987):
  <https://www.mirrorservice.org/sites/www.bitsavers.org/components/intel/80386/1987_80386_Hardware_Reference_Manual.pdf>
- Intel 80387 Programmer's Reference Manual, 231917-001 (1987):
  <https://ftp.mirrorservice.org/sites/www.bitsavers.org/components/intel/80386/231917-001_80387_Programmers_Reference_Manual_1987.pdf>
- Intel 386 DX Programmer's Reference Manual, 230985-003 (1990):
  <https://ftp.mirrorservice.org/sites/www.bitsavers.org/components/intel/80386/230985-003_386DX_Microprocessor_Programmers_Reference_Manual_1990.pdf>
- Intel387 DX datasheet, 240448-005:
  <https://datasheets.chipdb.org/Intel/x86/387/datashts/24044805.PDF>
- IBM PC AT Technical Reference, 6183355 (March 1986):
  <https://ftp.mirrorservice.org/sites/www.bitsavers.org/pdf/ibm/pc/at/6183355_PC_AT_Technical_Reference_Mar86.pdf>
- N. Juffa, "Everything you always wanted to know about math coprocessors"
  (COPRO16A, 1994):
  <https://discmaster.textfiles.com/file/10567/HPAVC%20CD-ROM.iso/pc/COPRO16A.ZIP/COPRO16A.TXT>
- Berkeley TestFloat 3e:
  <http://www.jhauser.us/arithmetic/TestFloat-3/doc/testfloat.html>
- QEMU `tests/tcg/i386`:
  <https://gitlab.com/qemu-project/qemu/-/tree/master/tests/tcg/i386>
- Bochs at the oracle's pinned commit `30cfd566ecc8`: `cpu/fpu/ferr.cc`,
  `cpu/fpu/poly.cc`, `cpu/softfloat3e/`, `iodev/extfpuirq.cc`.
- Quake's manual: "Pentium 75 MHz processor or better (absolutely must have
  a Math Co-Processor!)".
