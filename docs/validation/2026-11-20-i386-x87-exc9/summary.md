# i386 x87: exception 9 and an operand across a page or a limit, 20 November 2026

Slice 2 of the [x87 plan](../2026-11-12-i386-x87-plan/summary.md#slices)
named exception 9, coprocessor segment overrun, as in scope and left it
unbuilt ([slice 2's record](../2026-11-14-i386-x87-s2/summary.md#what-is-left)).
This is that piece ([#2020](https://github.com/DeanoC/fes/issues/2020)): a
directed lockstep case for an x87 operand that straddles a page whose second
half is not present, the CPU's behaviour for it following the 386 and 387,
and the decision on what the unit keeps when an operand faults part way.

## The answer, which is not the one the brief expected

**A page straddle does not raise exception 9 on a 386, and nothing moves
before it faults.** The 386 checks both ends of the operand before any of it
reaches the 387, so the fault is an ordinary, restartable #PF (or #GP or #SS
past a limit), the 387 is untouched, and the instruction runs again once the
handler has mapped the page. Slice 2's record said the opposite ("the
transfer has begun, so the 387 is partly updated and the fault cannot be
restarted"); that sentence is wrong, and [the correction](#the-correction-to-slice-2s-record)
is below.

**Exception 9 itself is not raised by this RTL**, and the 386's microcode
has no path that raises it either: [the sources](#what-the-sources-say)
below.

**The straddle case found a real defect,** which this PR fixes: the RTL's
m64 and m80 stores wrote their first dword into the present page and then
faulted on the second, leaving a partial store that neither the 386 nor Bochs
makes. Every load, and every store moved in one access, was already free of
it, and no faulting attempt ever changed the unit.

## What the sources say

**The 386 Programmer's Reference Manual** (MIT's HTML copy of the 1986
edition, sections 9.8.9 and 11.1.5.2):

- 9.8.9: "This exception is raised in protected mode if the 80386 detects a
  page or segment violation while transferring the middle portion of a
  coprocessor operand to the NPX. This exception is avoidable."
- 11.1.5.2 limits it to an operand that **wraps an addressing limit**
  ("0FFFFH for small segments, 0FFFFFFFFH for big segments, zero for
  expand-down segments"), whose "first byte and the last byte ... (considering
  wrap-around) are at addresses located in the segment and in present and
  accessible pages", and which "spans inaccessible addresses" in between. "The
  failing numerics instruction is not restartable." Its remedy ("aligning all
  segments on page boundaries or ... not starting them within 108 bytes of the
  start or end of a page") is about that wrap, not about straddles.

So the manual itself says the ends are checked before the middle moves, and
a plain straddle, whose two ends cover both its pages, cannot reach exception
9.

**The 386's own microcode** (reenigne/x86_microcode, the May 2026 die
extraction, `80386/microcode_notes.txt`, `fields.txt` and `small_pla.txt`)
shows how:

- **The probes.** `fields.txt` names bus operation 0Ch `CR B`, "Perform
  access/limit/paging checks for reads but don't actually do a read", and
  08h `CW B`, the same for writes. `FPU_LOAD_CORE` (4FC-4FE), which an m80
  load, an m16 load and FLDCW reach, does `CR B` at `IND` and then at the
  operand's first byte; `FPU_STORE_CORE` (562-564), which every store
  reaches, does `CW B` the same way. `IND` is set at each form's entry to the
  operand's last byte: `IND_DELTA = 9` at 4D5 (m80 load) and 53A (m80
  store), 7 at 4DD and 542, 3 at 4D9 and 53E, 1 at 4E2 and 546. All of it
  comes before the opcode reaches the 387's port F8h.
- **The m32 and m64 loads do not probe.** `FPU_LD3264_387` (51C-534) sends
  the opcode, then reads the dword at the operand and the one at 4 past it,
  upwards, each its own access. A fault on the second leaves the 387 with
  an opcode and one dword it never acts on; the restarted instruction sends
  the opcode again.
- **The x87 pointers come after the transfer.** `CSOPCD`, `FSVeIP` and
  `OPROFF`, the instruction and data pointers and the opcode that FNSTENV
  stores, are written at 502-50C, 532-534 and 56B-575, each after its
  form's transfer. A fault leaves the last completed ESC's.
- **No vector 9.** The fault entries the small PLA reaches are `PAGE_FAULT`
  (8E9, vector 0Eh) and `GENERAL_FAULT5`/`ACCESS_VIOLATI` (857: #GP, #SS,
  #NP, #TS by their tests), with #DF, the triple fault, the divide error and
  the rest; `FAULT_ERR_CODE` (874) forms 9 plus a base for vectors 10 to
  13, and no entry found uses a base of 0, nor does any routine set
  `SIGMA = 9` before `FAULT`. A page or limit fault in the middle of a
  transfer would vector as a #PF or a #GP. This is an annotated extraction,
  so it is stated as "no path found", not as proof.
- **The probe's add is 32-bit.** `BITS32` on each `IND += IND_DELTA` line
  (4D5, 4D9, 4DD, 4E2, 53A, 53E, 542, 546) makes the add a 32-bit one, so a
  16-bit operand at FFF8h has its probe at 10001h, past a small segment's
  limit, and faults before anything moves. The notes mark what `BITS32` does
  with a question in places (at 3FE they say it "changes ALU but not WORDSZ
  or bus"), so this is the reading most consistent with them, not a settled
  fact: [open](#what-is-left).

**Bochs** (the pinned 3.1 oracle) was measured, not read, through the
lockstep: [the differences](#the-differences-from-bochs).

## What the RTL does now

| Form | Before any byte moves | Then | CR2 when the second page faults |
| --- | --- | --- | --- |
| FLD m80, FBLD | a one-byte probe of the last byte | dwords at 0 and 4, word at 8 | the last byte (the 386's `CR B`) |
| FST, FSTP, FISTP m64 | a one-byte probe of the last byte, checked as a write | dwords at 0 and 4 | the last byte (`CW B`) |
| FSTP m80, FBSTP | a one-byte probe of the last byte, checked as a write | dwords at 0 and 4, word at 8 | the last byte (`CW B`) |
| FLD, FILD m64, the m64 arithmetic forms | nothing | dwords at 0 and 4 | the second page's first byte, as the 386's upward reads |
| m16 and m32 forms | the one access | | the second page's first byte |

- **The probe** is the decode's first uop for those forms (`fes_i386_decode`),
  a byte load whose data nothing uses (`nodata`) but which translates (not
  `nx`) as the access it stands for, a write's for a store (`wx`)
  (`fes_i386_cpu`, "x87 operand probes"). It moves nothing to the unit: its
  commit is gated off the unit's `wb`. The parts then move upwards from the
  operand (`ucode.toml`: `xld8`, `xld10`, `fst8`, `fst10`; FBSTP now shares
  `fst10`, and `fbst10` is deleted), so the first part checks the first byte
  before anything is written. The no-387 forms keep their own routines and
  their own order.
- **Every part lies between the two checked bytes**, so once the probe and
  the first part pass, nothing after them can fault: both pages are
  translated, and with no wrap every offset between two in-limit offsets is
  in the limit. The only way past that is a wrap, which the next point
  closes.
- **A 16-bit probe that wraps is checked unwrapped** (`m1_xwrap`): an
  m80's or m64 store's probe whose 16-bit offset wrapped past FFFFh is
  checked at 10000h plus that offset, as the 386's 32-bit add of it would
  put it. Past the limit (FFFFh, or an expand-down segment's upper bound)
  it is #GP(0), or #SS(0) for an SS operand, before a byte moves. Under a
  larger limit it is in it, and the parts wrap by the address size, as
  Bochs's do, so the wrap completes on both. The first cut faulted every
  wrapped probe whatever the limit; review found it, and
  `pg-x87-wrap-biglimit` is the case for it.
- **A probe walks the second page first.** With the first page absent and
  the second present, the probe sets the second page's A bits, and D for a
  store, before the first part faults on the first page: the 386's order,
  where Bochs's translation of the first page touches nothing of the
  second. `pg-x87-first-absent-fresh` checks the PTE the handler sees.
- **A held store** (an unmasked exception other than precision, #1872) makes
  no access, and now neither does its probe: no limit check, no translation,
  as #1998 set it out. The 386 makes its probes before the 387 declines the
  transfer, so there a held store to an absent page would fault; that is
  [left open](#what-is-left).
- **The x87 pointers and the opcode** were already written by the
  instruction's last uop as it retires (`x87p`), so a fault leaves them: the
  386's order.
- **The unit's interface is unchanged.** It takes parts by index, not in
  order, and its loads and stores already commit at the instruction's end;
  the order change needs nothing of it. The unit bench now presents parts in
  the CPU's new order, with the probe.

## The directed cases

In the paging harness (`lockstep_tb.cpp`'s `run_paging_cases`, protected
mode, 32-bit flat code, paging on) with the 387 fitted (CR0.ET, the
oracle's AT coprocessor), every retirement compared with Bochs, the 387's
whole state included.

- **Four programs of twelve forms each**: FLD m80, FSTP m80, FLD m64, FST
  m64, FSTP m64, FILD m64, FISTP m64, FLD m32, FADD m64, FSTP m32, FILD m16
  and FISTP m16. Before each form the program unmaps the page or pages and
  reloads CR3; each form faults into a #PF handler that logs CR2, the error
  code and a marker dword, maps the page CR2 names and returns, so the ESC
  runs again, completes, and is compared. The bench checks the log against
  the 386's rule on the RTL, apart from Bochs: every CR2, every error code,
  and that every store's marker survived the fault.
  - `pg-x87-straddle`: the second page absent, the operand split after its
    first dword (m80, m64), inside it (m32) or after its first byte (m16).
  - `pg-x87-straddle-mid`: the same with the split inside the first dword,
    and an m80 at 10FF8h with only its top word on the second page.
  - `pg-x87-first-absent`: the first page absent: every form faults on the
    operand's first byte.
  - `pg-x87-both-absent`: both absent: a probing form faults on its last
    byte first, then on its first; every form faults twice.
- **Five limit cases**, DS a 16-bit data segment of limit FFFFh, each ending
  in #GP(0) with nothing moved: FSTP m64 at FFFAh and FLD m80 and FSTP m80 at
  FFF8h under a 16-bit address size, FSTP m80 at 0000FFF8h and FST m64 at
  0000FFFCh under a 32-bit one.
- **`pg-x87-wrap-biglimit`**: FST m64 at FFFAh and FSTP and FLD m80 at FFF8h
  under a 16-bit address size with DS's 4 GiB limit, which complete, the
  m80's word at 0000h, in lockstep with Bochs.
- **`pg-x87-first-absent-fresh`**: FSTP m64 across an absent first page and
  a second page whose PTE has neither A nor D; the handler logs that PTE as
  the fault leaves it, A and D set.

**The first nine fail on main's RTL**, seven of them (e8f922ab6, the new
bench and oracle adapter on main's CPU:
[main-negative-control.txt](main-negative-control.txt)), which was measured
before the last two cases were added: partial stores (the bench's write comparison, and the markers), CR2s,
and the 16-bit wrap stores. `pg-x87-first-absent` passes there, because
main's RTL loads downwards and stores upwards and either way the first page
faults first with nothing written, and so does `pg-x87-limit32-fstp80`,
where main's RTL and Bochs both write the eight bytes below the limit and
then fault: that case is now one of the two the partial-store rule takes.

## The differences from Bochs

Four, in three `[[lockstep]]` entries of `divergences.toml`, each taken by
a narrow rule whose count the runner fixes (`X87_PAGING_RULES`), so a rule
that stops applying, or starts applying more widely, fails the run. Two of
the counts were predicted before the first run, the probe rule's 15 and the
partial rule's 2; the other two were measured and then fixed, and that is
all they are: the pointer rule's 34 (every one a store's) and the A/D
rule's 7.

| Entry | Bochs | The RTL, as the 386 | Rule, count |
| --- | --- | --- | --- |
| `x87-operand-probe` | a #PF on the second page names its first byte, or the operand's when both fault; an m80 store writes a qword, then faults on its word; with the first page absent, nothing of the second is touched | names the last byte; stores nothing; sets the second page's A and D | `x87_probe_rule` 15, `x87_partial_rule` 2, `x87_ad_rule` 7 |
| `x87-pointers-on-fault` | writes the x87 pointers and the opcode before a store's access (a load's after it, as the 386) | leaves them | `x87_fault_rule` 34 |
| `x87-operand-wrap` (`follow = "open"`) | an m80 at FFF8h under a 16-bit address size, limit FFFFh, wraps its word to 0000h and completes | #GP(0) | the RTL alone, 2 cases |

`x87_probe_rule` takes the RTL's CR2 only where it is the operand's last
byte and Bochs's is the second page's first byte, or, both pages absent,
the operand's first: at most the operand's length apart. `x87_partial_rule`
takes the RTL's bytes back only where Bochs wrote an m80 store's first
eight in one access before its fault and the RTL none. `x87_fault_rule`
gives the oracle back its pointers and opcode only where the RTL kept its
own; for a load, where Bochs keeps them too, the lockstep's 387 comparison
is the check that the RTL did. `x87_ad_rule` gives the oracle only the A
and D bits of the second page's PDE and PTE, and only where the first page
faulted on a probing form. The oracle adapter gained `set_x87_opcode` for it.

## Decisions, and where they differ from the 386

- **Single-access forms keep their CR2.** An m32 store across a page names
  its last byte on a 386 (`CW B` at 3 past it); the RTL's one access names
  the second page's first byte, as Bochs's does. Following the 386 would put
  a probe on FST m32 and FIST, the stores games run most (Quake's FSTP m32);
  the difference is a CR2 within the same page, which no handler reads apart
  from its page. Not followed, and recorded here.
- **The environment and save forms are not probed.** FNSTENV, FNSAVE,
  FLDENV and FRSTOR store and load field by field in Bochs's order, as they
  did; the 386 probes them too (`CW B` and `CR B` at 400-402 and 474-476),
  but the notes put that probe two bytes short of the image's end ("Mostly
  harmless bug?"), so even the 386 can fault part way there. Left for a real
  386 to settle.
- **No exception 9.** See [what the sources say](#what-the-sources-say).

## The correction to slice 2's record

Slice 2's record, under "What is left", says exception 9 "is raised when a
coprocessor operand's middle part crosses into a not-present or protected
page or segment: the transfer has begun, so the 387 is partly updated and
the fault cannot be restarted". The microcode it then cites (`IND = EA +
size - 1` before the transfer) is the reason that does not happen for a
page or segment straddle: the transfer has not begun when the fault comes.
Its earlier paragraph's "a multi-part operand that faults part way leaves
this unit partly updated, as the coprocessor bus's transfers leave a real
387" is likewise not what the 386 does. That record is left as it was
written; this one supersedes those two sentences.

## Gates

Every gate below is of `1b84f6edd`, the commit with the last RTL and bench
change; the commits after it change this record only, so the design, and
the seal's BUILD_ID, are the head's. Each lockstep report records that
revision with no tracked changes. The first round of gates, of `9ac34ef67`
(seal `b1427783`, Kit B run `k2020a`, all passing), is superseded: review
found the wrap check above, and everything was run again.

| Gate | Where | Result |
| --- | --- | --- |
| RTL lockstep, DE10 backend | Powerboat | pass: paging 169 (the 11 new), x87 125, no-387 1,933, directed 84, pmode 726, BCD 3, transcendental 10, corpus 1,758,699 tests of 941 files, random 32,000, pmode 8,000, tasks 4,000, debug 4,000, undefined 4,000 programs |
| RTL lockstep, DE25 backend | Powerboat | pass, the same suites, BCD 28 (FBLD 112, FBSTP 127, through the probe), transcendental 181 at `--transcendental-ulp-oracle 2097152` |
| Netlist lockstep, DE10 | chip-30x0-ai-dev | pass, the same; netlist `5305714c`; the seal's `--lockstep-report` |
| Netlist lockstep, DE25 | chip-30x0-ai-dev | pass, the same as the DE25 RTL run; netlist `04e96e66` |
| The rules' counts | all four | `x87_probe_rule` 15, `x87_fault_rule` 34, `x87_partial_rule` 2, `x87_ad_rule` 7, as the runner fixes them |
| Unit bench, DE10 | Powerboat, of `9ac34ef67` | 934 pass, 0 fail, in the CPU's new uop order (the unit and its bench are unchanged since) |
| Unit bench, DE25 | Powerboat, of `9ac34ef67` | 18,337 pass, 0 fail, at `--transcendental-ulp-host 4` |
| test386, `--cpu rtl` | Powerboat | POST FFh, the ROM halted at 00D0:FFB9 after 79,640,765 retirements, no errors |
| pc386 seal | chip-30x0-ai-dev, Quartus 17.0.2 Lite, seed 1, CUDA toolchain slot | package `eef36703`, BUILD_ID `f9d258aa`; worst setup slack +0.432 ns, hold +0.101 ns, recovery +6.852 ns, removal +1.254 ns; 36,407 of 41,910 ALMs |
| Kit B, run `k2020b` | Powerboat | pass: the sealed package from the library, FreeDOS prompt in 0.5 s, Wolfenstein 3D drawn and walked; core-only, nothing installed, kit restored to its baseline (image `179affbb`, bundle `c9ae79e2`), idle and lease-free, logged in `kitB-STATUS.md` ([result](kit-b-k2020b-result.json)) |

The kit run checks the sealed package with the `i386dx+387` identity off,
which is how every package ships until the x87 phase's slice 7; the 387's
behaviour is the lockstep's and the unit bench's to check, as in the
slices before.

## What is left

- **A real 386 and 387.** Three things here rest on the die extraction's
  annotations and the manual: the probe's 32-bit add (the 16-bit wrap
  faults), the absence of a vector 9 path, and the 386's CR2 for a probe.
  A real machine would settle all three, and the held-store probe below.
- **A held store to a bad address.** The 386 probes before the 387 declines
  the transfer, so it faults; the RTL and Bochs make no access. No title in
  any census stores with an exception unmasked to an absent page.
- **The environment and save forms' probes**, above.

## Reproducing

From `sources/misteross`, with the i386 lock's toolchain slot:

```bash
python3 scripts/test_fes_i386_lockstep.py --download --cache-root /abs/fes-toolchain-cache --backend de10
```

```bash
python3 scripts/test_fes_i386_lockstep.py --download --cache-root /abs/fes-toolchain-cache --backend de25
```

One case of the testbench the runner builds, for example:

```bash
FES_I386_MODE=paging FES_I386_CASE=pg-x87-straddle FES_I386_LATENCY=1 FES_I386_DIVERGENCES=build/sim/fes-i386-lockstep/divergences.txt build/sim/fes-i386-lockstep/rtl/Vfes_i386_cpu
```
