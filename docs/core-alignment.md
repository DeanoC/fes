# Core alignment

This is the currency contract for described FPGA cores. The ABI and image
matrix stays [core status](core-status.md). This page says which choices are
the standard, which cores still follow an older path, and the order for
retiring those paths.

The core, CPU and display choices below were recorded at `6d9939c2`; image
contents were refreshed at `714108a9` (22 October 2026) and lock pins at
`4546d822` (#1007, 23 October 2026). Issue
#843 updates the lock-file layout while preserving those toolchain pins. A
change that moves a lock, CPU or display contract updates this page in the
same commit. A dated note under
[validation/](validation/) accepts only the artifact it names.

## Rule

For an alternate implementation of the same core, the later choice is the
standard. Move consumers onto it. Do not keep an obsolete mechanism and call
it a variant. The accepted Quartus policy is different: it allows a core to
have a labelled build-toolchain variant, without changing its core ABI or
silently replacing another build.

`fes_z80_fast` is a documented-only development build. Splash is board
firmware. ZX81 session-display is an in-session plane. Quartus may be a
labelled product toolchain variant under the accepted
[Quartus-built cores policy](design/quartus-cores.md), or an oracle where a
recipe says so.

Do not add a core by copying the historical path of an older sibling. A new
core uses the standards below.

## Standards

These standards record the accepted core policy and the implementation state
of the current tree.

1. FES allows both OSS (`oss`) and Quartus (`quartus`) as product toolchains
   (policy accepted in #1163; see
   [Quartus-built cores](design/quartus-cores.md)). Current registered recipes
   and package selection still build only the HIP/nextpnr OSS lane. A recipe
   that later offers both lanes must select one explicitly, use a pinned host
   authorized for the Quartus edition and device (with a license when required),
   and pass the same timing and seal checks. A failed HIP
   route never falls back to Quartus. Where a recipe still says oracle only,
   Quartus remains a check and does not produce the installed package.
   `FES_OSS_ONLY` is a proposed rollout option, not a control in this tree.
2. Registered producers use per-core locks named by `lock_path` in
   `config/core-recipes.toml`. Five locks match the repository-level
   `sources/misteross/toolchain.lock`: Yosys `3a5accbd`, Mistral `8fcc4cb4`,
   nextpnr `9128800f`, deliberately before nextpnr #172 (Catch, menu, RAM
   Tester, SG-1000, SMS). Apple II, C64 and Spectrum keep that Yosys and
   Mistral with nextpnr `5fe55f3d` (nextpnr #276, #1007) for its F0/F1
   boundary `$ROUTETHRU` map; those three locks share one cache slot. Five
   registered locks each have their own slot: Pong pins nextpnr `552ba246`
   (fes#887), ZX81 pins nextpnr `1eecf877` (#936), RISC-V pins nextpnr
   `8f453ebc` (#945), Atari ST pins nextpnr `5fe55f3d` (#910), and Coleco pins
   Yosys `5391eeb1` with nextpnr `bec82e36` and the shared Mistral. The
   unregistered PC/XT diagnostic also sits on `552ba246` (#840). HIP
   architectures are `gfx1100;gfx1201`. Pin changes stay scoped to the
   affected core; a lock is that core's last known good, not a required pin.
   The repository-level lock remains for unregistered consumers.
3. Registered M10K is synchronous. No core under `sources/misteross/cores`
   sets `CFG_ASYNC_READ(1)`. Do not reintroduce it.
4. The Z80 product CPU is `fes_z80_nmos` at native cadence. `fes_z80_fast` is
   not a second product CPU. TV80/T80 is the historical selection still
   instantiated by Coleco and SMS. ZX81, SG-1000 and the Spectrum models
   select `fes_z80_nmos`. A sibling already on the first-party
   CPU does not qualify the others. There is no newer first-party 6502 or
   68000; `cpu6502` and `fx68k` stay until a replacement exists. Do not add a
   third Z80.
5. One display pipeline: machine source, then an optional video part, then the
   shared display plane, then the HDMI pins. [The display pipeline](display-pipeline.md)
   is the design and lists which shells have each stage.
   - An application or ROM-less shell uses shared `fes_video_720p`. That is
     Pong, Catch, the demos, RAM test and RISC-V. The optimizer-generated
     scaler is that module. Do not force video parts onto these shells.
   - A machine shell that emits a raster source uses linked Direct/Scanlines
     parts and does not own HDMI timing. Coleco native is the standard: no
     built-in Direct fallback; a missing or invalid part blocks launch.
     Atari ST's built-in Direct fallback is a migration aid, not the end state.
   - Launcher overlays and surrounds use `fes.video.session-display`, the
     shared `fes_display_plane` after the part. It is not a video part. ZX81,
     the Spectrum models and Coleco native have it; each other shell moves in
     its own change.
   - Splash remains the pinned idle bitstream. Menu scanout does not replace
     it until its own acceptance gate.
6. An external ROM uses a sealed ROM map. Format 3 is that shape: ZX81, SMS,
   SG-1000, Apple II, C64, Spectrum, Atari ST and the factory Coleco cartridge.
   Coleco format 3 is one exact 16 KiB cartridge, mirrored at `0x8000`. Pad a
   shorter cart with `0xff`. Format 2 remains for ROM-less packages. Format 4
   is the Coleco MegaCart development producer. It is not a third cartridge
   mechanism to copy.
7. Functional identity is 2. Factory producers use audited per-producer source
   closures. A producer moved onto the shared lock adopts an audited closure
   in the same change if it does not have one.
8. Simulation, a sealed package, a kit diagnostic and factory-image acceptance
   stay separate. Alignment does not carry an older note onto new bytes, and
   it does not add a package to the factory image.

## Standing

The bootstrap image installs `fes.menu` only (#919, 20 October 2026).
Playable packages are catalog installations through the core library, and
`config/core-library.toml` registers them with a standing: supported
(`fes.pong`, `fes.zx81`, `fes.coleco`, `fes.sms`, `fes.sg1000`,
`fes.ramtest`), experimental (`fes.spectrum`, `fes.spectrum128`,
`fes.spectrum-plus3`, `fes.pentagon`, `fes.zx80`, `fes.ts1000`, `fes.ts1500`,
`fes.atari-st`, `fes.c64`, `fes.apple2`,
`fes.riscv`) and demo (`fes.catch`). Registration is not acceptance, and no
playable package is in the image. The first table below is the former
nine-package image set; its evidence limits are unchanged.

| Package | Usable as | Matches | Does not match |
| --- | --- | --- | --- |
| `fes.menu` | Idle scanout. Not a game. Splash is still the product idle fallback. | Current lock: `cores/fes-menu/toolchain.lock`. | No factory-image acceptance of this bitstream. |
| `fes.pong` 1.1.0 | ROM-less game. Settings and best rally persist. The [10 Oct kit B load](validation/2026-10-10-optimizer-video-area.md) showed the field and Stop. | `cores/fes-pong/toolchain.lock`, single-PLL gate, optimizer scaler. | That note is a bounded video/Stop check, not gameplay or image acceptance. |
| `fes.zx81` 1.7.0 | Most complete 8-bit machine in the library: live `.p` tape, BASIC SAVE to a library cassette, session display, bus 2.0. The [2 Oct image diagnostic](validation/2026-10-02-zx81-session-display.md) showed cassette swap with BASIC kept. | `fes.computer` 1.0, `fes_z80_nmos`, sync M10K, shared display plane, format 3. Current lock: `cores/fes-zx81/toolchain.lock`. | The 2 Oct note covers the earlier opaque plane and does not accept a shared-pin seal. The 1.6.0 display plane, physical keyboard, expansion and audio are not accepted. Completed SAVE images still need a stored library cassette. |
| `fes.zx80` 0.1.0, `fes.ts1000` 0.1.0, `fes.ts1500` 0.1.0 | Experimental package-only shells of the ZX81 machine. Not in the factory image. | One shell, `fes_z80_nmos`, sync M10K, format 3. Lock `cores/fes-zx81/toolchain.lock`. | No sealed package and no hardware acceptance. ZX80 is PAL, 4 KiB ROM, 1 KiB RAM, no NMI. TS1000 is NTSC, 8 KiB ROM, 2 KiB RAM. TS1500 is NTSC, 8 KiB ROM, 16 KiB RAM. |
| `fes.coleco` 1.4.0 | Reduced ColecoVision. Native video parts are mandatory. Optional SGM. One exact 16 KiB cartridge, padded with `0xff`. Launcher overlay and surround through the session display. | `fes.application` 1.0, native video parts, shared display plane after the part, sync M10K, format 3. Current lock: `cores/fes-coleco/toolchain.lock`. | Still TV80. Not retail-complete. The 3 Oct note does not accept these bytes. The 1.4.0 display plane is not accepted on a kit. |
| `fes.sms` 1.4.0 | 32 KiB Master System slice. No mapper. Do not use `fes.mastersystem`. | Sync M10K, format 3. Current lock: `cores/fes-sms/toolchain.lock`. | Still TV80. Named kit audio is artifact-scoped and does not accept a shared-pin seal. |
| `fes.sg1000` 1.3.0 | 16 KiB slice. | `fes_z80_nmos`, sync M10K, format 3. Current lock: `cores/fes-sg1000/toolchain.lock`. | The 1.2.0 kit audio note does not qualify 1.3.0 or a shared-pin seal. |
| `fes.spectrum` 0.4.0, `fes.spectrum128` 0.2.0, `fes.spectrum-plus3` 0.2.0, `fes.pentagon` 0.2.0 | Experimental library packages; no longer in the image. Open diagnostic firmware only; no Sinclair or Amstrad ROM is shipped. | One shell, `fes_z80_nmos` with ULA clock stretching, session display plane, tape 2.0 played from HPS DDR port 1 (`fes_media_ddr`), sync M10K, format 3. Current lock: `cores/fes-spectrum/toolchain.lock`. | Timing matches Fuse in simulation, and the host's tape conversion matches Fuse on its fixtures. The [22 Oct Kit A diagnostic](validation/2026-10-22-spectrum-models-seal.md) booted each named ROM and ran a typed BASIC line. On kit A the superseded `7589e281` archives of these versions loaded a turbo-loader tape from DDR on the 48K, 128K and Pentagon; that +2A/+3 archive loaded it too and held HDMI once the runtime moved the ADV7513 input clock delay to −0.8 ns (#1011). Joysticks, AY music and expansion remain untested on kit. |
| `fes.ramtest` 1.1.0 | SDRAM and HPS DDR tester. Supported library utility using the OSS 100 MHz variant; not in the bootstrap image. | Current lock: `cores/fes-ramtest/toolchain.lock`. | 130 MHz remains open. No exact-image hardware acceptance. |
| `fes.atari-st` 0.2.0 | Library standing experimental; no longer in the image. GEM, mouse, writable geometry-aware disks and video parts have passed on named artifacts. Not a compatible ST. | Video parts, sync M10K, format 3. Current lock: `cores/fes-atari-st/toolchain.lock`. | The October kit notes do not accept a shared-pin seal. Built-in Direct fallback remains. Borders, later demo sections, menu flash, audio fidelity and full current-image acceptance are open. |

| Package | Usable as | Matches | Does not match |
| --- | --- | --- | --- |
| `fes.riscv` 0.1.0 | Original RV32I demo. No historical machine, no audio, no user programs. | Current lock: `cores/fes-riscv/toolchain.lock`. Qualified ALU package `c6c11fcd…` (build `a7118693…`, [receipt](validation/riscv-timing-alu-2026-10-11.json)) closed 74.25 MHz. Its both-kit loads showed the scene, timer colour and Stop. | Not in the image. Those ALU loads did not test the gamepad. The [11 Oct video diagnostic](validation/2026-10-11-riscv-video-diagnostic.md) of baseline `72f3b2be…` (build `39ccdf04…`) is a static-scene and Stop check; candidate `85bf8676…` there is a video comparison only. Neither is a gamepad check. The [4 Oct kit A load](validation/2026-10-04-riscv-kit-a.md) of `e5c4bd79…` (build `55de600f…`) observed dpad-right. No seal accepts another package. |
| `fes.c64` 0.1.0 | Package-only pathfinder. The [6 Oct Kit A diagnostic](validation/2026-10-06-c64-functional-closure.md) passed firmware, both probes, input and a read-only D64 on that package. | Sync M10K, format 3. Shared-pin retirement is complete. Sealed on `toolchain.lock`: package `1d599353eaef5502…` (build `6f865fddebe62184…`), seed 5, CUDA lane on the 3090, final signoff system 54.29/52.224, pixel 75.10/74.25, audio 229.67/12.288. That CUDA seal is the recorded pin result. Audited static source closure. | The 6 Oct diagnostic used nextpnr `0c5ed400` and does not accept this pin. No kit evidence exists for these bytes. The static closure is not a compiler-read derive. No disk writes. Not in the image. Keyboard transposition ([#750](https://github.com/DeanoC/fes/issues/750)) is unrelated to the pin. |
| `fes.apple2` 0.1.0 | Package-only pathfinder. A Sept 27 kit diagnostic covered linked firmware, four cards and a live disk swap. | Sync M10K, format 3. Current lock: `cores/fes-apple2/toolchain.lock`. Audited static source closure. | The Sept 27 note does not accept a shared-pin seal. Product default remains HIP. The shared-pin route reported with the lock move was exploratory and is not a recorded seal, so this pin is still open. A recorded compiler-read derive of the source closure waits on that seal. No writes. No Apple ROM. No image acceptance. |
| `fes.catch` 1.0.0 | Registered ROM-less paddle demo. | Current lock: `cores/fes-catch/toolchain.lock`. Sims cover the optimizer scaler. | Not in the image. No separate kit acceptance. |

`fes.demo`, `fes.demo-media` and `fes.demo-audio` are references. Catch is the
registered game on that shell. Splash is `sources/misteross/sealed/fes-splash.rbf`:
720p60, GPU router off, no user-io. `cores/pong` is a simulation module, not
`fes.pong`. There is no NES, SNES or Mega Drive package.

What a person can use, with those evidence limits: Pong, then ZX81 inside its
1 KiB / `.p` / bus 2.0 contract. Coleco, SMS and SG-1000 are fixed-map slices,
not cartridge-compatible machines. The Spectrum models are package-only.
Atari ST boots GEM and moves disks on named builds. RISC-V closed on its own
newer pin (`8f453ebc`, #945) and remains package-only. C64 is
the strongest package-only computer diagnostic. Its shared-pin retirement
is complete. Keyboard transposition
([#750](https://github.com/DeanoC/fes/issues/750)) is unrelated to that pin.

## Retirement order

Do not batch a pin move and a CPU swap in one seal unless the task says so.
Each step ends with a clean-tree seal of that core on the target pin and
the focused simulation. HIP and CUDA lanes are equivalent for that pin
result (Deano, 2026-10-13): a CUDA seal is valid on its own, and HIP only
adds GPUs. Kit evidence is a separate task. It does not ride along. An
older kit note does not accept the new bytes.

### 1. Per-core lock files

Issue #843 adds one lock file for each registered core. Since #882 each lock
can pin its own nextpnr, provided `scripts/pnr_recipe.py` classifies that
nextpnr's options and the core's recipe lists them. Coleco's Yosys pin is
`5391eeb1`; Pong, ZX81, RISC-V and Coleco pin their own nextpnr; Apple II,
Atari ST, C64 and Spectrum pin nextpnr `5fe55f3d`; the other five registered locks
are byte-identical to the repository-level lock. Keep the files at the registered
`cores/fes-<core>/toolchain.lock` paths and route future pin changes through
that core's lock and recipe. A new Z80 machine instantiates `fes_z80_nmos`. A
new external ROM uses format 3. A new shell picks one of the two display
contracts.

### 2. Current pins

Eight registered locks differ from the repository-level pin (Yosys
`3a5accbd`, Mistral `8fcc4cb4`, nextpnr `9128800f`): Pong on nextpnr
`552ba246`, ZX81 on `1eecf877`, RISC-V on `8f453ebc`, Apple II and C64 on
`5fe55f3d` (#1007), Spectrum on nextpnr main `bd7a1c0d`, Atari ST on
`5fe55f3d` (#910), and Coleco on Yosys `5391eeb1` with nextpnr `bec82e36`;
the unregistered PC/XT diagnostic is also on `552ba246`. The other five match
it. The following historical seal records remain tied to
the artifacts they name; current recipe lock paths are shown in
`config/core-recipes.toml`.

| Order | Core | Current lock / recorded pin | Why this order |
| --- | --- | --- | --- |
| 1 | `fes.c64` | `cores/fes-c64/toolchain.lock` (nextpnr `5fe55f3d` since #1007; recorded pin `9128800f`) | The recorded CUDA package `1d599353eaef5502…` (build `6f865fddebe62184…`) predates the per-core lock layout. Supporting evidence: a HIP build of [#749](https://github.com/DeanoC/fes/pull/749) head `8b713d5d5716f47bfbc58582957e77ade4878565` on the same commits (Yosys `5391eeb1`, Mistral `8fcc4cb4`, nextpnr `9128800f`), on Powerboat's RX 7900 XTX, package `22ecf46827b3f57b55f7b5e5187167c7177bf29dc931870721aba629639a9dbf`, build `4c90ff27c072d8fbfa81366f2bd7d9a1`, timing system 54.51/52.22, pixel 82.79/74.25, audio 215.33/12.29. The [kit B HIL](https://github.com/DeanoC/fes/pull/749#issuecomment-6229821313) showed BASIC READY and clean video. The 6 Oct kit note does not accept the CUDA bytes. Keyboard transposition ([#750](https://github.com/DeanoC/fes/issues/750)) is unrelated. |
| 2 | `fes.atari-st` | `cores/fes-atari-st/toolchain.lock` (nextpnr `5fe55f3d` since #910, own cache slot) | Factory. The October kit notes do not accept a shared-pin seal. |
| 3 | `fes.coleco` | `cores/fes-coleco/toolchain.lock` (Yosys `5391eeb1`, nextpnr `bec82e36`: current main includes #296 and #299 and retains #289, #290 and #292, own cache slot) | Factory, cart fence, native video. `toolchains/coleco-expansion.lock` moved onto the shared pin with the socket diagnostic and is not a second selected lock. |
| 4 | `fes.apple2`, `fes.spectrum` | `cores/fes-apple2/toolchain.lock`, `cores/fes-spectrum/toolchain.lock` (Apple II on nextpnr `5fe55f3d` since #1007; Spectrum on nextpnr main `bd7a1c0d`) | Same multi-region cart support. Both files started at the shared commits. |
| 5 | `fes.sms`, `fes.sg1000` | `cores/fes-sms/toolchain.lock`, `cores/fes-sg1000/toolchain.lock` | Both files start at the shared commits. The old Coleco producer selects the repository-level lock. |
| 6 | `fes.zx81` | `cores/fes-zx81/toolchain.lock` (nextpnr `1eecf877` since #936) | Bus 2.0 and session display. The 2 Oct note does not accept a shared-pin seal. |

The recipe `lock_path` selects each per-core file, and its producer
authenticates that file. Pong, ZX81 and RISC-V have moved their nextpnr pins
forward under the per-pin timer tracker (#876). Apple II and C64 use nextpnr
`5fe55f3d` for its F0/F1 boundary route-through map (#1007), and Atari ST
followed (#910). Spectrum uses nextpnr main `bd7a1c0d`, which contains #276 as
merged; Coleco uses `bec82e36` and keeps its own Yosys. The other five
registered locks still share the repository-level slot.

The 130 MHz timing diagnostic uses `cores/fes-ramtest/toolchain.lock`. OSS has not closed 130 MHz.
The shared-pin seed-2 route reached 110.91 MHz on `ram_clock.clocks[0]`. That
miss is known and is not a private pin.

### 3. One Z80

SG-1000 is the pattern. ZX81 and the Spectrum producer also select
`fes_z80_nmos`. That does not qualify a sibling still on TV80.

1. SMS. Fixed cartridge, no expansion bus.
2. Coleco. Clock enable, VDP NMI, bus 2.0.

Preserve each machine's existing cadence and interrupt wiring. Do not switch
a product recipe to `fes_z80_fast` to make timing easier.

When the last TV80 consumer is gone, delete the per-core TV80/T80 copies from
the product producers. A Quartus oracle or variant for that core builds the
same CPU, or it stops being called an oracle or variant of the product.

### 4. Display end state

Remove the Atari ST built-in Direct fallback only after the factory path
already requires the linked part, matching Coleco. An uncomposed ST shell
then blocks launch.

Do not retrofit video parts onto ZX81, SMS, SG-1000, Spectrum, C64 or Apple II
in the same change as a pin move or a CPU swap. Those shells still own
`fes.video.fixed-720p60`. A later design can move a raster source out of one
shell. Adding the display plane to a shell is separate from adding a part
socket.

### 5. Coleco cartridge shape

The factory Coleco recipe is format 3: one exact 16 KiB cartridge and one
sealed map. Short carts are padded with `0xff`. MegaCart format 4 stays a
development lane. It must not become a third cartridge interface that the
image can select by accident.

### 6. Image

Do not add C64, RISC-V, Apple II or Catch to the factory image as a side
effect of a pin move or a CPU swap. Promotion is its own acceptance gate.

## What this page does not do

It does not seal a bitstream, lease a kit, or accept an image. Those steps
are [misteross cores](../sources/misteross/docs/cores.md), the
[core developer workflow](core-development.md) and
[package acceptance](package-acceptance.md). Build mechanisms — identity,
lanes, compilers and package bytes — stay
[misteross architecture](../sources/misteross/docs/architecture.md).
