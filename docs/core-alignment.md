# Core alignment

This is the currency contract for described FPGA cores. The ABI and image
matrix stays [core status](core-status.md). This page says which choices are
the standard, which cores still follow an older path, and the order for
retiring those paths.

Taken at `6d9939c2`. A change that moves a lock, CPU or display contract
updates this page in the same commit. A dated note under
[validation/](validation/) accepts only the artifact it names.

## Rule

When two mechanisms do the same job, the later choice is the standard. Move
consumers onto it. Do not keep the earlier mechanism and call it a variant.

A second mechanism is allowed only when it does a different job, and the
owning README names that job. `fes_z80_fast` is a documented-only development
build. Splash is board firmware. ZX81 session-display is an in-session plane.
Quartus is an oracle. None of those is a second product path.

Do not add a core by copying the historical path of an older sibling. A new
core uses the standards below.

## Standards

These are the choices already in the tree at `6d9939c2`.

1. The product path is HIP/nextpnr. Quartus is an oracle only where the recipe
   says so. A failed HIP route does not fall back to Quartus. Quartus never
   programs a kit.
2. One compiler pin: `sources/misteross/toolchain.lock`. At this base that is
   Yosys `5391eeb1`, Mistral `8fcc4cb4`, nextpnr `9128800f`, deliberately
   before nextpnr #172. HIP architectures are `gfx1100;gfx1201`. A recipe on
   that lock is current. A dedicated lock is not, even if it still routes.
   A dedicated lock is allowed only while its rationale names a capability the
   shared pin lacks. When the shared pin has that capability, delete the
   dedicated lock. Do not keep a pin because an older seal used it.
3. Registered M10K is synchronous. No core under `sources/misteross/cores`
   sets `CFG_ASYNC_READ(1)`. Do not reintroduce it.
4. The Z80 product CPU is `fes_z80_nmos` at native cadence. `fes_z80_fast` is
   not a second product CPU. TV80/T80 is the historical selection still
   instantiated by Coleco, SMS and ZX81. A sibling already on the first-party
   CPU does not qualify the others. There is no newer first-party 6502 or
   68000; `cpu6502` and `fx68k` stay until a replacement exists. Do not add a
   third Z80.
5. Two display contracts, not three.
   - An application or ROM-less shell uses shared `fes_video_720p`. That is
     Pong, Catch, the demos, RAM test and RISC-V. The optimizer-generated
     scaler is that module. Do not force video parts onto these shells.
   - A machine shell that emits a raster source uses linked Direct/Scanlines
     parts and does not own HDMI timing. Coleco native is the standard: no
     built-in Direct fallback; a missing or invalid part blocks launch.
     Atari ST's built-in Direct fallback is a migration aid, not the end state.
   - ZX81 `fes.video.session-display` stays a named capability until a separate
     design says it is a video part.
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

Factory image, in profile order: `fes.menu`, `fes.pong`, `fes.zx81`,
`fes.coleco`, `fes.sms`, `fes.sg1000`, `fes.spectrum`, `fes.ramtest`,
`fes.atari-st`. The selector also admits `fes.c64`. Admission is not image
inclusion. `fes.riscv`, `fes.apple2` and `fes.catch` are `make core-dev` only.
Library standing can still say experimental for a factory package. Spectrum
is that case.

