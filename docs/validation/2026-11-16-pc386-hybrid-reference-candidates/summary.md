# pc386: three more 386DX-25 reference candidates

16 November 2026. These are **reference-emulator benchmarks**, not kit or game
acceptance. The original [selection rule](../2026-11-14-pc386-u7-npc-pace/method.md#the-rule)
requires a cached 386DX at 25 MHz in the middle of the scaled real-board
TopBench distribution, whose lower quartile is 47 and median is 57. Three
additional 86Box 6.0 boards that accept a 386DX at 25 MHz were tested with that
same rule. All three measured configurations scored below 47, and **no new
reference was selected**. This is not a finding about all 86Box machines or
unmeasured configurations.

| 86Box machine | 386DX-25 TopBench | Score capture | 100% speed samples |
| --- | ---: | --- | ---: |
| `exp4349` | 40 | [screen](exp4349/score.png) | 115/115 |
| `c747` | 36 | [screen](c747/score.png) | 115/115 |
| `ami495` | 40 | [screen](ami495/score.png) | 115/115 |

These are hybrid 386DX/486 boards with the 386DX selected, not 486 results.
The upstream 86Box v6.0 [machine table](https://github.com/86Box/86Box/blob/4fef696a4eead1d55a28d6ac0e5bd2864e5454da/src/machine/machine_table.c#L7853-L7970)
records the 386DX package and permits the 25 MHz frequency. The configuration
files retained here show the actual selection. Each directory's
`score-start.png` and `score.png` show early and later iterations in the **same
boot**, not independent repeats.

`exp4349`'s BIOS [showed External cache Enabled](exp4349/bios-cache.png) and
`ami495`'s BIOS [showed External Cache Memory Enabled](ami495/bios-cache.png)
when default CMOS was configured. Those captures were taken **before saving**;
neither BIOS setting was re-read in the benchmark boot. The separate
register check below provides additional EXP4349 evidence; neither the
emulator's effective CPU flag nor physical hardware was observed. The
`c747` cache state was not established. Its boot screen reports two 39 MB DOS
disks; their underlying relationship was not established, and the score is
specific to that configuration.

## EXP4349 cache-register check

After the benchmark, a separate reference-only probe booted with the **same
pre-run CMOS bytes** on Powerboat, using a copy of the EXP4349 disk with the
small [`CACHE.COM`](exp4349-cache-probe/ECS.COM) probe added. Its
[screen](exp4349-cache-probe/reading.png) reports ALi M1429G register 18h =
`27h`, whose bit 1 is set. The probe reads this one register and restores the
index latch; its [source](exp4349-cache-probe/cache.asm), exact config, CMOS,
input hashes and [driver](exp4349-cache-probe/run.sh) are retained. Its config
is byte-identical to the benchmark config except for the copied disk path. The
probe was assembled with the previously recorded NASM 2.16.03 using
`-DINDEX_PORT=0x22 -DDATA_PORT=0x23 -DREGISTER=0x18`; the binary extracted
from the private pre-run disk matches the retained binary and a fresh build.
The added probe means this is a **different disk and boot**, not a register
reading from the benchmark run itself.

The v6.0 [machine setup](https://github.com/86Box/86Box/blob/4fef696a4eead1d55a28d6ac0e5bd2864e5454da/src/machine/m_at_386dx_486.c#L53-L65)
uses ALi M1429G, and the shared [chipset handler](https://github.com/86Box/86Box/blob/4fef696a4eead1d55a28d6ac0e5bd2864e5454da/src/chipset/ali1429.c#L198-L201)
maps register 18h bit 1 to its external-cache enable flag on a write. The
probe establishes the register value, not a direct observation of the CPU
flag; the executable was not independently rebuilt from that source. This
separate run's speed readout was **69%** in the capture. That does not affect
an untimed register read, but it means this run supplies **no timing evidence**;
the 40 score and speed samples above belong to the earlier 3090-host run.
The probe's disk and full capture remain privately under
`out/issue1902-hybrid-reference/local/cache-probe-exp4349-20261116T094109Z/`.

## Inputs and reproduction

The experiments ran one at a time on `chip-30x0-ai-dev`, with 86Box 6.0 build
9001, the interpreter, `i386dx` at 25 MHz, 8192 KiB, IBM VGA, XT-IDE and
Sound Blaster 2.0. The executable SHA-256 was
`ae3dc231824cf0c2facd1af4e2164814cbbb15bb1a9fa0603cdd8e9ee0ad2969`.
The copied ROM tree was verified against [roms.sha256](roms.sha256)
(SHA-256 `ec38293be553dabc711ef5dd62cb61b45167d2a00e04cc6c49fb8032aac09b94`);
the manifest contains filenames and hashes, not ROM bytes.

Each board's exact pre-run `86box.cfg` and `.nvr` are in its directory, with
those hashes and the pre-run benchmark disk hash in `input.sha256`. Separate
writable disk copies were used and the snapshots were hashed before boot;
those snapshots and the original 800×520 screen recordings are retained
privately under `out/issue1902-hybrid-reference/qualified-<machine>-<timestamp>/`,
not committed. `TOPBENCH.EXE` extracted from all three pre-run disk snapshots
has SHA-256 `4428fc27d5c2987e5bd177540843190ebb33f31ca6ddacc6e9f8fb994076e63d`.
No game compatibility is established by that identity.

The first `exp4349` and `ami495` attempts stopped on blank-CMOS checks. Their
BIOS defaults were saved on private copies before the retained benchmark runs.
The first `c747` attempt booted more slowly than the 30-second input delay,
so no valid benchmark was captured there; the retained run uses a 50-second
boot delay. [run-board.sh](run-board.sh) and [longboot.sh](longboot.sh) are
the actual two driver versions, invoked with the machine name under a bounded
`xvfb-run` on the 3090 host. Each retains heartbeat and emulator output.

The 4 fps `speed.mkv` files retain the 65×22 indicator crop, losslessly relative
to the decoded H.264 capture: from 35 seconds for `exp4349`, and 55 seconds for
`c747` and `ami495`, through the end of the benchmark capture. Each has 115
samples and every sample shows 100%. The `exp4349` window positions that text
at a different horizontal coordinate; its distinct expected hash accounts for
that. Values **between** samples are not established. `verify.py` checks the
retained config and CMOS digests, frame rates, counts and thresholded frame
hashes, plus the probe source and binary hashes and its matching CMOS. It does not OCR the score, validate the absent private disks, or prove
that the emulator speed indicator is accurate.

No kit was touched and no Ultima VII pace window was captured. The reference
selection, game-corpus check and #1902 remain open.
