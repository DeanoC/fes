<!-- Generated from docs/support-matrix.json by docs/support-matrix.py. Edit the JSON, then run python3 docs/support-matrix.py --write -->

# Core support matrix

Short view of cores, boards and toolchains. Locks, evidence, feature notes and the full policy wording stay in [`support-matrix.json`](support-matrix.json). Edit that file, then run `python3 docs/support-matrix.py --write`.

Package ABI and standing: [core status](core-status.md). Build steps: [misteross cores](../sources/misteross/docs/cores.md).

## Legend

Tiers are derived from signals in the JSON (as of 2026-10-29). Catalog standing is recorded and is not a tier. Owned RTL is a commit that changes that core's `rtl/` and names the core, or that core holds at least half of the commit's RTL files. A one-file edit inside another core's commit does not count.

| Tier | Meaning |
| --- | --- |
| Firmware | Boot package or pinned idle RBF. |
| Current | Owned RTL on or after 2026-10-16, and media is not the old generation. |
| Aging | Owned RTL in the 60-day window, before 2026-10-16. |
| Stale | No owned RTL in the 60-day window. |
| Unresolved | Recent RTL and old media. The rule does not pick a tier. |
| Bring-up | No recipe. Diagnostic RTL or host simulation in progress. |
| Shared | CPU RTL used by other cores. Not a system package. |
| Demo | Registered demo package, not a machine. |
| Reference | Simulation module or non-play shell. |

**DE10-Nano** (MiSTer, Cyclone V) **OSS** is Yosys + nextpnr-mistral, the installed toolchain. i386, pc386 and PC/XT are Quartus (primary, policy) (policy: Deano 2026-10-29). nextpnr builds there are advisory; a miss is filed as a nextpnr issue, not a blocker. A dash means this row has no DE10-Nano bitstream. pc386 and i386 are Cyclone V spikes; Quartus is still their primary toolchain.

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
| `fes.catch` | Demo | OSS | Quartus | pad, HDMI | [cores](../sources/misteross/docs/cores.md) |
| `fes.ramtest` | Current | OSS | Quartus | pad, HDMI | [README](../sources/misteross/cores/fes-ramtest/README.md) |
| ZX81 (ZX81, ZX80, TS1000, TS1500) | Current | OSS | Quartus | tape, keys, HDMI | [cores](../sources/misteross/docs/cores.md) |
| `fes.coleco` | Unresolved | OSS | Quartus | cart, pad, HDMI | [README](../sources/misteross/cores/fes-coleco/README.md) |
| `fes.sms` | Aging | OSS | Quartus | cart, keys, HDMI | [README](../sources/misteross/cores/fes-sms/README.md) |
| `fes.sg1000` | Aging | OSS | Quartus | cart, keys, HDMI | [README](../sources/misteross/cores/fes-sg1000/README.md) |
| `fes.apple2` | Aging | OSS | Quartus | disk, keys, HDMI | [README](../sources/misteross/cores/fes-apple2/README.md) |
| C64 (PAL) | Current | OSS | Quartus | disk, keys, HDMI | [README](../sources/misteross/cores/fes-c64/README.md) |
| Spectrum (48K, 128K, +2A/+3, Pentagon) | Current | OSS | Quartus | tape, disk, keys, HDMI | [README](../sources/misteross/cores/fes-spectrum/README.md) |
| Atari ST (520ST) | Current | OSS | Quartus | disk, keys, mouse, HDMI | [README](../sources/misteross/cores/fes-atari-st/README.md) |
| `fes.riscv` | Aging | OSS | Quartus | pad, HDMI | [README](../sources/misteross/cores/fes-riscv/README.md) |
| `fes.demo`, `fes.demo-media`, `fes.demo-audio` | Reference | OSS | Quartus | pad, HDMI | [cores](../sources/misteross/docs/cores.md) |
| `cores/pong` simulation | Reference | — | — | — | [cores](../sources/misteross/docs/cores.md) |
| PC/XT | Bring-up | Quartus | Quartus | HDMI | [README](../sources/misteross/cores/fes-pcxt/README.md) |
| pc386 | Bring-up | — | — | sim | [README](../sources/misteross/cores/fes-pc386/README.md) |
| `fes_i386_cpu` | Shared | — | — | — | [design](../sources/misteross/docs/i386-cpu-design.md) |
| `fes_z80_nmos` / `fes_z80_fast` | Shared | — | — | — | [README](../sources/misteross/cores/fes-common/rtl/z80/README.md) |
| `cpu6502` | Shared | — | — | — | [README](../sources/misteross/cores/fes-common/rtl/cpu6502/README.md) |
| `fx68k` | Shared | — | — | — | [README](../sources/misteross/cores/fes-common/rtl/fx68k/README.md) |
| TV80 | Shared | — | — | — | [alignment](core-alignment.md) |

## Open

These cells are still open. The wording is in [`support-matrix.json`](support-matrix.json).

- ZX80, TS1000, TS1500 — Quartus oracle
- Every row with a pad or keyboard — Bluetooth
- Every row — Mesh / FogCast
- Registered recipes — Ship both builds
- Catalog-supported and experimental rows — Current-byte kit acceptance

## Needs Deano

- Coleco is unresolved: owned RTL 2026-10-28 (#1165) and a host cartridge swap 2026-10-29 (#1034), while media_gen is old (empty-library status, low priority, #1216). The rule does not pick current or aging.
- SMS and SG-1000 catalog standing is supported. Tier aging uses owned RTL on 2026-10-06 and kit diagnostics on 2026-09-29. The catalog flag was not used.
- Pong catalog standing is supported. No owned RTL in the 60-day window, so the tier is stale. The catalog flag was not used.
- Atari ST kit note is 2026-10-10. Owned RTL is 2026-10-23 (#952). No kit PASS of that later bitstream was found.
- Spectrum kit PASS is 2026-10-26. Owned RTL 2026-10-27 (#1093 snapshots) is not in that PASS.
- No separate Spectrum +2 package was found. fes.spectrum-plus3 is +2A/+3.
- The C64 package is PAL 6569 only. The C64 README says NTSC is unqualified. No NTSC row was added.
- The Atari README says STE is outside the 520ST scope. No STE row was added.
- ZX80, TS1000 and TS1500 share the ZX81 shell. Kit notes name fes.zx81 (2026-10-22), not those three. The 2026-10-25 model commit is after that note.
- Apple II kit diagnostic is 2026-09-27. Owned RTL is 2026-10-06 (#549). No later kit note was found.
- Open issues on each row are title matches, not a full blocker audit.
- Coleco last_hil_pass is empty. Notes on 2026-10-02 and 2026-10-16 mention the package and do not read as a machine PASS.
