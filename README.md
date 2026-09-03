# libmister-runtime

`libmister-runtime` is the standalone C++14 lifecycle library and local control
daemon for the native MiSTer hardware-control direction. The repository keeps
the lifecycle API, native Linux primitives, profile model, and
`mister-runtime` daemon behind one build and one ownership boundary.

## Current status

Software-tested only. Production native construction includes one Mega Drive
profile, fixed 1280x720@60 game and menu video paths, and one exact FogCast
virtual-gamepad input session. Host tests cover complete launch, Stop, idle
cleanup, asynchronous input-fault cleanup, and immediate relaunch. Physical
status remains pending later evidence from an exact pinned FogCast image.
Hardware-supported systems: 0.

Mega Drive is software-supported, not hardware-supported. This result does not
claim visible HDMI or playable input on a physical MiSTer. Native audio, save
RAM, save states, six-button X/Y/Z/Mode input, multiplayer, remapping, hot-plug
recovery, and development-RBF loading/video acceptance remain outside this
slice. SNES and every other
production system remain unsupported. The native path does not preserve a
running game across restart and does not start conventional Main, transient
MGLs, or an automatic legacy fallback. Fakes under `tests/` verify software
contracts only and are not physical evidence.

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
