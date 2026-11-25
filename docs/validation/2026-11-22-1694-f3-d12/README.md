# F3 (#1694): D12 for the cores the floppy ABI touches

F3 adds `fes.media.pc-floppy` to the shared `fes_computer_mailbox_core`
(`ENABLE_PC_FLOPPY`, default 0) and fifteen defines to the generated
`cores/fes-common/generated/fes_computer.vh`. Every core whose producer reads
either file has its `BUILD_ID` moved, so [D12](../../../AGENTS.md) applies
(design decision D12 in [pc386-removable-media](../../design/pc386-removable-media.md)).

- **Base:** `61c33cc2fc1c9fcfb053940e8f7ddbb638e108b2` (the merge base).
- **Head:** `fbb3b13d1` and later; the measured `misteross` inputs are those of `fbb3b13d1`.
- **Tools:** one install for both sides, `/data/fes-f3-d12/cache` on Powerboat,
  Yosys `3a5accbd7` (sha256 `dc232ba5…8619`), the same binary for every row.

## Scope

`docs/validation/2026-11-13-1581-s7-identity-fields/closure_touched.py 61c33cc2f HEAD`
names the nextpnr producers hit (Apple II, C64, Spectrum, Atari ST, ZX81).
It does not know the Quartus producers, so `git grep fes_computer_mailbox`
over the producers adds `quartus_fes_pcxt.py` and `build_fes_zx81_quartus.py`.
Rows are artefacts, not cores: Spectrum and ZX81 are four machines each.
Apple II is `check_level: reduced` and owes condition 1 only; the rest are full.

## Condition 1: netlists

Each row's netlist with `BUILD_ID` pinned to 32 zeros on both sides, two
passes. nextpnr rows run each producer's own Yosys program through
`netlist_digest.py` (copied from the #1862 record, Apple II and Atari ST
added); `fes.pcxt-dos` runs its Quartus producer's Yosys elaboration
(package and DOS profiles) through `pcxt_netlist.py`, both sides in one work
path. Commands: `run_cond1.sh <base|head> <p1|p2> <core...>` and
`pcxt_netlist.py`; each side's summary is in `cond1/`. `summarise_cond1.py`
writes `cond1.json` and this table:

| Artefact | Base netlist | Head netlist | Inputs moved | Verdict |
|---|---|---|---|---|
| `fes.c64` | `e2d6b41b9ef3a8bc` | `e2d6b41b9ef3a8bc` | yes | byte-identical |
| `fes.apple2` | `0a9847b47616922c` | `0a9847b47616922c` | yes | byte-identical |
| `fes.atari-st` | `5b2c7bd4ea56a46e` | `5b2c7bd4ea56a46e` | yes | byte-identical |
| `fes.spectrum` | `29436bf7023a44f9` | `29436bf7023a44f9` | yes | byte-identical |
| `fes.spectrum128` | `bb9bedf8fd01c20d` | `e6362c043502ac9e` | yes | differs |
| `fes.spectrum-plus3` | `e7e24c520633d9ff` | `e7e24c520633d9ff` | yes | byte-identical at 32 zeros only; differs at main's real `BUILD_ID` (see [Spectrum +3](#spectrum-3-moved-under-mains-build_id-reseal-and-hil)) |
| `fes.pentagon` | `2d81474ad4151e4d` | `59fdbc7a56c74c5c` | yes | differs |
| `fes.zx81` | `bbaa42dd69f16fc3` | `bbaa42dd69f16fc3` | yes | byte-identical |
| `fes.zx80` | `3e51fd9384cf81d1` | `3e51fd9384cf81d1` | yes | byte-identical |
| `fes.ts1000` | `35d6e5536e98fc05` | `35d6e5536e98fc05` | yes | byte-identical |
| `fes.ts1500` | `22c418180d11bb50` | `22c418180d11bb50` | yes | byte-identical |
| `fes.pcxt-dos` | `06b2e8cf5d1d7a18` | `06b2e8cf5d1d7a18` | yes | byte-identical |

