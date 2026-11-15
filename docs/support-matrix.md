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
| Bring-up | No catalog package. Diagnostic or development packages only. |
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
| PC/XT | Bring-up | Quartus | Quartus | disk, HDMI | [README](../sources/misteross/cores/fes-pcxt/README.md) |
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
- **v2:** `fes.pcxt`, `fes.pcxt-dos`, `fes.pc386`
- **Held back:** `fes.pong`, `fes.ramtest`, `fes.coleco`, `fes.sms`, `fes.sg1000`, `fes.apple2`, C64 NTSC, Atari STE, `fes.riscv`

## Quality tier and checks (proposed)

Proposed, not in force. Until Deano accepts it, every row keeps the evidence the hard rules in [AGENTS.md](../AGENTS.md) already require of it. This page records a proposal; it does not relax a seal, a timing check or a HIL gate, and the D12 rule in AGENTS.md still reads as written.

`quality_tier` is a recorded fact, not a new taxonomy: the ladder, its bars and its tier 1 set were decided in [the forward plan](plans/forward-plan.md) (tiers apply per (core, board) cell; DE10-Nano is the tier 1 board). The freshness `tier` on each row answers a different question and stays derived from dates, so a tier 1 cell can be Aging and a row off the ladder can have RTL from this week.

`check_level` is the proposal. `full`: every change that moves the core's `BUILD_ID` carries that core's own evidence, as it does today. `reduced`: a repo-wide change that does not touch the core's own RTL, programs, lock or placement recipe is not expected to carry that core's D12 netlist and RBF comparison, its reseal or its HIL; a change aimed at the core, and any change that ships it, keeps every gate it has today. `via-consumer`: the row is RTL, not a package, so its evidence rides with the packages that embed it. `none`: there is no built artifact to carry evidence for yet.

The cost this answers is a sweep, not a core. `cb127203d` (#1714) removed one obsolete mailbox and moved six cores' `BUILD_ID`s at once; of those six only the menu is in the bootstrap image, and only ColecoVision is under active work. Date signals cannot separate the two: `c0deb8e39` (#1755) is the newest owned RTL on SG-1000 and is an ABI migration, and the newest on Apple II, RAM Tester and C64 is the original-glyph campaign (#1819, #1818, #1822). The ladder separates them; recency does not.

Accepting this needs a sentence beside the D12 rule in [AGENTS.md](../AGENTS.md), which is Deano's wording to write. No agent should read `reduced` here as permission to skip a gate before then.

| Quality tier | Meaning |
| --- | --- |
| Tier 1 | Release quality and supported. Decided set: ZX81 family, Spectrum family, C64, Atari ST, PC/XT. |
| Tier 2 | Supported preview. pc386 reaches tier 1 when #1368 and #1494 close. |
| Tier 3 | Experimental: builds, no promise. Coleco, SMS, SG-1000 and Apple II are tier 2 once they meet the port. |
| Outside the ladder | Utilities and demos: Pong, RAM Tester and RISC-V. |

| Checks | Expected evidence |
| --- | --- |
| Full | Every change that moves the core's `BUILD_ID` carries that core's own evidence. |
| Reduced | A sweep that does not touch the core itself carries no D12 evidence, reseal or HIL for it. Work on the core keeps every gate. |
| Via consumer | Not a package. Evidence rides with the packages that embed it. |
| None | No built artifact yet. |

- **Tier 1:** `fes.zx81`, `fes.zx80`, `fes.ts1000`, `fes.ts1500`, `fes.c64`, `fes.spectrum`, `fes.spectrum128`, `fes.spectrum-plus3`, `fes.pentagon`, `fes.atari-st`, `fes.pcxt`, `fes.pcxt-dos`
- **Tier 2:** `fes.pc386`
- **Tier 3:** `fes.coleco`, `fes.sms`, `fes.sg1000`, `fes.apple2`
- **Outside the ladder:** `fes.pong`, `fes.ramtest`, `fes.riscv`

- **Full checks:** `fes.menu`, splash / idle, `fes.zx81`, `fes.zx80`, `fes.ts1000`, `fes.ts1500`, `fes.c64`, `fes.spectrum`, `fes.spectrum128`, `fes.spectrum-plus3`, `fes.pentagon`, `fes.atari-st`, `fes.pcxt`, `fes.pcxt-dos`, `fes.pc386`
- **Reduced checks:** `fes.pong`, `fes.ramtest`, `fes.coleco`, `fes.sms`, `fes.sg1000`, `fes.apple2`, `fes.riscv`
- **Via consumer checks:** `fes_i386_cpu`, `fes_z80_nmos` / `fes_z80_fast`, `cpu6502`, `fx68k`, TV80
- **None checks:** C64 NTSC, Atari STE, `cores/pong` simulation

## Open

These cells are still open. The wording is in [`support-matrix.json`](support-matrix.json).

- ZX80, TS1000, TS1500 — Quartus oracle
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
- The check_level proposal needs a yes or no. It is recorded as proposed, not in force: accepting it means a sentence beside the D12 rule in AGENTS.md, which is Deano's wording.
- ColecoVision is check_level reduced by the ladder (tier 3) while being under active work. Reduced exempts only sweeps it is not about, but if Coleco should carry full evidence for every sweep as well, it wants quality_tier 2 early rather than an exception.
- RAM Tester is outside the ladder as a utility and is under active timing work (#1832, #1767). The same question as Coleco: reduced for sweeps, or full because the memory path is shared.
- pc386 is recorded at quality_tier 2. The forward plan says it joins tier 1 when #1368 and #1494 close; nothing in the plan names its tier before that, so 2 is this page's reading of the tier 2 bar it meets.
- SG-1000's newest owned RTL is an ABI migration (#1755) and Apple II's is the original-glyph campaign (#1819). The owned-RTL rule counts both as core activity, so the freshness tier cannot be read as development activity on either row.
