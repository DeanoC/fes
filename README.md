# FES

Fogger Entertainment System builds a pinned FogCast Linux host and native MiSTer image from independent component repositories.

The baseline profile is `native-dev`: FogCast `cd85971bf0bffe36e69c381917f618620b901726` and libmister-runtime `443b603de991b56b5f4d0d11c5bc88a3f83fad13`. The `native-source-dev` profile adds misteross `7912a3e7ee82a24c9aed82dfbb30a8974b7eec07`, rebuilds Mega Drive with Quartus 17.0.2, validates its sealed bundle, and packages that RBF with the same native image recipe. `native-dev` remains unchanged and continues to use the upstream RBF.

## Build

On a Linux amd64 machine with Git, GNU Make, Python 3.11+, Go and a working Docker daemon:

```sh
git submodule update --init --recursive
make doctor
make build
make verify
```

For a new clone, use `git clone --recurse-submodules git@github.com:DeanoC/fes.git`. Access to this private repository and its component repositories is required.

Go selects the toolchain from the pinned FogCast `go.mod` (currently Go 1.26.5). The image uses the child's pinned Debian container, Buildroot sources and ARM toolchain. The first build downloads those inputs and runs two independent image builds. Allow several GB for downloads, tools and intermediate outputs. Subsequent builds hash-check the recorded inputs and outputs before reuse.

`CONTAINER_RUNTIME=podman make build` selects a Docker-compatible runtime; Docker is the validated choice. `make doctor` checks source pins, cleanliness, runtime-lock agreement, builder architecture, host tools and container-daemon access. It does not prove every upstream download is reachable.

Source builds require the exact Quartus tree:

```sh
QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0 \
  make build PROFILE=native-source-dev
QUARTUS_ROOTDIR=/home/deano/intelFPGA_lite/17.0 \
  make verify PROFILE=native-source-dev
```

The source profile publishes under `out/native-source-dev/`, including
`megadrive.rbf` and `megadrive-rbf.toml`. Failed image builds can reuse a
validated bundle from the staged misteross revision. `make rebuild
PROFILE=native-source-dev` forces Quartus and both clean image passes.
The online fetch phase obtains Buildroot packages before compilation runs
with container networking disabled. `doctor` does not check Quartus; a fresh
FPGA build requires the tree above. Verification and output reuse do not.

## Commands

| Command | Result |
| --- | --- |
| `make build` | Build Linux API server, CLI and native image; reuse unchanged verified outputs |
| `make host` | Build only the Linux API server and CLI |
| `make image` | Build only the native image and structurally verify it |
| `make verify` | Verify published hashes, rerun image structural checks and QEMU packaging smoke, compare with the recorded baseline image |
| `make rebuild` | Force host compilation and two fresh image passes |
| `make test` | Test the parent's source-pin checks and output-reuse rules |
| `make doctor` | Check prerequisites without building |

Supported profiles are `native-dev` and `native-source-dev`. Version and host architecture live in their profile files; ambient Go build flags, Go workspaces and inherited Make overrides are normalized so they cannot silently change cached products. The Go toolchain version is part of the cache fingerprint.

## Outputs

```text
out/native-dev/
  fogcast-api             Linux amd64 API server with browser UI
  fogcast                 Linux amd64 CLI
  linux.img               ARMv7 native target root filesystem
  reproducibility.txt     hashes from both independent image passes
  manifest.tsv            filesystem manifest
  library-report.tsv      target library closure
  inputs.json             source revisions, profile, Go version and parent recipe hashes
  host.json               host input fingerprint and product hashes
  image.json              image input fingerprint and product hashes
  verification.json       verification gates and historical hash comparison
  qemu-smoke.log          captured packaging smoke output
```

Run the host explicitly with your configuration:

```sh
out/native-dev/fogcast-api --config /absolute/path/config.toml --listen 127.0.0.1:8787
```

The SDL tenfoot client is not included in this first slice. Neither build nor verify deploys, reboots or contacts the MiSTer. QEMU checks root filesystem and init packaging; it does not test FPGA behavior. The historical hardware baseline applies to its recorded source/image pair, not to arbitrary future updates.

## Boundaries