Every row's pinned inputs differ between the sides while ten netlists do not,
and both passes agree on every side. **This table pins `BUILD_ID` to zeros, and
for `fes.spectrum-plus3` that did not predict the routed netlist:** under main's
real `BUILD_ID` the head maps differently from the base (condition 2 below). An
identical netlist at zeros is necessary for D12, not sufficient; condition 2 is
what decides each row.

**ZX81 family.** Its registered producer is Quartus Lite reading the RTL
itself, so D12's object for it is the QSF, whose path did not change while the
mailbox's content did. That comparison says nothing here. The rows above are
the advisory nextpnr producer's netlists; for the registered artefacts only
the RBF comparison (condition 2) carries the argument.

### Line numbers (fixed)

At `5419caf3f` the C64 netlist differed from main in 814 `src` attributes,
each a `fes_computer_mailbox.v` line number 3 or 4 higher (`probes` and the
C64 rows in `cond1/` under `c64-head-5419caf3f` on Powerboat): the edit added
four lines above the module's logic. D12 exempts `src` paths, not line numbers.
`fbb3b13d1` keeps every line in place, and the C64 netlist is byte-identical.

### Spectrum 128 and Pentagon (moved; reseal and HIL)

These two netlists differ from main in content (LUT masks and nets), the same
on both passes. Probes, each a local-only commit in a scratch checkout
(`probes/`):

| Probe | Spectrum 128 | Reading |
|---|---|---|
| main, from the head's checkout path | `bb9bedf8…` = main | not the path (DeanoC/yosys#80 ruled out) |
| both sides in one checkout path | head `e6362c04…` | same as two paths |
| main + the 15 new defines only | `bb9bedf8…` = main | the header is inert |
| head, defines moved to the header's end | `e6362c04…` | not the header's line positions |
| head, `ENABLE_PC_FLOPPY` declared last | `e6362c04…` | not the parameter's position |
| head, `ENABLE_PC_FLOPPY` declared and unused | `e6362c04…` | declaring the parameter moves it |

Declaring one more parameter on the mailbox, at 0 and unused, changes how Yosys
maps these two machines; the logic is the same (every instance sets parameters
by name, and the capability word is unchanged). It is the effect #1821 measured
for comment-only edits. Deano chose on 2026-11-22 to keep the parameter and
give `fes.spectrum128` and `fes.pentagon` the normal reseal and HIL rather
than gate it behind a define.

## Conditions 2 and 3

