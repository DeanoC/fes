# Support matrix

Hardware-supported package paths: 0.

| Capability | Software coverage | Hardware acceptance |
| --- | --- | --- |
| FES described package admission and GP activation | covered | pending for exact current artifacts |
| Simple-game, simple-computer, application ABI | covered | pending |
| Sealed splash and fixed ADV7513 video | covered | pending |
| Gamepad and controller/keypad ports | covered | pending |
| Blob/stream media and firmware | covered | pending |
| Mid-session ZX81 tape blob (no hold-reset) | covered | pending |
| Described-core library persistence and retry | covered | pending |
| Format-3 ROM-map inspection and receipt-bound activation | covered | none |
| Static ZX81 composition | covered | pending |
| Initialized machine-ROM bitstream | covered | pending |
| Explicit contained RBF diagnostic and recovery | covered | diagnostic only; pending |

Conventional Main launches, raw game profiles, protocol 1 and MiSTer package
activation are retired. Existing catalog/cache/save files are not migrated or
deleted by this cleanup. The dated
[Mega Drive baseline](../../FogCast/docs/hardware/native-megadrive-baseline.md)
remains historical evidence for its old runtime/image, not acceptance of this
package-only integration. Acceptance requires one designated-kit lease and
exact runtime/package/image identity for startup, launch/media/input,
replacement, Stop/relaunch, persistence and contained recovery.
