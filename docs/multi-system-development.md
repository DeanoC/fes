# Pong, SNES and NES development

> Historical reference: these raw-game paths were retired by the
> [package-only cleanup](validation/2026-09-21-fpga-compat-cleanup.md).
> Current products use [described packages](core-packages.md) and
> [core persistence](core-persistence.md). Existing save files are preserved.

The selected sources implement Pong, basic SNES and a bounded native NES slice
alongside Mega Drive. Pong and SNES have dated diagnostic hardware evidence;
the selected NES image has exact assembled-image video and session-lifecycle
acceptance in the [narrow-wire record](validation/2026-09-08-native-nes-wire-acceptance.md).
The normal parent profile selects
source-built bundles for all four systems, with per-core selection records and
image-content verification.
Native cartridge save persistence is described in the [SNES save guide](snes-saves.md).
Enhancement chips remain outside this implementation.

## Normal image inputs

The default profile selects `megadrive`, `pong`, `snes`, and `nes` together. FES validates
each source-built bundle against its selected producer/source revision and recipe
hash before passing it to the existing image builder. The image installs all
four RBFs and retains per-core selection records; verification rejects missing,
changed, or unexpected cores. Historical profiles retain Mega Drive only.

The parent `native-integration-dev` profile is the starting point for this
four-system software slice. Use the same selected runtime checkout and Mega
Drive bundle, then provide the three additional sealed bundles:

```sh
make build
make verify
```

The parent profile already exports the four-system selection into the FES
`image/` recipe. Do not run FogCast `make target-image-native` as the
integration path.

The NES producer is pinned to
`https://github.com/MiSTer-devel/NES_MiSTer` commit
`9a63821173b6da4d6e95dcbe2e2a322ec8171144`, project `NES.qpf`, artifact
`releases/NES_20260823.rbf`, SHA-256
`a4c023defa4f7856585e5dba429a3b61aee3e01eb3de2c731bb0036c12f11701`, and size
`3282472` bytes. Its native contract accepts `.nes` files only, at most 32 MiB,
with one cartridge at native filetype index `0x40`. The runtime validates iNES
1.0 and NES2 headers, rejects trainers, zero PRG, impossible sizes and truncated
payloads before programming, then transfers each source byte in the narrow
`WIDE=0` wire format. FDS, UNIF/UNF, NSF,
saves, cheats and accessory peripherals are outside this slice.

## Historical three-system source-build evidence

The following three-system records predate the NES slice and remain useful only
for the exact artifact revisions listed there:

| Core | SHA-256 | Timing classification |
| --- | --- | --- |
| Mega Drive | `195fad26e792e4d023d3d73f2ab6ce94c9edfa93114d6da7ec008ee10dc72c6e` | Existing baseline; negative setup slack remains |
| Pong | `1567e5ea4db1f18b9f23b48e7a4b7604024a998ddf1378bf77fe5968e00c64d1` | Checked timing passes |
| SNES | `fdd6d3c51cf3662cb59c5250eee8d4aa48fdab14a272c756fb892677d5ff1226` | Checked seed 3; setup 0.240 ns, hold 0.243 ns |

Mega Drive retains its existing artifact qualification, including setup slack
of -3.114 ns and -1.214 ns in its timing summary. Identical hardware-tested bytes
do not make that result timing-clean. Pong and SNES exports reject failed timing;
all three exports bind the recipe used at build time. Pong also validates its
committed local sources and pinned framework before export.

## Normal assembled image validation (2026-09-06)

The selected normal image passed two independent cold builds, structural checks,
and the QEMU packaging smoke test. Both builds produced the 64 MiB root filesystem
image SHA-256 `9af1a0140acb36a689e9de9101806d1ad6f879a7cbc8e307aca9df7b6b3bea03`.
QEMU checks packaging and userspace boot, not FPGA behavior. This artifact is a
root filesystem image for the existing board boot setup, not a complete SD-card layout.

The tested component revisions are:

