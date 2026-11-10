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
  lock's Yosys, with identical trace hashes.
- **test386** on the RTL reaches POST 22 and stops at `int 0x28`, a delivery
  through a task gate (not built), as on main.
- **Seal.** Quartus 17.0.2 seed 1 from a clean checkout of `5b1a87678`
  meets every analysis: setup +0.657 ns, hold +0.080 ns. It uses 36,048
  ALMs (main 36,398, −350) and 268 M10K (main 268).

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

The memory form writes ESP at `07F`, the delay slot before `080`. The notes do
not say whether a #GP(0) there keeps that ESP. The RTL leaves ESP as it was
for every form, as Bochs does. Bochs 3.1 (the pinned oracle) pushes first in
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

**#1666** (#1646, open) makes CALL rel's past-limit push translate under
paging (`dfl`) so its #PF comes first. After this change the push has no
fault, so it translates as any store does, and #PF comes first without that
term. The paging cases need #1666's DPL-3 paging descriptors, so they land
with whichever of the two PRs merges second. A trial merge of #1666's head
`1a8b973a9` into this branch's `a5467f777` (not pushed):
- **Resolution.** One conflict in the RTL, M1's fault and `dfl`: this
  branch's side, `dfl = !cs_late && dfault != NONE` (no `m1_tpast` term).
  #1666's XCHG m changes and its M2 replay for `dfl` uops merge cleanly.
- **Three paging cases added** (69 → 72), at CPL 3 with ESP 10008h and the
  target 20000h past CS 53h's limit:
  - `pg-r3-call-past-limit-ok`: `CALL rel32`, the push's page present and
    writable (frame 20000h): the slot written at 20004h, then #GP(0).
  - `pg-r3-call-rm-past-limit-np`: `CALL EBX`, the page not present: #PF,
    error 6, CR2 10004h.
  - `pg-r3-call-rm-past-limit-ok`: `CALL EBX`, the page present: the slot
    written, then #GP(0).
- **Merged RTL:** directed 77, protected-mode 278 and paging 72 cases, all
  pass, #1666's `pg-r3-call-past-limit-np`, `-ro` and XCHG cases included.
- **#1666's RTL alone** with that testbench fails the three new paging
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
rel32, CALL BX and CALL EBX besides the JMPs.

## Lockstep

`scripts/test_fes_i386_lockstep.py --jobs 16` at `5b1a87678`, on the RTL and
on the netlist from the i386 lock's Yosys (`238eac9a4`, slot `44e245aa…`),
with the lock's Verilator 5.054:

| Part | RTL | Netlist |
| --- | --- | --- |
| SingleStepTests corpus | 1,758,699 of 1,758,699 | same |
| Random programs | 32,000 of 32,000 (7,965,646 retirements) | same |
| Protected-mode programs | 8,000 of 8,000 (425,221 retirements) | same |
| Unbuilt encodings | 49,760, each stopped | same |
| Directed cases | 77 of 77 | same |
| Unreal, protected-mode, paging, no-387 and memory cases | 7, 278, 64, 1,933 and 27, all pass | same |

Every trace hash is the same on the RTL and the netlist.
[lockstep-netlist-report.json](lockstep-netlist-report.json) is the
netlist's full run. The RTL's full run,
[lockstep-report.json](lockstep-report.json), ran its corpus part on a
partial local corpus (1 file, 100 tests). The full corpus then ran alone on the same
RTL: [lockstep-corpus-report.json](lockstep-corpus-report.json).

`random#1010#118` (#1663), the known failure on main, does not fail here.
Not checked further; presumably the real-gp exit's new CALL draw changes
the programs drawn after it from that seed. Programs under a CS
limit other than 64 KiB and 4 GiB (`cs_limit`): pm-gp 615, pm-retired
19,730, real-gp 405, real-retired 1,195.

## test386

`scripts/test_fes_i386_test386.py --cpu rtl`: POST 00-09, 20, 21, 22, and
the stop at `int 0x28` (E0A18h, a task gate) with 799,276 retirements and
no errors, as on main ([test386-rtl-report.json](test386-rtl-report.json)).

## Seal

Quartus 17.0.2 Lite, seed 1, from a clean checkout of `5b1a87678`
([seal-summary.json](seal-summary.json)):
- **Package.** `f8bfae51…`, build `d1b9654e…`, `core.rbf` `bf6415dc…`.
- **Analyses.** All met: setup +0.657 ns, hold +0.080 ns, recovery +7.188
  ns, removal +0.395 ns, minimum pulse width +1.122 ns. Clocks: system
  61.44 MHz, pixel 74.27 MHz, audio 12.29 MHz.
- **Against main.** Main's seal is #1645's (`2026-11-10-i386-phase5-v86`):
  setup +0.841 ns, hold +0.082 ns.

  | Resource | This seal | Main (#1645) |
  | --- | ---: | ---: |
  | ALMs | 36,048 | 36,398 |
  | Registers | 32,561 | 32,797 |
  | M10K blocks | 268 | 268 |
  | DSP blocks | 38 | 38 |

Setup slack is 0.184 ns below main's and still met; the area change is fit
noise. No kit run: the change is a fault's order, which the lockstep
covers, and the seal shows no significant change in timing or area.

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
