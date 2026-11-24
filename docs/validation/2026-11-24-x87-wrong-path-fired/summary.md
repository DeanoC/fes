# x87: a wrong-path ESC form the unit does not build wedges every later ESC

Found by the DE25 i486DX slice 1 (#2135, PR #2194), where Quake stalled for
ever on its FNINIT (`00a7:0005256c` `DB E3`) on the RTL with the 387 on.

## The bug

`fes_i386_x87` holds an instruction's first uop while the instruction before
it owns the unit: `fired` says that instruction's first uop has issued
(`hold`, the `fired ||` terms). `fired` was set for any ESC first uop that
issued, whether or not the unit had taken the instruction. A form the unit
does not build is never taken (`take` excludes `X87_UNIMPL`), so `started`
stays clear. A flush clears `fired` only together with `started`. So when an
unbuilt form issues on a **wrong path** and is flushed, `fired` stays set,
and every later ESC, FNINIT and FNCLEX included, holds for ever.

On the machine it happened 65 clocks after a handover to the RTL, in the
BIOS (`F000:535D`): the front end ran on past a branch into bytes that
decoded as `D9 /1` with a memory operand (op `14F`, reserved). Quake's
FNINIT, long after, was the first ESC to meet the hold. The diagnostic runs
and their scratch bench edits are in #2194's record
(`docs/validation/2026-11-24-i486dx-s1/quake-diag/`).

The lockstep never met it: its x87 cases are directed, and its random
programs run with no 387. Real hardware has no such state, so there is no
386/387 behaviour to follow here: the unit's own bookkeeping is wrong.

## The cases and the fix

- **Fix** (`fes_i386_x87.sv`): `fired` is set only for a form the unit
  builds (`!unbuilt`) and only for an instruction the unit has taken or takes
  on that edge (`started || take`).
- **Case** `wrong-path-unbuilt` (lockstep x87 cases): FNINIT; `CMP AX, AX`;
  `JZ` over a `D9 4F 00`; FNINIT; FLD; then the same over a waiting form.
- **Cases** `wrong-path-unbuilt-busy-*`: the same wrong-path `D9 /1` after a
  slow FSQRT and after a slow FDIV, at every distance from 0 to 80 NOPs, in
  five programs that each start at `CODE`. The x87 runner now refuses any
  program that would reach the handler's page (Codex found the first single
  program ran into the handler at `1800h` from 32 NOPs on).

**Which version fails which case** (the default x87 cases, quick runner
settings, main `0c71d434b` with this branch's cases):

| RTL | Result |
| --- | --- |
| main, no fix | 127 of 131: `wrong-path-unbuilt` and `busy-48-60`, `busy-61-71`, `busy-72-80` time out ([runs/case-before-fix.txt](runs/case-before-fix.txt)) |
| the first version (`started \|\| take` only; [runs/first-version-fix.patch](runs/first-version-fix.patch)) | 130 of 131: `busy-61-71` times out ([runs/case-first-version-fix.txt](runs/case-first-version-fix.txt)) |
| this fix | 131 of 131 |

The first version's failure is Codex's finding on it: `hold` lets an unbuilt
form issue while an older operation still holds `started` (committed,
running on to its apply), and on the edge that operation applies, the set
overrides the apply's clear, leaving `fired` set with `started` clear again.

## Gates, at `c4711cb12` (main `0c71d434b` + this change; no tracked changes)

| Gate | Result |
| --- | --- |
| RTL lockstep, level 3, the default suite | pass; x87 cases 131 of 131 ([runs/l3-full.json](runs/l3-full.json)) |
| RTL lockstep, level 4, both identities | pass: 386 56 agree / 92 stop as expected, 486SX 148 agree, 56,000 random programs keep step ([runs/l4-full.json](runs/l4-full.json)) |
| x87 unit bench | 18,337 passed, 0 failed ([runs/x87unit.json](runs/x87unit.json)) |
| DE10 pc386 machine netlist | **moves**: `65cbd412…` without the fix, `e037580e…` with it (Yosys `d273c1cb`, one thread, one checkout; [runs/de10-netlist.txt](runs/de10-netlist.txt), harness `fixd12.sh`). The DE10 ties `x87_present` to 0, but the added terms are not swept |
| Netlist lockstep | pass, the seal's own fresh full run (`--with-lockstep`): netlist `8dee08b1…`, x87 cases 131 of 131, revision `c4711cb12`, no tracked changes ([seal/lockstep-summary.json](seal/lockstep-summary.json)) |
| DE10 pc386 seal | **pass**: `build_pc386_product.py --with-lockstep --seed 1 --gpu-router HIP` from a clean worktree of `c4711cb12` on Powerboat, Quartus 17.0.2 Lite, under the Quartus lease. Package `6dd0a612…`, `BUILD_ID` `3b7c06969d52f2964aa906a64467dea2`, RBF `05382664…`; system 61.44 MHz, pixel 74.27, audio 12.29; worst setup +0.668 ns, hold +0.091, recovery +6.895, removal +0.264, minimum pulse width +1.122; 35,972 of 41,910 ALMs ([seal/build-summary.json](seal/build-summary.json)) |
| Kit B run `k2195c` | **goals pass**: the sealed package core-only on Kit B's baseline (image `179affbb…`, bundle `85dd3b4a`; nothing installed, no image change), the census `pmode` drive: the FreeDOS prompt in 0.6 s; DOOM v1.9 `-timedemo demo3` drawn and moving, then back at the prompt in 82 s with **"timed 3863 gametics in 3386 realtics"** (39.9 fps; [kit/k2195c-result.json](kit/k2195c-result.json), [frames](kit/frames/)). **The session stop then failed**: `/v1/input/detach` returned HTTP 503 `MISTER_UNAVAILABLE`, the lease went to `blocked` (operator recovery). Recovered by removing the run's three data paths and a warm reboot; the kit's state equals its pre-run probe on every key but `boot_id`, idle and lease-free, logged in `kitB-STATUS.md` |

### The kit runs

- `k2195a` was refused at admission ("package identity differs from request"): the `.fcore` was named by its own hash, not the package identity. Nothing ran; the kit was released at once.
- `k2195b`: FreeDOS passed and DOOM's demo was drawn and playing, but the driver's own DOOM check read a dark frame between the capture's black side bars as a text screen and failed the goal ([kit/k2195b-result.json](kit/k2195b-result.json)). The stop failed as in `k2195c`; recovered the same way.
- `k2195c`: the driver's check fixed (colour and motion for the 3D view; a still, colourless screen for the timedemo's end). Both goals pass; the stop fails, as above.
- **Not established:** why the stop's input detach fails. `k2020c` (#2022, 20 Nov) stopped cleanly through the same driver path on an earlier kit bundle (`c9ae79e2`); Kit B's bundle is now `85dd3b4a`. A control run of main's package would tell a kit-software change from this package; it was not run.

## Open before merge

- Whether the kit's stop failure (`MISTER_UNAVAILABLE` on input detach) is this package's or the kit software's.
- Quake past the stall on the DX with this fix: #2194's G5.
