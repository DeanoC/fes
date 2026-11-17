<!-- Generated from docs/support-matrix.json by docs/support-matrix.py. Edit the JSON, then run python3 docs/support-matrix.py --write -->

# Core support matrix

Short view of cores, boards and toolchains. Locks, evidence, feature notes and the full policy wording stay in [`support-matrix.json`](support-matrix.json). Edit that file, then run `python3 docs/support-matrix.py --write`.

Package ABI and standing: [core status](core-status.md). Build steps: [misteross cores](../sources/misteross/docs/cores.md).

## Legend

Tiers are derived from role and media signals in the JSON and an owned-RTL activity snapshot as of 2026-10-29. Role, media and dated kit notes may be updated after that snapshot. Catalog standing is recorded and is not a tier. Owned RTL is a commit that changes that core's `rtl/` and names the core, or that core holds at least half of the commit's RTL files and at least two files. A one-file edit inside another core's commit does not count. A recorded override from Deano 2026-10-29 replaces the derived tier.

Coleco is Aging: work toward Current is in progress and still needs work. Spectrum and Atari ST stay Current; their kit HIL pass on the latest RTL is pending and expected soon. C64 NTSC and Atari STE are Bring-up (queued).

| Tier | Meaning |
| --- | --- |
| Firmware | Boot package or pinned idle RBF. |
| Current | Owned RTL on or after 2026-10-16, and media is not the old generation. |
| Aging | Owned RTL in the 60-day window, before 2026-10-16. |
| Stale | No owned RTL in the 60-day window. |
| Bring-up | The machine is still in bring-up. Its package is a diagnostic or development profile, and what it is accepted for is the dated evidence on the row. |
| Bring-up (queued) | No package yet. Queued by Deano 2026-10-29. |
| Shared | CPU RTL used by other cores. Not a system package. |
| Demo | Registered demo package, not a machine. |
| Reference | Simulation module or non-play shell. |

**DE10-Nano** (MiSTer, Cyclone V) **OSS** is Yosys + nextpnr-mistral, the installed toolchain. i386, pc386 and PC/XT are Quartus (primary, policy) (policy: Deano 2026-10-29). nextpnr builds there are advisory; a miss is filed as a nextpnr issue, not a blocker. A dash means this row has no DE10-Nano bitstream. i386 is a CPU spike with no board shell of its own; the sealed pc386 development package runs on the DE10-Nano.

**DE25-Nano** (Agilex 5) follows Quartus-only (policy); shell pending (policy: Deano 2026-10-29). Rows with a board bitstream show Quartus; rows without one show a dash. Agilex 5 has no OSS reverse-engineering. Per-core ports are re-checked only after the shell is on this branch.

`oss` and `quartus` are the Debian-style labels in [Quartus-built cores](design/quartus-cores.md). Both labelled builds ship only when results differ by the 15% rule. No registered recipe ships both today. Bluetooth and mesh are host services, so they are not columns.

ColecoVision still uses the old media handling. Its example arcade room shows an empty-library status when no matching titles are available. Updating it is low priority and is folded into the wider Coleco work, including Adam support (policy: Deano 2026-10-29).

Atari ST is still the only optional recipe (`FES_SKIP_OPTIONAL_CORES`). That flag is not its tier.

## Cores

