# libmister-runtime

`libmister-runtime` is the standalone C++14 lifecycle library and local control
daemon for the native MiSTer hardware-control direction. The repository keeps
the lifecycle API, native Linux primitives, profile model, and
`mister-runtime` daemon behind one build and one ownership boundary.

## Current status

Production native construction includes Mega Drive, ROM-less Pong and basic SNES profiles, fixed
1280x720@60 game and menu video paths, and one exact FogCast virtual-gamepad
input session. Host tests cover complete launch, Stop, idle cleanup,
asynchronous input-fault cleanup, and immediate relaunch. Dated exact-image
physical acceptance covers the same launch/input/Stop/relaunch slice.
Hardware-supported systems: 1.

Mega Drive is software- and hardware-supported on the designated FogCast kit.
The accepted slice proves visible HDMI and playable one-player D-pad and jump
input on a physical MiSTer. Native audio, save RAM, save states, six-button
X/Y/Z/Mode input, multiplayer, remapping, hot-plug recovery, and
development-RBF loading/video acceptance remain outside this slice. Pong, SNES and
every other production system remain unsupported. The native path does not
preserve a running game across restart and does not start conventional Main,
transient MGLs, or an automatic legacy fallback. Fakes under `tests/` verify
software contracts only and are not physical evidence. See the
[dated FogCast hardware baseline](https://github.com/DeanoC/FogCast/blob/main/docs/hardware/native-megadrive-baseline.md).

Pong has software-tested profile admission and ROM-less launch/Stop/relaunch
through the ordinary native lifecycle. It uses the image-owned
`/usr/share/mister-runtime/cores/pong.rbf`, core identity `Pong`, and empty
media/settings objects. Up/Down/Start use the existing one-player packet;
Left/Right/A/B/C are reserved and ignored by the wrapper. A matching core must
be installed by image assembly. Pong video, input and audio have no physical
acceptance; native audio remains unaccepted for Mega Drive as well. Game
bringup now clears the MiSTer framework mute after HDMI link verification;
software tests cover its command, deadline and cleanup, not audible output.

HPS MMIO constants and the Mega Drive/Pong/SNES profile tables are generated C++14
headers checked in under `src/native/generated/`. They are target text
for the ARMv7 Linux HPS, not host objects. The target build does not run
Go.

## Build and test

```sh
make all
make test
```

The host build produces `build/libmister-runtime.a` and
`build/mister-runtime`. See the [support matrix](docs/support-matrix.md) for
the single canonical support record, [architecture](ARCHITECTURE.md) for the
runtime boundaries, and [development guide](DEVELOPMENT.md) for full local and
cross-build checks.

## Basic SNES software integration

The `snes` profile uses `/usr/share/mister-runtime/cores/snes.rbf`, core identity
`SNES`, and required cartridge media at index 1. Artifact preflight accepts a
32 KiB–4 MiB power-of-two LoROM/HiROM payload, optionally preceded by a 512-byte
copier header. It rejects malformed/ambiguous headers, enhancement cartridges,
special mappings and unsupported sizes before FPGA mutation. The runtime
streams a synthesized 512-byte core metadata prefix followed by the retained
ROM file window; the host supplies the original ROM and never builds metadata.
Ordinary cartridge types 0–2 and RAM exponents up to 7 are admitted. RAM is
volatile; saves, enhancement chips and full-library compatibility are excluded.

The retained gamepad now accepts X/Y/L/R/Select in addition to existing controls.
Zero-mask controls have no effect on Mega Drive/Pong and do not end sessions.
Tests cover individual SNES masks and Stop neutralization. SNES physical
video/input/audio acceptance is pending; this software profile is not evidence
of a working hardware system.
