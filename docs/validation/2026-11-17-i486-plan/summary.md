# i486: what 486SX and 486DX identities take, 17 November 2026

This record plans 486SX and 486DX CPU identities for `fes_i386_cpu` and
`fes.pc386`. The design doc already names the idea: an optional `i486-isa`
profile, a Phase 7 option, "only with a measured need"
([`i386-cpu-design.md`, "The 386, not the 486"](../../../sources/misteross/docs/i386-cpu-design.md#the-386-not-the-486)).
This record checks the 486's differences against the RTL item by item,
builds the Bochs oracle at CPU level 4 to see what that takes, measures
which titles in the drives use a 486, and orders the work into slices. It
changes no RTL, seals nothing and used no kit. The runs' method was
committed first: [`method.md`](method.md). Tracker:
[#1962](https://github.com/DeanoC/fes/issues/1962).

**Result.**
- **The gap to a 486SX is an instruction-set slice, not a new CPU.**
  Checked against the RTL, a 486 adds six instructions (BSWAP, XADD,
  CMPXCHG, INVD, WBINVD, INVLPG; all #UD today), five CR0 bits (NE, WP,
  AM, CD, NW; none stored, WP not enforced), EFLAGS.AC with exception 17,
  TR3, a hard-wired ET, and reset values (DX is zero today; DR6's bit 12).
  CPUID and EFLAGS.ID belong only to later steppings. Estimated at 350 to
  850 ALMs; nothing in it is measured
  ([the delta](#the-delta-checked-against-the-rtl), [area](#area-and-fit)).
- **A 486SX needs no x87 work. A 486DX is the SX plus the x87 unit behind
  a different shell:** CR0.NE and exception 16, FERR# and IGNNE# in place
  of BUSY#, and the 486's ESC rules. It has two prerequisites that are not
  met: the x87 unit complete ([#1768](https://github.com/DeanoC/fes/issues/1768))
  and a DE25 build of pc386, which today is analysis fits only
  ([SX and DX](#sx-and-dx)).
- **Fit.** 486SX on the DE10: estimated to fit, about 37,100 of a
  38,500-ALM ceiling. 486SX on the DE25: estimated to fit with room.
  486DX on the DE10: no, measured, because the 387 does not fit. 486DX on
  the DE25: estimated at about 42,000 of 46,800 (89.8%), under the 92%
  line by about 1,000 ALMs before the transcendentals' area is known.
- **The speed half exists already.** Presets 486DX-33 and 486DX2-66 run
  on an i486 timing table under the 386 identity. On main today the
  governor bench paces every loop at 486DX-33; at 486DX2-66 the DE10 core
  is beyond its reach on three of 19 loops, and a 50 MHz 486 on two. No
  486 preset has been benchmarked on a kit since Phase 4, and no committed
  record holds a real 486's scores, so no preset's accuracy can be stated
  ([performance](#performance-identity)).
- **The oracle moves to level 4 with adapter and provisioner changes, not
  a flag.** With them FreeDOS boots, and test386 and the SingleStepTests
  vectors pass with the same divergences. The 386 RTL's lockstep failures
  against it are CR0's read value and the #UD probes for the 486 opcodes.
  Bochs's only 486 is a DX4 with CPUID, and it cannot be a 486SX
  ([the oracle](#the-oracle-and-verification)).
- **Measured demand is one title, as its drive configures it.** Of the 53
  titles in the drives, run on the level-4 oracle, only Duke Nukem 3D 1.3D
  retires a 486 instruction other than CPUID: BSWAP, 32 times, first at
  the address where the 386 takes exception 6. It then exits because the
  machine has no VESA mode. 27 more titles and WfW 3.11 only test for a
  486, and 25 titles do neither in their launch window. The drives were chosen for a 386, so
  this is not the demand for a 486 ([demand](#demand)).
- **No source says what a 486 does where the RTL follows the 386's
  microcode,** and Intel's documents contradict each other on a 486SX's
  ESC with EM clear. Those are questions for a real 486
  ([open questions](#open-questions-for-deano)).
- **First slice:** the level-4 oracle and lockstep scope (slice 0), then a
  fit spike (slice 1). Both leave the CPU alone and can run beside the
  x87 work ([slices](#slices)).

## Sources and what each establishes

- **The 486's architecture** comes from three Intel documents, cited by
  section because the scans' page text is unreliable:
  - the Intel486 Microprocessor Family Programmer's Reference Manual, 1992
    ("PRM"): §4.1 (CR0), §10.1–10.2 (reset, the 486SX), §19.2.3 (the SX and
    emulation), §21.3 (differences from the 386), §25.1 (differences from a
    386 with a 387);
  - the Intel486 Microprocessors and Related Products data book 241731-002,
    January 1995 ("data book"), which covers the SX, DX, SX2, DX2 and DX4:
    §4.2.3.1 (control registers), §9.5 with Tables 9-8 and 9-9 (reset
    values and revision IDs), §13 Table 13-15 (clock counts), §14.0–14.1
    (differences from the 386), Appendix B and AP-485 (CPUID);
  - the i486 Microprocessor data sheet 240440-002, November 1989, the
    first DX.
  They establish what Intel documented. They do not establish undocumented
  behaviour, which is where the RTL follows the 386's microcode today.
- **What the RTL does** is read from the files named in each row at main
  `e2b8130b5`. A row marked absent was searched for and not found.
- **What Bochs's level 4 is** is read from its source at the pin
  `30cfd566ecc8` and from the runs below. It is Bochs's model, not a 486.
- **Sizes and speeds** are quoted from committed records, named where used.
  Anything not in a record is labelled an estimate with its basis.

## Method, and where the runs departed from it

[`method.md`](method.md) was committed (`406f2fff6`) before any run had a
result. The runs departed from it in these ways, none of which changes a
classification rule:

- **More than the configure flag changed.** The level-4 oracle needed the
  adapter and provisioner changes listed under
  [the oracle](#what-it-took-to-build), which `method.md` did not foresee.
- **The census ran on chip-30x0-ai-dev,** not Powerboat, which was at a
  load of about 45 from other work. The tree is the same commit with the
  same patches; the six drives, and `win-msdos` for the WfW run, were
  copied there and deleted after the runs, with the tree. The scratch
  patches and each run's `census.txt` and `report.json` are kept on
  Powerboat in `~/fes-scratch/i486plan-logs`, uncommitted.
- **Three more census keys** than the two `method.md` names:
  `cr0.486:<bits>:<stem>` for a change to CR0's NE, WP, AM, NW or CD, and
  `exc:<vector>:<stem>` for exceptions 6, 16 and 17 by the instruction
  that raised them. `method.md` listed those counts without saying how
  the census would get them.
- **The lockstep ran at reduced program counts,** given in the table
  below, to fit the host's load. `method.md` gave no counts.
- **The `--disable-fpu` build was tried only as far as its library.**
  Level 4 with `--disable-fpu` configures and `libcpu.a` builds. It was
  not linked or booted: the adapter requires the FPU
  (`bochs_oracle.cpp:30`; without it Bochs raises #UD for WAIT and never
  faults on ESC), and Bochs's one 486 model enables the x87 extension
  unconditionally and panics if a 486 model lacks it
  (`cpu/cpudb/intel/i486dx4.cc:39`; `cpu/cpuid.cc:1623-1624`). So it is
  not a 486SX either way.
- **The pilot was the sweep's own Duke Nukem 3D run.** The separate
  24-second run started for the pilot was stopped once the sweep's
  50-second run of the same title, launched the same way, had finished.
  Nothing was read from the stopped run.
- **The sweep was started twice.** The first start launched 15 titles at
  once and they raced on the shared simulator build; it was stopped and
  its outputs discarded. The second staggered the starts by 20 seconds.
  All 53 titles then ended without a harness error, about eight and a
  half hours after that start.
- **CHKCPU's run reports `FAIL`.** It is a text program that prints and
  exits, so the game check sees no screen progress. The level-3 census
  recorded the same false fail
  ([Phase 5 census](../2026-11-12-pc386-census-p5/summary.md)). Its
  census is counted.
- **`census.jsonl` names the instruction that changed a flag** (`popfd`,
  `iret`, and so on), as well as the 486 stems `method.md` lists.
- **WfW 3.11 ran; the Linux drives did not.** `method.md` names the
  `wfw-msdos` drive; what ran is `wfw-386` on the `win-msdos` drive, the
  Windows plan's enhanced-mode launch, after the 53, with its record's
  14-second limit. It is the 54th row. `wfw-msdos` was not run. The three Linux drives were not run at level 4,
  for time; their suite is not the game suite. Their level-3 records show
  which 486 tests they make ([demand](#demand)).
- **The exception key fired only in the WfW run.** The 53 titles have no
  `exc:` line for vector 6, 16 or 17. WfW 3.11 has 613,313 for vector 6,
  which shows the key counts vector 6. No run raised 16 or 17, so those
  two are untested.

## The delta, checked against the RTL

Paths are under `sources/misteross/cores/fes-common/rtl/i386/` unless a
longer path is given. "#UD (default)" means the opcode falls to the 0Fh
map's `default: undefined = 1'b1` (`fes_i386_decode.sv:2009`), which becomes
vector 6.

### Instructions

| Item | The 486 | The RTL today | Status |
| --- | --- | --- | --- |
| BSWAP r32 (0F C8–CF) | PRM §21.3.3; 1 clock (data book Table 13-15) | #UD (default). No length entry: `fes_i386_oplen.sv`'s `two_byte()` sizes it as two bytes with no ModRM, which is its real length | **absent** |
| XADD (0F C0, C1) | PRM §21.3.3; 3 clocks register, 4 memory | #UD (default). Length only: `fes_i386_oplen.sv:109`, with the comment at `:92-93` that the i486 forms "get their lengths for the later i486-isa profile" | **absent**, length present |
| CMPXCHG (0F B0, B1) | PRM §21.3.3; 6 clocks register, 7 or 10 memory | #UD (default). Length only: `fes_i386_oplen.sv:108` | **absent**, length present |
| CMPXCHG, A-step encoding (0F A6, A7) | Not in the PRM or the data book. That the first 486 stepping encoded CMPXCHG there is general knowledge, not something a source read for this record states | #UD on purpose, as the 386's XBTS and IBTS: `fes_i386_decode.sv:2002-2007`. No ModRM length | **absent**; only matters if an A-step is the target |
| INVD (0F 08), WBINVD (0F 09) | PRM §21.3.3; data book §14.0 item 4 | #UD (default). No decode, no length | **absent** |
| INVLPG (0F 01 /7) | PRM §21.3.3 | #UD: group 7's `default` at `fes_i386_decode.sv:1730`, and it is not in the `priv_op` list (`:2140-2141`) | **absent** |
| CPUID (0F A2) | Not in the 1992 PRM. Data book §14.0 item 4 and Appendix B: on the steppings that have it | #UD (default) | **absent** |
| RSM (0F AA) | Data book §14.0 item 4, with SMM, on the SL-enhanced parts | #UD (default) | **absent**; out of scope unless SMM is |
| MOV to and from TR3 | PRM §21.3.3 ("new test registers have been defined for the cache"); data book §14.0 item 9 | #UD: `undefined = !reg_f[2]` (`fes_i386_decode.sv:1970-2001`) makes TR0–TR3 #UD | **absent** |
| MOV to and from TR4–TR7 | TR4, TR5 for the cache, TR6, TR7 for the TLB with a changed model (PRM §21.3.3) | Plain 32-bit storage, no test function: `logic [3:0][31:0] trn` (`fes_i386_cpu.sv:405`) | **partial**: they hold values, as on the RTL's 386 |

### Registers and flags

| Item | The 486 | The RTL today | Status |
| --- | --- | --- | --- |
| CR0.NE, WP, AM, NW, CD | New bits (PRM §21.3.4). Reset value 60000010h: CD and NW set, ET set (data book Table 9-8) | Not stored. CR0 holds PE, MP, EM, TS, ET and PG (`fes_i386_cpu.sv:389`); a write drops the rest (`:3530`); a read returns bits 30–5 as ones, "as Bochs's 386" (`:384-391`) | **absent** |
| CR0.ET | "Not used by Intel486 processor hardware. The ET bit is hardwired to 1" (PRM §25.1 item 1) | Writable; reset from `init_cr0[4]`, which the machine sets from `x87_present` (`fes-pc386/rtl/pc386_system.sv:489`) | **differs** |
| LMSW | Must not change ET or NE (data book §4.2.3.1 note) | Writes TS, EM, MP and sets PE only (`fes_i386_cpu.sv:3541-3542`) | **present** |
| Supervisor write protection (WP) | With WP set a supervisor write to a read-only user page faults (PRM §4.1; data book §14.0 item 6) | Supervisor writes ignore R/W in the DTLB check (`fes_i386_cpu.sv:2374-2375`) and in the walker (`fes_i386_l1d.sv:462`, the R/W test is gated by `q_user`) | **absent** |
| CR3.PCD, PWT and the same bits in page entries | New (PRM §21.3.4–21.3.5); they drive the cache pins | CR3 keeps and returns all 32 bits (`fes_i386_cpu.sv:390`, `:3545`); only `cr3[31:12]` is used (`:4099`); entry bits 3 and 4 are never read | **present as storage**, which is all software can see without the pins |
| CR4 | Only on parts with VME, PVI or PSE (data book §4.2.3.1) | MOV CRn accepts CR0, CR2 and CR3; the rest are #UD (`fes_i386_decode.sv:1733-1760`) | **absent**; only for a later stepping |
| EFLAGS.AC (bit 18) | New flag (PRM §21.3.1) | EFLAGS is 18 bits wide: `logic [17:0] eflags` (`fes_i386_cpu.sv:349`), and so is every copy down the pipeline (`fes_i386_pkg.sv`, `pipe_t.eflags`) | **absent** |
| EFLAGS.ID (bit 21) | "The ability of a program to set and clear the ID flag indicates that the processor supports the CPUID instruction" (data book §4.2.2.3) | As AC | **absent**; only with CPUID |
| DR6 | Resets to FFFF0FF0h (PRM Table 10-1): bit 12 reads 0 | Resets to FFFF1FF0h (`fes-pc386/rtl/pc386_system.sv:491`); bit 12 is a writable flop (`fes_i386_cpu.sv:400`, `:3600`) | **differs** |
| DR4, DR5 | Alias DR6 and DR7 (no CR4.DE on a 486 without CR4) | Alias them (`fes_i386_decode.sv:1775-1776`; `fes_i386_cpu.sv:3598-3601`) | **present** |
| DR7 | As the 386 in the documents read | Stored whole, bit 10 forced (`fes_i386_cpu.sv:3601`) | **present** |
| Reset EDX | DH = 04h, DL the revision: SX 2xh, DX 0xh or 1xh, DX2 3xh, DX4 8xh (data book Table 9-9; PRM §10.1) | Zero. `.init_gpr(256'd0)` (`fes-pc386/rtl/pc386_system.sv:489`); the 386's 03xxh is not there either | **absent** on both identities |
| TLB | Four-way, 32 entries, pseudo-LRU replacement in place of random (data book §14.0 item 8) | Fully associative, 16-entry DTLB and 8-entry ITLB (`fes_i386_tlb.sv`) | **differs**, invisible except through TR6 and TR7 and timing |

### Exceptions and restart

| Item | The 486 | The RTL today | Status |
| --- | --- | --- | --- |
| #AC, vector 17 | Unaligned access at CPL 3 with AM and AC set; error code 0 (PRM §9.9.16, §21.3.2). "Except for the stack portion of an FSAVE/FRSTOR" (PRM §25.1 item 3) | No fault code, no vector: `fault_e` (`fes_i386_pkg.sv:358-378`) and `fault_vector` (`fes_i386_cpu.sv:3238-3251`) have none | **absent** |
| #MF, vector 16 | With NE set, an unmasked x87 exception is delivered as vector 16 before the next waiting ESC or WAIT (data book §4.2.3.1) | Never raised. The unit's error goes out only as `x87_irq13` (`fes_i386_x87.sv:32-35`, `:129`); WAIT's comment says the CPU "never raises exception 16" (`fes_i386_decode.sv:1464-1466`) | **absent**; DX only |
| Vector 9 | "Interrupt 9 does not occur in the Intel486 processor … simply aborts the instruction" (PRM §25.1 item 3); "interrupt 13 occurs instead" (data book §14.1 item 1) | Not built (the x87 plan's exception 9 was never built) | **nothing to remove**; the two documents differ on what replaces it |
| Undefined ESC opcodes | #UD, or #NM when TS or EM is set (PRM §25.1 item 3) | The x87 unit's own rule for unbuilt forms (`fes_i386_decode.sv:1165-1170`) | **to check** per form; DX only |
| Descriptor accessed bit | The locked read and write happen only if the bit is clear (PRM §21.3.6); the 386 always wrote | Writes the accessed bit only when the descriptor's is clear: `desc_drop` drops the write when `t3[8]` is set, and the handler-segment one when `hs_acc[0]` is (`fes_i386_cpu.sv:1202-1214`) | **present**: the RTL already has the 486's rule. The write a 386 makes to an already-accessed descriptor is invisible except on the bus |
| Self-modifying code | Prefetch queue 32 bytes, up from 16; "a jump always needs to execute after modifying code" (data book §14.0 item 10) | Two profiles sampled at reset, `coherent` and `queue16` (`fes_i386_cpu.sv:167-169`, `:4277-4397`). `queue16` models the 386's 16 bytes | **differs**: a `queue32` setting, or `coherent`, which is stricter than the chip and what packages use |
| Partial x87 stores and FLDENV on a fault | A fault can store the first half of an operand, or load part of an environment (PRM §25.1 item 3). The 386 with a 387 stores nothing | The 387's rules | **differs**; DX only, and in a corner no title in any census has reached |
| WAIT before a waiting instruction | "Will sometimes be executed as NOP", so an error may be reported one instruction later (PRM §25.1 item 3) | The 387's rule | **differs**; DX only |

### What no document settles

The RTL takes several behaviours from the 386's microcode and from the
386EX's measured results, not from a manual:

- DR6 is only ever ORed, never cleared by the CPU
  (`fes_i386_cpu.sv:3399-3401`);
- the #DB frame keeps RF as it was (`fes_i386_cpu.sv:3421-3426`);
- ICEBP (F1) delivers INT 1 and UMOV (0F 10–13) runs as MOV
  (`fes_i386_decode.sv:1011-1017`, `:156-159`);
- a near CALL pushes before its CS-limit check;
- the undefined flags after multiply, divide, shifts and BCD "follow the
  386EX" (about twenty sites, `fes_i386_ex.sv:34-67` and
  `fes_i386_muldiv.sv:28-53` among them);
- every `follow = "386"` entry in the lockstep's
  [`divergences.toml`](../../../sources/misteross/cores/fes-common/sim/i386/divergences.toml).

For none of these did this record find what a 486 does. The Intel documents
do not say. A web search on 17 November 2026 found no SingleStepTests suite
for the 80486 (the organisation publishes the 8088, 8086, 80286 and 80386)
and no published disassembly of the 486's microcode (the reenigne
repository covers the 8086 and the 80386). That search is not exhaustive.
So for these the choices are: keep the 386's behaviour on the 486
identities and say so, follow Bochs's level 4, or measure a real 486. That
is [open question 6](#open-questions-for-deano).

## Which 486

The documents describe two generations, and Bochs models a third thing.

| | A. The first 486 (1989–92) | B. A CPUID-capable 486 (1992 on) |
| --- | --- | --- |
| Described by | The 1989 data sheet and the 1992 PRM | The 1995 data book and AP-485 |
| CPUID, EFLAGS.ID | No. The 1992 PRM's new-instruction list has six instructions and no CPUID (§21.3.3) | Yes (data book §14.0 item 4, §4.2.2.3) |
| Reset DX | SX 042xh, DX 040xh or 041xh | The same family and model, later revisions |
| CR4, VME, RSM and SMM | No | On some parts. AP-485's feature flags list VME (bit 1); the data book's §4.2.3.1 describes CR4's VME, PVI and PSE; RSM comes with SMM on the SL-enhanced parts. This record did not establish which revision has which |
| What it adds to the work | Nothing beyond the tables above | CPUID's leaves, ID, and a decision on CR4, VME and SMM for the chosen part |
| Oracle | Bochs cannot be this as built: CPUID, BSWAP, XADD, CMPXCHG, INVD and WBINVD all hang off one feature, `BX_ISA_486` (`cpu/decoder/ia_opcodes.def:875-885`), and AC and ID become writable together (`cpu/flag_ctrl_pro.cc:73`). It needs a small oracle patch or a lockstep rule: CPUID is #UD and ID does not toggle | Bochs's one 486 model is an i486DX4, CPUID signature 0480h, with VME (`cpu/cpudb/intel/i486dx4.cc:37-42`, `:69`). VME can be excluded by feature string, which is how this record's runs were made |

Era software tells a 486 from a 386 by toggling AC (PRM §3, Figure 3-23,
and AP-485's detection sequence both do), and the 1992 PRM, written
three years after the first 486, has no CPUID, so software written for a
486 runs on A. B is needed only by software that wants CPUID on a 486.
[The census](#demand) does not say whether any does. Bochs's 486 has
CPUID, and 22
of the 53 titles execute it, one to five times in a run, and each of
them also toggles ID: detection, not use. On A the ID test would fail and that
software would take its no-CPUID path, as it does on a 386 today. So
nothing in the census contradicts A, and nothing in it could show a title
that needs CPUID on a 486, since no run was made without it. A is the
smaller target and is the one the 1992 PRM, the fullest source,
describes, and it is this record's lean. Neither target is an
A-step (the 0F A6/A7 CMPXCHG): no document here covers it and no title is
known to need it. The choice is Deano's:
[open question 1](#open-questions-for-deano).

## SX and DX

### What the documents say

- **The 486DX has the FPU on the chip.** "The complete Intel387 math
  coprocessor instruction set and register set have been added. No I/O
  cycles are performed during floating point instructions" (data book
  §14.1 item 1). There is no BUSY#, PEREQ or ERROR# pin; the pins are
  FERR# out and IGNNE# in.
- **Two error paths, chosen by CR0.NE** (data book §4.2.3.1; PRM §25.1
  item 1):
  - NE = 0, the reset state, is the DOS-compatible path. FERR# goes
    active on an unmasked exception; the board turns it into IRQ 13; with
    IGNNE# inactive the CPU "freezes immediately before executing the next
    floating point instruction" until the interrupt is taken. The freeze
    skips FNCLEX, FNINIT, FNSAVE, FNSTENV, FNSTCW and FNSTSW and does
    happen at WAIT.
  - NE = 1 is the native path: vector 16 before the next waiting ESC or
    WAIT, and IGNNE# is ignored.
  - FERR# timing differs from a 387's ERROR#: "in some cases FERR# is
    asserted when the next floating point instruction is encountered and
    in other cases it is asserted before", where "the Intel387 math
    coprocessor asserts ERROR# when the error occurs" (data book §14.1
    item 3).
- **ET reads 1 and cannot be cleared; MP "should be set to 1"** on a DX
  (PRM §25.1 item 1). Reset and FINIT do not signal an error as a 387's do,
  and FINIT clears the instruction and data pointers (PRM §25.1 item 2).
- **The 486SX has no FPU.** Software is told to set EM = 1 and MP = 0, and
  then every ESC takes vector 7 (PRM §19.2.3.2; data book Table 4-5). With
  EM = 0 the documents do not agree:
  - PRM §10.2 gives the detection sequence FNINIT, FSTCW: "the FSTCW
    instruction will give a value of FFFFh for the Intel486 SX
    microprocessor". That needs the ESC to run without a trap and FFFFh
    to arrive in memory;
  - data book §4.2.3.1: "if EM bit is 0 for the Intel486 SX and IntelSX2
    processors, the system will hang".
  (Both also say an SX takes "interrupt 7 … upon encountering any
  floating point instruction regardless of the value of the NE bit";
  that sentence is about NE and sits under the advice to set EM.) The
  detection sequence only works if the first holds, and it is exactly
  what the RTL's empty socket does today: FNSTCW stores FFFFh
  (`fes_i386_decode.sv:50`). The plan takes the first and lists the
  question for a real 486SX
  ([open question 6](#open-questions-for-deano)).
- **The SX's CR0 resets to 60000010h like the DX's** (data book Table 9-8
  gives one value for the family), so ET reads 1 on an SX. On the RTL's
  386 without a 387, ET resets to 0.
- **The 487SX** goes in a second socket beside the SX (PRM §10.2: "two
  distinct sockets"). AP-485's Table 3 gives it its own signature, model
  3, and notes it has no CPUID. To software an SX with a 487SX is a 486DX whose reset DX differs.
  It is not a third identity worth building: the plan treats "486SX with
  the FPU setting on" as the DX.

### Mapped onto what exists

| | Today | 486SX | 486DX |
| --- | --- | --- | --- |
| The x87 unit | `fes_i386_x87` and its backends, selected by `x87_present` and `X87_BACKEND` (`fes_i386_cpu.sv:130`, `:266-270`) | Not instantiated. `x87_present = 0`, as the package ships today (`fes-pc386/rtl/pc386_system.sv:470`) | The same unit with a different shell: see below |
| ESC with EM = 0, TS = 0 | The empty-socket path: no trap; FNSTCW and FNSTSW store FFFFh (`fes_i386_decode.sv:47-61`) | The same path gives PRM §10.2's FFFFh | Executes |
| ET | Follows `x87_present` | Hard-wired 1 | Hard-wired 1 |
| Error reporting | The AT's latch on IRQ 13 with ports F0h and F1h (`fes_i386_x87.sv:730-737`) | None | NE = 0: FERR# to the same IRQ 13 and F0h latch, with the 486's freeze rule in place of BUSY#. NE = 1: vector 16 |
| CMOS 14h bit 1, INT 11h | From `x87_present` (`fes-pc386/rtl/pc386_rtc.sv:166`) | 0 | 1 |
| DE10 | Fits with the identity off | Estimated to fit ([area](#area-and-fit)) | Does not: the 387 does not fit the DE10 with the identity on ([x87 slice 4](../2026-11-15-i386-x87-s4/STOP-RULE-FIRED.md)) |
| DE25 | The 387's home; analysis fits only | Fits | Estimated to fit, on the pipelined backend |

**So a 486SX is the 386 identity plus the instruction-set delta, with no
x87 work at all.** It is the ISA slice and nothing else.

**What a 486DX needs from the x87 work that a 386 with a 387 does not:**

1. CR0.NE and vector 16, with the saved CS:EIP at the next waiting ESC or
   WAIT.
2. The NE = 0 path's freeze rule in place of the 387's BUSY# hold, and
   IGNNE#. On the AT board the latch that port F0h clears is what drives
   IGNNE#, so the machine side stays; the CPU side changes from "hold
   every ESC but FNINIT and FNCLEX while BUSY#" (`fes_i386_x87.sv:52-58`)
   to "freeze before the next waiting ESC or WAIT while FERR# and not
   IGNNE#".
3. No BUSY# or PEREQ protocol, and no ERROR# sampled at reset for ET.
4. The 486's instruction rules: #UD for undefined ESC forms, the partial
   store and FLDENV cases, WAIT as NOP before a waiting form, FINIT
   clearing the pointers, no error signalled at reset or FINIT, no vector
   9.
5. Timing: the governor's 486 column for the x87 classes. Today every
   arithmetic ESC is charged as FNOP (`governor.toml:885-952`), which the
   x87 plan's slice 6 already owes for the 387. The data book's Table
   13-19 gives the 486's floating-point clock counts, with lower and upper
   ranges and a concurrent-execution column.

Items 1 to 4 are a second shell around the same arithmetic. The envelope
that [#1907](https://github.com/DeanoC/fes/pull/1907) merged keeps the
arithmetic behind `X87_BACKEND`, so a 486DX reuses the backend and the
register stack unchanged and replaces the bus-facing rules. Nothing in the
486DX list needs the transcendentals to exist first, but a 486DX that
cannot compute FSIN is no more use than a 387 that cannot: the x87
tracker's rule that the identity stays off until the unit is complete
applies to the DX identity equally.

**Ordering against [#1768](https://github.com/DeanoC/fes/issues/1768).**
The 486DX depends on the x87 unit being complete and on a DE25 build of
pc386 existing. Neither is true today: the transcendentals are in flight,
and pc386 on the DE25 is analysis fits only, with no board shell, pins,
PLL or producer
([Agilex fit record](../2026-11-16-i386-agilex5-fit/summary.md)). The
486SX depends on neither. The 486's rule changes should not be folded into
the x87 slices in flight: they are a different chip's rules for the same
state, and lockstep for them needs the level-4 oracle.

## Performance identity

### What exists

**The speed half of a 486 identity is already built, as a timing table
under the 386 identity.** The governor has presets 6 (486DX-33) and 7
(486DX2-66) on an i486 cost table (`governor.toml:126-140`), exposed in
the `fes.pc386.machine` record's word 0
(`sources/mister-packages/packages/abi/fes_computer.yaml:488-499`). What
is missing is the identity those presets should belong to: today a title
launched at "486DX2-66" sees a 386 that happens to run at a 486's pace.

How the governor works, from `fes_i386_governor.sv` and the
[design doc](../../../sources/misteross/docs/i386-cpu-design.md#speed-control):

- Each retired instruction adds its class's clock count, from the selected
  table, to a debt counter. The 486 column is the i486's real-mode counts
  from the compiled "80x86 Integer Instruction Set" table, checked against
  MAME's (`governor.toml:19-31`, `:58-62`). A fractional enable at the
  preset's frequency pays the debt down, and D stops issuing while the
  debt is over 64 reference clocks.
- The counts are the data sheets' with every access a cache hit. "Prefixes,
  and the 386's and 486's queue and cache effects, are not charged." So a
  486 preset is a 486 that never misses its cache and never pays its
  prefetch penalties.
- The governor only slows the CPU. Where the RTL at `max` is slower than
  the table, that code runs at the RTL's speed.
- Word 1 of the record overrides a preset's frequency, 1 to 100 MHz in
  100 kHz steps. So 486 at 25 or 50 MHz is preset 6 with word 1 set to 250
  or 500, with no new preset ID. The eight IDs of the 3-bit field are all
  taken (`fes_computer.yaml:488-499`; `fes_i386_governor.sv:50`), so any
  new named preset widens that field across the RTL, the ABI and FogCast.
- I/O and VGA memory are charged period ISA bus time under every preset,
  the 486 presets included. A 486 board with a local-bus VGA did not pay
  that. The machine design notes it for device time
  (`pc386-machine-design.md`, "I/O timing"); under a preset the governor
  still charges it.

### The 486's clock counts against the 386's

From the data book's Table 13-15 and the 386 column of `governor.toml`
(first number 386, second 486, as the governor holds them):

| Class | 386 | 486 |
| --- | ---: | ---: |
| MOV register to register | 2 | 1 |
| MOV load | 4 | 1 |
| ALU register, register | 2 | 1 |
| ALU load | 6 | 2 |
| Jcc not taken / taken | 3 / 8 | 1 / 3 |
| CALL near | 8 | 3 |
| RET near | 11 | 5 |
| REP MOVS per element | 4 | 3 |
| MUL r32 (an early-out range; the governor charges its middle) | 24 | 28 |
| DIV r32 | 38 | 40 |

The 486 column is in the governor already; the six new instructions need
rows (BSWAP 1, XADD 3 and 4, CMPXCHG 6 and 7–10, INVD 4, CPUID 9 or 14,
all Table 13-15; the table's WBINVD and INVLPG rows were not legible in
the scan and are left to the slice).

### Can the core reach a 486?

What is measured, and on which core:

- **Benchmarks by preset, on a kit, before Phase 4's prediction.** The
  only per-preset table is the
  [Phase 4 baseline](../2026-11-05-i386-phase4-baseline/summary.md), Kit A,
  on the pre-BTB core: at 486DX2-66, 3DBench 26.3, PCPBench 7.4, TopBench
  133, pack DOOM at max detail 12.5 fps; at 486DX-33, 20.4, 5.5, 102 and
  10.1; at `max`, 50.0, 10.7, 297 and 24.3. It shows the presets slow the
  core in the right order. It does not show either preset matches a real
  486: the same record's sibling notes TopBench's database lists real
  486DX2-66 machines at 170–190
  ([Phase 4 profile](../2026-11-04-i386-phase4-profile/summary.md)),
  against the preset's 133 on that core, where the governor record had
  already found the core slower than the 486 tables on taken branches.
- **`max` after Phase 4.** DOOM `-timedemo demo3` went from 26.7 to 40.05
  fps and 3DBench from 50.0 to 76.9 across the Phase 4 slices
  ([indirect-prediction record](../2026-11-08-i386-phase4-indirect/summary.md)),
  with the DOOM window's clocks per instruction at 1.667. No per-preset
  benchmark has been rerun since.
- **The governor bench on main, run for this record.** `test_fes_i386_governor.py` on main
  `e2b8130b5`, host simulation of the RTL at the package's 61.44 MHz
  (19 microbenchmark loops; PASS). No committed record carries the 486
  presets' bench results from after Phase 4:
  - **486DX-33 paces every loop:** all 19 within +0.29% of the table's
    time, none beyond the CPU. Before Phase 4 it was beyond the CPU on
    four ([governor record](../2026-11-01-pc386-m5-governor/summary.md)).
  - **486DX2-66 is beyond the CPU on three loops:** `branch` (the RTL at
    `max` takes 76,654 clocks where the table allows 47,035, +63.18%),
    `pusha` (+58.77%) and `int` (+2.31%). The other 16 are within +0.32%.
  - **Each loop's ceiling,** the 486 frequency at which the table's time
    equals the RTL's own time at `max` (derived from the same report, not
    a run): `branch` 40.9 MHz, `pusha` 42.2, `int` 65.4, `alu` 81.9,
    `direct` 91.8, `crt` 98.2, and above 100 MHz for the rest.
  - What it does not show: a benchmark or a game. These are short loops
    with every access a cache hit, and `branch` is a jump, a call and a
    return in seven instructions, the RTL's worst case. Why that loop
    still takes 25.5 clocks an iteration after Phase 4's prediction was
    not examined.
- **No committed record holds a real 486's DOOM, 3DBench or PCPBench
  score.** The design doc's verification item for period benchmarks is
  still planned, and its gates ("at least a 486DX2-66 on every integer
  benchmark") have no reference figures behind them. TopBench's database
  figure above is the only one.

What that table does show about reach is on whole benchmarks, and it is
kit evidence: even before Phase 4, 486DX2-66 scored about half of `max`
(3DBench 26.3 against 50.0, pack DOOM 12.5 fps against 24.3). A score
below `max` means the governor, not the core, set the pace over the
benchmark as a whole. It does not mean every stretch kept up: where the
core is slower than the table the governor banks nothing, and that
stretch runs late. One limit is by construction: 486DX2-66 pays down
667 reference clocks per 614.4 core clocks, and the core retires at most
one instruction a clock, so a run of one-clock 486 instructions cannot
be paced at 66.7 MHz at any CPI.

So, by preset, on the bench's loops, which are the worst case:

| Preset | DE10, 61.44 MHz | DE25 |
| --- | --- | --- |
| 486SX/DX-25 (preset 6, word 1 = 250) | Reached on all 19 bench loops (derived: the lowest ceiling is 40.9 MHz). No kit benchmark | Not measured; no DE25 build exists |
| 486SX/DX-33 (preset 6) | Reached on all 19 bench loops (measured, host simulation). Kit benchmarks exist only from before Phase 4 | The same |
| 486DX-50 (preset 6, word 1 = 500) | Beyond the CPU on `branch` and `pusha` (derived: ceilings 40.9 and 42.2 MHz); reached on the other 17. No run at 50 MHz | The same |
| 486DX2-66 (preset 7) | Beyond the CPU on `branch`, `pusha` and `int` (measured, host simulation); reached on 16. Kit benchmarks exist only from before Phase 4 | The same |

**On the DE25** the CPU alone closes at 130 MHz and the machine has been
fitted only at 61.44 MHz; its restricted-Fmax estimates of 106–120 MHz are
"estimates and nothing more", and "the clock the DE25 configuration would
run at is not settled"
([Agilex fit](../2026-11-16-i386-agilex5-fit/summary.md);
[DE25 envelope](../2026-11-16-de25-x87-envelope/summary.md)). A DE25
machine clock above 61.44 MHz would raise `max` in proportion and put the
faster 486 presets within reach, but that is a design decision and a
measurement nobody has made.

**DX2 and DX4.** A DX2-66 is a DX-33 bus with a doubled core: the
governor's preset 7 already models its core clock, and bus-speed effects
are not modelled for any preset. It belongs, since the preset exists and
DOOM-era titles were tuned on one. A DX4-100 is past what the DE10 core
reaches on any evidence here and its 16 KB cache and CPUID belong to
option B; the plan leaves it out.

**What the 486 presets need,** in order of how much they would change a
score:

1. A reach measurement after Phase 4 on a kit, per preset, with the Phase
   4 baseline's benchmarks, so that "reachable" is a kit figure.
2. Reference scores from real 486s (or, as the 386 presets are doing, an
   86Box reference with its limits stated) for the same benchmarks.
   Without them a preset's accuracy cannot be stated at all.
3. Cache and queue effects, which the tables leave out. An 8 KB cache
   misses on game code, so a real 486 is slower than its table; how much
   is not measured here.
4. The 486 rows for the new instructions and, for the DX, the x87 column.

## The oracle and verification

The oracle is Bochs at `30cfd566ecc8`, configured `--enable-cpu-level=3`
(`scripts/fes_i386_oracle.py:30-37`). This record built it at level 4 in a
scratch tree and ran the existing suites against it unchanged.

### What it took to build

Changing the flag is not enough. Four things stopped it, in order:

1. **The adapter refuses any level but 3 at compile time**
   (`bochs_oracle.cpp:30-32`).
2. **Level 4 needs Bochs's CPU-model library.** `BX_CPU_C::initialize()`
   calls `create_i486dx4_cpuid()`, which is in `cpu/cpudb/libcpudb.a`; the
   provisioner builds and links `libcpu.a`, `libfpu.a` and
   `libsoftfloat.a` only. The link fails.
3. **Level 4 reads three settings the adapter does not supply.** The
   adapter's parameter stub answers `get_param_bool` only; level 4 also
   asks for the CPU model (`get_param_enum`) and the feature strings to
   exclude and add (`get_param_string`), whose defaults return null. The
   scratch adapter answers `i486dx4` and excludes `vme`.
4. **Two levels cannot share a tree.** The provisioner deletes every
   other `bochs-level3-*` build, adapter object and testbench when it
   builds (`fes_i386_oracle.py:148`, `:197-199`, `:234-236`), and the
   machine bench's build directory is not keyed by level. The scratch
   build used its own worktree.

The scratch changes are 26 lines across `fes_i386_oracle.py`,
`bochs_oracle.cpp` and `census.h` and are not committed. Also noted and
not changed:

- The oracle's stack-limit patch
  ([record](../2026-11-16-i386-bochs-flat-stack-limit.md)) is wrapped in
  `#if BX_CPU_LEVEL == 3`, so at level 4 it compiles to nothing. Whether a
  486 keeps that #SS is not established.
- The provisioner and the patch are sealed inputs of pc386
  (`build_fes_pc386_quartus.py:157`, `:515-525`), so a level-4 oracle
  added in place moves the seal's inputs. Slice 0 has to keep the level-3
  identity of both byte for byte, or accept the move.
- The adapter's `State` has no CR4 (`i386_oracle.h:37-51`).

### What Bochs's level 4 is

Read from the source at the pin:

| | Bochs level 4, model `i486dx4`, VME excluded | A 486, per the documents |
| --- | --- | --- |
| BSWAP, XADD, CMPXCHG, INVD, WBINVD | Run (`BX_ISA_486`) | Run |
| CPUID | Runs; leaf 1 EAX is 00000480h (`i486dx4.cc:69`). Cannot be separated from the row above | Option A: #UD. Option B: runs |
| INVLPG | Runs, and runs at level 3 too (`ia_opcodes.def:886`, ISA 0), which is why the level-3 lockstep has a `ud-invlpg` entry | Runs |
| EFLAGS.AC and ID | Both writable (`flag_ctrl_pro.cc:73`) | AC; ID only with CPUID |
| CR0 | Writes masked with E005003Fh (`crregs.cc:1095-1096`): NE, WP, AM, NW and CD are real, the reserved bits read 0. ET forced on while the x87 is present (`:1105-1108`) | The same bits; ET hard-wired |
| WP | Enforced in the walker (`paging.cc:796`, `:813`) | Enforced |
| #AC | Built above level 3 (`BX_SUPPORT_ALIGNMENT_CHECK`) | Vector 17 |
| NE | With NE set an unmasked x87 exception raises #MF directly (`cpu/fpu/ferr.cc:191-194`); with it clear, the FERR and IGNNE path the adapter's AT coupling uses | The same two paths |
| Reset EDX | Zero at every level; the line that would set it is commented out (`init.cc:896`) | 04xxh |
| DR6 at reset | FFFF1FF0h; the 486 value is under `BX_CPU_LEVEL >= 5` (`init.cc:1038-1043`) | FFFF0FF0h (PRM Table 10-1) |
| MOV TRn | #UD at every level | TR3–TR7 |
| A 486 without an FPU | Not available: the 486 model enables x87 unconditionally, and a 486 model without it is fatal (`i486dx4.cc:39`; `cpuid.cc:1623-1624`) | The 486SX |

So Bochs's level 4 is close to option B without its reset values and
test registers, and it cannot be a 486SX. The SX is checked the way the
386's empty socket is: the lockstep's no-387 rule, with ET's value
excepted.

### What ran, and what it showed

All on Powerboat in the scratch tree, level-4 oracle, 17 November 2026.
Logs stay on the host.

| Run | Result | What it establishes |
| --- | --- | --- |
| `sim_fes_pc386.py --suite boot` | **PASS.** FreeDOS reaches its prompt at 3.7 simulated seconds after 89,865,920 steps; the VGA replay matches with 0 mismatches | The Bochs BIOS, VGABIOS and FreeDOS boot on the level-4 oracle unchanged. Nothing in the boot path depends on the 386's CR0 or EFLAGS answers |
| `test_fes_i386_test386.py` (oracle) | **Passes, identical to level 3.** POST reaches FFh; nine divergence groups; the POST EEh text equals the reference in all 44,926 lines; the SingleStepTests cross-check agrees. A level-3 run on main the same day gives the same POST codes, the same 79,641,629 steps, the same count in each divergence group and the same final-RAM SHA-256 (`44597188…`) | test386 built for `CPU_FAMILY=3` still passes on a level-4 Bochs, undefined flags included: Bochs's flag results do not change with the level. It does not establish that a real 486's undefined flags equal the 386's |
| `test_fes_i386_vectors.py` | **Unchanged.** 1,437,596 of 1,758,700 states pass and 321,103 fail, every failure covered by the same 21 divergence entries with the same counts, at level 4 and, rerun on main the same day, at level 3 | The 386EX corpus cannot tell the two levels apart. It is a 386 corpus and says nothing about a 486 |
| `test_fes_i386_lockstep.py`, reduced (`--limit-per-file 20 --random-programs 200 --pm-programs 50 --task-programs 20 --debug-programs 20 --undefined-programs 50`) | **Fails, as expected of a 386 against a 486.** Details below | The size of the lockstep's rule change |

The lockstep run, the RTL as it is (a 386) against the level-4 oracle:

- **Unaffected:** the SingleStepTests corpus programs (18,820 of 18,820
  pass), the 84 directed cases, the 1,933 no-387 cases, the 120 x87 cases
  with the identity on, and the 27 memory cases.
- **One cause accounts for nearly every other failure: the value CR0
  reads.** The RTL returns bits 30–5 as ones (7FFFFFF1h in the unreal-mode
  case); the level-4 oracle returns 60050031h. Any program that copies CR0
  into a register then differs in that register and usually in everything
  after it. The report's first mismatch is that read in all 6 failing
  unreal-mode cases (of 7), all 151 failing paging cases (of 158), 11 of
  the 36 failing protected-mode cases (of 726), and every one of the 200
  real-mode random-program failures the report itemises (770 of 3,200
  programs fail; each itemised one stops at `MOV EAX, CR0` in the
  program's protected-mode entry). The run does not separate a failure
  that is only the read value from one that would also meet WP or
  another 486 bit later: the comparison stops at the first mismatch, and
  the programs write back what they read, which on the oracle sets WP,
  AM, NW and CD.
- **The 386's #UD probes for the 486 opcodes fail by design:** the other
  25 failing protected-mode cases are the BSWAP, CMPXCHG, CPUID, INVD and
  MOV CR4 probes, each in real mode, protected mode at CPL 0 and 3, and
  V86 at IOPL 0 and 3. In the every-encoding sweep 3,904 of 53,008
  encodings no longer raise #UD on the oracle; the 200 the report
  itemises are in the 0F 08, 09, A2, B0, B1, 20 and 22 rows.
- **Not itemised, so not attributed:** the protected-mode, task-switch,
  breakpoint and undefined-encoding random programs fail in 401 of 800,
  160 of 320, 63 of 320 and 381 of 800. The report keeps no per-program
  failure for these families. They enter protected mode through the same
  CR0 read, so the same cause is likely, but this record did not check.
- **What the itemised failures do not contain:** any mismatch that is
  not CR0's value or a 486 opcode. That is a statement about the
  itemised failures of a reduced run, not about the suite.

**What moving the lockstep takes,** from that:

- a per-identity CR0 read value and write mask (`CR0_MASK` is 8000000Fh,
  `lockstep_tb.cpp:1263-1266`) and an EFLAGS mask that includes AC, and ID
  for option B (`EFLAGS_MASK` is 0003FFFFh, `:206`);
- the #UD probe list (`ud_probes16`, `:7344-7355`) and the runner's
  386 decoder classes (`test_fes_i386_lockstep.py:211-214`) per identity;
- the divergence list: `ud-invlpg` becomes agreement on a 486; `mov-tr`
  grows TR3; `dr6-sticky`, `db-frame-rf`, `icebp-gd`, `umov` and
  `ud-0f0d` each need a 486 answer
  ([what no document settles](#what-no-document-settles)); the five
  `[[x87]]` entries describe the 387's BUSY# latch and need 486DX
  versions;
- new rules for what Bochs gets wrong for a 486: reset EDX, DR6's reset
  value, the test registers, and for option A CPUID and ID;
- the program generators must stop treating CR0's upper bits as inert:
  a random program that writes all ones to CR0 now turns on WP and AM.

### test386, SingleStepTests and 486 conformance

- **test386.asm targets the 386 only.** Its configuration accepts
  `CPU_FAMILY` 3, and its undefined-flag checks are the 386's
  (`scripts/test_fes_i386_test386.py:56-60`). On a 486 identity it can run
  only with `TEST_UNDEF` off, as an integer regression.
- **SingleStepTests has no 80486 suite** (see above); the 80386 suite was
  captured from a 386EX.
- **The repo has no 486 test** beyond the lockstep's #UD probes, which
  expect the 386's answer for CPUID, INVD, CMPXCHG and BSWAP
  (`lockstep_tb.cpp`, `ud_probes16`).
- So 486 conformance rests on: directed lockstep cases for each new
  instruction and bit against the level-4 oracle; the standard detection
  routines (Intel's AC test from PRM Figure 3-23 and AP-485's CPUID
  sequence) run on both identities; QEMU's `tests/tcg/i386` for CMPXCHG,
  XADD and BSWAP, pinned as test inputs the way the x87 plan pins its x87
  tests; and the title census rerun on the RTL.

## The machine

- **BIOS.** The machine runs the Bochs `rombios.c` at `REL_3_1_FINAL`,
  built `-DLEGACY`, and the LGPL VGABIOS 0.9d
  (`sources/misteross/cores/fes-pc386/firmware/`). It does no CPU
  detection: `BX_CPU` is a build constant (3), and the CPUID, MSR and APIC
  code is in the 32-bit part that `LEGACY` leaves out. It sets nothing by
  CPU type. It also leaves CR0.CD and NW as reset left them, where a 486
  board's BIOS enables the cache: on the RTL the bits will have no effect
  (the design doc's position), so a title reading CR0 sees "cache
  disabled" unless the BIOS clears them. A two-instruction BIOS change, in
  the ISA slice.
- **Reset DX.** Intel recommends "that the BIOS save the contents of the
  DX register, immediately after RESET, so that this information can be
  used later" (data book §16.5.1).
  The Bochs BIOS does not, so nothing consumes the signature today; it is
  still the documented way to tell an SX from a DX.
- **A20 and cache control.** A20 comes from the 8042 and port 92h
  (`pc386_at_core.sv:361`). There are no chipset, turbo or cache-control
  ports (no 22h/23h), and every undecoded port reads FFh. INVD and WBINVD
  can map to the existing FLUSH path (`fes_i386_biu.sv:354-356`) or do
  nothing; either is invisible to software on a machine whose caches are
  coherent.
- **Chipset features of 486 boards.** External cache, VLB and a chipset's
  configuration registers are out of scope: no title in the drives is
  known to need one, and the census could not test for one. The one
  board-level thing a 486 changes that software sees is VGA speed, above.
- **CMOS and INT 11h.** Bit 1 of CMOS 14h already follows the x87
  identity. Nothing else in CMOS says what the CPU is.
- **FERR# and IGNNE#.** The AT's latch and ports F0h and F1h are built for
  the 387 (`pc386_at_core.sv:194-195`); the 486DX reuses them.

### Identity: one core or two, a setting or a build

The CPU has one identity input today, `x87_present`, and the design doc
defines a "Profile" input sampled at reset: "`i386dx` or `i486-isa`,
`coherent` or `queue16`". Three ways to offer the 486, with what each
costs:

| | Reset-sampled setting in `fes.pc386` | Module parameter, one package per identity | A separate core (`fes.pc486`) |
| --- | --- | --- | --- |
| How it is chosen | A word of the `fes.pc386.machine` record, per library game, like the preset | At build: a recipe value set per identity, each with its own `BUILD_ID` ([lanes design](../../design/rtl-frontend-lanes.md), "a core-level difference is a module parameter") | A different package ID |
| Area | Both identities' logic in one bitstream. The SX delta is small; the DX delta rides on the 387 | Each build carries only its own | The same |
| Seals and kit runs | One | One per identity | One per core |
| Lockstep | Both identities on one netlist, against a level-3 and a level-4 oracle | Each value set against its oracle | The same |
| Precedent | The governor preset; `x87_present`'s planned record word | The x87 plan's second-variant option (`fes.pc386` and `fes.pc386-387`), which "doubles the seals and kit checks" | PC/XT is its own core, but it is a different CPU and machine |
| Library | A title declares what it needs ([forward plan](../../plans/forward-plan.md): "a game or setup may declare what it requires (EMS, a 387, a CPU speed)") | The library picks a package | The library picks a core |

The record has room: word 7 of `fes.pc386.machine` is reserved in
`computer-io.md`, though `pc386-machine-design.md` gives it to the EMS
board, which is a conflict to settle before either uses it. FogCast's
`MachineSettings` carries words 0 to 6 and no CPU identity field
(`sources/FogCast/protocol/pc386.go:225-259`). The choice is Deano's:
[open question 2](#open-questions-for-deano).

## Demand

### What the existing records already say

- **One title is recorded as needing a 486: Duke Nukem 3D 1.3D.** On a
  kit and on the level-3 oracle it stops in DOS/4GW with exception 6 at
  0180:00222BE2, after "Loading palette/lookups", 21.45 simulated seconds
  in ([compatibility census](../2026-11-07-pc386-compat-census/summary.md);
  [Phase 5 census](../2026-11-12-pc386-census-p5/summary.md#duke-nukem-3d)).
  That record did not name the opcode.
- **No record lists a title as too slow on a 386 preset.** No census
  tested for it: they ran at `max`.
- **Four titles are blocked on the 387,** not the CPU: Quake, Screamer,
  Chris's 3D Benchmark and Angband (Phase 5 census). On real hardware a
  machine with a 387's worth of FPU in 1995–96 was a 486DX or a Pentium;
  Quake's manual asks for a Pentium. A 386 with a 387 runs them at a
  386's speed.
- **WfW 3.11 and Linux use nothing a 386 lacks.** WfW 3.11 in 386
  enhanced mode runs "no CPUID, BSWAP, CMPXCHG, XADD, INVD, WBINVD or
  INVLPG" ([Windows plan](../2026-11-11-pc386-windows-plan/summary.md)).
  Linux 0.99.15, 1.2.13 and 2.0.0 "find a 386 by failing to set
  EFLAGS.AC" and report `i386`
  ([Linux plan](../2026-11-11-pc386-linux-plan/summary.md)). So on a 486
  identity those kernels would take their 486 paths (WP, INVLPG, and on a
  DX with NE the native x87 error path): that is where a 486 identity
  first runs code no 386 run has reached. It was not run here.
- **The benchmarks that name the CPU:** CHKCPU calls `max` a "classic
  386", Norton SI 8 and SpeedSys print a system summary, TopBench matches
  its database
  ([M5 game checks](../2026-11-04-pc386-m5-game-checks/summary.md)).

### The level-4 census

53 titles, every title of the six drives, and then WfW 3.11 in 386
enhanced mode ran on the level-4 oracle with the 387 fitted, as
[`method.md`](method.md) sets out. All 54 ended without a harness error.
The counts are in [`census.jsonl`](census.jsonl), one row per run.

By `method.md`'s classification:

| Class | Titles | Which |
| --- | --- | --- |
| Requires a 486 | 1 | Duke Nukem 3D 1.3D, as the `pmode` drive configures it |
| Uses 486 instructions when it finds a 486 | 0 | |
| Tests for a 486 only | 28 | 27 titles and WfW 3.11; see below |
| Nothing | 25 | Includes Ultima VII and Serpent Isle, which write CR0 (below) |

- **Duke Nukem 3D.** The instruction at 0180:00222BE2, where the 386
  takes exception 6, is BSWAP EAX (`0F C8`). The run retires 32 of them, all in ring 0
  protected mode, and the census's first-seen site for it is that
  address. It retires no XADD, CMPXCHG, INVD, WBINVD or INVLPG.
  The title goes on past the BSWAPs and at 21.85 s exits to the prompt
  with "VESA driver for ( 1024 * 768 ) not found/supported". The drive's
  `DUKE3D.CFG` selects `ScreenMode = 1` (VESA 2.0) at 1024 by 768, which
  the title did not find on this machine.
  - What this establishes: the 386 stop is a 486 instruction, and a CPU
    with BSWAP gets the title 0.4 simulated seconds further.
  - What it does not: that the title plays on a 486 identity. The next
    stop is the machine's, not the CPU's. No run was made with the title
    set to a VGA mode, on either level, so whether the 320 by 200 path
    also executes BSWAP is not known.
- **No other title retires a 486 instruction other than CPUID.** None of
  the other 52 has a BSWAP, XADD, CMPXCHG, INVD, WBINVD or INVLPG count.
- **Tests for a 486 only: 27 titles, and WfW 3.11.**
  - 26 toggle EFLAGS.AC with POPFD or POPF, the test that tells a 486
    from a 386 (the 27 that do, less Duke Nukem 3D). On a 386 the toggle
    fails; here it took effect.
  - 22 titles, Duke Nukem 3D among them, also toggle ID and execute
    CPUID. CHKCPU toggles ID and not AC. Hot Speed toggles ID and executes no CPUID.
  - CPUID runs one to five times a title, in real mode except for one
    execution in Grand Prix 2 (ring 0) and three in Angband (ring 3).
    Twelve titles execute it at one of two offsets, 662Bh or 664Bh, and
    five more at one of three offsets (2D71h, 2DA5h, 2DF5h) in one
    segment. That
    pattern fits a detection routine in a shared DOS extender or DPMI
    host rather than in each title, but which program owns each site was
    not established.
  - The AC and ID counts in `census.jsonl` are every change to the bit,
    not every test. Angband's 2,104 ID changes are 2,067 far jumps and 34
    IRETs, which are task and privilege switches carrying a set bit, and
    three POPFDs or POPFs. Two titles (One Must Fall 2097, Norton SI 8) have one AC
    change by INT. Only the POPF and POPFD counts are tests.
- **Ultima VII and Serpent Isle write CR0's CD and NW.** Each has one MOV
  to CR0 that changes both bits. Bochs resets with both set and the BIOS
  never changes them (no other run has the key, and every run boots the
  BIOS), so this is a clear: the titles' own memory manager loads CR0
  without them. `method.md`'s classes do not cover a CR0 write, so by the
  rule as written these two are "nothing"; the count is reported here
  instead. On a 486 identity the write would turn the cache on, which is
  what slice 2's BIOS change does anyway.
- **Seven titles ended at the prompt before their limit.** Duke Nukem
  3D is above and CACHECHK below. The other five match their committed
  386 records, so their short windows say nothing new: Star Control
  II (not enough conventional memory, [#1539](https://github.com/DeanoC/fes/issues/1539)),
  Master of Orion (its sound-setup exit), Grand Prix 2 and U.S. Navy
  Fighters (their CD checks), and CHKCPU (prints and exits)
  ([Phase 5 census](../2026-11-12-pc386-census-p5/summary.md)).
  A title that stops at its setup is not evidence that it uses nothing.
- **CACHECHK ends in "Divide error" at level 4.** It prints a 486 DX4
  line with a clock of 1044.0 MHz, then "Divide error", and is back at
  the prompt at 9.5 s. The committed records of it are kit runs on the
  386, which print its memory timing table
  ([Phase 5 census](../2026-11-12-pc386-census-p5/summary.md)); no
  level-3 oracle run of it is committed to compare against, and the cause
  was not looked for. It is a case for slice 0 to explain.
- **WfW 3.11 in 386 enhanced mode tests for a 486 and uses nothing.** It
  toggles AC four times by POPFD and retires no 486 instruction, no
  CPUID and no ID toggle in its 14 seconds, and ran to its limit. Its
  613,313 exception 6 events are 613,312 ARPLs in virtual-8086 mode,
  where ARPL is an invalid opcode on a 386 too, and one `0F FF`; both
  are 386 behaviour (the `0F FF` is the one
  [#1701](https://github.com/DeanoC/fes/issues/1701) recorded). So at
  level 4, as at level 3, this WfW launch needs nothing a 386 lacks. It
  is the 28th "tests only" row.

### Reading it

The classification is [`method.md`](method.md)'s and did not move with
the results: "requires a 486" needs a level-3 stop in a committed record
as well as a level-4 run; a level-4 run alone shows "uses it when
offered" or "tests for it". What the sweep cannot show is in `method.md`
too:

- Launch windows of 20 to 50 seconds miss in-play use. A title in
  "nothing" or "tests only" may use a 486 instruction later.
- The drives were chosen for a 386. One title in 53 is a count in a 386
  corpus, not the demand for a 486: titles that ask for a 486 were mostly
  never added.
- Behaviour on Bochs's DX4 with CPUID is not behaviour on a 486 without
  it. The 22 CPUID counts would be zero on option A.
- The oracle has no speed. "Too slow on a 386" is not measured, and it is
  the commoner reason a title of 1993–96 asks for a 486.

So the census supports this much: 486 detection is common (29 of 54
runs test for one), 486 instruction use in this corpus is one title,
and that title needs a VESA mode as well before it shows anything.

## Area and fit

### Measured

| Figure | Value | Source |
| --- | ---: | --- |
| DE10 capacity | 41,910 ALMs | 5CSEBA6 |
| DE10 working ceiling | about 38,500 ALMs (92%); stated as 38,557 in later records | [area audit](../2026-11-08-i386-area-audit/summary.md); [x87 slice 2](../2026-11-14-i386-x87-s2/summary.md) |
| DE10, pc386 as it ships (387 identity off) | 36,298 to 36,480 ALMs (86.6–87.05%), closing at +0.459 to +0.896 ns | x87 slices [2](../2026-11-14-i386-x87-s2/summary.md), [3](../2026-11-15-i386-x87-s3/summary.md), [4](../2026-11-15-i386-x87-s4/summary.md) seals |
| DE10, 387 identity on, through slice 3 | 39,682 ALMs (94.68%), 5 of 9 seeds fail | [x87 slice 3](../2026-11-15-i386-x87-s3/summary.md) |
| DE10, 387 identity on, through slice 4 | Does not fit | [x87 slice 4](../2026-11-15-i386-x87-s4/STOP-RULE-FIRED.md) |
| DE25 capacity | 46,800 ALMs | A5EB013BB23B |
| DE25, CPU alone | 29,300 ALMs needed (62.61%) | [Agilex fit](../2026-11-16-i386-agilex5-fit/summary.md) |
| DE25, machine, no 387 | 35,492 ALMs needed (75.84%) | the same |
| DE25, machine with the 387 on the pipelined backend | 40,986 to 41,122 ALMs needed (87.58–87.87%), four seeds, timing met at 61.44 MHz | [DE25 envelope](../2026-11-16-de25-x87-envelope/summary.md) |
| Decode, EX, sequencer, EX control, DTLB | 586, 803, 121, 80 and 1,273 ALMs | [area audit](../2026-11-08-i386-area-audit/summary.md), `entities.tsv` |
| Fit-to-fit noise | ±200 ALMs | the same |

The DE25 figures are analysis fits with virtual pins (651 ALMs of them
counted in the machine fits), not a board build. The DE25's transcendentals
are in flight and not in any figure above.

### Estimated

Nothing below is measured. Each line gives its basis.

| Addition | Estimate (ALMs) | Basis |
| --- | ---: | --- |
| BSWAP | 30–60 | A 32-bit four-way byte permute on EX's result, plus one decode row. EX is 803 ALMs in all |
| XADD, CMPXCHG | 80–200 | Microcode rows (in M10K) reusing XCHG's and CMP's datapath, a conditional write-back for CMPXCHG, the accumulator compare, decode rows. The sequencer is 121 ALMs |
| INVD, WBINVD, INVLPG, CPUID | 50–150 | INVD and WBINVD as a request on the existing FLUSH path or nothing; INVLPG as a full TLB flush, which the architecture permits, or one compare on tags that exist; CPUID as four constant writes |
| CR0's five bits, EFLAGS.AC (and ID), TR3, reset values | 80–200 | About 45 flops, the CR0 read and write muxes, and EFLAGS widened by one or two bits in every pipeline copy |
| WP | 20–60 | One more term in the DTLB rights check and the walker's fault condition; a handful of LUTs, but on M1's path |
| #AC | 40–120 | An alignment compare per data access at CPL 3 (address bits 1:0 against the operand size), a new fault code and vector. Also on M1's path |
| The identity gating itself | 30–80 | The decoder's #UD terms and the reset values selected by one input |
| **486SX delta, total** | **about 350–850** | The sum; the working figure below is 600 |
| 486DX shell over the SX | 100–300 | NE, vector 16, the freeze rule in place of the BUSY# hold, IGNNE#. The 387's own shell logic it replaces is of the same order (the CPU-side cost of the 387 identity was 143 ALMs in slice 2) |

The timing risk is larger than the area risk. WP and #AC add terms to the
memory stage's fault path, and the DE10 seal closes by 0.46 to 0.90 ns.

### Does it fit

Capacity is ALMs needed against the working ceiling, never the LAB row.

| | DE10 (ceiling about 38,500) | DE25 (92% is 43,056) |
| --- | --- | --- |
| 486SX | **Estimated to fit.** 36,480 + 600 is about 37,100 (88.5%); the top of the range is 37,330 (89.1%). Measured base, estimated delta | **Estimated to fit** with room: 35,492 + 600 is about 36,100 (77%). Analysis-fit base, estimated delta, and no DE25 build exists |
| 486DX | **No.** It needs the 387, which the DE10 cannot hold. Measured | **Estimated to fit** on today's unit: 41,122 + 600 + 300 is about 42,000 (89.8%), under 92%. The transcendentals will add to it by an amount that is not known yet, and there is under 1,100 ALMs between that estimate and the 92% line |
| Both identities in one bitstream | SX and 386 together cost the same as the SX alone: the delta is the 486's logic, and the 386 identity is that logic gated off | The same |

### A spike that replaces the estimate

A decode-and-register spike would turn the SX figure into a measurement
for about an hour of Quartus on chip-30x0-ai-dev: the six instructions'
decode rows and microcode, the CR0 and EFLAGS bits, WP's and #AC's fault
terms, all behind one input tied on, fitted with the
[area audit](../2026-11-08-i386-area-audit/summary.md)'s per-module method
(`quartus_x87_spike.py --hierarchy`, no flatten) for the ALM split and
with the sealer's flow for slack. It needs no working instruction
semantics, only the logic's shape, and no lockstep. It is slice 1 below.
It was not run for this record: the brief was to propose it.

## Slices

One PR per slice. A slice that changes the CPU merges only with what Phase
5's did: lockstep on the RTL and on the locked-Yosys netlist, test386.asm
to POST FFh on the 386 identity unchanged, a Quartus 17.0.2 seal meeting
every analysis, and a kit run on that seal with DOOM's `-timedemo demo3`
and the acceptance games rechecked. A timing miss stops the slice. The 486
identities default off in every package until Deano says otherwise, and
with them off the 386 identity's lockstep, test386 and seal must be
unchanged in behaviour.

0. **Oracle and lockstep at level 4.** The provisioner builds a second,
   level-4 oracle beside the level-3 one (its own tree prefix, the CPU
   model library, the model parameters); the adapter accepts both and
   exposes what the lockstep needs (the wider EFLAGS and CR0 masks; CR4
   only for option B); `machine_tb` and the census take the level as an
   option, with this record's `ac:` and `id:` keys and a key for
   unretired #UD stems. The lockstep gets a 486 scope: its #UD probes and
   the `ud-invlpg` entry become per-identity. For option A, the oracle
   patch or rule that makes CPUID #UD and ID fixed. No CPU change.
   **Gate:** level-3 lockstep, test386 and the boot unchanged; the level-4
   oracle boots FreeDOS and reruns this record's census to the same
   counts.
1. **Fit spike.** As [above](#a-spike-that-replaces-the-estimate).
   **Gate:** measured ALMs and slack for the SX delta inside pc386 on the
   DE10 flow. Deano picks the identity mechanism and the DE10 scope with
   the numbers.
2. **486SX instruction set.** The identity input; BSWAP, XADD, CMPXCHG,
   INVD, WBINVD and INVLPG; CR0's NE, WP, AM, CD and NW with WP enforced;
   EFLAGS.AC; #AC; TR3; ET hard-wired; the reset DX signature and DR6
   value; for option B, CPUID and ID. The BIOS clears CD and NW on a 486
   identity. Governor rows for the new instructions. **Excluded from the
   identity:** the cache and TLB test functions of TR3–TR7. TR3 is added
   as storage and TR4–TR7 stay storage, because the core's cache and TLB
   are not the 486's and Bochs raises #UD for MOV TRn at every level, so
   there is no oracle for them. No census run attempted a MOV TRn (it
   would be an exception 6 on the oracle, and none was counted outside
   WfW's ARPL and `0F FF`). **Gate:** lockstep
   against the level-4 oracle with directed cases for each item and
   random programs; the AC and CPUID detection routines answer 386 with
   the identity off and 486 with it on; Duke Nukem 3D gets past
   0180:00222BE2 on the RTL and on a kit and reaches what the level-4
   oracle reaches, its "VESA driver for (1024 * 768) not found/supported"
   exit at 21.85 s (that it plays is not part of this gate: see
   [demand](#demand)); the census rerun on the RTL with the identity on.
3. **486 speed.** The reach measurement per preset on a kit after Phase
   4; reference scores for a 486 from the chosen source; the frequency
   override documented as the way to 25 and 50 MHz, or named presets if
   Deano wants them (that widens the preset field). **Gate:** each 486
   preset's benchmark scores recorded against the reference, with the
   presets the core cannot reach on the DE10 marked so in the library.
   This slice does not depend on slice 2 and can run first.
4. **486DX.** After the x87 unit is complete and pc386 builds for the
   DE25. NE and vector 16, the freeze rule and IGNNE#, the 486's ESC
   rules, the governor's 486 x87 column. **Gate:** the x87 lockstep's
   387-present rule in its 486 form against the level-4 oracle; a
   directed NE = 1 case and an operating system that sets NE (Linux
   2.0.0 is the candidate; what it does on a 486 identity was not run
   for this record); the x87 acceptance titles on the DE25 kit.

### SX first or DX first

| | SX first | DX first |
| --- | --- | --- |
| Depends on | Slices 0 to 2 only. No x87 work, no DE25 build | The x87 unit complete, a DE25 pc386 build, and slices 0 to 2 anyway |
| Board | DE10 and DE25 | DE25 only |
| Unlocks | The titles that need a 486 instruction and no FPU | Those and the titles that need both |
| Risk | DE10 timing on the fault path | Everything the SX has, plus the x87 work's remaining area |
| Earliest start | Now, beside the x87 work: it touches the decoder, the control registers and the fault path, which the x87 slices do not | After #1768's transcendentals and a DE25 machine build |

The plan orders SX first because the DX contains the SX: every DX slice
needs the instruction set, and nothing in the instruction set needs the
x87. DX first only reorders the same work behind two prerequisites that
are not met.

**Against the x87 work in flight.** Slices 0, 1 and 3 touch nothing the
x87 session is editing. Slice 2 edits `fes_i386_decode.sv` and
`fes_i386_cpu.sv`, which the x87 slices also edit (the ESC decode and the
unit's instance), so it should start from main after the transcendentals
land or be kept to files they do not touch until then. Slice 4 waits for
the tracker's slices 5 and 6.

## Open questions for Deano

These are Deano's decisions; the plan does not settle them.

1. **Which 486.** A, the first 486 with no CPUID, or B, a CPUID-capable
   part, and for B which revision (with or without VME and CR4; SMM is
   assumed out). A is smaller and needs an oracle rule; B is what Bochs
   models.
2. **Where the identity lives.** A reset-sampled setting in `fes.pc386`
   chosen per title, a module parameter with one package per identity, or
   a separate core. And whether the SX and DX are one setting plus the x87
   setting (486 with `x87_present`) or two named identities.
3. **DE10 and DE25 scope.** Whether the DE10 gets a 486SX at all, given
   it can never have a DX, or whether 486 identities are DE25-only like
   the 387. A DE10 486SX is the only 486 that can ship before a DE25
   machine build exists.
4. **Which presets.** Keep 486DX-33 and 486DX2-66 and reach 25 and 50 MHz
   by the frequency override, or add named presets (486SX-25, 486DX-50),
   which widens the 3-bit preset field in the RTL, the ABI and FogCast.
   And whether a 486 preset should imply the 486 identity, so that the two
   cannot be set apart by accident.
5. **What a 486 preset is measured against.** Real 486 scores, an 86Box
   486 as the 386 presets are trying, or the tables alone. And whether the
   cache's misses should be modelled or left out as now.
6. **Behaviour no document settles.** For the 386-microcode behaviours
   (DR6, RF on #DB, ICEBP, UMOV, the undefined flags) and for a 486SX's
   ESC with EM clear: keep the 386's, follow Bochs's level 4, or measure a
   real 486. If a 486 machine can run a capture program, these and the
   FERR# timing cases are what to capture.
7. **Whether the demand justifies it.** The design doc's Phase 7 rule is
   "only with a measured need". The measured need is
   [the census](#demand)'s; the corpus it was measured on was chosen for a
   386.
8. **Self-modifying code on a 486 identity.** `coherent`, which is
   stricter than a 486 and is what every package uses, or a `queue32`
   profile that reproduces the 486's stale prefetch.
9. **VGA bus time under 486 presets.** Keep charging ISA VGA time, or
   give 486 presets local-bus VGA time as the boards of 1993 had.

## Sources

- Intel486 Microprocessor Family Programmer's Reference Manual (1992):
  <https://www.mirrorservice.org/sites/www.bitsavers.org/components/intel/80486/1992_Intel_486_Programmers_Reference_Manual.pdf>
- Intel486 Microprocessors and Related Products, 241731-002 (January
  1995), with AP-485, "Intel Processor Identification with the CPUID
  Instruction" (December 1994):
  <https://www.mirrorservice.org/sites/www.bitsavers.org/components/intel/80486/241731-002_Intel486_Microprocessors_and_Related_Products_Jan95.pdf>
- i486 Microprocessor data sheet, 240440-002 (November 1989):
  <https://www.mirrorservice.org/sites/www.bitsavers.org/components/intel/80486/240440-002_i486_Microprocessor_Nov89.pdf>
- Bochs at the oracle's pinned commit `30cfd566ecc8`: `cpu/init.cc`,
  `cpu/crregs.cc`, `cpu/flag_ctrl_pro.cc`, `cpu/paging.cc`,
  `cpu/decoder/ia_opcodes.def`, `cpu/cpudb/intel/i486dx4.cc`,
  `cpu/fpu/ferr.cc`.
- SingleStepTests: <https://github.com/SingleStepTests>
- reenigne's x86 microcode disassemblies, as cited by the RTL.
- The scans' text layer misreads digits and letters (DX as "OX", for
  instance), so no value here was taken from a table cell without reading
  its neighbours; the clock counts quoted are the ones that were legible.
  No extract of the documents is committed.
