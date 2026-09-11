# Appliance releases and automatic fallback

This extends the native appliance with versioned system images,
network updates and watchdog-bounded trial boots. FES assembles the files; the
selected FogCast agent manages transfers and the existing kit lease. It adds no
UI. Physical acceptance is recorded separately against exact artifact hashes.

**Hardware status:** bootstrap root switching passes isolated Linux tests. The
production watchdog helper passes five consecutive physical recoveries, a
three-minute stability check, and confirmation-close with three-minute stability.
The prepared 1 GiB card was subsequently booted on the designated kit; a good
network update and confirmation, invalid-init fallback, and rollback to factory
all passed. The exact live-card evidence is in
[the appliance release record](validation/2026-09-08-appliance-release.md).
The deterministic assembly receipts still mark their artifact hardware field
`not-run`; they do not replace the live-card record.
The [current candidate record](validation/2026-09-08-appliance-release.md) lists
the exact verified outputs and live acceptance scope.

## Build once from selected inputs

Start with clean selected component checkouts, then run the existing integration
checks and stabilized image build:

```sh
make check
make build
make verify
make release RELEASE_VERSION=0.2.0-dev.1
```

The release command prints an immutable directory containing `rootfs.img`,
`release.json`, and external verification provenance. It requires the existing
two-pass image receipt, structural checks and QEMU packaging result. Its FES
revision identifies the actual cold build; later documentation or assembly-only
commits do not force recompilation when selected sources and build recipe match.

The kernel and validated FPGA bundles retain their existing locks and cached
artifacts. Cold two-pass builds rebuild the compiler and base packages in each
independent pass; use `make dev` for incremental development. Each ordinary update
ships only the new system image and manifest.
The release manifest binds the kernel hash, board, bootstrap ABI, version, and
exact FES/FogCast/runtime revisions. It never embeds the rootfs's own hash inside
that rootfs.

## Create a bootable appliance card file

Use the printed release directory to assemble the stable bootstrap and card:

```sh
make bootstrap RELEASE=/absolute/path/to/release-directory
make appliance-media RELEASE=/absolute/path/to/release-directory \
  BOOTSTRAP=/absolute/path/to/bootstrap-directory \
  OUTPUT=/absolute/path/to/new-card-directory
make verify-appliance-media RELEASE=/absolute/path/to/release-directory \
  BOOTSTRAP=/absolute/path/to/bootstrap-directory \
  OUTPUT=/absolute/path/to/new-card-directory
```

`card.img` is a file, never an automatic block-device write. The directory also
contains external evidence. Both commands use the existing exclusive media lease;
verification reconstructs the complete card bytes using the same source-bound
inputs and provisioning. Unknown files, hidden changed bytes, stale provenance
or different credentials fail verification. Outputs containing credentials are
owner-only. Keep them private.

The default automatically generates agent and launcher configuration from the
existing private host configuration, as the conventional media builder does.
The operator does not create configuration files on the card. For intentional
unprovisioned CI media use `scripts/appliance_media.py build --unprovisioned`
with the same release/bootstrap/output arguments. An explicit `--agent-config`
supports nonstandard provisioning. The legacy `make media` path remains available
for direct-root images.

The appliance layout `de10-nano-appliance-1g-v1` expands FAT partition 1 to 1 GiB
and places the unchanged A2 boot partition immediately after it. FAT starts at
sector 2048; A2 starts at sector 2099200. The disk file is 1,075,838,976 bytes.
The assembler still emits that fixed image. Leftover capacity on a larger card
is a live mutation: `scripts/appliance_expand_dry_run.py` can add aligned p3
ext4 labelled `FESDATA3` without moving A2 or rewriting FAT. FogCast bind-mounts
cache, saves, core-data, launcher-cache and evidence from that partition. The
bootloader, kernel and boot environment retain their locked bytes. The
kernel mounts `/linux/linux.img`, now the small stable bootstrap. Factory system
bytes live at `/fogcast/releases/images/<sha256>.img`; the corresponding manifest
is present on FAT and baked into the bootstrap. `/menu.rbf`, kernel, agent and
launcher configuration are included. On first boot no state file is necessary:
the verified factory is selected, then the native services start normally.

The extra space holds factory, known-good, previous and a staged candidate.
Unreferenced images are retained for now; there is no automatic garbage
collection. Staging refuses insufficient free space without changing the boot
selection. The conventional 256 MiB layout is unchanged and is too small for
this appliance update workflow.

Writing a physical card still requires exact-device authorization and the kit
sharing procedure. A new installation of the bootstrap is distinct from an
ordinary network update. Preserve the last named working rootfs during any
one-time conversion of a direct-root card: never replace/unlink a mounted FAT
image's last filename. Existing saves/configuration remain outside the images.

## Update and recover

Use the selected FogCast operator client with the existing private host config:

```sh
make -C /absolute/path/to/FogCast build-fes-update
/absolute/path/to/FogCast/bin/fes-update --action status
/absolute/path/to/FogCast/bin/fes-update --action update \
  --release /absolute/path/to/release-directory
/absolute/path/to/FogCast/bin/fes-update --action rollback
```

The client acquires and renews the existing lease. Activation stops the runtime,
persists supported saves, neutralizes input/cast and records the pending image
before reboot. It observes the changed authenticated boot and expected image,
waits for startup cleanup, obtains fresh ownership and confirms raw native idle.
Lost responses are observed without replaying activation. Keep the client running
through confirmation; another owner is never automatically displaced.

The stable bootstrap consumes one trial before executing it and runs a hardware
watchdog guard independently of the replaceable rootfs. Missing/broken init,
failure to start services/networking, a hung candidate, or a disappearing host
cannot confirm themselves. The trial deadline is 180 seconds plus hardware reset
latency. An unconfirmed trial returns to verified known-good, then factory if
needed. A rejected trial and factory fallback are recorded so future updates and
rollback refer to the image actually booted.

Rollback selects the previous confirmed release as a new protected trial.
Corrupt/torn selection state chooses factory and preserves the corrupt record
for repair. Updates never replace bootstrap, kernel, U-Boot, credentials or saves.
Recovery assumes those stable files and the card/watchdog hardware still work.

See the selected FogCast `docs/appliance-updates.md` for the target routes and
`docs/ARCHITECTURE.md` for the bootstrap/store/coordinator source entry points.
