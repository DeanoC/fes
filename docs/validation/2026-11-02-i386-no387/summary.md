# i386: the ESC instructions of a 386 with an empty 387 socket, 2 November 2026

Issue [#1413](https://github.com/DeanoC/fes/issues/1413): the design's
[FPU section](../../../sources/misteross/docs/i386-cpu-design.md#fpu), which
the Phase 2 slice 2 record ([#1411](https://github.com/DeanoC/fes/pull/1411))
found is the first thing that stops DOOM under DOS/4GW on the RTL.

**Result:**

- **Built:** the ESC instructions (D8h-DFh) of a 386 with an empty 387
  socket, as the design's FPU section says: #NM on EM or TS; FFFFh from
  FNSTSW and FNSTCW; FNSTENV's and FNSAVE's 387 layouts with the tracked x87
  pointers, as microcode loops; the loads read and discarded; the stores and
  register forms doing nothing but the instruction pointer
  ([What is built](#what-is-built)).
- **Lockstep:** 1,933 directed no-387 cases pass on the RTL and the netlist,
  in lockstep with Bochs under a no-387 rule (Bochs models a 387), and every
  other run passes with results equal to #1411's
  ([Lockstep](#lockstep)). Seven of eight mutations fail cases.
- **DOOM on the RTL** passes DOS/4GW's FPU probe; its first stop is now the
  general CS-limit check, a separate task
  ([DOOM](#dooms-start-on-the-rtl)).
- **Governor:** ten classes for the ESC instructions, checked on the RTL by
  a new bench loop under every preset ([Governor](#governor)).
- **Quartus 17.0.2 seals `fes.pc386`** at `4f73c51d8`: system clock 73.65
  MHz and pixel clock 78.31 MHz at their slowest corners, every analysis
  passing ([Seal](#quartus-seal)). The branch head's RTL is that tree's plus
  #1411's #1424 fix, merged after the seal and, as in #1411, not sealed.
- No kit run.

## Inputs

- FES branch `deano/i386-no387`, stacked on #1411's branch
  `deano/i386-phase2-idt` at `7224b11f6` (main `3d2e17f84` plus slice 2).
- The trees before the merge: `4f73c51d8` for the seal, the boot suite and
  DOOM's run; `7559fa52f` (the lockstep bench's fix) for the first full
  lockstep; `08efdd8d5` (the governor bench's ESC loop) for the first
  governor bench; `66b8ece79` (a unit test's report) for the unit tests and
  test lanes. The i386 RTL, microcode and governor table are the same in
  all four.
- `f58d5012f` merges #1411's tip `3f8687ed0`, which brings the #1424 fix
  `d9773b8f2` (a protected-mode IRET and RETF read their stack slots in
  Bochs's order: DC and the `piret` and `pretf` routines, no routine
  moved). #1411 landed it after its final seal and did not seal it; this
  seal does not cover it either (the pc386 reseal exemption: the next
  sealing PR covers the tree). The lockstep (RTL and netlist), the governor
  bench and the i386 unit tests ran again at `f58d5012f`; the reports here
  are those.
- Bochs `30cfd566` (REL_3_1_FINAL) at CPU level 3 with its FPU, the
  lockstep's oracle; SingleStepTests/80386 `459d49fb`.
- Verilator 5.032; the i386 lock's Yosys `238eac9a4` for the netlist run;
  Quartus Prime Lite 17.0.2 and the pc386 lock's Yosys `0a7927bf` for the
  seal.
- DOOM v1.9 (DOS/4GW) on the slice 2 census disk, rebuilt by its recipe
  (`~/fes-scratch/p2idt/census/make_disk.py`; SHA-256 `fa26f60b…`, as
  slice 2's). No game bytes are in this record or the repository.

## What is built

The design's FPU section, in `fes_i386_cpu`
([README](../../../sources/misteross/cores/fes-common/rtl/i386/README.md#esc-with-no-387)):

| Form | With EM or TS set | Otherwise |
| --- | --- | --- |
| Every ESC form (D8h-DFh) | #NM, after LOCK's #UD and the 15-byte #GP | as below |
| WAIT (9Bh, Phase 1) | #NM when MP is set too | nothing |
| FNSTSW m16 (DD /7), FNSTCW (D9 /7) | | store FFFFh |
| FNSTSW AX (DF E0) | | AX = FFFFh |
| FNSTENV (D9 /6), FNSAVE (DD /6) | | the 387's 16- or 32-bit layout by operand size: the control, status and tag words, the opcode and the 32-bit layouts' reserved halves all ones, the x87 instruction and data pointers as tracked; FNSAVE then 80 bytes of ones for the registers. Microcode (`fnstenv`, `fnsave`), a field per uop, in Bochs's order |
| FLDCW (D9 /5), FLDENV (D9 /4), FRSTOR (DD /4) | | read every field (FRSTOR the registers too) and discard; microcode (`fldenv`, `frstor`) |
| FLD, FILD, FBLD, arithmetic with a memory operand | | read and discard: a word or dword in one uop; an 8- or 10-byte operand in two or three, from its highest part down (`frd8`, `frd10`) |
| FST, FSTP, FIST, FISTP (every size), FBSTP, the reserved memory forms (D9 /1, DB /1, /4, /6, DD /1, /5, DF /1) | | no access, no check; one uop whose offset AG works out as LEA's, for the data pointer |
| FNINIT, FNCLEX, FENI, FDISI, FSETPM (DB E0-E4) | | nothing |
| Every other register form | | nothing but the instruction pointer |

**The x87 pointers.** The 386 keeps them for the 387, and FNSTENV and
FNSAVE store them with no 387 fitted. The CPU holds CS and EIP of the last
non-control ESC instruction and, for a memory form, its operand's segment
selector and offset, written as its last uop retires, with the real-mode
layouts' linear forms (selector × 16 + offset) beside them. The control
forms (FNINIT, FNCLEX, FENI, FDISI, FSETPM, FNSTSW, FNSTCW, FLDCW, FNSTENV,
FLDENV, FNSAVE, FRSTOR) write nothing, as Bochs and Intel partition them.

**Choices this build makes,** where the design or the measured 386DX-33
leaves the answer open:
- FBSTP and the reserved memory forms behave as FST and FIST were measured
  to: no access and no check. Each is a store the 387 would make, and with
  no 387 nothing asks the 386 for the transfer. The design leaves FBSTP
  uncharacterized.
- FNINIT and FNSAVE leave the x87 pointers and FLDENV and FRSTOR load
  nothing into them, as the design's "FNINIT does nothing" and "read and
  discard" say; a 387 (and Bochs) clears or loads them.
- The opcode field and the 32-bit layouts' reserved halves are all ones,
  with the 387's other fields; the formats' fixed zero bits are zero.
- An 8- or 10-byte read is a uop per dword or word; the design said one
  uop, and now says this. Bochs reads a qword: where an operand straddles
  the 64 KiB wrap under a 16-bit address size the two differ (a known
  limit, no case puts one there).
- The governor charges the coprocessor-fitted counts (below): no empty-
  socket timing is recorded.

**Cost:** the microcode ROM's words grow from 90 bits to 101 (one more
field each for a field offset, a dword offset, the pointer write and a
fifth system-register bit) and from 130 to 197 of 256 words; 160 flip-flops
hold the pointers.

## Lockstep

`scripts/test_fes_i386_lockstep.py` at the merge head `f58d5012f`
([report](lockstep-report.json)), and with `--yosys` on the i386 lock's
Yosys `238eac9a4` ([report](lockstep-netlist-report.json)). The two reports
are identical but for the netlist's `yosys` field. Before the merge, at
`7559fa52f` (which changes only the lockstep bench from `4f73c51d8`, the
sealed tree), the same runs passed, every field equal but for the four
protected-mode cases the #1424 fix adds.

| Run | Result |
| --- | --- |
| Corpus (SingleStepTests/80386, 941 files) | 1,758,699 pass, 0 fail; 3,517,401 retirements compared; 147,537 in an exception's handler |
| Random programs (16 seeds × 2,000) | 32,000 pass, 0 fail; 8,347,724 retirements; the latency-free reruns of seeds 1 and 2 retire the same trace |
| Protected-mode programs (16 seeds × 500) | 8,000 pass, 0 fail; 481,484 retirements; 13,640 deliveries through the IDT; 1,427 shutdowns; 201 not-built stops |
| Unbuilt encodings | 50,272 outside SCOPE, all stopped as not built (52,320 before: D8h-DFh's 2,048 are now built) |
| Directed cases | 40 pass |
| Unreal-mode cases | 6 pass |
| Protected-mode cases | 51 pass |
| No-387 cases (new) | 1,933 pass |
| Memory cases (DMA, FLUSH) | 26 pass |

**Against #1411's.** Compared field by field with the RTL and netlist
reports in #1411's record (at `d9773b8f2`, the #1424 fix, whose i386 RTL
this branch's merge head has but for the ESC forms), every field is equal
but the no-387 cases' own, the unbuilt count, and timing: the corpus's
cycles (+0.02% in all, each file within ±0.3%, 155 of 941 files) and the
random and protected-mode programs' memory waits and port 0 counts.
Retirements, results, faults, deliveries and every trace hash are
unchanged. #1411's own reruns at one tree repeat the corpus's cycles
exactly, so the change is this branch's: as in the boot (Machine, below), a
D8h-DFh byte on a path a branch cancels, the random bytes past a corpus
test's HLT among them, now issues as an ESC form where it stopped D.

**A bench hang, fixed.** The first full run at `4f73c51d8` hung in every
corpus file with a test of LOCK followed by another prefix (from opcode 00
on, `lock add [gs:bp+si],bh`): the no-387 rule's prefix scan stopped
advancing once it saw LOCK. `7559fa52f` fixes it and adds the directed case
`lock.es-fld` (LOCK, then ES, on FLD); the RTL is unchanged.

### The no-387 rule

Bochs, the oracle, models a 387. For each ESC instruction with EM and TS
clear and no LOCK the lockstep applies a rule
([sim README](../../../sources/misteross/cores/fes-common/sim/i386/README.md#known-limits)):
the adapter makes Bochs's 387 read as an empty socket for the step (its
control and tag words, opcode and registers all ones; its status word all
ones for FNSTSW, FNSTENV and FNSAVE, else clear so no exception is
pending), so Bochs stores what the FES CPU stores; after FNINIT, FNSAVE,
FLDENV and FRSTOR the oracle keeps its x87 pointers; and a form that
accesses no memory on the FES CPU, or a register form Bochs's 386 rejects as
#UD, is not stepped: the oracle moves past it and takes the pointers it
writes. With EM or TS set, a form Bochs's 386 rejects as #UD raises #NM on
the FES CPU: the oracle steps FNOP or FLD m32real in its place, which
raises it. Bochs's own pointer updates follow the FES CPU's for every other
form, so FNSTENV's and FNSAVE's pointer fields are compared with Bochs's.

### Directed no-387 cases

`FES_I386_MODE=no387` (`run_no387_cases`, part of the full run): 1,933
cases, each a short program in real mode, 16-bit protected mode (CS and DS
16-bit, the `pm16` environment) or 32-bit protected mode (`pm32`), run in
lockstep with Bochs under the no-387 rule and ending in HLT or the exception
it names. Each case's own checks (bytes left in memory, AX, BL) are compared
with the expected values the case states, besides the lockstep's comparison
with Bochs.

| Cases | Count | What each checks |
| --- | ---: | --- |
| `form.<env>.<op>.<m0-m7 or c0-ff>` | 1,728 | every ESC opcode with each reg field as a memory form and every register ModRM, in each environment: FLD dword [180h], the form (memory forms at [100h]), FNSTENV [300h]. The operand's 70h bytes left as they were but for what the form stores (FFFFh, or the FNSTENV or FNSAVE image), AX 1234h but after FNSTSW AX (FFFFh), and FNSTENV's pointers: the form's CS:EIP and DS:100h unless it is a control form, which leaves the FLD's |
| `fault.real.<op>.m<reg>` | 64 | each memory form at DS:FFFFh in real mode: #GP(0) where the form accesses memory, else it retires |
| `fault.real.ss.*` | 4 | FLD, FNSTSW and FNSTENV at SS:FFFFh through BP: #SS(0); FST retires |
| `fault.pm16.*` | 12 | a read-only DS: #GP(0) for FNSTSW, FNSTCW, FNSTENV and FNSAVE, none for FLD or FST; a 256-byte DS: #GP(0) for FLD, FLD m64, FLDENV and a partial FNSAVE, none for FST or an FNSTENV that fits |
| `wrap.real.fnstenv-o32` | 1 | a 32-bit FNSTENV at DS:FFF0h wraps its last 12 bytes to DS:0 |
| `nm.<form>.<bits>` | 96 | FNINIT, FNSTSW AX, FNOP, FCMOVB (a 387 #UD form), DB E7, FLD, FST, D9 /1, FNSTENV, FNSAVE, FRSTOR and WAIT under each of the 8 combinations of MP, EM and TS: #NM where the design says, else the form's effect |
| `lock.*` | 4 | LOCK FNINIT, LOCK FLD, LOCK FNINIT with EM, and LOCK then ES on FLD: #UD, before #NM |
| `layout.<env>.<o16 or o32>.<fnstenv or fnsave>`, `layout.real.a32`, `layout.pm32.sib` | 14 | after FLD dword ES:[120h], FNSTENV or FNSAVE [200h] at each operand size in each environment, byte for byte against the 387's layout (the pointers, the selector and the fixed zeros); a 32-bit address in real mode; a SIB data pointer |
| `detect.<real or pm32>.*` | 10 | five FPU-detection routines end with BL 0, no coprocessor: Intel's (FNINIT, FNSTSW low byte 0, FNSTCW AND 103Fh = 3Fh), the control word's high byte 03h, the waited FINIT and FSTSW forms, FNSTSW AX, and DOS/4GW's probe as DOOM runs it (FNINIT, FNSTSW, FNSTCW, CMP word 037Fh) |

All 1,933 pass on the RTL and on the netlist. The no-387 rule applied to
5,355 ESC instructions, 647 of them (the forms that access no memory on the
FES CPU and the register forms Bochs's 386 rejects as #UD) not stepped on the
oracle.

### Mutations

Each mutation is a one-line change to the RTL or microcode, built and run
on the directed cases at `7f8789b88` (1,928 cases; `4f73c51d8` added the
three LOCK cases and the 32-bit address one, `7559fa52f` a fourth LOCK case).

| Mutation | Change | Cases failed |
| --- | --- | ---: |
| `fnstsw-zero` | FNSTSW and FNSTCW store 0000h | 14 |
| `fcs-word` | the protected-mode FCS dword's high half FFFFh, not the opcode (7FFh) above five zero bits | 581 |
| `fst-reads` | FST and FIST memory forms read their operand | 20 |
| `reg-no-ip` | register forms leave the instruction pointer | 1,518 |
| `nm-no-ts` | ESC's #NM on EM only, not TS | 22 |
| `dp-unwrapped` | the data pointer 64 KiB off | 1,161 |
| `fsave-short` | FNSAVE writes its last dword at the one before it, and leaves its last | 11 |
| `no-hold` | a uop reading the pointers does not wait for older writers | 0 (see Known limits) |

## Governor

`governor.toml` gives the ESC instructions ten classes (What is built,
above, and the table's comment): FNINIT's (with FNCLEX, FENI, FDISI and
FSETPM), FNSTSW AX's, FNSTSW and FNSTCW to memory, FLDCW, FNSTENV, FLDENV,
FNSAVE, FRSTOR, and the other register and memory forms at FNOP's count.
`fes_i386_gov_pkg.sv` and `fes_i386_gov_rom.sv` are regenerated from it
(`scripts/fes_i386_governor.py --check` passes).

`scripts/test_fes_i386_governor.py` at `08efdd8d5`, and again at the
merge head `f58d5012f` with an identical report
([report](governor-bench-report.json)), whose bench program gains a
seventeenth loop, `esc`: FNINIT, FNSTSW AX, FNSTSW m16, FLDCW, FLD m32 and
FNOP, 1,000 times. PASS:
- Under every preset the `esc` loop's charge equals the table's (86,002
  reference clocks at 286-12, 120,009 at 386SX-16 with its dword reads on a
  16-bit bus, 114,009 at the 386DX presets, 37,016 at the 486 presets) and
  its time is within 0.21% of the table time; under max it takes 18,999 cycles and
  charges nothing; turbo runs all 17 loops in max's cycles.
- Against #1411's report, the other sixteen loops charge exactly what they
  did under every preset. Under max, `branch` takes 147,116 cycles (150,115)
  and `int` 28,028 (28,528): the new loop moves `near_return` and `int30`,
  the CALL's and INT's targets, which sit after it, so their code lines
  move. Every other loop's cycles are unchanged.
- Interrupt latency stays within its bound under every preset (the largest,
  95.1 reference clocks against 128 at 386SX-16), and the 6 MHz override
  paces every loop within 0.09%.

## DOOM's start on the RTL

The census disk, rebuilt by its recipe (SHA-256 `fa26f60b…`), on
`machine_tb --cpu rtl` at `4f73c51d8`, with `cd doom` and `doom` typed at the
prompt and a 5-second limit, as #1411 ran it.

- **DOS/4GW's FPU probe runs on the RTL.** No stop comes in real mode:
  #1411's first five stops (`FNINIT`, `FNSTSW m16` and `FNSTCW m16` at
  0727:68B0, `FNINIT` at 07A2:021D) are gone, and DOS/4GW goes on into
  protected mode.
- **The first stop is the CS limit,** at cycle 392,307,120 (3.92 s), at
  0080:2C63 (`MOV AH, 30h`, then `INT 21h` at 0080:2C65), in DOS/4GW's
  16-bit kernel segment whose limit is below FFFFh: the general CS-limit
  check, not built and a separate task.
- The bench printed 784 stops (the first three of each stem) of 268 stems,
  all under CS 0070h (430), 0080h (325) and 0088h (29), none of them an ESC
  or WAIT form. #1411's run printed 789 of 271: these 784 and its five
  FPU-probe stops of three stems. It stepped 1,331,138 one-instruction
  stretches with PE set on Bochs, under CS 0088h, 0070h, 0080h, 0018h and
  0090h.
- The run's 4-hour wall-clock limit ended it at cycle 499,710,987, 0.06%
  short of its 5-second limit, so it printed no end-of-run summary (its
  `GAP` and `RTL` lines). The figures above are from its `STOP` and `PMODE`
  lines. The log stays out of this record, since it carries DOS/4GW's
  instruction bytes.

## Machine

`sim_fes_pc386.py --suite boot --cpu rtl` at `4f73c51d8`
([report](boot-rtl-report.json)), the boot fixture `b9d36549…` as #1411's:
`C:\>` at 2.35 s, as before. All 113,098,555 instructions to the 4.1-second
end retired on the RTL, with 0 oracle steps and 0 stops; the VGA replay has
0 mismatches.

That is 1,657 more than #1411's run at `43bff581b` (113,096,898), with the
same POST timing to 2.95 M clocks and the first self-modifying store 2,350
clocks sooner. The likely cause, not traced: a D8h-DFh byte that D decodes
on a path a branch then cancels used to stop D (a not-built stop issues
nothing after it until the redirect) and now issues as an ESC form, so the
cancelled path's accesses, and so the port 0 traffic, differ. The boot
retires no ESC instruction: #1411's run, where each would have stopped, had
none.

## Tests

At `66b8ece79`, before the merge: the misteross unit tests pass (1,627, 74
skipped), and the lanes `make test-changed` plans against `7224b11f6` pass:
the planner's tests, whitespace, the parent Python regressions,
generated-consumer consistency and the pc386 RTL simulation (devices,
storage, audio, OPL2 and the benches). The planner also chose every other
core family's RTL simulation, because `scripts/test_fes_i386_governor.py`
was missing from its i386 scripts; `47e4c1932` adds it, after which the
plan is misteross's unit tests alone, and those lanes were not run. At the
merge head the i386 unit tests (69, 3 skipped) and the planner's pass.

## Quartus seal

`make build-fes-pc386-quartus` at seed 1 from a clean checkout of
`4f73c51d8` ([`seal-summary.json`](seal-summary.json)), Quartus Prime Lite
17.0.2 on the pc386 lock's Yosys `0a7927bf` netlist. All 109 input hashes
match the branch before the merge of #1411's tip (`47e4c1932`); against
#1411's final seal only the eight i386 RTL files differ. The merge changes
three: `fes_i386_decode.sv` and `fes_i386_ucode_rom.sv` with the #1424 fix,
and a comment in `fes_i386_pkg.sv`. Every analysis passes, with no illegal or unconstrained
clocks.

| | This branch, `4f73c51d8` | #1411's final seal, `7224b11f6` |
| --- | --- | --- |
| Build ID, package | `55bc5263…`, `3f13f381…` | `b70597c6…`, `ad844e72…` |
| System Fmax, slow corners (61.44 MHz) | 73.65-73.91 MHz, setup +2.698 ns | 69.51-69.88 MHz, setup +1.889 ns |
| Pixel Fmax, slow corners (74.27 MHz) | 78.31-78.96 MHz, setup +0.695 ns | 81.75-82.56 MHz, setup +1.231 ns |
| Hold, recovery, removal, pulse width | +0.117, +6.867, +0.450, +1.122 ns | +0.085, +7.183, +1.190, +1.122 ns |
| ALMs | 28,899 of 41,910 (69%) | 28,831 |
| Registers | 25,883 | 25,755 |
| RAM blocks, memory bits | 261, 1,275,417 | 259, 1,272,360 |
| DSP blocks | 32 | 32 |
| RBF | 3,619,528 bytes, SHA-256 `43731755…` | 3,644,880 bytes |

Against #1411's final seal the ESC forms cost about 70 ALMs, 130 registers
and two RAM blocks. The system clock, the CPU's, gains 4 MHz at its slowest
corner; the pixel clock, whose logic this branch leaves alone, loses 3.4
MHz and keeps 0.695 ns of setup slack: placement, which moves either way
from seal to seal (#1411's three seals' system clocks span 67.59 to 72.22
MHz). The first attempt at this seal reached Quartus's fitter and was
stopped by its 3-hour wall-clock limit on a host shared with another seal;
this one, with an 8-hour limit, took 2 h 23 min in Quartus (7.5 CPU-hours;
#1411's took 22 minutes).

## Known limits

- **An 8-byte part straddling the 64 KiB wrap.** Under a 16-bit address
  size Bochs reads or writes the low 8 bytes of a 10-byte operand, and of
  each register in FNSAVE's and FRSTOR's image, as one qword checked
  without the wrap; the FES CPU uses two dwords, each wrapped. No case puts
  one across the wrap.
- **The no-387 rule's steps that skip the oracle** run only where no event
  can be due at the boundary, so the random programs draw no ESC form; the
  directed cases cover them.
- **The hold on the x87 pointers** (a uop that reads them waits for older
  writers to retire) never acts today: the ROM issues a word every two
  cycles, so FNSTENV's first pointer word issues after the instruction
  before it retires. The mutation that removes it passes every case.

## Not in this record

- **The general CS-limit check,** the next stop for DOOM on the RTL
  (DOS/4GW's 16-bit kernel segments), a separate task.
- **A kit run.** The sealed package was not run on a kit.
- **The x87 unit** (Phase 6).
