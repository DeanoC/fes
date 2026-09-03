# System support matrix

This is the single canonical system-support record. Test-only synthetic
fixtures verify software mechanics and do not establish system support.

| System ID | Implementation | Software status | Hardware status |
| --- | --- | --- | --- |
| `megadrive` | native fixed-video, one-player launch/Stop/relaunch | software: yes | hardware: yes |
| `snes` | not implemented | software: no | hardware: no |
| `nes` | not implemented | software: no | hardware: no |
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

Production native construction is available for the image-owned idle baseline,
including a software-tested fixed menu-core 1280x720@60 video path. This idle
baseline is not a separate game-system row.

Hardware-supported systems: 1 (`megadrive`).

Acceptance date: 2026-09-04. Accepted runtime commit:
`443b603de991b56b5f4d0d11c5bc88a3f83fad13`. Exact native image SHA-256:
`7591ee6a943fabd3e40134f662bb76e242bffa201716ee0d4201a7859d31e480`.
The [FogCast native Mega Drive baseline](https://github.com/DeanoC/FogCast/blob/main/docs/hardware/native-megadrive-baseline.md)
records the accepted FogCast source, reproducibility gates, boot and installed
identities, six consecutive public launch/input/Stop cycles, individually
inspected HDMI frames, and successful legacy rollback.

The Mega Drive result covers runtime validation, exact
core/media/video/input ordering, bounded ADV7513 main-power quiesce before
supported FPGA core transitions, bounded fault cleanup, Stop, and immediate
relaunch under host tests and on the designated physical kit. Failed quiesce
writes are treated as hardware mutations and enter the existing idle cleanup
path. Audio, saves, six-button input, multiplayer, remapping, hot-plug recovery,
development-RBF loading/video acceptance, every other
system, running-game restart preservation, conventional Main, transient MGLs,
and automatic legacy fallback remain outside this slice. Change a row only in
the same commit as its implementation and support evidence.
