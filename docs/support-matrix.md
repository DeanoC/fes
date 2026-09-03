# System support matrix

This is the single canonical system-support record. Test-only synthetic
fixtures verify software mechanics and do not establish system support.

| System ID | Implementation | Software status | Hardware status |
| --- | --- | --- | --- |
| `megadrive` | native fixed-video, one-player launch/Stop/relaunch | software: yes | hardware: no |
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
baseline is not a game-system row or a hardware pass. Physical status awaits
later evidence from an exact pinned FogCast image; hardware-supported systems
remain zero. The Mega Drive software result covers runtime validation, exact
core/media/video/input ordering, bounded ADV7513 main-power quiesce before
supported FPGA core transitions, bounded fault cleanup, Stop, and immediate
relaunch under host tests. Failed quiesce writes are treated as hardware
mutations and enter the existing idle cleanup path. This does not claim visible
HDMI or playable input on a physical MiSTer. Audio, saves, six-button input,
multiplayer, remapping,
hot-plug recovery, development-RBF loading/video acceptance, every other
system, running-game restart preservation, conventional Main, transient MGLs,
and automatic legacy fallback remain outside this slice. Change a row only in
the same commit as its implementation and support evidence.
