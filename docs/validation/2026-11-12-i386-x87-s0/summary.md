# i386 x87 slice 0: the AT error path on the oracle, 12 November 2026

Slice 0 of the [x87 plan](../2026-11-12-i386-x87-plan/summary.md#slices)
([#1768](https://github.com/DeanoC/fes/issues/1768)) lands in the
simulation and the oracle only. It changes no CPU RTL.

It does four things:

- It lands the plan's census keys in `machine_tb --census`.
- It gives the Bochs oracle the error path a 387 has on an AT board.
- It adds an x87 state accessor and the lockstep's 387 identity.
- It runs Linux 2.0.0 with its 387, which used to hang the oracle.

**Result.**
- **The hang is gone.** Linux 2.0.0, booted on the oracle without `no387`,
  prints "Checking 386/387 coupling... Ok, fpu using old IRQ13 error
  reporting" and logs in. Its `/proc/cpuinfo` shows `fpu: yes` and
  `fpu_exception: no`, which means IRQ 13, not exception 16. Main's oracle
  stopped 0.19 s into this kernel.
- **The error path follows the AT.** Ten directed cases on the oracle alone
  (`x87_tb.cpp`) pass, and fail under two mutations of the adapter. X87TEST.COM
  takes the path at the FreeDOS prompt: IRQ 13 through the BIOS's INT 75h to
  INT 02h, a raw IRQ 13 frame, an ESC held until a write to F0h, and the F1h
  reset.
- **It found a BIOS bug.** pc386's INT 75h handler clobbered AX, and with
  FreeDOS's interrupt stacks that corrupted DOS's memory chain
  ([below](#the-bios-int-75h-handler)). The fix is its own PR,
  [#1785](https://github.com/DeanoC/fes/pull/1785)
  ([#1784](https://github.com/DeanoC/fes/issues/1784)).
- **With the identity off, nothing changes.** The lockstep matches main on
  every architectural result, max and preset 3, and test386 reaches POST FFh.
  The census reruns of the plan's titles give the plan's files key for key.
- **The identity setting is waiting on a decision.** The CMOS 14h bit 1
  setting needs a machine-side RTL input. Per the task this slice stopped and
  asked; see [Open](#open).

## Sources

- Intel 80386 Hardware Reference Manual (1987), §5.1 (BUSY#, ERROR#: "When
  the 80386 encounters any coprocessor instruction except FNINIT and FNCLEX,
  the BUSY# input must be inactive").
- Intel 80387 Programmer's Reference Manual 231917-001:
  - Appendix F.1–F.2: the PC/AT interface. ERROR# goes to IRQ 13 and clocks
    a flip-flop that holds BUSY# active. "The interrupt 13 handler is
    guaranteed to execute before any other 80287 instructions may begin."
    Port F0h clears the latch.
  - §4.8.1 and Appendix C.1: a RESET leaves FINIT's state except IM clear
    and IE and ES set, with ERROR# active.
- Intel 386 DX Programmer's Reference Manual 230985-003, WAIT (17-181) and
  §11.1.2.
- IBM PC AT Technical Reference 6183355:
  - "NMI and Coprocessor Controls" (1-38): F0h clears the latched busy, F1h
    resets the coprocessor.
  - Section 2 (2-4, 2-5).
  - The BIOS listing's `INT_287`: PUSH AX, OUT F0h, EOI to both PICs,
    POP AX, INT 02h, IRET.

Links are in the [plan's sources](../2026-11-12-i386-x87-plan/summary.md#sources).

## What changed

- **`Oracle::set_at_coprocessor`** (`bochs_oracle.cpp`) fits Bochs's 387 as
  on an AT. It is off by default, so the lockstep (identity off), test386
  and the corpus runners use exactly the old path.
  - The CPU's ERROR# is tied inactive. Bochs's IGNNE# input is held active
    unless the latch is set, so its 486-style wait for IGNNE# inside a step
    is never reached and exception 16 is never raised.
  - `x87_irq13()` is the 387's ERROR# (status word ES). machine_tb drives it
    onto the AT core's `device_irq[13]`, beside the sound card's IRQ.
  - ES going active latches BUSY#. While the latch is set, every ESC except
    FNINIT and FNCLEX, and WAIT, holds before it starts:
    - The step retires nothing and sets `StepResult::x87_held`.
    - #NM (EM or TS for an ESC; MP and TS for WAIT) and #UD (LOCK) come
      first.
    - An NMI or INTR (IF set) is taken while held, and its frame returns to
      the held instruction.
    - machine_tb treats a held step as a halt: the machine runs until INTR.
  - Bochs's own freeze entered its wait after the decode loop had moved RIP
    past the ESC, so after waking the instruction would have been skipped.
    The adapter holds before the instruction's handler instead.
  - An OUT whose bytes cover F0h clears the latch. F1h resets the 387 to
    control word 037Eh and status word 0081h, every tag empty and ERROR#
    active, and leaves the latch alone. Both still reach the machine, which
    decodes neither.
- **`x87_state()`** returns `X87State`: control, status with TOP, the tag
  word FNSTENV computes, the opcode, the pointers and the eight physical
  registers. `x87_differences()` names the fields two states differ in.
- **The lockstep's identity** (`lockstep_tb.cpp`): `FES_I386_X87=387` fits
  the 387 as on an AT and turns the no-387 rule (`EscStep`) off. This
  build's RTL has no 387, so a run ends as not built (`unbuilt` 387) at its
  first ESC or WAIT, which neither side steps. The comparison of `X87State`
  at each retirement joins this rule when the RTL has a 387 (slice 2).
  Unset, nothing changes. The testbench reads the variable itself; the
  Python runner does not set it, and the no387 mode refuses it.
- **The census keys** (`census.h`) are the plan's scratch patch,
  `x87_words()` replaced by the accessor, plus `x87held` for held steps.
  They are documented in
  [`pc386-game-drives.md`](../../../sources/misteross/docs/pc386-game-drives.md#instruction-census).
- **`divergences.toml`** gains five `[[x87]]` entries:
  - `at-error-path`, `held-esc-restarts`, `no-wait-forms-wait-busy` and
    `f1-reset`, where the oracle's 387 now follows the AT;
  - `cpu-reset-387-state`, which stays open: Bochs's CPU reset leaves its
    387 at CW 0040h with every tag valid. Whether an AT's CPU-only resets
    reach the 387 is not settled by these sources.

## Checks

The checks ran on this branch's commits as they landed:

- `b2776a065` (the coupling, the accessor and the census keys): the boot
  suite, the Linux run and the census reruns, from its machine_tb build;
- `9c62ab2e1` (with the lockstep identity and X87TEST.COM): the lockstep,
  test386 and the X87TEST.COM runs;
- `c2dcd299a` (`9c62ab2e1` with #1785's BIOS, a local tree): the X87TEST.COM
  pass;
- the head: the unit tests and the boot runs listed last.

| Check | Where | Result |
| --- | --- | --- |
| `x87_tb.cpp`: if1, held-nmi, no-wait, fnstsw, nm-first, wait-mp0, wait-mp1, f1-reset, accessor, off | Powerboat, `tests/test_fes_i386_oracle.py` | 10 of 10 |
| The same with OUT F0h made a no-op | Powerboat | 7 fail (no HLT: the latch never clears) |
| The same with the hold removed | Powerboat | hangs inside `step()`: main's bug |
| `sim_fes_pc386.py --suite boot` (FreeDOS on the oracle, coupling on) | chip | PASS, prompt at 3.75 s |
| `sim_fes_pc386.py --suite x87` (X87TEST.COM) | chip | every check ok, then FreeDOS halts ("MCB chain corrupted"); with #1785's BIOS, PASS (below) |
| Linux 2.0.0 with its 387 on the oracle, 40 s | chip | logs in at 24 s; see below |
| Lockstep, main `e27d6af8f` and this branch, same arguments (corpus 40 per file, 8 × 200 random, 8 × 100 protected-mode, 50 task, debug and undefined programs), `--same-as` main | chip | "every architectural result" |
| The same at governor preset 3, `--same-as` this branch's max | chip | "every architectural result" |
| test386.asm on the oracle | chip | POST … E0 EE FF, 0 EE differences, cross-check agrees |
| The lockstep testbench with `FES_I386_X87=387`: random (300 programs), directed, no387 | chip | random and directed pass unchanged (their programs run no ESC); no387 stops all 1,933 cases as `unbuilt` 387 at their first ESC, so that mode now refuses the identity |
| `tests.test_pc386_machine` (72 tests), `tests.test_fes_i386_oracle` | Powerboat | OK |

The x87_tb cases:

- **if1:** IF set. IRQ 13 after FDIVP; the frame returns to the FSTP after
  it; that FSTP runs with ES still set.
- **held-nmi:** IF clear. FSTP holds step after step until an NMI whose
  handler writes F0h; the NMI's frame returns to the FSTP.
- **no-wait:** FNCLEX and FNINIT run past the latch, and FNCLEX drops
  IRQ 13; FLD1 then holds.
- **fnstsw:** FNSTSW AX holds.
- **nm-first:** TS raises #NM before the hold.
- **wait-mp0, wait-mp1:** WAIT with TS holds when MP is clear, and raises
  #NM when MP is set.
- **f1-reset:** the 387's RESET state, as above.
- **accessor:** `x87_state()` equals the FNSAVE image that follows it, the
  pointers and opcode included, with tags 00, 01, 10 and 11.
- **off:** without the coupling, no IRQ 13 and no latch.

### Linux 2.0.0 with its 387

Linux 2.0.0 ran on the early-Linux record's disk with `no387` taken off the
LOADLIN line (the plan's `linux-2.0.0-387.img`), `--cpu oracle`, 40
simulated seconds. These are the plan's runner's settings
(`~/fes-scratch/x87plan/linux-run.sh`).

- **Boot.** The kernel reports "Checking 386/387 coupling... Ok, fpu using
  old IRQ13 error reporting", then boots to a login.
- **Commands.** `uname -a` gives "Linux slackware 2.0.0 #1 Mon Jun 10
  21:11:56 CDT 1996 i386". `/proc/cpuinfo` gives `fpu: yes`,
  `fpu_exception: no`, `hlt_bug: no`. The plan's run had stopped at 4.19 s.
- **What the census saw:**
  - 109 x87 instructions.
  - The coupling check's FDIV by zero: `x87exc:dc.r6:raised=04:unmasked=04`,
    then `x87es:dc.r6:pm0`.
  - One held WAIT, `x87held:9b:es:pm0`.
  - Linux's IRQ 13 handler writing F0h, `x87io:f0:out:pm0`.
  - The real-mode setup code's coprocessor reset, `x87io:f0:out:rm` and
    `x87io:f1:out:rm`.
  - Lazy switching, 23 #NM at CPL 3 with TS set, and FNSAVE and FRSTOR at
    CPL 0.

### The census on the plan's titles

This branch's machine_tb reran the plan's titles: same drives, launch
text, keys and limits, and the CMOS equipment byte of main (04h; the plan
patched it to 06h). The diff is of the whole `census.txt` against the
plan's.

| Title | x87 retired | x87 keys equal | all keys differing |
| --- | ---: | ---: | ---: |
| CHKCPU | 0 | 1 of 1 | 0 of 348 |
| DOOM | 11 | 9 of 9 | 0 of 699 |
| Norton SI 8 | 26 | 26 of 26 | 0 of 394 |
| Chris's 3D Benchmark | 15,236,311 | 113 of 113 | 0 of 812 |
| Quake timedemo (25 s) | 45,787,636 | 160 of 160 | 0 of 905 |

Chris's benchmark is the one that runs with invalid and divide-by-zero
unmasked (control word 3Ah). None of the five raises an unmasked exception,
so the coupling changes nothing they do, and none reads INT 11h's
coprocessor bit in a way that shows.

## The BIOS INT 75h handler

X87TEST.COM's checks all printed ok on the first run, but FreeDOS then
halted with "PANIC: MCB chain corrupted". The MCB at 0603h held a stack
frame.

**How it was found.** Running the program's sections alone and in pairs
narrowed it to the BIOS section followed by later interrupts. A machine_tb
trace of every step writing 6030h, and of the steps around each IRQ, showed
the cause:

1. IRQ 13 arrived in user code.
2. FreeDOS's stack-switching wrapper for INT 75h (`irqstack.asm`) switched
   to its stack pool and called the BIOS's handler.
3. The BIOS returned with AX changed.
4. The wrapper keeps its stack size in AX across that call
   (`add [stack_top], ax`), so the pool moved by the difference.
5. The next timer interrupt from user code, the one that released the held
   FSTP, pushed its frame onto the MCB.

**The fix.** rombios's `int75_handler` was `out F0h; call eoi_both_pics;
int 2; iret`, and `eoi_both_pics` loads 20h into AL. IBM's `INT_287` saves
and restores AX around the same steps. The fix makes the handler keep AX
and write 00h, as IBM's does (`firmware/README.md`, `firmware.lock`).

**Scope.** This changes `pc386-bios.bin`, which is in the sealed ROM, so it
is its own PR, [#1785](https://github.com/DeanoC/fes/pull/1785). On a kit it
matters only once IRQ 13 can rise, which is slice 2. With it, X87TEST.COM
passes to the prompt (this branch at `c2dcd299a`, a local tree with the fix,
on chip). Without it, the suite fails here until #1785 merges.

**A second BIOS difference.** rombios's default INT 02h prints "NMI Handler
called" and halts on any NMI, its own comment says it should return when
called from INT 75h, and an AT BIOS's NMI handler looks for a parity error
first; what IBM's does without one is not checked here. A DOS program that unmasks an exception without hooking
INT 02h now stops there on the oracle; on main it hung inside `step()`.
That is a follow-up for the firmware, not this slice.

## Open

- **The `i386dx+387` identity setting (CMOS 14h bit 1).** CMOS 14h is a
  constant 04h in `pc386_rtc.sv`, and the `fes.pc386.machine` record's
  setting words 0–7 are all taken (7 is reserved for the EMS board's size).
  So the setting needs a machine-side RTL input. The task said to stop and
  ask before any machine-side RTL. The options put to Deano:
  - **A.** A `pc386_machine` input that sets bit 1, tied 0 in
    `pc386_system`, with a machine_tb flag.
  - **B.** A new record word now.
- **The 387's state after a CPU reset**, and **FNSTCW's ERROR# behaviour**,
  stay for real hardware or a later slice (the plan's open questions).
- **The ESC forms Bochs's level 3 rejects as #UD** (the P6 forms in D8–DF)
  do not hold on the latch, since Bochs never decodes them as ESC. A real
  386 sends every D8–DF to the 387 after BUSY#.
