# Coleco expansion bus: first composable slice

Status: CPU edge, timed development shell, independently routed diagnostic module, Go linker policy and host/runtime admission implemented. The exact-source development build passes all three clocks and its CRAM changes remain inside the socket. Coleco uses `fes.application` 1.0, while ZX81 uses `fes.simple-computer` 1.0. No product registration or hardware acceptance. Base: FES `dbfb6e332221c0df23aee2b89527c1d4593a7cd6`.

## Purpose and boundary

Give the FES ColecoVision a genuine, normally vacant expansion connector so an independently built FPGA module can be selected per library title and linked at download time. The cartridge connector and optional 8 KiB BIOS remain separate inputs. The first module is an original diagnostic peripheral that proves memory and I/O cycles; it does not claim to emulate an Atari adapter or ADAM.

Coleco's original technical manual describes a distinct 60-pin expansion connector that exposes the CPU bus, CPU control lines, and signals for external video selection. Its cartridge connector is a separate 30-pin connector. The current FES Coleco core explicitly excludes expansion hardware. This design first models a useful CPU peripheral subset and reserves a new contract version for bus mastering or video/audio takeover. Source: [ColecoVision Technical Manual](https://www.colecovisionzone.com/downloads/cv_Technical_Manual.pdf), System Description and CPU sections.

## Selected approach

Extend the existing frozen-shell composition architecture with a Coleco-specific slot and bus contract. Keep a single sealed `fes.coleco` base package with an empty socket. An expansion asset binds to that exact package, BUILD_ID, payload digest, device, slot version and geometry; it cannot be loaded against a ZX81 shell. The host persists one optional expansion choice per Coleco library entry. At launch, the host and target independently validate and compose the exact asset, then the runtime programs the resulting full-chip RBF under the existing lease. No placement, routing or Python runs on the kit.

The first contract carries the 16-bit Z80 address, 8-bit write data, memory/I/O read and write controls, M1 and refresh, and machine reset. Its response carries read data, an explicit read claim, WAIT, and maskable interrupt. The shell connects WAIT and INT to the TV80 inputs; a vacant socket holds both inactive. Simulation must prove a module can stretch a CPU read without duplicating a write or losing its interrupt. BUSRQ/BUSAK, external bus-master addresses, NMI override, and video/audio takeover require their own later version because this shell cannot safely claim them merely by exposing pins.

An empty socket changes no Coleco address decoding or gameplay. A selected module may claim only memory `0x2000–0x5fff` and currently unclaimed I/O reads in this first version. The shell masks a response claim outside those regions, so module logic cannot override console-owned BIOS, RAM, VDP/controller or cartridge reads. An admitted claim supplies data where the console otherwise returns `0xff`. Writes are observed by the module, not suppressed from the console. The diagnostic module exposes a small deterministic register/RAM window in that address space so a Coleco diagnostic cartridge can verify read, write, reset and empty-versus-populated behavior on screen. The module is a validation consumer, not the definition of the bus.

## Implementation ownership

- **misteross:** add the Coleco shell edge and vacant socket to the Coleco RTL; define its packed, registered boundary and fixed CRAM rectangle; route and seal the base shell with the existing authenticated HIP/nextpnr lane; build an independently routed diagnostic module and reject any non-CRC change outside the reserved socket. The first feasibility gate is a passing empty-shell route and timing report; no product package is claimed before that gate.
- **misteross Go linker, FogCast and libmister-runtime:** declare optional `fes.expansion.coleco-bus` 1.0 in the package manifest and parameterize the currently ZX81-specific slot admission by an explicit versioned slot/map policy. Preserve the existing ZX81 policy and assets byte-for-byte. The host library's expansion selection remains separate from ROM, media and BIOS selections; target staging and restart adoption recompute the composed bytes and identities. Runtime accepts only advertised, known slot policies and verifies the admitted payload before programming. This interface describes a composition socket, not a new GP mailbox operation or capability bit, so `mister-packages` generated mailbox definitions do not change.
- **FES:** retain Coleco's existing format-2 factory package until the new shell, module and consumers are integrated and exact-kit accepted. Updating the factory package is a separate source-selection and image-validation step.

No current ROM or firmware mailbox changes belong to this slice. After bus acceptance, convert cartridge and BIOS inputs to named download-time ROM linking as a separate change; it must account for two independent assets and preserve the expansion composition identity.

## Validation and acceptance

Simulation must show that the vacant socket preserves the existing Coleco graphics, input and audio diagnostics; an attached module observes real CPU cycles and can answer its dedicated memory and I/O reads; reset clears module state; and no module can claim BIOS, RAM, VDP/controller or cartridge-owned reads. The module build must authenticate the exact shell and lock, meet timing, and prove its CRAM delta is confined to the socket. Go/C++ tests must reject wrong-shell assets, wrong slot/map/version, corrupt archives, out-of-socket changes, stale selections, and altered relink bytes, while preserving ZX81 composition tests.

The designated-kit test uses a private host catalog and the ordinary lease. Launch the exact empty Coleco package, then the same title with the diagnostic module selected, then relaunch after a host restart and clear the selection. Record package, module, composition and programmed-RBF digests, visible diagnostic results, target/software identity, Stop and lease release. A build or simulation is not hardware acceptance; an accepted package is not automatically a new factory image.

## Deliberate limits

This first contract does not claim ADAM, Atari adapter, external video/audio, bus-master DMA, arbitrary address overlays, bank switching, or retail cartridge compatibility. Those features need a separately specified bus version and module-specific tests. Coleco cartridge ROM, BIOS firmware, and expansion module remain distinct library assets and cannot silently substitute for one another.
