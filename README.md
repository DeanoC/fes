# libmister-runtime

`libmister-runtime` is the standalone C++14 lifecycle library and local control
daemon for the native MiSTer hardware-control direction. The repository keeps
the lifecycle API, native Linux primitives, profile model, and
`mister-runtime` daemon behind one build and one ownership boundary.

## Current status

Production native construction includes one Mega Drive profile, fixed
1280x720@60 game and menu video paths, and one exact FogCast virtual-gamepad
input session. Host tests cover complete launch, Stop, idle cleanup,
asynchronous input-fault cleanup, and immediate relaunch. Dated exact-image
physical acceptance covers the same launch/input/Stop/relaunch slice.
Hardware-supported systems: 1.

Mega Drive is software- and hardware-supported on the designated FogCast kit.
The accepted slice proves visible HDMI and playable one-player D-pad and jump
input on a physical MiSTer. Native audio, save RAM, save states, six-button
X/Y/Z/Mode input, multiplayer, remapping, hot-plug recovery, and
development-RBF loading/video acceptance remain outside this slice. SNES and
every other production system remain unsupported. The native path does not
preserve a running game across restart and does not start conventional Main,
transient MGLs, or an automatic legacy fallback. Fakes under `tests/` verify
software contracts only and are not physical evidence. See the
[dated FogCast hardware baseline](https://github.com/DeanoC/FogCast/blob/main/docs/hardware/native-megadrive-baseline.md).

HPS MMIO constants and the Mega Drive profile table are generated C++14
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
