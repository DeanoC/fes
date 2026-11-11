# i386 near CALL past CS's limit: push first, 11 November 2026

This record covers [#1510](https://github.com/DeanoC/fes/issues/1510) and
item 3 of [#1514](https://github.com/DeanoC/fes/issues/1514) (a comment).

**Result:**
- **What changed.** A near CALL (CALL rel, CALL r/m by register or memory)
  whose target is past CS's limit now writes its return address first, then
  raises #GP(0) with ESP as it was. Before, the RTL raised the #GP(0) at the
  push and wrote nothing.
- **Directed cases.** Three new CPL-3 cases (275 → 278 protected-mode
  cases). Main's RTL fails exactly those three; a mutant that lets the push
  move ESP fails those three and six others ([mutants.txt](mutants.txt)).
- **Lockstep.** Everything passes on the RTL and on the netlist from the i386
  lock's Yosys, with identical trace hashes, except `random#1010#118`, the
  known failure #1663, which fails as #1663 records.
- **test386** on the RTL reaches POST 22 and stops at `int 0x28`, a delivery
  through a task gate (not built), as on main.
- **Seal.** Quartus 17.0.2 seed 1 from a clean checkout of `e054d6ecc`
  (this branch with main and #1666 merged in) meets every analysis: setup
  +0.803 ns, hold +0.072 ns. It uses 36,118 ALMs (main 36,139) and 268
  M10K (main 268).
- **#1666** merged first. Main is merged in, M1 keeps this branch's side,
  #1666's `dfl` term for CALL rel's push is gone, and three paging cases
  are added (69 → 72).

## Why push first

Deano's rule (10 November): where the RTL and Bochs differ, follow what the
original 386 did. Intel's documents give no order for a near CALL:
- The 386 PRM's CALL pseudocode has no limit check, only "#GP(0) if
  procedure location is beyond the code segment limits".
- SDM vol. 3 §22.31.3 says the Intel486 pushes CS and EIP before a branch
  fault is resolved during far calls, and the P6 family branches first. It
  does not name the Intel386 or near calls.

The die-extracted 386 microcode
([reenigne/x86_microcode](https://github.com/reenigne/x86_microcode)
`80386/microcode_notes.txt`) writes the slot before it forms the target
through CS's descriptor:

| Form | Write (`OPR_W = EIP; WR`) | Target through CS (`DESCOD`) | ESP (`eSP = SIGMA`) |
| --- | --- | --- | --- |
| CALL rel (`CALL cw`) | `075` | `076` | `077` |
| CALL r/m by register | `079` | `07A` | `07B` |
| CALL r/m by memory | `07E` | `080` | `07F` |

The memory form writes ESP at `07F`, the delay slot before its target check
at `080`, so the microcode does not settle whether a #GP(0) there keeps that
ESP. **Deano's decision (11 November):** ESP stays as it was at the #GP(0)
for every near CALL form, CALL r/m's memory form included, as Bochs does.
Bochs 3.1 (the pinned oracle) pushes first in
`CALL_Jw`, `CALL_Jd`, `CALL_EwR` and `CALL_EdR` (`RSP_SPECULATIVE`, the push,
then the check). Its rollback restores ESP but not the written bytes, so the
RTL now agrees with the oracle, and no divergence entry is needed.

**This rests on the microcode, not a silicon test.** SingleStepTests' 386EX
corpus has no near CALL past a CS limit (66 E8: no exceptions; FF /2's #GPs
are 16-bit operand cases).

## The change

- **CALL rel.** M1 still checks the push's target (`tchk`). A target past
  the limit no longer faults the push: it sets `cpast`. CALL rel's jump
  already checks the same target and raises #GP(0) at EX.
- **CALL r/m.** D's check of T1 (`far_chk`) no longer makes the push fault at
  M1 (`cs_late`), nor stops D after it. It sets `cpast`. The jump to T1
  checks it at EX (`b_ind`), as JMP r/m's does.
- **WB** writes no ESP for a `cpast` push. Its store commits, after its own
  #SS(0) or #GP(0) and, under paging, its page's #PF.
- **Comment (#1514 item 3).** A fetch fault's EIP is the faulting
  instruction's first byte, not limit+1, and the delivery's entry is not
  checked against CS's limit. Every real-mode delivery redirect restarts at
  the low 16 bits of EIP.

**#1666** (#1646, merged first as `13d217ca9`) made CALL rel's past-limit
push translate under paging (`dfl`) so its #PF came first. After this
change the push has no fault, so it translates as any store does, and #PF
comes first without that term. Main is merged into this branch at
`e054d6ecc`:
- **Resolution.** One conflict in the RTL, M1's fault and `dfl`: this
  branch's side, `dfl = !cs_late && dfault != NONE` (no `m1_tpast` term),
  which deletes #1666's term for CALL rel's push. Its comments in
  `fes_i386_pkg.sv` and the RTL README now name only the st2 store's
  segment check. #1666's XCHG m changes and its M2 replay for `dfl` uops
  merge cleanly.
- **Three paging cases added** (69 → 72), at CPL 3 with ESP 10008h and the
  target 20000h past CS 53h's limit:
  - `pg-r3-call-past-limit-ok`: `CALL rel32`, the push's page present and
    writable (frame 20000h): the slot written at 20004h, then #GP(0).
  - `pg-r3-call-rm-past-limit-np`: `CALL EBX`, the page not present: #PF,
    error 6, CR2 10004h.
  - `pg-r3-call-rm-past-limit-ok`: `CALL EBX`, the page present: the slot
    written, then #GP(0).
- **Merged RTL:** directed 77, protected-mode 278 and paging 72 cases, all
  pass, #1666's `pg-r3-call-past-limit-np`, `-ro` and XCHG cases included
  (see Lockstep).
- **#1666's RTL alone** with that testbench (a trial before #1666 merged)
  fails the three new paging
  cases and the three CPL-3 protected-mode cases: no slot written, and
  CALL EBX's push not present raises #GP(0) instead of #PF.

## Directed cases

`FES_I386_MODE=pmode`, now 278 cases. At CPL 3 (CS 73h, 16-bit, limit
FFFFh; SS 6Bh, SP 0D00h), each to a target past the limit, #GP(0) on the
TSS's stack:
- `r3-call-rel32-past`: `CALL rel32` to 20000h.
- `r3-call-rm32-past`: `MOV EBX, 10000h; CALL EBX`.
- `r3-call-m32-past`: `PUSH 10000h; MOV BP, SP; CALL DWORD [BP]`.

The slot lies on the ring-3 stack, outside the #GP's frame, so the
lockstep's comparison of the bytes each side wrote checks it. At CPL 0 the
frame covers the slot.

The random generator's real-gp exit (#1651) also draws CALL rel16, CALL
rel32, CALL BX and CALL EBX besides the JMPs. It picks CALL or JMP from a
side stream seeded from a copy of the seed's stream, so it takes no draw
from that stream: every seed gives main's programs, with CALL in place of
some of those JMPs.

## Lockstep

`scripts/test_fes_i386_lockstep.py --jobs 16` at the merge `e054d6ecc`, on the RTL and
on the netlist from the i386 lock's Yosys (`238eac9a4`, slot `44e245aa…`),
with the lock's Verilator 5.054:

| Part | RTL | Netlist |
| --- | --- | --- |
| SingleStepTests corpus | 1,758,699 of 1,758,699 | same |
| Random programs | 32,000 of 32,000 (7,977,455 retirements) | same |
| Protected-mode programs | 7,999 of 8,000 (432,655 retirements); `random#1010#118` fails, the known #1663 | same |
| Unbuilt encodings | 49,760, each stopped | same |
| Directed cases | 77 of 77 | same |
| Unreal, protected-mode, paging, no-387 and memory cases | 7, 278, 72, 1,933 and 27, all pass | same |

Every trace hash is the same on the RTL and the netlist. The full runs are
[lockstep-report.json](lockstep-report.json) (RTL) and
[lockstep-netlist-report.json](lockstep-netlist-report.json).

**Known failure: `random#1010#118` (#1663).** It fails on the RTL and the
netlist exactly as #1663 records: at 42da:356f, Bochs nests a #PF (vector
0Eh, CR2 FFEAF29Fh, CPL 0, CS 80h) in the delivery of INTR vector 8 to CS
2Fh, while the RTL's delivery ends at the handler. An earlier draft of this
PR drew the CALL choice from the seed's stream, which changed the programs
after it and hid this case; Codex flagged it, and the side stream restores
it. Programs under a CS limit other than 64 KiB and 4 GiB (`cs_limit`):
pm-gp 606, pm-retired 19,391, real-gp 390, real-retired 1,331.

## test386

`scripts/test_fes_i386_test386.py --cpu rtl` at `e054d6ecc`: POST 00-09, 20, 21, 22, and
the stop at `int 0x28` (E0A18h, a task gate) with 799,276 retirements and
no errors, as on main ([test386-rtl-report.json](test386-rtl-report.json)).

## Seal

Quartus 17.0.2 Lite, seed 1, from a clean checkout of the merge `e054d6ecc`
([seal-summary.json](seal-summary.json)):
- **Package.** `6b4a917e…`, build `b7a48556…`, `core.rbf` `f3b85bf4…`.
- **Analyses.** All met: setup +0.803 ns, hold +0.072 ns, recovery +5.675
  ns, removal +0.271 ns, minimum pulse width +1.122 ns.
- **Against main.** Main's seal is #1666's (`2026-11-11-i386-pf-order`):
  setup +0.673 ns, hold +0.085 ns.

  | Resource | This seal | Main (#1666) |
  | --- | ---: | ---: |
  | ALMs | 36,118 | 36,139 |
  | Registers | 32,429 | 32,531 |
  | M10K blocks | 268 | 268 |
  | DSP blocks | 38 | 38 |

The pre-merge seal of `5b1a87678` (setup +0.657 ns, hold +0.080 ns, 36,048
ALMs) is superseded by this one. No kit run: the change is a fault's order,
which the lockstep covers, and the seal shows no significant change in
timing or area.

## Commands

From `sources/misteross`:

```sh
python3 scripts/test_fes_i386_lockstep.py --cache-root "$CACHE_ROOT" --jobs 16
python3 scripts/test_fes_i386_lockstep.py --cache-root "$CACHE_ROOT" --jobs 16 --yosys <i386 lock's yosys 238eac9a4>
python3 scripts/test_fes_i386_test386.py --cpu rtl --cache-root "$CACHE_ROOT"
python3 scripts/build_fes_pc386_quartus.py --root "$PWD" --cache-root "$CACHE_ROOT" --seed 1
```

`CACHE_ROOT=/home/deano/fes/out/cache/misteross-toolchains`;
`QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0/quartus`.