| Core | Tier | DE10-Nano | DE25-Nano | Features | Details |
| --- | --- | --- | --- | --- | --- |
| `fes.menu` | Firmware | OSS | Quartus | HDMI | [README](../sources/misteross/cores/fes-menu/README.md) |
| splash / idle | Firmware | OSS | Quartus | HDMI | [README](../sources/misteross/cores/fes-splash/README.md) |
| `fes.pong` | Stale | OSS | Quartus | pad, HDMI | [cores](../sources/misteross/docs/cores.md) |
| `fes.ramtest` | Current | OSS | Quartus | pad, HDMI | [README](../sources/misteross/cores/fes-ramtest/README.md) |
| ZX81 (ZX81, ZX80, TS1000, TS1500) | Current | OSS | Quartus | tape, keys, HDMI | [cores](../sources/misteross/docs/cores.md) |
| `fes.coleco` | Aging | OSS | Quartus | cart, pad, HDMI | [README](../sources/misteross/cores/fes-coleco/README.md) |
| `fes.sms` | Aging | OSS | Quartus | cart, keys, HDMI | [README](../sources/misteross/cores/fes-sms/README.md) |
| `fes.sg1000` | Aging | OSS | Quartus | cart, pad, HDMI | [README](../sources/misteross/cores/fes-sg1000/README.md) |
| `fes.apple2` | Aging | OSS | Quartus | disk, keys, HDMI | [README](../sources/misteross/cores/fes-apple2/README.md) |
| C64 (PAL) | Current | OSS | Quartus | disk, keys, HDMI | [README](../sources/misteross/cores/fes-c64/README.md) |
| C64 (NTSC) | Bring-up (queued) | — | — | — | [README](../sources/misteross/cores/fes-c64/README.md) |
| Spectrum (48K, 128K, +2, +2A/+3, Pentagon) | Current | OSS | Quartus | tape, disk (some), keys, HDMI | [README](../sources/misteross/cores/fes-spectrum/README.md) |
| Atari ST (520ST) | Current | OSS | Quartus | disk, keys, mouse, HDMI | [README](../sources/misteross/cores/fes-atari-st/README.md) |
| Atari ST (STE) | Bring-up (queued) | — | — | — | [README](../sources/misteross/cores/fes-atari-st/README.md) |
| `fes.riscv` | Aging | OSS | Quartus | pad, HDMI | [README](../sources/misteross/cores/fes-riscv/README.md) |
| `cores/pong` simulation | Reference | — | — | — | [cores](../sources/misteross/docs/cores.md) |
| PC/XT (FreeDOS) | Current | Quartus | Quartus | disk, keys, HDMI | [README](../sources/misteross/cores/fes-pcxt/README.md) |
| pc386 | Bring-up | Quartus | Quartus | disk, keys, mouse, HDMI | [README](../sources/misteross/cores/fes-pc386/README.md) |
| `fes_i386_cpu` | Shared | — | — | — | [design](../sources/misteross/docs/i386-cpu-design.md) |
| `fes_z80_nmos` / `fes_z80_fast` | Shared | — | — | — | [README](../sources/misteross/cores/fes-common/rtl/z80/README.md) |
| `cpu6502` | Shared | — | — | — | [README](../sources/misteross/cores/fes-common/rtl/cpu6502/README.md) |
| `fx68k` | Shared | — | — | — | [README](../sources/misteross/cores/fes-common/rtl/fx68k/README.md) |
| TV80 | Shared | — | — | — | [alignment](core-alignment.md) |

## Release scope

The first release lists only v1 cores in tenfoot Home, the rooms and the core catalog view. The tenfoot Settings row Show all cores lists the rest, for development and HIL. The scope is not a tier, and v1 does not mean the exact release package has passed kit HIL.

- **v1:** `fes.zx81`, `fes.zx80`, `fes.ts1000`, `fes.ts1500`, `fes.c64`, `fes.spectrum`, `fes.spectrum128`, `fes.spectrum-plus3`, `fes.pentagon`, `fes.atari-st`
- **v2:** `fes.pcxt-dos`, `fes.pc386`
- **Held back:** `fes.pong`, `fes.ramtest`, `fes.coleco`, `fes.sms`, `fes.sg1000`, `fes.apple2`, C64 NTSC, Atari STE, `fes.riscv`

## Quality tier and checks

In force. Deano accepted it on 2026-11-15, and the rule itself is the hard rule beside D12 in [AGENTS.md](../AGENTS.md): a core at `check_level: reduced` owes a closure sweep only D12's netlist comparison, which is still measured and recorded, and not the RBF reproduction, the timing and seal gate or the HIL. This page is where the levels are listed; AGENTS.md is where the obligation is written. Neither lowers a gate on work being done to a core.