- Submodule gitlinks pin component revisions. Builds reject changed HEADs, tracked edits and untracked source files.
- FogCast's runtime lock must agree with the runtime gitlink.
- FogCast retains the existing image recipe and source/RBF locks. The parent fetches common image sources before calling its native build.
- Disposable standalone component clones under `out/work/` isolate each child's Git identity from the parent and keep metadata accessible inside container mounts. Each clone is derived from its pinned submodule and checked for matching identity and cleanliness.
- Every parent checkout uses a distinct Docker output volume. Builds in the same checkout are serialized.
- Downloads and component intermediates live in the staged clones under `out/work/`. Published products live under `out/native-dev/`.
- The parent temporarily builds `cmd/fogcast-api` with the child's Go flags and explicit Linux settings because the pinned child Makefile hard-codes Darwin for that target.

For a deliberate version update, check out the intended commits in both submodules and stage their gitlinks with `git add sources/FogCast sources/libmister-runtime`. Their runtime lock must already agree. The profile's baseline image hash is a historical comparison, reported separately from the mandatory two-pass, structural and QEMU gates. Updating it requires a newly established baseline; do not substitute a new build hash just to make the comparison match.

The next integration slice is complete legacy FAT-side assembly. Main_MiSTer and mister-packages should enter the parent only when that slice consumes them.

## Historical image provenance

The [recorded acceptance](https://github.com/DeanoC/FogCast/blob/249988a7a4eb37ea7f974ee6d5b4700af35bebb1/docs/hardware/native-megadrive-baseline.md) image has SHA-256 `95c9b4671e0d19781a6428d2168ab631453740215b194519f12788ade03c7c2e`. Its acceptance record names FogCast `cd85971`, but the embedded agent's Go build metadata names `19a3d5e`. An independent reconstruction using clean cd85971 source in a linked worktree beneath the old 19a3d5e checkout reproduced the historical agent byte for byte (SHA-256 d5cfab61a45e79205a5aab922bbed46b4905968a665f8432f1121a426dcb60c1). A standalone cd85971 build produces agent SHA-256 7790789857b4bb4cbb3f6083f799811f4fcf4a12eb86b15599ea571acd8fb928 with the correct source stamp. Building directly inside a nested submodule also stamped the parent's Git state during this implementation.

The parent uses standalone source copies so the agent embeds the selected FogCast revision correctly. Therefore the new image is not assumed to byte-match the historical image. `make verify` records `historical_baseline_match` in `verification.json` and independently requires matching hashes from both new build passes, structural verification and QEMU packaging checks. The first image's file-content manifest differs from the historical image only in `usr/sbin/mister-agent` and `usr/share/mister-runtime/build-inputs`; the latter differs only in its agent SHA-256. The runtime and both RBFs match the historical hashes. No new physical-hardware acceptance is claimed.

See [design](docs/native-parent-design.md) and [implementation plan](docs/implementation-plan.md).

## Validated on powerboat, 2026-09-05

- Six parent tests pass, including source identity, lock compatibility, source staging and corrupt-output rejection.
- Both independent native image passes produce SHA-256 `03197cb4fa867df1fa8fe6d0ef4f42eefecc2a7a47b8e87b73f0323e6a6a5a8d`.
- Structural checks and QEMU startup packaging pass.
- A subsequent `make build` reuses both verified outputs.
- A fresh recursive clone passes doctor and tests and produces byte-identical Linux CLI/API binaries.
- A forced host rebuild with ambient `GOAMD64=v3 GOFLAGS=-race VERSION=9.9.9` retains the profile's amd64/v1 target and version 0.1.0 and produces the same host hashes.

Local logs and evidence are under `out/`; baseline build products and verification results are under `out/native-dev/`. The parent repository is [DeanoC/fes](https://github.com/DeanoC/fes).

The source profile also passed ten parent tests, two identical image passes,
structural checks, QEMU packaging smoke and output reuse. Its exact image was
deployed to the designated MiSTer and passed a bounded Sonic 2 launch,
direction/jump, Stop/relaunch and idle/reboot check. See
[source build validation](docs/source-build-validation.md) for hashes,
evidence paths and the distinction from the older hardware acceptance.
