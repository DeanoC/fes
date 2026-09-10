# System support matrix

This is the single canonical system-support record. Test-only synthetic
fixtures verify software mechanics and do not establish system support.

| System ID | Implementation | Software status | Hardware status |
| --- | --- | --- | --- |
| `megadrive` | native fixed-video, one-player launch/Stop/relaunch | software: yes | hardware: yes |
| `pong` | native ROM-less profile, fixed-video lifecycle and one-player packet | software: yes | hardware: no |
| `snes` | basic LoROM/HiROM transform, one-player lifecycle, optional battery RAM | software: yes | hardware: no |
| `nes` | native iNES/NES2 cartridge preflight, one-player lifecycle | software: yes | hardware: pending |
| `sms` | not implemented | software: no | hardware: no |
| `gb` | not implemented | software: no | hardware: no |
| `gbc` | not implemented | software: no | hardware: no |
| `gba` | not implemented | software: no | hardware: no |
| `pce` | not implemented | software: no | hardware: no |
| `gg` | not implemented | software: no | hardware: no |
| `a2600` | not implemented | software: no | hardware: no |
| `a7800` | not implemented | software: no | hardware: no |
| `coleco` | not implemented | software: no | hardware: no |
| `lynx` | not implemented | software: no | hardware: no |
| `ws` | not implemented | software: no | hardware: no |
| `wsc` | not implemented | software: no | hardware: no |
| `intv` | not implemented | software: no | hardware: no |

## Non-system capabilities

| Capability | Implementation | Software status | Hardware status |
| --- | --- | --- | --- |
| Native idle HPS framebuffer | 640×480 BGRX, validated Linux mode and Menu SPI enable, restored by Stop | software: yes | hardware: pending |
| MiSTer-compatible development RBF | open, HDMI power-down, program, synchronize, optional core observation, Stop to idle | software: yes | hardware: pending |
| Format-2 core-package preflight | strict manifest/payload admission, retained descriptor, package identity and compiled compatibility check | software: yes | hardware: not applicable |
| Format-2 MiSTer development activation | owned admission, retired outgoing input, optional system/profile identity, explicit `mister-v1` programming, no inferred media/input or ambiguous quiesce retry | software: yes | hardware: pending |
| Explicit programming profiles | containment-first `mister-v1`, `fes-gp-v1`, and `development-contained-v1` manager recipes | software: yes | hardware: pending |
| Described-core persistence | FES Pong 1.0 settings/progress codec, production-factory data operations, atomic records, library restore/flush, CAS and explicit failed-save resume | software: yes | hardware: pending |
| FES GP core driver | production bounded GPO/GPI identity and controls, fixed ADV7513-only video, normalized generation-bound input | software: yes | hardware: pending |
| Target diagnostic event ring | FogCast #206 event shape for FIFO consume (including persistence mutation completion), named-cap/fd, fpga_manager, CORENAME, Main pid, and typed fences | software: yes | hardware: not applicable |

Production native construction is available for the image-owned idle baseline,
including a software-tested fixed menu-core 1280x720@60 video path. This idle
baseline is not a separate game-system row.

Hardware-supported systems: 1 (`megadrive`).

Acceptance date: 2026-09-04. Accepted runtime commit:
`443b603de991b56b5f4d0d11c5bc88a3f83fad13`. Exact native image SHA-256:
`95c9b4671e0d19781a6428d2168ab631453740215b194519f12788ade03c7c2e`.
The [FogCast native Mega Drive baseline](https://github.com/DeanoC/FogCast/blob/main/docs/hardware/native-megadrive-baseline.md)
records the accepted FogCast source, reproducibility gates, boot and installed
identities, six consecutive public launch/input/Stop cycles, individually
inspected HDMI frames, and successful legacy rollback.

The Mega Drive result covers runtime validation, exact
core/media/video/input ordering, bounded ADV7513 main-power quiesce before
supported FPGA core transitions, bounded fault cleanup, Stop, and immediate
relaunch under host tests and on the designated physical kit. Failed quiesce
writes are treated as hardware mutations and enter the existing idle cleanup
path. In the unsupported scope below, development-RBF means generic loading
outside the separate MiSTer-compatible capability.
Audio, saves, six-button input, multiplayer, remapping, hot-plug recovery,
development-RBF loading/video acceptance, every other
system, running-game restart preservation, conventional Main, transient MGLs,
and automatic legacy fallback remain outside this slice. The separate
MiSTer-compatible development capability is software-implemented; its physical
acceptance remains pending and it does not change the supported-system count.
Change a row only in the same commit as its implementation and support
evidence.

## Pong software scope

The generated Pong profile admits empty media, rejects extra media and RBF
path changes, and follows the native launch/Stop/relaunch sequence in software
tests. No physical Pong image, HDMI, input or audio acceptance is claimed.
The matching misteross wrapper/RBF must be installed by image assembly. Game bringup now clears MiSTer framework mute after HDMI link verification,
with software-tested bounded command and failure cleanup. Audible output is
not accepted; the Mega Drive acceptance above also excludes native audio.

## SNES software scope

Software tests cover the required cartridge transform before programming,
512-byte metadata, copier stripping, retained-file streaming, full one-player
button mapping and Stop/relaunch. Only basic power-of-two 32 KiB–4 MiB
LoROM/HiROM images are admitted. Optional ordinary type-2 battery RAM saves
(2–128 KiB) are software-tested through restore, clean Stop, atomic replacement
and retryable save failure. Omission of `save_path` keeps RAM volatile. Autosave,
power-loss capture, enhancement chips, special formats and non-power-of-two
mirroring are unsupported. Physical save acceptance is pending. Physical
SNES video/input/audio acceptance remains pending.

## NES software scope

Software tests cover generated profile admission, native filetype-index `0x40`
cartridge delivery with the narrow low-byte SPI wire,
iNES 1.0 and NES2 header validation, trainer rejection, bounded payload checks,
and preflight rejection before FPGA programming. Only standard `.nes` files up
to 32 MiB are admitted; source bytes are streamed unchanged. FDS, UNIF/UNF,
NSF, trainers, saves, cheats, mapper-specific policy and extra peripherals are
unsupported. The designated kit has loaded the upstream NES release RBF and
reported the `NES` core identity; this validates programming and observation,
not the exact assembled-image game/video/input path. Physical NES
video/input/audio acceptance remains pending, so the hardware-supported count
stays at one.