`quality_tier` cites a decision rather than inventing a taxonomy: the ladder, its bars, the tier 1 set, the two pending placements and the utilities outside it are all decided in [the forward plan](plans/forward-plan.md) (tiers apply per (core, board) cell; DE10-Nano is the tier 1 board). Where the plan names a condition rather than a tier, this page records the condition: pc386 is `1-pending` and not tier 2, and Coleco, SMS, SG-1000 and Apple II are `2-pending`, because the plan names no tier for any of them before their condition is met. The freshness `tier` on each row answers a different question and stays derived from dates, so a tier 1 cell can be Aging and a row off the ladder can have RTL from this week.

`check_level` says what a change owes each row, and the D12 conditions are what it is about. `full`: every change that moves the core's `BUILD_ID` carries that core's own evidence, all three D12 conditions included. `reduced`: on a change that does not touch the core's own RTL, programs, lock or placement recipe, the netlist comparison (D12's first condition, which needs no GPU route) is still expected, and the RBF reproduction, the timing and seal gate and the HIL (its second and third, and the reseal the rule falls back to) are not. If that comparison fails, the core counts as touched and keeps every gate, which is what D12 already says. A change aimed at the core, and any change that ships it, keeps every gate. `via-consumer`: the row is RTL, not a package, so its evidence rides with the packages that embed it. `none`: there is no built artifact to carry evidence for yet.

The cost this answers is a closure sweep, which is what D12 covers: a change with no RTL that moves a core's `BUILD_ID` through its build record or source closure. As of 2026-11-15, PR #1862 edits five producer scripts and one shared helper, touches no RTL, QSF, SDC, lock or placement recipe, and moves the `BUILD_ID` of all 14 cores whose producers import a changed file, so D12 applies to each of them. Its first condition was measured for all 14; Deano scoped the route-based second and third on 2026-11-15 to the firmware plus one representative per affected machine family, leaving `fes.pong`, `fes.ramtest`, `fes.coleco` and `fes.riscv` unmeasured because they are not shipped. This rule is close to that call but not the same one: it drops the same two conditions for the same held-back cores by rule, while it keeps at full the sibling variants that call also dropped (zx80, ts1000, ts1500, spectrum128, spectrum-plus3, pentagon). Whether the representative scoping should be a rule too is a separate question.

The levels never lower a gate on work being done to a core: a netlist that is not byte-identical, a change that touches the core's own RTL, programs, lock or placement recipe, and any change that ships it, all put that core back under the full rule. AGENTS.md's hard rule against relaxing a timing or seal check stands above this one and is unchanged.

| Quality tier | Meaning |
| --- | --- |
| Tier 1 | Release quality and supported. Decided set: ZX81 family, Spectrum family, C64, Atari ST, PC/XT. |
| Tier 1 pending | The plan puts pc386 at tier 1 once #1368 and #1494 close, and names no tier before that. |
| Tier 2 pending | The plan puts Coleco, SMS, SG-1000 and Apple II at tier 2 once they meet the port, and names no tier before that. |
| Outside the ladder | Utilities and demos: Pong, RAM Tester and RISC-V. |

| Checks | Expected evidence |
| --- | --- |
| Full | Every change that moves the core's `BUILD_ID` carries that core's own evidence. |
| Reduced | On a sweep the core is not about, its netlist comparison still holds; the RBF repeat, seal gate and HIL do not. A moved netlist keeps every gate. |
| Via consumer | Not a package. Evidence rides with the packages that embed it. |
| None | No built artifact yet. |

- **Tier 1:** `fes.zx81`, `fes.zx80`, `fes.ts1000`, `fes.ts1500`, `fes.c64`, `fes.spectrum`, `fes.spectrum128`, `fes.spectrum-plus3`, `fes.pentagon`, `fes.atari-st`, `fes.pcxt-dos`
- **Tier 1 pending:** `fes.pc386`
- **Tier 2 pending:** `fes.coleco`, `fes.sms`, `fes.sg1000`, `fes.apple2`
- **Outside the ladder:** `fes.pong`, `fes.ramtest`, `fes.riscv`

