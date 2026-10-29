<!-- Generated from docs/support-matrix.json by docs/support-matrix.py. Edit the JSON, then run python3 docs/support-matrix.py --write -->

# Core support matrix

Short view of cores, boards and toolchains. Locks, evidence, feature notes and the full policy wording stay in [`support-matrix.json`](support-matrix.json). Edit that file, then run `python3 docs/support-matrix.py --write`.

Package ABI and standing: [core status](core-status.md). Build steps: [misteross cores](../sources/misteross/docs/cores.md).

## Legend

| Tier | Meaning |
| --- | --- |
| Firmware | Boot or kit UI bitstream. `fes.menu` is the bootstrap package. Splash is the idle RBF. |
| 1 | Catalog standing `supported`. Not in the bootstrap image. |
| 2 | Optional recipe. Catalog standing appears in parentheses; Atari ST is experimental. |
| 3 | Catalog standing `experimental`. |
| Demo | Catalog standing `demo`. Today only Catch. |
| Reference | Not a registered play package. |
| Bring-up | RTL and a diagnostic path, no recipe. |
| Shared | CPU RTL used by other cores. Not a board bitstream. |
| Planned | No row uses this tier. |

**DE10-Nano** (MiSTer, Cyclone V) **OSS** is Yosys + nextpnr-mistral, the installed toolchain. i386, pc386 and PC/XT are Quartus (primary, policy) (policy: Deano 2026-10-29). nextpnr builds there are advisory; a miss is filed as a nextpnr issue, not a blocker. A dash means this row has no DE10-Nano bitstream. pc386 and i386 are Cyclone V spikes; Quartus is still their primary toolchain.

**DE25-Nano** (Agilex 5) follows Quartus-only (policy); shell pending (policy: Deano 2026-10-29). Rows with a board bitstream show Quartus; rows without one show a dash. Agilex 5 has no OSS reverse-engineering. Per-core ports are re-checked only after the shell is on this branch.

`oss` and `quartus` are the Debian-style labels in [Quartus-built cores](design/quartus-cores.md). Both labelled builds ship only when results differ by the 15% rule. No registered recipe ships both today. Bluetooth and mesh are host services, so they are not columns.

ColecoVision still uses the old media handling. Its example arcade room shows an empty-library status when no matching titles are available. Updating it is low priority and is folded into the wider Coleco work, including Adam support (policy: Deano 2026-10-29).

## Cores

| Core | Tier | DE10-Nano | DE25-Nano | Features | Details |
| --- | --- | --- | --- | --- | --- |
| `fes.menu` | Firmware | OSS | Quartus | HDMI | [README](../sources/misteross/cores/fes-menu/README.md) |
| splash / idle | Firmware | OSS | Quartus | HDMI | [README](../sources/misteross/cores/fes-splash/README.md) |
| `fes.pong` | 1 | OSS | Quartus | pad, HDMI | [cores](../sources/misteross/docs/cores.md) |
| `fes.catch` | Demo | OSS | Quartus | pad, HDMI | [cores](../sources/misteross/docs/cores.md) |
| `fes.ramtest` | 1 | OSS | Quartus | pad, HDMI | [README](../sources/misteross/cores/fes-ramtest/README.md) |
| `fes.zx81` | 1 | OSS | Quartus | tape, keys, HDMI | [cores](../sources/misteross/docs/cores.md) |
| `fes.zx80` | 3 | OSS | Quartus | tape, keys, HDMI | [cores](../sources/misteross/docs/cores.md) |
| `fes.ts1000` | 3 | OSS | Quartus | tape, keys, HDMI | [cores](../sources/misteross/docs/cores.md) |
| `fes.ts1500` | 3 | OSS | Quartus | tape, keys, HDMI | [cores](../sources/misteross/docs/cores.md) |
| `fes.coleco` | 1 | OSS | Quartus | cart, pad, HDMI | [README](../sources/misteross/cores/fes-coleco/README.md) |
| `fes.sms` | 1 | OSS | Quartus | cart, keys, HDMI | [README](../sources/misteross/cores/fes-sms/README.md) |
| `fes.sg1000` | 1 | OSS | Quartus | cart, keys, HDMI | [README](../sources/misteross/cores/fes-sg1000/README.md) |
| `fes.apple2` | 3 | OSS | Quartus | disk, keys, HDMI | [README](../sources/misteross/cores/fes-apple2/README.md) |
| `fes.c64` | 3 | OSS | Quartus | disk, keys, HDMI | [README](../sources/misteross/cores/fes-c64/README.md) |
| `fes.spectrum` | 3 | OSS | Quartus | tape, keys, HDMI | [README](../sources/misteross/cores/fes-spectrum/README.md) |
| `fes.spectrum128` | 3 | OSS | Quartus | tape, keys, HDMI | [README](../sources/misteross/cores/fes-spectrum/README.md) |
| `fes.spectrum-plus3` | 3 | OSS | Quartus | disk, keys, HDMI | [README](../sources/misteross/cores/fes-spectrum/README.md) |
| `fes.pentagon` | 3 | OSS | Quartus | disk, keys, HDMI | [README](../sources/misteross/cores/fes-spectrum/README.md) |
| `fes.atari-st` | 2 (experimental) | OSS | Quartus | disk, keys, mouse, HDMI | [README](../sources/misteross/cores/fes-atari-st/README.md) |
| `fes.riscv` | 3 | OSS | Quartus | pad, HDMI | [README](../sources/misteross/cores/fes-riscv/README.md) |
| `fes.demo`, `fes.demo-media`, `fes.demo-audio` | Reference | OSS | Quartus | pad, HDMI | [cores](../sources/misteross/docs/cores.md) |
| `cores/pong` simulation | Reference | — | — | — | [cores](../sources/misteross/docs/cores.md) |
| `fes.pcxt` | Bring-up | Quartus | Quartus | HDMI | [README](../sources/misteross/cores/fes-pcxt/README.md) |
| `fes.pc386` | Bring-up | — | — | sim | [README](../sources/misteross/cores/fes-pc386/README.md) |
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