| Component | Revision |
| --- | --- |
| FogCast | `02378114c25a4da26e424630b22146a88249cd4e` |
| misteross | `830937d0ebb6f0bf6a0b83cf519843c3445d3c45` |
| libmister-runtime | `960e61ece108d996eae4e09566b9fdc56c4ce952` |
| mister-packages | `b5a92e511a111c428f1f39057e3c6386e9d19c81` |

The first build was deployed under a kit lease while the second build ran. Its
bytes match the final verified output exactly. On the designated kit, Pong →
SNES LoROM → Mega Drive → SNES HiROM → Pong completed on one boot, with nonzero
HDMI audio for each game and Stop returning to idle and releasing ownership.
Extended interactive checks captured Super Mario World movement/B jump at Yoshi's
House, Sonic 2 movement/A jump in Emerald Hill, and Fievel movement/B jump in stage
one. These extended checks followed a separate reboot; the installed image hash
was rechecked afterward. The kit was left ready with ownership free.

Local evidence is in `out/three-system-image-evidence/`: `acceptance.json`,
`hardware-results.json`, build/test logs, selection and verification records,
and HDMI captures. The normal image is `out/native-integration-dev/linux.img`.
Keep the evidence with its exact inputs rather than attributing acceptance to
later component revisions. Raw captures and ROMs are not committed.

Warm `make dev` reused the verified Buildroot tree, rebuilt the runtime package,
and assembled a separate structurally checked diagnostic image. A second,
unchanged invocation reused all three validated RBF bundles and reported
“nothing to rebuild.” The container cache key also now ignores checkout location
when its actual input files are identical. Cold `make build` still deliberately
uses two independent passes to check reproducibility.

Parent tests: 29 passed. FogCast's full `make test` passed, with its Chrome
integration fixture skipped because Chrome was unavailable. The basic SNES
scope and Mega Drive timing limitation above still apply.

The subsequent review fix selects the existing macOS host selector during
host-side verification and ensures the native-fetch target builds it on Darwin.
Linux/container selection retains its previous executable. The selected FogCast
pin includes this portability fix, validated with simulated Darwin/Linux routing
and real extra-core fixtures (no native macOS run); the cold-build and hardware record above remains tied to the listed
pre-fix revision. No new hardware acceptance is inferred for a later revision.

## Working locations

The initial implementation used parent branch `feat/pong-snes` and isolated
component worktrees beneath `out/dev/pong-snes/`:

| Worktree | Scope |
| --- | --- |
| `mister-packages` | Generated system/media/input contracts and source pins |
| `libmister-runtime` | Native profiles, cartridge transfer and physical input/lifecycle |
| `FogCast` | Built-in Pong title, native adapters and network input |
| `misteross` | Pong game/wrapper, simulations and pinned FPGA builds |
| `Template_MiSTer` | Read-only upstream wrapper reference |
| `SNES_MiSTer` | Read-only upstream SNES release reference |
| `Main_MiSTer-reference` | Read-only cartridge-transfer comparison reference |

Parent `sources/` gitlinks select the merged component implementations.
`make check` and `make dev` operate on those selected revisions. Use separate
worktrees for further edits; retain the original `out/dev/` workers and frozen
diagnostic evidence rather than deleting them as caches.

Run checks from FES:

```sh
make -C sources/mister-packages test
make -C sources/misteross sim-pong VERILATOR=/absolute/path/to/verilator
```

The simulation command reuses an installed Verilator and does not bootstrap
the FPGA toolchain. It tests the game and raster separately; it does not compile
a board wrapper or produce an RBF.

Read the [design](superpowers/specs/2026-09-06-pong-snes-design.md) and
[implementation plan](superpowers/plans/2026-09-06-pong-snes.md) before picking
up a component task. Keep ownership as defined in [the agent guide](agent-workflow.md).

## Decisions from source inspection

Pong uses an ordinary native game launch with system `pong`, expected identity
`Pong`, installed artifact `pong.rbf`, and empty media. Runtime protocol 1 already
supports that representation. The development-RBF operation leaves HDMI down
and does not establish a playable input session.

