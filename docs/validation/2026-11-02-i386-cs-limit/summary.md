# i386 Phase 2: the general CS-limit check, 2 November 2026

Issue [#1414](https://github.com/DeanoC/fes/issues/1414), part of the i386
design's Phase 2, after slice 2 ([#1406](https://github.com/DeanoC/fes/issues/1406),
PR #1411), whose DOOM census found DOS/4GW's 16-bit kernel code segments
under CS limits below FFFFh, where `fes_i386_cpu` stopped every instruction
as not built.

**Result:**
- `fes_i386_cpu` checks CS's limit under any limit, in 16- and 32-bit code,
  in protected and real mode, as Bochs 3.1 does. An instruction that starts
  or runs past the limit raises #GP(0) at its fetch. A near branch, call or
  return whose target is past it raises #GP(0) at the branch, only when the
  branch is taken. A real-mode far transfer's offset, and a real-mode INT's
  vector offset, are checked against the limit CS already has
  ([What changes](#what-changes)). `WHY_CS_LIMIT`, `cs_odd`, `wrap16` and
  the 16-bit-only special cases are gone.
- The RTL and the i386 lock's Yosys netlist give the same lockstep report
  (but for its `yosys` field) ([Lockstep](#lockstep)):
  - every corpus state passes (1,758,699), with every trace hash equal to
    slice 2's;
  - every protected-mode program passes (8,000);
  - every directed case passes, among them 38 new CS-limit cases, 37 of
    which fail on slice 2's RTL.

  The new generator block runs 23,199 instructions and raises 1,299 #GP
  deliveries under small CS limits. 1 of the 32,000 random programs fails,
  the same way on slice 2's RTL: a harness gap for a REP string that stores
  over its own bytes ([#1425](https://github.com/DeanoC/fes/issues/1425)).
  Deano chose to record it and carry on. Five more failed, also on slice
  2's RTL, until this branch merged #1411's fix for them
  ([#1424](https://github.com/DeanoC/fes/issues/1424)).
- The pc386 suites, the governor bench and test386 on the RTL give slice
  2's results: the boot on Bochs, system, devices and the governor bench
  pass, the governor suite meets the bench gap #1372 at 386DX-25 as
  before, test386 stops at POST 08h's paging as before, and BOOTRTL
  ([Machine](#pc386-suites-governor-and-test386)).
- Quartus 17.0.2 seals `fes.pc386` from a clean checkout of `5d231b804`:
  every analysis passes at 61.44 MHz, the system clock at 72.76-73.53 MHz
  across the slow corners (setup +2.533 ns; slice 2's final seal 69.5-69.9
  MHz), for 28 more ALMs and 198 more registers ([Seal](#quartus-seal)).
- DOOM's start on `machine_tb --cpu rtl`: DOOM
  ([DOOM](#dooms-start-on-the-rtl)).

## Inputs

- FES branch `deano/i386-cs-limit`, stacked on `deano/i386-phase2-idt`
  (PR #1411) at `7224b11f6`. `01bc050ef` (#1411's record only) is merged in
  as `53bef7644`, and #1411's head `3f8687ed0` as `9683e2cb0`. That head
  brings the #1424 fix `d9773b8f2`, which changes only the order of a
  protected-mode IRET's and RETF's stack reads, and its four directed cases.
- The CS-limit RTL is `5d231b804`'s; later commits change only docs, this
  record and, by the merge, the #1424 fix. Which tree each run used:
  - `5d231b804`, a clean checkout: the seal; also DOOM on the RTL.
  - `9683e2cb0`, the merge: the lockstep and the netlist lockstep, and
    `--suite boot --cpu rtl`.
  - `9adde623c` (`5d231b804` with the README, sim README and design doc):
    the other pc386 suites, the governor bench and test386.
  The runs before the merge do not depend on the #1424 fix. It changes only
  which slot a protected-mode IRET or RETF reads first, which matters only
  when the frame runs past the stack's limit. The system, governor and
  devices suites run real-mode DOS, and test386 stops as it enters
  protected mode, before any IRET or RETF; DOOM's protected-mode returns
  would read the same slots either way unless a frame faulted. The seal is
  before the merge too: under the pc386 reseal exemption a post-seal RTL fix
  needs no reseal before merge, as #1411's own record says of `d9773b8f2`.
- Oracle: Bochs 3.1 at CPU level 3 (the `fes-i386-oracle` build), corpus
  SingleStepTests/80386 `459d49fbe628`.
- Verilator 5.032 on Powerboat. The netlist lockstep used the i386 lock's
  Yosys `238eac9a4`. The seal used Quartus Prime Lite 17.0.2 with the
  netlist from the pc386 lock's Yosys `0a7927bf`.
- DOOM v1.9 under DOS/4GW, on slice 2's census disk (the games image,
  HIMEMX's full XMS, DOOM in `C:\DOOM`, SHA-256 `fa26f60b…`), a scratch copy
  only. No game bytes are in the repository.

## What changes

Bochs 3.1's rules (`cpu/cpu.cc` `prefetch`, `cpu/icache.cc`
`boundaryFetch`, `cpu/ctrl_xfer16.cc` and `ctrl_xfer32.cc`, `cpu/exception.cc`
`real_mode_int`), which the RTL follows:
- `prefetch` raises #GP(0) when EIP is past CS's limit, and narrows the
  fetch window to the limit, so an instruction whose bytes run past it
  faults in `boundaryFetch`. The fault restores EIP to the instruction's
  first byte. Nothing the decode found comes first.
- `branch_near16` and `branch_near32` (Jcc, JMP rel, LOOP, JCXZ), the near
  CALL, JMP and RET handlers check the new EIP, the 16-bit one wrapped to 16
  bits, against the limit: #GP(0) at the branch, only when it is taken. A
  CALL pushes first and a RET pops first (their #SS comes first), and the
  speculative ESP is put back.
- A real-mode far JMP or CALL checks its offset against the limit CS has
  before the transfer, ahead of its pushes; RETF and IRET after their pops.
  `real_mode_int` checks the vector's offset after its pushes, ESP put back.
- In 16-bit code EIP is not cut to 16 bits: an instruction that ends at
  FFFFh leaves EIP 10000h, which faults next under a 64 KiB limit and runs
  on under a larger one.

The [RTL README](../../../sources/misteross/cores/fes-common/rtl/i386/README.md)
has the detail. In `fes_i386_cpu`:

| Where | What |
| --- | --- |
| D | EIP plus the length against the limit plus one (`cs_gp`), split at bit 4 so it runs beside `d_eip_next`'s adder: the low nibble plus the length carries at most 1 into EIP's upper 28 bits, which are compared with the limit plus one's upper bits and those less 1, registered as CS is written (`cs_fl`). A delivery's entry, at length 0, passes at the EIP of an instruction that starts just past the limit. `d_eip_next` is EIP plus the length in 16-bit code too |
| D | A far transfer's offset (`far_chk`, CALL r/m near's push too) against the limit: at a far CALL's probe, ahead of the probe's own check; at any other uop (`cs_late`) after the uop's own checks, at M1 |
| AG | CALL rel's push: its target, the next EIP plus the displacement wrapped by operand size, beside the AGU's chains (`ag_call`) |
| M1 | A near branch's or CALL rel's push's target against the limit (`tchk`): #GP(0) here for the push, after its own checks; carried to EX for a branch |
| EX | #GP(0) for a taken branch whose target is past the limit; RET's and JMP r/m's from EX's operand; a real-mode INT's probe whose vector offset (T1, retired by then) is past it |
| Redirect | A delivery's D EIP is the faulting instruction's, no longer cut to 16 bits in real mode |

Deleted: `WHY_CS_LIMIT` (so `WHY_TASK_RETURN` is now 8), `cs_odd`,
`wrap16`, `past`, `eip16`, the 16-bit CALL rel32 target trick
(`call_target_*`), `seg_cs_ffff`, `seg_cs_small`, `seg_cs_flat` and their
staged `ds_*` copies, and the real-mode cut of a delivery's EIP. Nothing in
the change waits: no uop issues later than before.

## Lockstep

`scripts/test_fes_i386_lockstep.py` at `9683e2cb0`, the merge of #1411's
head ([report](lockstep-report.json)), and with `--yosys` on the i386 lock's
Yosys `238eac9a4` ([report](lockstep-netlist-report.json)). The two reports
are identical but for the netlist's `yosys` field. Before the merge, at
`9adde623c`, the reports were the same but for the five #1424 programs
below and the four directed cases the merge brings.

| Run | Result |
| --- | --- |
| Corpus (SingleStepTests/80386, 941 files) | 1,758,699 pass, 0 fail; 3,517,401 retirements compared; 147,537 in an exception's handler |
| Random programs (16 seeds × 2,000) | 31,999 pass, 1 fails (pre-existing, below); 8,204,246 retirements; 3,020 end at a shutdown, 707 at a fault in a delivery delivered |
| Protected-mode programs (16 seeds × 500) | 8,000 pass, 0 fail; 464,949 retirements, 374,688 of them (with the random programs') with PE set; 14,235 deliveries through the IDT; 1,483 shutdowns; 165 not-built stops |
| Unbuilt encodings | 52,320 outside SCOPE, all stopped as not built |
| Directed cases | 40 pass |
| Unreal-mode cases | 6 pass |
| Protected-mode cases | 89 pass (38 new here, 4 from #1411's #1424 fix) |
| Memory cases (DMA, FLUSH) | 26 pass |

The latency-free reruns of seeds 1 and 2 retire the same trace.

**The new directed cases** (`--mode pmode`, 38 of the 89). Each far-jumps
to code it places under a code descriptor of a small limit:
- 16-bit and 32-bit code (78h, 80h, limit 0Fh):
  - running off the end, an instruction across the limit, a HLT at the
    last byte;
  - JMP to the last byte and past it, a Jcc not taken and taken to past it,
    LOOP, CALL rel16 and rel32 (66h), CALL and JMP r/m, RET and RET imm16
    to the last byte and past it, JMP rel16 (66h) in 32-bit code;
  - the far jump's own offset past the limit.
- Page-granular limits (88h 32-bit, 90h 16-bit, limit 1FFFh): off the end,
  the last byte, a 66h prefix as the last byte.
- 16-bit code under a 4 GiB limit (base FFFF1C08h) running from offset
  FFF8h on past FFFFh to a HLT at 10008h.
- Real mode under the limit CS kept (1Fh), after LIDT of the real-mode
  table and PE clear: off the end, INT n past the limit (#GP inside its
  delivery) and under it, JMP rel8, and far JMP, CALL, RETF and IRET past
  it.

Each ending is checked (the #GP's handler, error code 0 in protected mode,
or the HLT), and every retirement against Bochs. With slice 2's RTL 37 of
the 38 fail (stops as not built, or #GP at the wrong place); the 38th, the
far jump's own offset, was already built.

**The random protected-mode programs** gain two code descriptors at the
program's CS base in place of two of the GDT's random entries: 22 (B0h),
16-bit, and 23 (B8h), 32-bit, each with a small limit. A new block
(`pm_limit`, a 1-in-18 pick among a protected-mode block's items)
far-jumps to one of them. The program's first such jump sets that
descriptor's limit up to 1K past its target, mostly a few bytes. Then come
instructions, near jumps and Jcc (rel8, rel16 and rel32, 66h at random),
LOOP, JCXZ, JMP through a register, and CALL rel or through a register to
a subroutine that returns with RET or RET imm16, each forward over a few
instructions, any of which may lie past the limit. The block ends with a
far jump back to 30h. 16-bit code may leave the jump out, so the rest of
the block, and its exit to real mode, run under the small limit. The
32-bit code has its own encodings (NOP, INC, DEC, MOV, ADD, CMP, XOR,
branches), not the 16-bit generator's.

A new tally (`cs_limit`) counts what ran under a CS limit other than FFFFh
and FFFFFFFFh: 23,199 retirements and 1,299 #GP deliveries with PE set, and
223 retirements in real mode. The always-accept rule for not-built reason 8
(`WHY_CS_LIMIT`) is gone; reason 8 is now an IRET with NT set.

**One random program fails, before and after this change**, the same way
on slice 2's RTL with this branch's testbench, which generates the same
programs. It is not about CS's limit:
- `random#1#1473` ([#1425](https://github.com/DeanoC/fes/issues/1425)): a
  real-mode `CS: REP MOVSB` stores over its own first byte. The RTL runs all
  8 elements, as a 386 with the instruction decoded would, and Bochs stops
  after the first. The harness's rule for an unplanned store into the
  program's code applies only after it has compared that step.

Five more failed at `9adde623c`, and on slice 2's RTL before #1411 fixed
them: `random#4#691`, `#5#910`, `#7#1161`, `#14#399` and `#15#234`
([#1424](https://github.com/DeanoC/fes/issues/1424)). An injected
interrupt arrives with SP near 0 on a 16-bit stack, so its frame wraps to
SP FFFCh or FFFEh. The handler's IRET or IRETD then faults #SS(0) on its
FLAGS slot at 10000h on both sides. Bochs's `iret_protected` reads FLAGS
first; the RTL read the EIP and CS slots first, which land in the MMIO
window, where Bochs makes no read (`mmio-reads.0.N`). With #1411's fix
(`d9773b8f2`, merged in as `9683e2cb0`) all five pass, with the same
retirements and trace hashes.

**Against slice 2's report** (`3f8687ed0`'s `lockstep-report.json`, with
the #1424 fix):
- **Corpus:** every result field and trace hash is equal. The cycles grow
  by 2,628 of 174,952,508, spread up and down over 45 opcode files. They
  trace to one kind of test, `FE.0#106` the first: an 8-byte instruction at
  FFF8h ends at FFFFh, so the HLT after it, at 10000h, faults #GP(0).
  - Slice 2 cut D's EIP to 0000h for that delivery. The corpus's 386EX
    prefetch-queue model (`queue16`) places its self-modifying-code window
    from the uops' next EIP, so the window sat 64 KiB below the code.
  - With the cut gone the window is where the code is. That test alone
    runs 33 cycles faster, with 2 fewer DDR reads and writes, and the same
    trace hash.
  - The fetch-latency draws of the file's later tests move with it.
- **Directed cases:** the trace hash is slice 2's (`68d44755edca35dc`);
  the cycles grow from 5,781 to 5,993 and the DDR writes fall from 98 to 96.
  Run without fetch latency, two cases differ, both a real-mode CALL rel32
  (66h) to past the limit: `call-rel32-cs-limit` (+22 cycles) and
  `gp-stack-wrap` (+57). Their #GP now comes from M1, after the push's own
  check, as Bochs pushes first, where slice 2 raised it in D. The rest of
  the difference is the later cases' fetch-latency draws moving.
- **Unreal-mode and memory cases:** equal, trace hashes and cycles
  included.
- **Random and protected-mode programs:** not comparable, as the generator's
  protected-mode blocks now draw the new block and its descriptors.

## pc386 suites, governor and test386

`scripts/sim_fes_pc386.py` and the two i386 runners, at `9adde623c` but for
`--suite boot --cpu rtl`, which is of `9683e2cb0`. The first run of it, at
`9adde623c`, hit its 90-minute cap on a host at load 65-70 before it
finished, so it gives no result either way.

| Run | Result |
| --- | --- |
| `--suite boot` (oracle) ([report](boot-oracle-report.json)) | Pass: `C:\>` at 4.3 s; the VGA replay has 0 mismatches |
| `--suite boot --cpu rtl` ([report](boot-rtl-report.json)) | BOOTRTL |
| `--suite system` ([report](system-report.json)) | Pass: `C:\>` at 3.309 s, all 16 checks, as in slice 2's record |
| `--suite devices` ([report](devices-report.json)) | 132 checks pass |
| `--suite governor` ([report](governor-report.json)) | As slice 2: `max`, 286-12, 386SX-16 and 386DX-40 pass with TP 210,892, 27,453, 39,932 and 104,609, each within 0.04% of the table. 386DX-25 prints `GOV TP=0000FF5F DC=04A4 C18=0011F492`, slice 2's, then `system_tb` throws `port 2: a write burst cut short, then another begun at clock 119811804`, the bench gap [#1372](https://github.com/DeanoC/fes/issues/1372), at slice 2's clock |
| `test_fes_i386_governor.py` ([report](governor-bench-report.json)) | Pass, equal to slice 2's report (`10956ac5c`) field for field |
| `test_fes_i386_test386.py --cpu rtl` ([report](test386-rtl-report.json)) | As slice 2: POST codes 00h-06h and 08h, no error; POST 08h's set-up then turns paging on (`mov cr0, eax`), which stops as not built (`WHY_PAGING`) after 793,777 retirements |

## Quartus seal

`make build-fes-pc386-quartus` at seed 1 from a clean checkout of
`5d231b804` ([`seal-summary.json`](seal-summary.json)), Quartus Prime Lite
17.0.2 on the pc386 lock's Yosys `0a7927bf` netlist. All 109 input hashes
match `5d231b804`; against the branch head only `fes_i386_decode.sv`,
`fes_i386_pkg.sv` and `fes_i386_ucode_rom.sv` differ, the #1424 fix merged
after the seal ([Inputs](#inputs)). Every analysis passes, with no illegal or
unconstrained clocks. A first run of the same checkout was stopped by this
session's 2-hour cap in the fitter, on a host at load 65-70 with another
fit running; this one took 3 h 22 min.

| | This seal, `5d231b804` | Slice 2's final seal, `7224b11f6` |
| --- | --- | --- |
| Build ID, package | `49cf2f1b…`, `b057f27f…` | `b70597c6…`, `ad844e72…` |
| System Fmax, slow corners (61.44 MHz) | 72.76-73.53 MHz, setup +2.533 ns | 69.51-69.88 MHz, +1.889 ns |
| Pixel Fmax (74.27 MHz) | 82.25-83.58 MHz | 81.75-82.56 MHz |
| Setup, hold slack | +1.306 (pixel), +0.094 ns | +1.231 (pixel), +0.085 ns |
| Recovery, removal, pulse width | +6.569, +0.403, +1.122 ns | +7.183, +1.190, +1.122 ns |
| ALMs | 28,859 of 41,910 (69%) | 28,831 |
| Registers | 25,953 | 25,755 |
| RAM blocks | 258 | 259 |
| DSP blocks | 32 | 32 |
| RBF | 3,635,576 bytes, SHA-256 `36cd7a32…` | 3,644,880 bytes |

The change costs 28 ALMs and 198 registers, among them `cs_fl`'s 62
bits; slice 2's two seals of one design differ by 139 ALMs. The system
clock's slowest corner rises from 69.5 MHz to 72.8 MHz, above the
67.6-71.5 MHz that slice 2's three seals spread over and near slice 1's
72.43 MHz. One seal does not separate what the change removed (`wrap16`,
`cs_odd`, the CALL rel32 target trick) from placement noise.

Where the slack is, from TimeQuest on the sealed project at the slow
1100 mV -40 °C corner, the system clock's slowest:
- The system clock's worst paths are the VGA scanout's (`crtc_6` to
  `scanout_f_row` and `f_base`), +2.533 ns.
- The CPU's worst path is the L1I's block to the BIU's line, +2.932 ns.
- The new D-stage compare is not near either. Its paths from `d_eip` and
  `cs_fl` into D's stop and its fault keep +5.6 to +6.3 ns. The worst into
  D's stop is from the decode's registers, +5.37 ns. The worst into
  `cs_fl`, at a CS write, +8.43 ns.

## DOOM's start on the RTL

DOOM

## Known limits

- Bochs's real-mode far transfers and INT check the new offset against the
  limit CS has before the transfer, which the RTL follows. After a
  real-mode CS load the RTL sets CS's limit to FFFFh, the 386 rule, where
  Bochs keeps the cache; the lockstep gives the oracle the RTL's CS cache
  there (`cs_rule`), as before.
- The pre-existing harness gap above,
  [#1425](https://github.com/DeanoC/fes/issues/1425).
