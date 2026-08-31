# FogCast Naming Cleanup Design

## Purpose

Make the repository describe the system by its current jobs rather than by
the order in which experiments happened. A fresh clone must expose one
obvious target-image path, one direct deployment path, and one real hardware
smoke path. Historical implementations remain available only through Git.

## Canonical names

- Project, GitHub repository, and Go module: `FogCast`,
  `DeanoC/FogCast`, and `github.com/DeanoC/FogCast`.
- Linux root image toolchain: `target-image`.
- Buildroot external identity and board: `FOGCAST_TARGET` and
  `buildroot/board/fogcast-target`.
- Buildroot configurations: `fogcast_target_dev_defconfig` and
  `fogcast_target_prod_defconfig`.
- Writable target directory: `/media/fat/fogcast`.
- Physical launch check: `target-smoke`.
- Host capture sender: `remote-play-sender`.

There will be no compatibility aliases for numbered POC names, stage names,
or `mister-remote`. Git history is the compatibility record.

## Runtime boundary

This cleanup preserves the proven game path:

```text
browser or target-smoke
  -> host session API
  -> target cache and launch API
  -> mister-agent
  -> transient MGL and /dev/MiSTer_cmd
  -> resident Main-compatible process
  -> FPGA core
```

The target image starts the resident Main-compatible executable from
`/media/fat/MiSTer`, starts the FogCast agent from
`/media/fat/fogcast/mister-agent`, and reads agent configuration from
`/media/fat/fogcast/agent.toml`.

The image requires `MiSTer` and `menu.rbf` to exist but does not pin their
hashes. This permits the dedicated kit to run the project's Main fork and
future libmister-runtime implementation without rebuilding a stock-artifact
acceptance chain.

## Target-image toolchain

All active POC1B build assets are renamed together:

- Make targets use the `target-image-*` prefix.
- scripts use `target-image` in filenames, messages, temporary paths, and
  test interfaces.
- environment variables use the `TARGET_IMAGE_*` prefix.
- container assets live under `containers/target-image`.
- inputs and generated files use `build/target-image.*`,
  `build/cache/target-image`, and `build/output/target-image`.
- Buildroot files use the canonical external identity, board directory, and
  defconfig names.
- init-time image checks use `fogcast_target_smoke=1` and
  `FOGCAST_TARGET_SMOKE_*` messages.

One committed `build/target-image.sources.lock.toml` records the container,
Buildroot, image-creator, and kernel source revisions needed to rebuild.
Generated image and kernel hashes belong in generated manifests under
`build/output/target-image`; they are not written back into the source lock.

The old stock-device inventory lock is removed. Buildroot configuration and
image tests directly assert the libraries and packages required by the
resident Main-compatible process.

## Deployment and physical smoke

One deployment script replaces the checkpoint installers. It:

1. accepts an explicit image or defaults to the development target image;
2. uploads to `/media/fat/linux/linux.img.new`;
3. checks that the upload is non-empty;
4. renames it to `/media/fat/linux/linux.img`;
5. syncs and reboots the designated target.

An interrupted upload cannot replace the active image because the rename is
the only mutation. There is no checkpoint database, provenance chain,
automatic rollback, or recovery supervisor. Re-imaging the disposable kit is
the fallback.

`target-smoke` accepts a game ID and expected core name. It checks the host
and target health endpoints, launches through the host session API, waits for
the expected `/tmp/CORENAME`, stops through the host API, and waits for
`MENU`. It does not inject NAS, process, upload, or reboot failures.

The dedicated MiSTer is migrated from `/media/fat/mister-remote` to
`/media/fat/fogcast` when the renamed image is deployed. No symlink or old
path is retained.

## Removed code and documents

Delete rather than rename these superseded areas:

- POC1A package, bootstrap, inventory, installer, and stock lock;
- POC2 output lock, installer, checkpoint, restore, and provenance code;
- both operator-assisted HIL/failure-injection frameworks;
- the stage-A0 publishing workflow and stale roadmap;
- tests whose only subject is deleted machinery.

Rename the working remote-media sender command from `remote-play-spike` to
`remote-play-sender`. Keep legitimate runtime terms such as content staging,
database transaction rollback, and ROM metadata value `prototype`.

`IDEA.md` remains only as concise product vision. `README.md`,
`docs/ARCHITECTURE.md`, `docs/DEVELOPMENT.md`, and `AGENTS.md` describe the
current positive path without preserving historical subsystem names as
warnings.

## Verification

The cleanup is accepted when all of the following are true:

- repository shell and Go tests pass;
- UI tests and `go vet ./...` pass;
- target-image fixture, reproducibility, QEMU, and deploy tests pass where
  they do not require external hardware;
- `git diff --check` passes;
- tracked paths and text contain none of the numbered POC names, stage-A0,
  POC6, `fpgadev`, `native-coordinator`, `mister-remote`, or
  `remote-play-spike`;
- the dedicated target reboots the renamed image, both APIs become healthy,
  a real game launches into the expected core, and stop returns to `MENU`;
- the GitHub repository is renamed to `DeanoC/FogCast` only after all local
  and hardware checks pass, and `origin` points at the renamed repository.

After implementation is complete, this design and its implementation plan
are deleted from the active tree. The resulting current behavior is recorded
only in the canonical README, architecture, development guide, and policy.

## Non-goals

- Adding development-RBF launch in this cleanup.
- Replacing the resident Main-compatible implementation.
- Adding security, ownership, failover, rollback, or recovery systems.
- Preserving old command names, paths, environment variables, or build
  outputs.