| Package | Usable as | Matches | Does not match |
| --- | --- | --- | --- |
| `fes.menu` | Idle scanout. Not a game. Splash is still the product idle fallback. | Shared pin. | No factory-image acceptance of this bitstream. |
| `fes.pong` 1.1.0 | ROM-less game. Settings and best rally persist. The [10 Oct kit B load](validation/2026-10-10-optimizer-video-area.md) showed the field and Stop. | Shared pin, single-PLL gate, optimizer scaler. | That note is a bounded video/Stop check, not gameplay or image acceptance. |
| `fes.zx81` 1.5.0 | Most complete 8-bit machine in the image: live `.p` tape, session display, bus 2.0. The [2 Oct image diagnostic](validation/2026-10-02-zx81-session-display.md) showed cassette swap with BASIC kept. | Sync M10K, format 3, HIP. | Still TV80. Lock is nextpnr `c2bb4363`, the oldest play pin. Physical keyboard, expansion and audio of 1.5.0 are not accepted. |
| `fes.coleco` 1.2.0 | Reduced ColecoVision. Native video parts are mandatory. Optional SGM. One exact 16 KiB cartridge, padded with `0xff`. | Native video parts, sync M10K, format 3. Product lock is `toolchain.lock`. | Still TV80. Not retail-complete. The 3 Oct note does not accept these bytes. |
| `fes.sms` 1.4.0 | 32 KiB Master System slice. No mapper. Do not use `fes.mastersystem`. | Sync M10K, format 3, HIP. Named kit audio is artifact-scoped. | Still TV80. Lock `a93fe013`. |
| `fes.sg1000` 1.3.0 | 16 KiB slice. | `fes_z80_nmos`, sync M10K, format 3, HIP. | Lock `a93fe013`. The 1.2.0 kit audio note does not qualify 1.3.0. |
| `fes.spectrum` 0.2.0 | In the image. Library standing is experimental. Open diagnostic firmware only; no Sinclair ROM is shipped. | `fes_z80_nmos` by default, sync M10K, format 3. Product lock is `toolchain.lock`. | The 28 Sep notes do not accept a shared-pin seal. The 56 MHz build is documented-only. No card, keyboard or tape kit check of this shell. |
| `fes.ramtest` 1.1.0 | SDRAM and HPS DDR tester. Factory ships 100 MHz. | Shared pin. | 130 MHz remains open. No exact-image hardware acceptance. |
| `fes.atari-st` 0.2.0 | In the image. GEM, mouse, writable geometry-aware disks and video parts have passed on named artifacts. Not a compatible ST. | Video parts, sync M10K, format 3, HIP. | Own lock: Yosys `886afa63`, nextpnr `3d4a5b35`. Built-in Direct fallback remains. Borders, later demo sections, menu flash, audio fidelity and full current-image acceptance are open. |

| Package | Usable as | Matches | Does not match |
| --- | --- | --- | --- |
| `fes.riscv` 0.1.0 | Original RV32I demo. No historical machine, no audio, no user programs. | Shared pin. Qualified ALU package `c6c11fcd…` (build `a7118693…`, [receipt](validation/riscv-timing-alu-2026-10-11.json)) closed 74.25 MHz. Its both-kit loads showed the scene, timer colour and Stop. | Not in the image. Those ALU loads did not test the gamepad. The [11 Oct video diagnostic](validation/2026-10-11-riscv-video-diagnostic.md) of baseline `72f3b2be…` (build `39ccdf04…`) is a static-scene and Stop check; candidate `85bf8676…` there is a video comparison only. Neither is a gamepad check. The [4 Oct kit A load](validation/2026-10-04-riscv-kit-a.md) of `e5c4bd79…` (build `55de600f…`) observed dpad-right. No seal accepts another package. |
| `fes.c64` 0.1.0 | Package-only pathfinder. The [6 Oct Kit A diagnostic](validation/2026-10-06-c64-functional-closure.md) passed firmware, both probes, input and a read-only D64 on that package. | Sync M10K, format 3. Yosys and Mistral match the shared pin. | nextpnr `0c5ed400`, not the shared pin. No disk writes. Not in the image. |
| `fes.apple2` 0.1.0 | Package-only pathfinder. A Sept 27 kit diagnostic covered linked firmware, four cards and a live disk swap. | Sync M10K, format 3. Product lock is `toolchain.lock`. Audited static source closure. | The Sept 27 note does not accept a shared-pin seal. The shared-pin route reported with the lock move was CUDA exploratory and is not HIP closure; the product default remains HIP and HIP seal evidence is still open. A recorded compiler-read derive of the source closure waits on that HIP seal. No writes. No Apple ROM. No image acceptance. |
| `fes.catch` 1.0.0 | Registered ROM-less paddle demo. | Shared pin. Sims cover the optimizer scaler. | Not in the image. No separate kit acceptance. |