Taken at base `856ef5c64` (the merge base after #2077 and #2130) and head
`0be20e575`. `run_cond23.sh` builds each row in one checkout, in turn: the base
(main's own build), the head with the base's `BUILD_ID` forced through
`docs/validation/2026-11-12-1747-toolchain-installs/forced_build_id.py`
(condition 2: its `core.rbf` must equal the base's byte for byte), and the
head's own unforced build through the producer's gates (condition 3). Each
row's two condition-2 builds used one tool install on one host. nextpnr rows
routed on Powerboat (HIP) or chip-30x0-ai-dev (CUDA); Quartus rows used
Quartus 17.0.2 Lite under the producers' own lease. Logs, build summaries,
manifests and hashes are in [`cond23/`](cond23/).

| Artefact | Host | Condition 2 (base RBF = forced RBF) | Condition 3 (own build) |
|---|---|---|---|
| `fes.c64` | chip, CUDA | `9666e1a6…` = `9666e1a6…`, reproduced | pass, system 54.0 MHz |
| `fes.atari-st` | chip, CUDA | `28c3c016…` = `28c3c016…`, reproduced | pass, system 54.1 MHz |
| `fes.spectrum` | Powerboat, HIP | `77a3a391…` = `77a3a391…`, reproduced | pass, system 59.3 MHz |
| `fes.spectrum-plus3` | Powerboat, HIP | `50876d4b…` ≠ `7df758e4…`, **differs** | pass, system 61.9 MHz (the reseal) |
| `fes.spectrum128` | Powerboat, HIP | not applicable (netlist moved) | pass, system 55.2 MHz (the reseal) |
| `fes.pentagon` | Powerboat, HIP | not applicable (netlist moved) | pass, system 58.4 MHz (the reseal) |
| `fes.zx81` | Powerboat, Quartus | reproduced | pass, `03e2b911…` |
| `fes.zx80` | Powerboat, Quartus | reproduced | pass, `fd30d78b…` |
| `fes.ts1000` | Powerboat, Quartus | reproduced | pass, `e152b542…` |
| `fes.ts1500` | Powerboat, Quartus | reproduced | pass, `11c13c51…` |
| `fes.pcxt-dos` | Powerboat, Quartus | `104bc9b2…` = `104bc9b2…`, reproduced | pass, `2c9509aa…` |

`fes.apple2` is `reduced` and owes condition 1 only. `fes.c64-ntsc` (#2072) is
a bring-up model with no seal on main; its condition 1 is byte-identical
(`36691197830c3a2d`, both sides, both passes, `cond1-base-ebe2477/` and the
Powerboat run logs) and it owes nothing further.

Runs that did not count, kept for the record: the first runner took the newest
`core.rbf` under a build directory, which for OSS producers is a QoR-search
candidate with no build summary beside it, so its forced runs had no base
`BUILD_ID` (fixed in `3ea5f37ac`; the streams were rerun as `c2`). The first C64
base build on chip hit the 600 s route timeout of its best seed under load and
was rerun on the idle host. Neither touched what is compared.

### Spectrum +3 (moved under main's BUILD_ID; reseal and HIL)

Condition 2 failed for `fes.spectrum-plus3` alone. `probes/spectrum_synth_id.py`
runs the producer's own Yosys step with a chosen `BUILD_ID`; with main's real
id `a8aab0409443a6ca2e0de4a1fba7827d`, twice per side in one checkout, the base
gives `101bf307…` both times and the head `d5a7bf89…` both times
(`probes/plus3-build-id/`), the same pair the condition-2 builds produced. So it
is a stable mapping difference, the same effect as the 128 and Pentagon rows,
showing only under a non-zero constant. D12's last clause applies: the normal
reseal (condition 3 above, package `a1c07be2…`, `BUILD_ID` `0c42157e…`) and HIL.

## Reseal HIL (Kit A)

`hil/spec_session.py` claims Kit A in its status file only when it is logged
FREE on the baseline `~/fes-hil/baselines/current-restore.json` names (image
`179affbb…`, bundle `c9ae79e2…`) with an idle agent and a free lease. It runs
the package core-only through a private FogCast host built from the head
(`hil/specrun.py`): no install, no image change, shared host untouched. It
checks the kit's image, bundle, lease and launcher state are unchanged
afterwards, removes only the data trees the run created, and releases. Each run
launches the package with the user's ROM and `hil/make_tap.py`'s one-line tape
(`10 BORDER 1: PAUSE 0`, auto-run) as its cassette, chooses the menu's tape
loader, presses a key, types, and grabs HDMI at each step.

| Package | Running `BUILD_ID` (kit report) | Result |
|---|---|---|
| `fes.spectrum128` `7eca1fb1…` | `41ce687d…` | 128 menu; tape loads and runs (blue border); key ends `PAUSE` with `0 OK, 10:2` |
| `fes.pentagon` `451d783e…` | `2db1de12…` | the same |
| `fes.spectrum-plus3` `a1c07be2…` | `0c42157e…` | +3 menu; tape loads and runs; key ends it with `Drive not ready, 10:2` |
| main's `fes.spectrum-plus3` `91aa2657…` (control) | `a8aab040…` | identical to the head's, `Drive not ready, 10:2` |

The +3's report is its ROM's, from an empty drive A:, and main's own package
gives the same, so it is not a change. Coverage: HDMI, keyboard, the ROM's tape
path and the deck. It does not cover disk on the +3 or Pentagon, and the typed
`PRINT 22` landed while the 128K-class Tape Loader had re-entered `LOAD`, so it
shows only that keys arrive. Evidence: `hil/<run>/`.

## pc386 seal

The registered producer `build_pc386_product.py --with-lockstep --gpu-router
CUDA --seed 1`, Quartus 17.0.2 Lite on chip-30x0-ai-dev under its Lite lease,
from a clean checkout of each head:

- `0be20e575`: the fresh lockstep passed; Quartus failed setup by 1.278 ns (TNS
  -68.7 ns). Every one of the 40 worst paths ran from the mailbox storage
  opcode through two chained DSP multiplies (cylinders x heads x sectors per
  track) into the DiskCommit geometry registers of `pc386_floppy_media`
  (`pc386-seal/failed-0be20e575-worst-paths.txt`). Fixed in `b55503b48`: the
  nine formats are a table of constant sizes; the `pc386-pc-floppy` scenario
  gained `a-spt-wrong-size`, and dropping the size comparison from the RTL fails
  `storage` there.
- `b55503b48`: pass. `BUILD_ID` `6ded586169fa818056b4cdeb0ca51ad8`, RBF
  `89b9fa50…`, package `46173af6…`, setup +0.457 ns, hold +0.082 ns, recovery
  +6.147 ns, removal +0.401 ns, system 61.44 MHz, 37,274 / 41,910 ALMs.
  `verify_pc386_product_seal.py` gives `SEAL_OK` (154 files bound)
  (`pc386-seal/`).
- `73a1d6f74`: the reseal after the held-floppy fix (`pc386_system` keeps
  `ddr_hold` off while a floppy verify runs; before it, a launch with a floppy
  inserted deadlocked at the floppy poll, and `system_tb` now inserts one and
  waits for it). Pass. `BUILD_ID` `6ffb592f1940c8b14e11586dd7f59bb1`, RBF
  `bd831dc4…`, package `5a61341d…`, setup +0.751 ns, hold +0.087 ns, recovery
  +6.704 ns, removal +0.469 ns, system 61.44 MHz, 37,373 / 41,910 ALMs
  (`pc386-seal/73a1d6f74/`; `seal-check/` binds it). The later merges of main
  leave its design inputs unchanged (`head_matches_seal.py`: same design).

## Runtime test timing flake

`native_hardware_test` fails now and then on a loaded host. Each failure is a
wall-clock deadline: "FES GP response stability deadline exceeded" or the 2 s
`WaitForState`. The failing test moves between runs (lines 5164, 5274,
5556/5569, 5608/5609, 6502). ASan, UBSan and TSan runs found no memory or race
fault. Main fails the TSan `WaitForState` run too (2 of 3).

| Host | Load | Branch fails | Main fails |
|---|---|---|---|
| Powerboat | about 15 to 75 | 6/97 | 0/83 |
| chip-30x0-ai-dev, idle | about 0 to 2 | 0/60 | 0/60 |

The idle runs alternate branch and main: branch at `324dd656f`, main at
`1f563436a`, with the same runtime sources as the merge base. Both binaries
were built with g++ 13.3.

On the idle host neither side fails. The loaded-host split leans toward the
branch (one-sided Fisher p about 0.02). The new floppy tests run after the
tests that fail, and the floppy paths do nothing until a floppy is loaded. So
the most likely cause is that these deadlines are tight under CPU contention,
not a fault in the branch. This record does not change any deadline.

## Merge forward to `904140f8f`

Main was merged into the branch at `f153f04b7` (new base `904140f8f`).
`closure_touched.py 61c33cc2f 904140f8f` hits two of these producers:
`build_fes_c64_oss` (C64 RTL and its producer) and `build_fes_spectrum_oss`
(`cores/fes-spectrum/diagnostic/firmware.py`). No toolchain lock or bootstrap
changed. So, under D12's merge-forward clause:

- **Re-measured** at `904140f8f` against `f153f04b7`, two passes (`cond1-newbase/`):
  `fes.c64` `0d107b7f3a2b24de` (main's own netlist moved), `fes.spectrum`
  `29436bf7023a44f9`, `fes.spectrum-plus3` `e7e24c520633d9ff` and
  `fes.pcxt-dos` `06b2e8cf5d1d7a18`, each byte-identical, base to head.
  PC/XT was not hit, and was re-measured anyway since it costs no route.
- **Carried:** `fes.apple2`, `fes.atari-st` and the four ZX81-family machines,
  whose compiled sources and route-deciding inputs did not change between the
  two bases.

## Merge forward to `ebe2477b8`

Main was merged again at `1b9434fd8` (new base `ebe2477b8`, with #2077).
`closure_touched.py 904140f8f ebe2477b8` hits every OSS producer through
`scripts/input_identity.py`, which moves BUILD_IDs and the Yosys slot key
(`2942c6ba…` to `b51f1db3…`, still commit `3a5accbd`) but no compiled source.
The only compiled sources it hits are C64's (#2072). No toolchain lock moved.

- **Re-measured:** `fes.c64` at `ebe2477b8` against `9037ef2c0`, two passes
  (`cond1-base-ebe2477/`): `eacc394f4063d77d` on all four, byte-identical, base
  to head. Main's own C64 netlist moved again. This run used the shared
  toolchain cache's `b51f1db3…` slot, because the record's cache holds only
  the old key.
- **Carried:** every other row. Their compiled sources and route-deciding
  inputs are unchanged since `904140f8f`.
- `a480e2a1e` (after this measurement) touches only `pc386_system.sv`, which
  no row here compiles.

## Merge forward to `c3be8d0a6` and `27999e9df` (Yosys d273)

Main was merged at `d68224547` (base `c3be8d0a6`, with #2161). #2161 moved the
Apple II, Atari ST and Spectrum toolchain locks to Yosys `d273c1cb`. A moved
lock voids the earlier RBF comparison, so condition 1 was re-run for those
cores at the new base and head. Two passes, `run_cond1_cache.sh`, 32-zero
`BUILD_ID` (`cond1-d273/`):

| Artefact | Base `c3be8d0a6` | Head `d68224547` | Result |
|---|---|---|---|
| `fes.apple2` | `a37c90ecb9ba5828` | `a37c90ecb9ba5828` | byte-identical; D12 complete (reduced) |
| `fes.atari-st` | `c75967fb6bb43d84` | `c75967fb6bb43d84` | byte-identical |
| `fes.spectrum` | `191e26c2b8d890d2` | `89f00fae272e6710` | differs (the 48K now moves too) |
| `fes.spectrum128` | `53f632362d8fed95` | `d523ca04a806c932` | differs |
| `fes.spectrum-plus3` | `5a0d19f3f94481e5` | `20fd9b659c1fc116` | differs |
| `fes.pentagon` | `543232aff6a4d1c9` | `46697809fd765919` | differs |

Main was merged twice more. The head that the routes and HIL below cover is
`b67bddbd6`, merge base `27999e9df`. `closure_touched.py` from `c3be8d0a6` to
`27999e9df` hits every producer through `scripts/toolchain_cache.py` (GPU-lane
pruning of pinned nextpnr installs, #2160). That file is in every closure but
compiles nothing, and it does not change the design `BUILD_ID`s here. Main
also changed C64 RTL (#2128). No lock moved. Condition 1 was re-run at
`27999e9df` against `b67bddbd6`, two passes, `TAG=m279` (`cond1-m279/`). Each
hash equals the d273 run's: Atari ST `c75967fb…` and C64 `4e82a122…`
byte-identical; the four Spectrum models differ by the same pairs.

Routes on Powerboat (HIP), `run_cond23.sh` at base `27999e9df` and head
`b67bddbd6` (`cond23/powerboat-m279-s1..s3/`):

| Artefact | Condition 2 (base RBF = forced RBF) | Condition 3 (own build) |
|---|---|---|
| `fes.atari-st` | `d2fe3931…` = `d2fe3931…`, reproduced | pass, `BUILD_ID` `65fc2d32…`, system 56.51 MHz, package `ea48447b…` |
| `fes.spectrum` | not applicable (netlist moved) | pass, `35950ff0…`, 62.14 MHz, package `c9f9467b…` (the reseal) |
| `fes.spectrum128` | not applicable (netlist moved) | pass, `41ce687d…`, 62.70 MHz, package `9f50650d…` (the reseal) |
| `fes.spectrum-plus3` | not applicable (netlist moved) | pass, `0c42157e…`, 59.55 MHz, package `d903ca6e…` (the reseal) |
| `fes.pentagon` | not applicable (netlist moved) | pass, `2db1de12…`, 54.13 MHz, package `57e750bd…` (the reseal) |

All constraints are 52.22 MHz. The 128, +3 and Pentagon `BUILD_ID`s equal the
earlier reseals': the design identity does not name the toolchain, and their
closures did not change. Their RBFs differ because Yosys did.

Kit HIL of the four reseals, core only through `hil/spec_session.py` as in
[Reseal HIL](#reseal-hil-kit-a). The wrapper now reads the baseline from
Lander's `~/lander-hilq/current.json`, one bundle per kit (image `179affbb…`;
Kit A `02eb7d67…`, Kit B `7e3495b2…`). Its pre-run probe checks the live kit
against that before anything changes. The 48K has no menu, so it types
`LOAD ""` and `PRINT 22` in keyword mode. Every run restored the kit and
logged it FREE, verified.

| Run | Kit | Result (HDMI grabs, `hil/<run>/`) |
|---|---|---|
| `s48-m1` `fes.spectrum` | B | © 1982 screen; `LOAD ""` loads `FES HIL`, which runs (blue border); key ends `PAUSE` with `0 OK, 10:2`; `PRINT 22` prints `22`, `0 OK, 0:1` |
| `s128-m1` `fes.spectrum128` | A | 128 menu; Tape Loader loads and runs it; `0 OK, 10:2`; the typed keys land while the loader has re-entered `LOAD` |
| `splus3-m1` `fes.spectrum-plus3` | B | +3 menu; Loader loads and runs it; `Drive not ready, 10:2` (the ROM's report from an empty A:, as main's own package gave) |
| `spent-m1` `fes.pentagon` | A | 128 menu; as the 128 |

The C64's earlier routed evidence was taken at `856ef5c64`, before main's C64
RTL change, so it was re-routed at the same pair (`cond23/powerboat-m279-s4/`):

| Artefact | Condition 2 (base RBF = forced RBF) | Condition 3 (own build) |
|---|---|---|
| `fes.c64` | `1c83be1d…` = `1c83be1d…`, reproduced (second forced run) | pass, `BUILD_ID` `e6b0d8e6…`, system 52.29 MHz, package `afa94d35…` |

The first forced run failed: "no placement met timing (best seed=10
weight=2000 worst_ratio=0.9742)". Main's build had won with seed 6 at the same
weight. `probes/c64_synth_id.py` runs the producer's own Yosys step with main's
real `BUILD_ID` `41e074bb…`. Twice per side, base and head both give
`125bf819…` (`probes/c64-build-id/`), which is also the `synth.json` main's
build wrote. So the forced build routed the same netlist main's did. The
search scores a crashed or timed-out seed as 0, and that run took 2 h against
main's 85 min. A seed-6 route that did not finish is the likely cause. Its
per-seed logs were lost when the next build cleared the directory, so this is
not shown. The second forced run kept them (`c64.forced2-search/`). It stopped
at seed 6, as main's did, and its RBF equals main's byte for byte.
Condition 3 clears its constraint by 0.13%, against 54.0 MHz at the earlier
head. Expect a later identity move to fail it on some seeds.

## Floppy kit smoke

`hil/f3_smoke_session.py` installs and activates the PR's kit-software bundle
on the kit's running image. It runs the sealed `fes.pc386` package
`5a61341d…` with A: (1.44M) and B: (1.2M) from `hil/make_floppies.py`, and
`hil/hil_f3_floppy.py` drives the two launches. The wrapper then restores the
baseline bundle and checks the kit against its pre-run capture.

Run k6, Kit B: pass. FreeDOS lists and types both drives. A:'s file is copied
to `B:\WRITTEN.TXT`. The relaunch lists `WRITTEN.TXT` on B: and types A:'s text
from it. The saved B: record is a `fes.pc386-floppy.image` of B:'s size
(1,228,970 bytes) holding A:'s text twice and B:'s own boot sector and file.

The first runs (k5) showed both drives empty (serial `1FC2-0004`). The cause
was the images, not the machine. The kit's FreeDOS drive uses an older kernel
that ignores a floppy BPB without the 55AA signature, and the hand-made images
lacked it. On that drive, the sim gives the same empty drives on both the
oracle and RTL CPUs. Copying in the sim drive's kernel, or restoring 55AA,
reads both. `make_floppies.py` writes 55AA with `INT 18h` boot code, so the
BIOS goes on to the hard disk. k6 first logged `RESTORE_INCOMPLETE`: the run had
created the empty `core-data/games` directory, and the cleanup removed only its
hashed child. It was removed with `rmdir`, then the kit state, paths, image,
bundle, idle agent and free lease were re-checked live and the kit logged FREE.
The wrapper now removes that parent itself.

## Review fixes (25 Nov)

An independent review of the RTL, the runtime and FogCast found three defects.
Each is now fixed, with a test that fails before the fix:

- **Runtime:** a refused or timed-out Eject after a good save left a writable
  floppy frozen. The controller's fence is shared, so both drives stalled.
  `EjectPc386Floppy` now reports it as `save_failed`, as the generic eject
  does, and the runtime's recovery resumes the drive with the disk still in
  (`TestPc386FloppySwapSaveAndResume`).
- **FogCast:** every disk-image staging gate used the hard disk's 1 MiB
  minimum. So 160K, 180K, 320K, 360K and 720K launch floppies could not be
  staged. The gates now admit a hard-disk or PC floppy size
  (`protocol.AdmitStagedDiskImageSize`). Each launch path still checks its own
  role, and a floppy-sized image is still refused as a hard disk
  (`TestStagedFloppiesResolveFromTheKitStore`).
- **FogCast:** after a pc386 floppy insert or eject, the kit agent set its
  status to `volatile`. Its rule only knew the Atari ST and PC/XT write
  interfaces. The host then refused the result, because a bound unit needs a
  persistent status. The agent now follows the runtime: a bound persistent unit
  makes the package persistent, and otherwise the launch's mode stands. The
  kit's eject and swap time budgets use the same test
  (`TestPc386FloppyUnitsKeepPersistentMode`).

The floppy kit smoke above ran the bundle from `d68224547`, before these fixes.
It used 1.44M and 1.2M images and launch-time floppies only, so it did not
reach the paths they change.

Three RTL findings in `pc386_floppy_media.sv` are recorded, not fixed here.
The runtime never reaches them, and a fix would need a pc386 reseal:

- A Prepare or Eject sent while a store is still draining can let that
  store's `written` pulse mark the next disk dirty. The runtime always
  freezes a writable drive before Eject, and Prepares only an empty drive.
- DiskInfo flag bit 2 (writer busy) reads 0 for floppy units. The runtime
  never reads it.
- A bad heads value in DiskCommit word 3 is refused at word 4. The runtime
  sends only table formats.

The review also noted test gaps: no machine-level sim commits a floppy while
the machine runs, and `computer_storage_tb` never makes the deferred replies
wait.

## Live floppy insert and eject on a kit (25 Nov)

The review fixes change the live insert and eject path, which the floppy smoke
above did not use. `hil_f3_floppy.py` now has launch 2 live-eject B:, then
live-insert a fresh 1.2M disk (`F3C`, from `make_floppies.py`) into B: as
writable. DOS lists and types it and copies A:'s file to `B:\LIVE.TXT`, and B:
is live-ejected again. Each insert and eject runs `hil/f3live`, a Go helper
that calls FogCast's own `targetclient` `InsertLibraryMedia` and `EjectMedia`.
So the host's status check, the one that refused the result before the fix,
judges every reply. The driver holds the kit lease, and the client has no
public way to use one it did not claim. So `f3live` is built with
`adopt_lease_shim.go.txt` copied into `targetclient` for the build only. The
shim hands the client that token and nothing else. The goal needs every reply
`persistent`, B: empty then ready with a binding then empty, and a saved C
record holding `FESF3C.TXT`, `LIVE.TXT` and A:'s text.

The kit bundle is `0dd6ff58…`, built from `518d313d2`: the head `6598fa7c9`
with #2204 cherry-picked, in a scratch tree that was not pushed. The head
carries #2098 (through the main merge) but not its fix #2204. Without #2204,
every stop fails with 503 `remote input is unavailable` (#2200). Merging main
instead would have touched every core's build inputs (`bootstrap.sh`,
`toolchain_cache.py`, the C64 producer). All runs are on Kit B, with Lander's
baseline image `179affbb…` and bundle `7e3495b2…` restored, verified and
logged FREE after each (`hil/live-kit/`):

| Run | Bundle | Result |
|---|---|---|
| k7 | `bdb3bf11…` (`6598fa7c9`) | FAIL: launch 1's stop, 503 (#2200) |
| k8 | `0dd6ff58…` | stops fixed, first three goals pass; live steps failed in `f3live` (no lease; a shadowed `err` hid the insert's error) |
| k9 | `0dd6ff58…` | every goal passes, live swap included; the driver's verdict is FAIL because its cleanup `rm -r` of a run folder exited 255 (ssh's own code). The wrapper removed it seconds later and verified the kit |
| k10 | `0dd6ff58…` | PASS: every goal, C record, nothing left on the kit |

In k10 the three live replies are `persistent`. Unit 2 goes empty, then ready
with a binding, then empty. After the insert, DOS reports `Volume in drive B is
F3C` and types `F3 C DRIVE DF4EA17A`, then lists `LIVE.TXT`
(`k10/frames/live-b.png`, `live-write.png`). C's record is 1,228,970 bytes,
`FES386F1`, with both files and A:'s text.

Not covered on the kit: the failed-Eject recovery (a kit cannot be made to
refuse an Eject) and launch floppies under 1 MiB. Unit tests cover both.

## Merge forward to `afac39328`

Main was merged at `d928f71dd` (base `afac39328`). Two conflicts:
- `fogcast/service.go`: main split it into `service_*.go`. The PR's two hunks,
  launch-floppy staging and the missing-launch-floppy rejection, moved unchanged
  into `service_launch.go`.
- The DE25 proof fixture, regenerated at `6abf23a73` (14 cases, builder
  `fes-build-base:12.11-pinned`).

The FogCast suite and `native_hardware_test` pass on the merged tree.

`closure_touched.py 27999e9df afac39328` hits every producer. Most hits are
`scripts/bootstrap.sh` and `scripts/toolchain_cache.py`, and
`input_identity.input_role` gives both the `toolchain` role, so no design
`BUILD_ID` moves for them. Two design inputs changed:
- **`scripts/build_fes_c64_oss.py`** (the 8580 C64C models). The C64 was
  re-measured at `afac39328` against `6abf23a73`:
  - Condition 1, two passes (`cond1-m3/`): `ec6f98015f79940d` on all four.
  - Conditions 2 and 3 on Powerboat with HIP (`cond23/powerboat-m3-s1/`). Main's
    RBF `694a6176…` is reproduced under its `BUILD_ID`. The head's own build
    passes: `BUILD_ID` `f433ee8d…`, system 54.57 MHz against 52.22, package
    `5ddaef54…`.
- **i386 RTL (`fes_i386_x87.sv`) and two pc386 test benches, all main's.** The
  pc386 tree now has design `BUILD_ID` `f05a1803…`, so `head_matches_seal.py`
  reports a different design from the `73a1d6f74` seal. These are not the PR's
  changes, and the PR's shared mailbox RTL is unchanged since that seal.
  Deano's standing exemption for pc386 while it is in progress (1 Nov) applies:
  no reseal before merge, and the next pc386 PR that merges main reseals the
  tree.

The other cores carry their evidence: no design input of theirs changed.

Kit run k11 (Kit B, `hil/live-kit/k11/`) uses bundle `8aa12d58…`, built from
`6abf23a73` itself, so it no longer needs the scratch #2204 build. Every goal
passes, the live swap included. C's record is intact, nothing is left on the
kit, and the kit was restored and logged FREE.

## Merge forward to `102fe0fed`

Main was merged again at `bd82d27cc`. Its one conflict was
`internal/misterruntime/runtime.go`, which main split into `runtime_*.go`. The
PR's launch-floppy admission and staging moved unchanged into
`runtime_core_load.go`. `closure_touched.py afac39328 102fe0fed` reports no
closure hits, so every measured `BUILD_ID` still ships. The DE25 proof inputs
did not change, and the FogCast suite passes. The kit software changed
(FogCast, runtime), but k11's bundle predates this merge.
