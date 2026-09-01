# libmister-runtime

`libmister-runtime` is the standalone C++14 lifecycle library and local control
daemon for the native MiSTer hardware-control direction. The repository keeps
the lifecycle API, native Linux primitives, profile model, and
`mister-runtime` daemon behind one build and one ownership boundary.

## Current status

Software-tested only. Production native construction includes one fixed
1280x720@60 menu-core video path for the image-owned idle baseline. Physical
status remains pending later evidence from an exact pinned FogCast image.
Hardware-supported systems: 0.

The current production profile table is empty. Native game video and
development-RBF video are not supported or claimed. The fake hardware and
profiles under `tests/` verify software contracts only and are not evidence of
system support.

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
