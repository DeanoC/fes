# Support matrix

Hardware-supported package paths: 0.

| Capability | Software coverage | Hardware acceptance |
| --- | --- | --- |
| FES described package admission and GP activation | covered | pending for exact current artifacts |
| Simple-game, simple-computer, application ABI | covered | pending |
| `fes.simple-computer` required audio declaration, exact live bit 4, and ADV7513 packet policy | covered by host tests | SG-1000 exact-artifact audio pending |
| Menu-display GP, immutable staging, idle lifecycle, reserved-memory presentation, selected startup, frame/mutation admission, lifecycle wait during present, and bounded underflow reactivation | covered by host tests and real local descriptor exchange | [menu presentation diagnostic](../../../docs/validation/2026-09-28-native-menu-kit-presentation.md) and [kit 2 launch/Stop diagnostic](../../../docs/validation/2026-09-28-menu-frame-mutation-kit2.md); exact product image acceptance pending |
| Sealed splash and fixed ADV7513 video | covered | pending |
| Gamepad and controller/keypad ports | covered | pending |
| Blob/stream media and firmware | covered | pending |
| Mid-session ZX81 tape blob (no hold-reset) | covered | pending |
| Simple-computer session HDMI plane, bound show/return, focus neutralization, immutable frames and fault isolation without reprogramming | covered by host tests | combined ZX81 exact-artifact acceptance pending |
| Described-core library persistence and retry | covered | pending |
| Format-3 ROM-map inspection and receipt-bound activation | covered | none |
| Format-4 two-ROM receipt-bound activation through production adapter | covered | [Coleco synthetic diagnostic](../../../docs/validation/2026-09-26-coleco-megacart-two-rom-hil.md); appliance acceptance pending |
| Static ZX81 bus 1.0/2.0 and Coleco composition | covered | pending |
| Library Coleco video/CPU parts with required core-data root and namespace admission before programming | covered by host tests | [Kit 2 normal-library diagnostic](../../../docs/validation/2026-10-02-video-library-kit2-hil.md): direct/scanline with and without SGM, profile fallback, media, Stop/relaunch; appliance acceptance pending |
| Explicit volatile Coleco video/CPU developer parts: inspect, typed identity, retained-file admission and existing programming lifecycle | covered by host tests | [Kit 2 exact-artifact diagnostic](../../../docs/validation/2026-10-02-video-parts-kit2-hil.md): direct/scanline with and without SGM, media, Stop/relaunch; appliance acceptance pending |
| `fes.computer` admission, identity and firmware ROM activation | covered | none |
| `fes.computer` HID keyboard rows and controller ports | covered | none |
| `fes.computer` live media units (insert/eject without reset hold) | covered | none |
| Atari ST exact 720 KiB `.st` unit-0 disks, live insertion/ejection and single socket bus composition | covered by host tests | none |
| Multi-slot Apple II slot-bus composition (socket set provisional) | covered | none |
| Multi-slot Spectrum edge-bus composition and `.tap` unit | covered | [48K BASIC ROM-link diagnostic](../../../docs/validation/2026-09-28-spectrum-basic-kit.md); keyboard, tape and cards pending |
| Initialized machine-ROM bitstream | covered | pending |
| Explicit contained RBF diagnostic and recovery | covered | diagnostic only; pending |
| `fes-gp-v1` bridge release after user mode; SDR FPGA ports only after `fes.memory.hps-ddr` identity and mirror match, on a boot whose U-Boot core latched the layout | covered | diagnostic 2026-09-27 (port release; boot capture pending) |

Conventional Main launches, raw game profiles, protocol 1 and MiSTer package
activation are retired. Existing catalog/cache/save files are not migrated or
deleted by this cleanup. The dated
[Mega Drive baseline](../../FogCast/docs/hardware/native-megadrive-baseline.md)
remains historical evidence for its old runtime/image, not acceptance of this
package-only integration. Acceptance requires one designated-kit lease and
exact runtime/package/image identity for startup, launch/media/input,
replacement, Stop/relaunch, persistence and contained recovery.