`fes.demo`, `fes.demo-media` and `fes.demo-audio` are references. Catch is the
registered game on that shell. Splash is `sources/misteross/sealed/fes-splash.rbf`:
720p60, GPU router off, no user-io. `cores/pong` is a simulation module, not
`fes.pong`. There is no NES, SNES or Mega Drive package.

What a person can use, with those evidence limits: Pong, then ZX81 inside its
1 KiB / `.p` / bus 2.0 contract. Coleco, SMS and SG-1000 are factory slices,
not cartridge-compatible machines. Spectrum is a pathfinder in the image.
Atari ST boots GEM and moves disks on named builds. RISC-V is the core most
aligned with the current pin, and it is not in the appliance image. C64 is
the strongest package-only computer diagnostic.

## Retirement order

Do not batch a pin move and a CPU swap in one seal unless the task says so.
Each step ends with a clean-tree HIP seal of that core on the target pin and
the focused simulation. Kit evidence is a separate task. It does not ride
along. An older kit note does not accept the new bytes.

### 1. Stop new forks

This page is that rule. A new `toolchains/*.lock` is rejected unless its
rationale names a capability missing from `toolchain.lock`. A new Z80 machine
instantiates `fes_z80_nmos`. A new external ROM uses format 3. A new shell
picks one of the two display contracts.

### 2. One pin

Move one core at a time. If the shared pin fails, name the missing capability,
fix it in nextpnr, then delete the dedicated lock. Do not add another lock.

| Order | Core | nextpnr now | Why this order |
| --- | --- | --- | --- |
| 1 | `fes.c64` | `0c5ed400` | Same Yosys and Mistral as the shared pin. Package-only. Closest requalification. |
| 2 | `fes.atari-st` | `3d4a5b35` | Factory, and its Yosys pin is also private. Do not keep this pin because an older seed missed pixel. |
| 3 | `fes.coleco` | `toolchain.lock` | Factory, cart fence, native video. `toolchains/coleco-expansion.lock` moved onto the shared pin with the socket diagnostic and is not a second lock. |
| 4 | `fes.apple2`, `fes.spectrum` | `toolchain.lock` | Same multi-region cart support. Both now select `toolchain.lock`. |
| 5 | `fes.sms`, `fes.sg1000` | `a93fe013` | `toolchains/fes-sms.lock` and `toolchains/registered-memory.lock`. |
| 6 | `fes.zx81` | `c2bb4363` | Oldest pin. Bus 2.0 and session display make it a bad first move. |

Completion: the recipe `lock_path` is `toolchain.lock`, the producer
authenticates that file, the HIP seal meets the core's existing clock gates,
and the old lock is deleted if nothing else selects it.

`toolchains/ramtest-timing.lock` is the timing diagnostic, not a product pin.
Do not bump it as if it were a core. Point the diagnostic at the shared pin
or delete it when the diagnostic no longer needs a private pin.

### 3. One Z80

SG-1000 is the pattern. It is not evidence for the others.

1. SMS. Fixed cartridge, no expansion bus.
2. Coleco. Clock enable, VDP NMI, bus 2.0.
3. ZX81. Exact-average 3.25 MHz CPU, 1.625 MHz AY, session display.

Preserve each machine's existing cadence and interrupt wiring. Do not switch
a product recipe to `fes_z80_fast` to make timing easier.

When the last TV80 consumer is gone, delete the per-core TV80/T80 copies from
the product producers. A Quartus oracle for that core builds the same CPU, or
it stops being called an oracle of the product.

### 4. Display end state

Remove the Atari ST built-in Direct fallback only after the factory path
already requires the linked part, matching Coleco. An uncomposed ST shell
then blocks launch.

Do not retrofit video parts onto ZX81, SMS, SG-1000, Spectrum, C64 or Apple II
in the same change as a pin move or a CPU swap. Those shells still own
`fes.video.fixed-720p60`. A later design can move a raster source out of one
shell. Session-display is not that design.

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