- **Full checks:** `fes.menu`, splash / idle, `fes.zx81`, `fes.zx80`, `fes.ts1000`, `fes.ts1500`, `fes.c64`, `fes.spectrum`, `fes.spectrum128`, `fes.spectrum-plus3`, `fes.pentagon`, `fes.atari-st`, `fes.pcxt-dos`, `fes.pc386`
- **Reduced checks:** `fes.pong`, `fes.ramtest`, `fes.coleco`, `fes.sms`, `fes.sg1000`, `fes.apple2`, `fes.riscv`
- **Via consumer checks:** `fes_i386_cpu`, `fes_z80_nmos` / `fes_z80_fast`, `cpu6502`, `fx68k`, TV80
- **None checks:** C64 NTSC, Atari STE, `cores/pong` simulation

## Open

These cells are still open. The wording is in [`support-matrix.json`](support-matrix.json).

- Every row with a pad or keyboard — Bluetooth
- Every row — Mesh / FogCast
- Registered recipes — Ship both builds
- Catalog-supported and experimental rows — Current-byte kit acceptance

## Needs Deano

- SMS and SG-1000 are outside the first-party catalog; their development recipes remain registered. Tier aging uses owned RTL on 2026-10-06 and kit diagnostics on 2026-09-29. The catalog flag was not used.
- Pong catalog standing is supported. No owned RTL in the 60-day window, so the tier is stale. The catalog flag was not used.
- ZX80, TS1000 and TS1500 share the ZX81 shell. Kit notes name fes.zx81 (2026-10-22), not those three. The 2026-10-25 model commit is after that note.
- Apple II kit diagnostic is 2026-09-27. Owned RTL is 2026-10-06 (#549). No later kit note was found.
- Open issues on each row are title matches, not a full blocker audit.
- ColecoVision is check_level reduced by the ladder (2-pending) while being under active work. Reduced exempts only sweeps it is not about, but if Coleco should carry full evidence for every sweep as well, the ladder has to place it rather than name its condition.
- RAM Tester is outside the ladder as a utility and is under active timing work (#1832, #1767). The same question as Coleco: reduced for sweeps, or full because the memory path is shared.
- SG-1000's newest owned RTL is an ABI migration (#1755) and Apple II's is the original-glyph campaign (#1819). The owned-RTL rule counts both as core activity, so the freshness tier cannot be read as development activity on either row.
- Does a mechanical RTL sweep count as touching a core? #1714 edited eight RTL files across five cores to remove one obsolete mailbox. Under the wording here it touches each core's own RTL, so every gate applies and reduced buys nothing on it. If sweeps like that should also be exempt, the wording has to say so, and D12 does not cover them because they change RTL.
- Should the 2-pending rows (Coleco, SMS, SG-1000, Apple II) carry full evidence on a closure sweep anyway? They are reduced here because the forward plan names no tier for them until they meet the port.
- Should the per-family representative scoping be a rule as well? On PR #1862 the sibling variants (zx80, ts1000, ts1500, spectrum128, spectrum-plus3, pentagon) were dropped from the route conditions along with the held-back cores; check_level keeps them at full because they are tier 1.
- fes.riscv's snapshot last_rtl_change (2026-10-05) reads its core directory only. `dcfa92c98` (2026-10-21) changed `fes_rv32_cpu.sv` in the shared riscv directory that this row holds under `paths`, and the subject names riscv, so whether that commit should move the row to Current inside the same snapshot is open. The owned-RTL check runs on shared rows only, so nothing catches it either way.
- A pending quality_tier has to be re-read by hand when its condition closes. The checker reads the forward plan's Tiers section and fails if a cited sentence moves, goes or is restated, and if the plan stops naming #1368, #1494 or the port condition; it cannot see a superseding decision written in new words. So when #1368 and #1494 close, pc386's 1-pending becomes tier 1 and its check level does not change, but the row still needs editing.
- fes.pc386 keeps tier_role bring-up and experimental catalog standing. Registration does not promote its quality tier; the exact registered package still needs a fresh seal and the pending game and hardware gates.
