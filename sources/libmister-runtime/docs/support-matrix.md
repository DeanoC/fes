# Support matrix

Hardware-supported package paths: 0.

| Capability | Software coverage | Hardware acceptance |
| --- | --- | --- |
| Production platform guard: ARMv7 plus exact DE10-nano device-tree identity; AArch64/DE25/unknown/malformed identity refused before window-driver loading or MMIO/I2C construction | covered by native identity/ordering and unavailable-adapter tests | exact-artifact DE10 guard acceptance pending; DE25 hardware unsupported |
| DE25 PR-control diagnostic: selected-base identity, freeze/drain, injected configuration and identity-probed resume | native/AArch64 protocol tests and actual-endpoint RTL co-simulation; production constructor remains closed | none; physical configuration and MMIO binding are unimplemented |
| DE25 atomic computer-register client: selected build/exact ABI 1.0/capability and tape-limit validation, one full-word submission, held-payload/fault/ACK checks, bounded polling, no replay after uncertainty, HID and CRC/chunked tape operations | scripted-register C++ tests and native/AArch64 client against actual endpoint/machine RTL | [2026-10-30 maintenance diagnostic](../../../docs/validation/2026-10-30-de25-linux-zx81/README.md) on one identified board; production construction remains closed |
| Explicit `de25-computer` maintenance CLI for an already configured image, selected boot/build and audited CPU resource | native build and early-refusal checks; existing protocol/MMIO consumers reused | source 24fc445a2 on TRWU8J8G with corrected FES rootfs source 24fc445a2: direct CPU probe, HOLD/RUN and observed BASIC LOAD/run/edit/reload/run pass; no production package or daemon admission |
| FES described package admission and GP activation | covered | pending for exact current artifacts |
| GP acknowledged-response settling guard, bounded full-word sampling, fail-closed ACK/signature/MMIO handling and no command replay | covered by host fault-injection tests | none |
| Simple-game, simple-computer, application ABI | covered | pending |
| `fes.simple-computer` required audio declaration, exact live bit 4, and ADV7513 packet policy | covered by host tests | SG-1000 exact-artifact audio pending |
| Menu-display GP, idle lifecycle, granted-slot presentation (fes-window grant of both slots, back-slot begin, write-back of the written regions before submit, revoke before a core owns the window and on a failed present), selected startup, frame/mutation admission, lifecycle wait from begin to commit, and bounded underflow reactivation | covered by host tests with injected driver calls and real local descriptor exchange | [Kit A diagnostic](https://github.com/DeanoC/fes/pull/959#issuecomment-6316246790) 2026-10-21 of bundle `692e511e` (runtime `16bdf0ce`, image `5af6162b`): Home, Library, rooms and Settings matched baseline HDMI captures (17 of 22 steps byte-identical, the rest differing in build text or room overrun timing), and Pong launch and Stop returned to a redrawn Home; the 2026-09-28 [presentation](../../../docs/validation/2026-09-28-native-menu-kit-presentation.md) and [launch/Stop](../../../docs/validation/2026-09-28-menu-frame-mutation-kit2.md) diagnostics cover the retired copy path; exact product image acceptance pending |
| Menu-blitter selection (minor 1 or later only), grant sizing (80 MiB), CPU frames until its first list and only lists after, command-list uploads and write-back, idle admission, completion polling and slot submission | covered by host tests | exact-artifact kit HIL pending, including target `mmap` of the 80 MiB grant |
| Sealed splash and fixed ADV7513 video, with a −0.8 ns input clock delay (`0xBA = 0x20`) | covered | [Kit A diagnostic](../../../docs/validation/2026-10-23-hdmi-input-clock-delay.md) 2026-10-23 of runtime `27d131a5` merged locally with #1001's `e5bd594a` (image `88e85ec5`): the idle display and six Spectrum, ZX81 and C64 archives held the HDMI link for 30 s each with `0xBA` read back as `0x20`, including the +2A/+3 archive that dropped it 14–15 times in 20 s with no delay; kit B, the other cores and the delay margin unchecked; exact-artifact acceptance pending |
| Gamepad and controller/keypad ports | covered | pending |
| Blob/stream media and firmware | covered | pending |
| Mid-session ZX81 tape blob (no hold-reset) | covered | pending |
| Simple-computer session HDMI plane (premultiplied blend), bound show/return, focus neutralization, granted-slot frames revoked on hide, and fault isolation without reprogramming | covered by host tests | combined ZX81 exact-artifact acceptance pending |
| Described-core library persistence and retry | covered | pending |
| Format-3 ROM-map inspection and receipt-bound activation | covered | none |
| Format-4 two-ROM receipt-bound activation through production adapter | covered | [Coleco synthetic diagnostic](../../../docs/validation/2026-09-26-coleco-megacart-two-rom-hil.md); appliance acceptance pending |
| Static ZX81 bus 1.0/2.0 and Coleco composition | covered | pending |
| Library Coleco video/CPU parts with required core-data root and namespace admission before programming | covered by host tests, including metadata-only native/raster namespace preflight, package/record rejection and unchanged native standalone-load guards | [Kit 2 raster normal-library diagnostic](../../../docs/validation/2026-10-02-video-library-kit2-hil.md); [native factory exact-image acceptance](../../../docs/validation/2026-10-03-native-video-factory-kit2.md) at `6ed1dad4`: Direct/Scanlines with and without SGM, open media, persisted preferences, captured video/audio and Stop/relaunch |
| Explicit volatile Coleco video/CPU developer parts: inspect, typed identity, retained-file admission and existing programming lifecycle | covered by host tests | [Kit 2 exact-artifact diagnostic](../../../docs/validation/2026-10-02-video-parts-kit2-hil.md): direct/scanline with and without SGM, media, Stop/relaunch; appliance acceptance pending |
| Native Coleco 256 by 192 indexed developer parts: distinct socket/layout binding, mandatory video part, and uncomposed-load rejection before mutation | covered by host tests | [Kit 2 native diagnostic](../../../docs/validation/2026-10-03-native-video-parts-kit2.md): linked Direct/Scanlines, HDMI video/audio, Stop/relaunch and vacant-load rejection; [factory image and native SGM acceptance](../../../docs/validation/2026-10-03-native-video-factory-kit2.md) for its exact image, compositions and open ROMs |
| `fes.computer` admission, identity, firmware ROM activation and persistence words/layout pairing before programming, including volatile developer loads | covered | none |
| `fes.computer` 1.1: identity word 16, the per-game `fes.pc386.machine` record, the wall-clock seed, and input-fault capture of the record with no ATA disk declared, with an empty ATA unit and with a bound disk on a poisoned mailbox | covered by host tests and the shared 1.1 fixture, including fresh record capture after failed-save recovery without a host input recipe | pc386 exact-artifact acceptance pending |
| `fes.media.ata-disk`: complete window load with delta chunks and CRC commit, regular-file base admission without blocking on FIFOs, fixed while running, dirty-chunk save at Stop/fault cleanup and manifest publication; empty-unit insert/eject rejection retains package persistence | covered by host tests with a scripted window and the shared 1.1 fixture | pc386 machine diagnostic (Phase M4) pending |
| `fes.computer` HID keyboard rows and controller ports | covered | none |
| `fes.computer` live media units (insert/eject without reset hold) | covered | none |
| `fes.computer` Spectrum snapshot metadata reported from declared optional interfaces | covered by runtime activation tests | none |
| `fes.computer` Spectrum disk on media unit 1 beside the unit-0 cassette: discovery, admission with the cassette and HPS DDR, and unit-addressed transfers | covered by runtime and mailbox simulation tests | none |
| ZX81 BASIC SAVE capture, frozen dirty snapshot, durable-host acknowledgement, Stop refusal while a SAVE is pending, and successful Stop when no SAVE completed | covered by host tests | none; exact-artifact hardware acceptance pending |
| `fes.computer` session HDMI plane (premultiplied RGBA blend, picture rectangle, show/return, HID neutralize, submitted frames, no reprogram on close) | covered by host tests | none |
| `fes.application` session HDMI plane (premultiplied RGBA blend, show/return, controller-port release, single-gamepad focus gate with held-input suppression through release, no reprogram on close) | covered by host tests | pending |
| Atari ST exact 720 KiB `.st` unit-0 disks, live insertion/ejection and single socket bus composition | covered by host tests | none |
| PC/XT exact 360 KiB read-only unit-0 disks, 1.1 capability discovery and session-preserving insertion/ejection | covered by host tests | none |
| Atari ST sector writes, frozen snapshot and explicit library disk records across Stop/eject/replacement, with binding retirement after successful menu/splash programming and same-daemon relaunch | covered by simulations and host tests, including save-failure ownership retention and removal of crash-orphaned save temporaries and write probes when the namespace is opened | none |
| Multi-slot Apple II slot-bus composition (socket set provisional) | covered | none |
| Multi-slot Spectrum edge-bus composition and `.tap` unit | covered | [48K BASIC ROM-link diagnostic](../../../docs/validation/2026-09-28-spectrum-basic-kit.md); keyboard, tape and cards pending |
| Initialized machine-ROM bitstream | covered | pending |
| Explicit contained RBF diagnostic and recovery | covered | diagnostic only; pending |
| `fes-gp-v1` bridge release after user mode; SDR FPGA ports only after `fes.memory.hps-ddr` identity and mirror match, on a boot whose U-Boot core latched the layout | covered | diagnostic 2026-09-27 (port release; boot capture pending) |
| Startup load of the release's `fes-window` driver with the `fes.memory.hps-ddr` window; a loaded copy kept only on matching version and window, otherwise replaced, or reported busy while in use; a failure is logged and startup continues | covered by host tests | [Kit A diagnostic](https://github.com/DeanoC/fes/issues/949#issuecomment-6310766923) 2026-10-21 of a dev driver build (grant, cached draw, clean, revoke); runtime load of the bundled driver pending exact-artifact acceptance |

Conventional Main launches, raw game profiles, protocol 1 and MiSTer package
activation are retired. Existing catalog/cache/save files are not migrated or
deleted by this cleanup. The dated
[Mega Drive baseline](../../FogCast/docs/hardware/native-megadrive-baseline.md)
remains historical evidence for its old runtime/image, not acceptance of this
package-only integration. Acceptance requires one designated-kit lease and
exact runtime/package/image identity for startup, launch/media/input,
replacement, Stop/relaunch, persistence and contained recovery.

Initial ST library floppy on format-3 ROM activation has host software coverage
and an [exact-package Kit A automatic-boot diagnostic](../../../docs/validation/2026-10-06-atari-st-auto-boot.md). Optional `initial_media` admission validates exact disk identity, writable
contract and durable record before mutation; upload/restore and ready
confirmation precede the first execution release. Tests cover retained source
bytes, same-namespace outgoing save refresh, malformed/corrupt preflight,
failed-save ownership and failed upload without release. Host regressions also
verify that ST never starts the legacy gamepad worker and rejects a required
legacy `fes.gamepad` declaration before programming. The ambiguous-Start
follow-up has host fault-injection coverage for accepted release with unavailable
acknowledgement, save-before-idle, and retained ownership after capture or durable
publication failure; it does not relabel the earlier hardware artifacts. The diagnostic verifies
fresh and saved-disk AUTO execution with full payload comparisons in one daemon
lifetime. Assembled appliance acceptance remains pending.

Required `fes.media.atari-st-floppy-geometry` 1.0 permits the 12 uniquely sized
raw ST layouts with 80–82 tracks, one/two heads, nine/ten sectors and 512-byte
sectors (368640–839680 bytes). Discovery proves capability bit 10 and exact
extended live limits. Nonlegacy immutable bases require matching BPBs; saved
records use minor 1, retain guest BPB edits, and must match the base length
before restore. Legacy 737280-byte behavior and minor-0 bytes are unchanged.
This extension has software coverage; no new hardware acceptance is claimed.