The [upstream arcade Pong](https://github.com/MiSTer-devel/Arcade-Pong_MiSTer)
documents analog controls. The first FES Pong game instead uses Up/Down and
Start against a simple opponent. Its board wrapper uses the pinned template and has been built and exercised on the kit.
The inspected [MiSTer template](https://github.com/MiSTer-devel/Template_MiSTer/tree/3ea1134cf05d62c2b1db30362277a823d739ced2)
provides an existing framework and 20 MHz game clock. It has no release RBF;
do not fabricate an upstream artifact hash to fit the existing source schema.

Package C++ emission previously redeclared shared structures in every system
header and emitted a nonstandard empty array for ROM-less systems. The emitter
uses a versioned shared-type guard and a null media pointer with zero count.
Go emission now omits a nonexistent cartridge index. SNES adds an explicit
media transform and X/Y/L/R/Select fields; all generated C++ headers now use the V2
shared layout. Regenerate consumer system
headers together when integrating; generated values remain package-owned.

## SNES source and cartridge contract

The package pins the upstream [20260823 release](https://github.com/MiSTer-devel/SNES_MiSTer/tree/93d359e6f23c734ae3928984e88bed1d9b53cbac):

```text
commit: 93d359e6f23c734ae3928984e88bed1d9b53cbac
project: SNES.qpf
artifact: releases/SNES_20260823.rbf
sha256: 0c13347c0939f597ead5f1f835532d4508b10613e82e7316e902eabe712a9c83
size: 4446024
```

This is a verified upstream source/artifact identity, not native support. The
first local Quartus rebuild missed setup timing by 0.064 ns. Seed 2 also failed;
a separate seed 3 trial passed with minimum setup slack 0.240 ns and hold slack
0.243 ns. Only the staged fitter seed changed; RTL and constraints were retained.
The first passing artifact was a diagnostic override. The selected normal recipe
now fixes SNES to seed 3 and rejects failed timing; a fresh build reproduced the
same RBF bytes. The selected runtime implements the bounded native
cartridge transform and one-player controller path.

The native contract selects cartridge index 1 (the core also accepts index 0,
which the existing Main adapter retains), with one continuous download containing a
512-byte generated metadata prefix followed by ROM bytes. `SNES.sv` reads that
prefix at addresses 0 and 2, and subtracts 512 from subsequent cartridge addresses.
The raw Mega Drive transfer path is therefore insufficient.

For basic LoROM/HiROM, prefix byte 0 encodes RAM/ROM size exponents, byte 1 the
mapping, and byte 3 bit 0 PAL. Main additionally writes the detected header offset
at bytes 4–7 and original headerless ROM length at bytes 8–11, little-endian.
Its detector scores cartridge headers; non-power-of-two ROMs require mirroring,
not zero-padding. Trace the pinned
[header implementation](https://github.com/MiSTer-devel/Main_MiSTer/blob/915ca3395aa5a26322007974faa757299a56b856/support/snes/snes.cpp)
and [transfer path](https://github.com/MiSTer-devel/Main_MiSTer/blob/915ca3395aa5a26322007974faa757299a56b856/user_io.cpp#L2811)
before implementing the native adapter. Ordinary LoROM/HiROM needs no separately
uploaded firmware. Enhancement-chip, BS-X, Sufami, SPC and MSU acceptance are
separate from the first basic-cartridge slice.

The pinned core's digital joystick masks are:

| Right | Left | Down | Up | A | B | X | Y | L | R | Select | Start |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 0x001 | 0x002 | 0x004 | 0x008 | 0x010 | 0x020 | 0x040 | 0x080 | 0x100 | 0x200 | 0x400 | 0x800 |

These require matching package fields, runtime input decoding and FogCast virtual
gamepad capabilities. Preserve the existing Mega Drive button mapping.

## Current next integration step

The four-system image and corrected NES wire path are now integrated and
accepted on the designated kit. The next work should extend the NES slice only
after choosing a bounded feature (for example explicit controller movement or
additional mapper support) and should keep exact-image evidence separate from
the current record.

## Historical next integration work

1. Complete the bounded SNES cartridge adapter and full controller map, with
   rejection before programming for unsupported or malformed cartridges.
2. Qualify the SNES artifact and extend image selection/installation with source
   and hash checks. The current diagnostic image is separate from parent builds.
3. Exercise basic LoROM and HiROM cartridges, then all three systems and their
   Stop/relaunch transitions without rebooting.
4. Select compatible component commits only after review and authorization;
   record exact-artifact acceptance of that selected combination.

Use focused tests and Verilator during development; reuse retained compiler/base
caches for integration. Save persistence and enhancement-chip support are outside
this first SNES slice.

## Initial checks, 2026-09-06

- Package-worker `make test` passed, including the existing oracles, new SNES
  source validation, strict C++14 combined/empty-media header compilation, and
  ROM-less Go emission. Mega Drive launch values remain unchanged.
- Builder-worker `make sim-pong` passed for both game and raster with Verilator
  5.051 and `-Wall`, reusing the existing installed compiler. Tests exercise
  center/edge paddle returns, both scoring sides, held-Start behavior, RGB and
  tone expiry, plus two complete raster frames. The final log is
  `out/dev/pong-snes/misteross/build/sim/pong-validation.log`.
- SNES package/builder pins agree with the exact release checkout and RBF hash.
  Core-lock tests and the named SNES lock check passed.
- The builder's broader Python suite ran 267 tests but hit 47 errors and one
  skip in the isolated worktree, including missing
  `build/toolchain/install/bin/yosys` in manifest tests. That suite is not green;
  the complete local FPGA toolchain was not rebuilt or linked into this worker.
  Its six focused blinky-source regression tests passed.
- Parent `make test` passed all 22 tests and `make check` passed for the unchanged
  selected Mega Drive combination. This is separate from worker validation.
- Read-only review found no remaining blockers in the emitted definitions or
  final game/raster code. The following later diagnostic extends these initial
  software checks without changing parent pins.

## Pong hardware diagnostic, 2026-09-06

The designated MiSTer Pi at `192.168.10.239` ran a separate worker image with
source-built Pong, the native runtime, and the target agent. A temporary host
API used an isolated copy of the library database; the normal host API and
catalog were preserved. The temporary API was stopped after testing, and the
kit was left idle.

Observed through HDMI capture and the normal host/target input path:

- Built-in Pong discovery and ROM-less launch, visible paddles/ball/scores.
- Up and Down move the player paddle; Start begins a rally. Input logs show
  zero sequence gaps and zero state resynchronizations.
- HDMI audio contains collision pulses with a measured dominant frequency of
  1,000 Hz. The runtime now explicitly unmutes framework audio before releasing
  the core from reset; the framework boots muted.
- Stop and relaunch reset the score and paddle positions.
- Pong → Stop → Mega Drive (Sonic 2) → Stop → Pong works in one boot. Sonic 2
  reaches gameplay, accepts movement/jump, and produces captured HDMI audio.
- The final Stop returns idle. Boot ID remained
  `b7d131bd-c18c-4bbd-896b-dece631ee8d9` throughout the sequence.

Exact diagnostic SHA-256 identities:

| Artifact | SHA-256 |
| --- | --- |
| Pong RBF | `1567e5ea4db1f18b9f23b48e7a4b7604024a998ddf1378bf77fe5968e00c64d1` |
| Runtime | `0dc895d957f40c371f9efafdf24235cd08978fedf59478b12bc389d5fe205f0c` |
| Target agent | `f694c1d2e26e0728dabb0f7e95b2b2fda07953de49c257548a60584a6a02d22d` |
| Host API | `09680a5d949c05df0fe4dad059638c2ffaf893af09cdd0f1f800a33e7ce687e9` |
| Diagnostic image | `a10281f3b04f49ef5bbdcb9c5fb7d5fa6cced2fbd9c346fce4f519506942c187` |

Pong used Quartus 17.0.2: zero errors, minimum setup slack 0.399 ns and hold
slack 0.247 ns. The pinned template's `sys/` files remained unchanged. Runtime
software tests and the affected FogCast tests passed before the binaries were
built; the package tests and Pong simulation also passed.

Local evidence is retained under `out/pong-native-diagnostic/`: image manifests,
source snapshots and diffs, installed hashes, API responses, input logs, PNGs,
and the `pong-play.mkv` / `megadrive-play.mkv` video/audio recordings. The
baseline image SHA-256 remains
`670ed8d9707728afcbbab1724aef019f6a137fa2470718c8f1677377f4f7b998`;
the kit also retains it as `linux.img.fes-megadrive-baseline`.

This is hardware evidence for the recorded uncommitted worker snapshot, not
acceptance of unchanged parent gitlinks or a published three-system release.

## SNES and three-system hardware diagnostic, 2026-09-06

The basic SNES worker integration passes package tests, the full runtime suite
and the affected FogCast suites. Independent consumer reviews found no blocking
issue. The runtime accepts ordinary coherent LoROM/HiROM cartridges with
power-of-two payloads up to 4 MiB, optional validated copier headers, and RAM
exponents up to 7. RAM is volatile; persistent saves and enhancement chips are
not implemented in this slice. Extra buttons with zero masks are harmless
no-ops on Mega Drive/Pong.

The seed 3 source-built SNES RBF was installed in a second diagnostic image:

| Artifact | SHA-256 |
| --- | --- |
| SNES RBF | `fdd6d3c51cf3662cb59c5250eee8d4aa48fdab14a272c756fb892677d5ff1226` |
| Runtime | `f57424f378eaf3a54b8d8db03fee0c92452e28e67f7452efd5f0e840eb467fe1` |
| Target agent | `19b34092688ea668f2ede3076299f3241bc5804bf22fb9d805d1561947325756` |
| Host API | `3c444851a33e4585f42293fcf99b8bc2d701b4196d3bc7d620953d2137cab35e` |
| Diagnostic image | `23a1be3603dfe16b30790261eca4253a4a917f212d97a5625cd295c605477f12` |

Installed runtime, agent and both new RBF hashes were rechecked after the kit
became available. On boot `0db9a961-d92e-4e0a-b45e-701242e5d0a1`:

- Super Mario World (LoROM) reached Yoshi's House through public launch/input.
  Left + B moved Mario and jumped into the message block; the captured frames
  show the changed position, airborne sprite and resulting message.
- Pong accepted Up/Down/Start after SNES Stop, with visible gameplay.
- Mega Drive launched Sonic 2 after Pong Stop; the first input sequence selected
  two-player mode and reached its split-screen stage. A subsequent one-player
  run confirmed Start, pause/unpause, movement and A jump in Emerald Hill.
- Fievel Goes West (HiROM) launched after Mega Drive Stop. Start entered stage
  1-1; Right + B produced visible movement and a jump.
- HDMI audio captures for both SNES titles, Pong and Mega Drive have nonzero
  samples; measurements are in `resumed-audio-analysis.json`.

The final Super Mario World relaunch reached its title. Final Stop returned idle
and health reported ready with the same boot ID. The temporary diagnostic host
API was stopped; the normal host API was left untouched.

Earlier attempts were interrupted by a user-confirmed manual reboot and an
external Stop/development-RBF load. Those interruptions are not evidence of a
runtime crash. At that stage kit sharing was still proposed. The selected
components now enforce renewable session ownership for game and development-RBF
operations; see [kit sharing](kit-sharing.md).

Evidence and frozen source archives are in `out/snes-native-diagnostic/`.
The seed 3 receipt and timing summaries remain in
`out/dev/pong-snes/misteross/build/rebuild/snes-seed3/`. This is an explicit
staged seed override from the diagnostic phase. Subsequent integration made
seed 3 the checked normal SNES recipe and added three-core image selection.
The new normal builds reproduced all three diagnostic RBF hashes. The frozen
Pong recipe snapshot remains preserved because its hash is part of that older
artifact provenance.

A later parent pin of misteross `cb89517` reused those same three RBF hashes in
a diagnostic `make dev` image. That selection's kit checks are in the
[dual-PLL native diagnostic](dual-pll-native-diagnostic.md).
