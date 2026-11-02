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

The CLI defaults and card workflow below target DE10. Release export and
storage also support an explicitly selected `de25-nano`: FES
`export_release(..., board='de25-nano')` and the Go codec/store `ForBoard`
entry points. Their defaults remain DE10-only, and neither selects a board
from an uploaded manifest. A store pins its factory, staged and retained images
to the selected board and stable kernel. Image assembly must independently
verify the rootfs payload and ELF target before calling the exporter. This
does not qualify a DE25 bootstrap, native update/recovery or managed image;
the [normal-kit requirements](design/de25-linux-platform.md) remain open.
For a prepared DE25 release, `fes-update --board de25-nano` also requires the
authenticated target's reported board to match before upload, and the image
must contain a DE25/AArch64 software-base record. The target independently
pins its store to its immutable image's `/etc/fes-board` and CPU architecture;
an upload cannot select that board. Direct-root headless images still provide
no appliance-update service. The base storage helper's existing build target
accepts `FES_KIT_DATA_ARCH=aarch64`, retaining ARMv7 as its default.

The FES platform builder also supports
`build_static_arm(..., architecture='aarch64')`, and the existing bootstrap
assembler supports `assemble_bootstrap(..., board='de25-nano')`. Both require
explicit selection: default validation refuses an AArch64 executable or DE25
factory. Build provenance pins the CPU environment and selected sources. The
bootstrap writes `/etc/fes-board` into its immutable ext4 root; PID1 checks it
against the executing CPU before opening board-specific release storage.

Terasic's unchanged boot script supplies a partition-2 ext4 root, unlike DE10's
kernel-mounted loop root. The DE25 bootstrap admits the exact board/SoC and
root device, remounts that stable root read-only, then mounts partition 1 at
`/media/fat` with device, executable and set-ID use disabled. It reuses the
existing verified release selection and root-switching implementation. The
prepared kernel has the required loop/ext4/FAT/codepage support built in.
DE25 trial boots are refused before starting the watchdog guard; Cyclone V
reset-register preparation remains DE10-only. Native watchdog recovery must
be qualified before DE25 trial updates can execute.

The [bootstrap record](validation/2026-11-01-de25-appliance-bootstrap/README.md)
retains native/AArch64 CPU checks and a reproducible bootstrap image whose
manifest uses the existing headless rootfs as a binding fixture. That fixture
is not a managed factory or a bootable appliance card. Full DE25 factory/card
assembly and native boot/update/recovery remain open; the normal Make/CLI card
workflow below has not switched platforms.

The shared disk constructor and two-pass reconstructor now also accept an
explicit DE25 selection. Its layout reserves a 2 GiB FAT partition for the
factory, Good, Previous and staged system images, and puts the shared 32 MiB
bootstrap in partition 2. It preserves the vendor's `u-boot.itb`, boot script,
reserved boot bytes, partition numbers and serial/root contract, while checking
the selected owner kernel/DTB and every owned FAT payload. The pinned formatter
uses 4,194,288 filesystem sectors within the 4,194,304-sector FAT partition;
verification requires the exact geometry and its 8 KiB zero tail.

This is the lower construction layer, with the
[disk-construction evidence](validation/2026-11-02-de25-managed-media/README.md).
The public release/media CLI still needs the actual admitted factory and its
image-level boot smoke evidence. Construction fixtures do not satisfy those
gates or establish a bootable normal kit. The existing release exporter has
not been given fabricated QEMU evidence or a headless-root substitution.

The DE25 builder now provides an
[actual AArch64 image boot check](validation/2026-11-02-de25-image-boot/README.md).
It boots the selected rootfs and its real kernel in QEMU, and the factory
producer binds its boot report/log into the factory receipt. The retained
managed-base intermediate passes the filesystem smoke. A subsequent
[D-Bus repair and fresh image boot](validation/2026-11-02-de25-dbus-rootfs/README.md)
also establishes responsive virtual system-bus startup. These do not supply
factory acceptance. The high-level
release/media adapter still needs explicit DE25 receipt selection and an
admitted factory with its own successful boot evidence.

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
`release.json`, and external verification provenance. Its default location,
`out/<profile>/appliance/releases/<rootfs-sha256>/<manifest-sha256>-<passes>pass/`,
keeps one- and two-pass evidence for the same image and version apart. It
requires the existing verified image receipt, structural checks and QEMU
packaging result. Its FES revision identifies the actual cold build; later
documentation or assembly-only commits do not force recompilation when selected
sources and build recipe match.

The kernel and validated FPGA bundles retain their existing locks and cached
artifacts. OS builds use one pass by default. Request `IMAGE_PASSES=2` consistently
for build, verify and release when checking two independent rootfs builds for
reproducibility; use `make dev` for incremental development. Each ordinary update
ships only the new system image and manifest.
The release manifest binds the kernel hash, board, bootstrap ABI, version, and
exact FES/FogCast/runtime revisions. It never embeds the rootfs's own hash inside
that rootfs. `image_passes` stays in the release evidence sidecar. A one-pass
development release records `1` there and preserves the requested version. The manifest
keeps the legacy field set so an already deployed agent can stage it.
`manifest_format` is `major.minor`; readers treat a missing value as `1.0`.
The same major with a higher minor may add unknown optional fields, which the
update path ignores and logs. An unknown major is rejected.

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

`make bootstrap` builds the static ARM `fes-boot` binary from FES `platform/`
against the selected FogCast `appliance` module. Bootstrap evidence records the
platform tree identity, appliance-module content identity, selected FogCast
revision, Go toolchain identity and exact build flags. Retained bootstrap
verification rejects a stale platform or appliance-module identity. Absolute
checkout paths are not stored in receipts or manifests.

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
ext4 labelled `FESDATA3` without moving A2 or rewriting FAT. It sizes p3 to the
card in the admitted reader; `--card-bytes`, if given, must match that card.
FogCast bind-mounts cache, saves, core-data, launcher-cache and evidence from
that partition. The
bootloader, kernel and boot environment retain their locked bytes. The
kernel mounts `/linux/linux.img`, now the small stable bootstrap. Factory system
bytes live at `/fogcast/releases/images/<sha256>.img`; the corresponding manifest
is present on FAT and baked into the bootstrap. FAT `/idle.rbf` (the core the
derived U-Boot programs as `core=idle.rbf`), kernel, agent and launcher
configuration are included. On first boot no state file is necessary:
the verified factory is selected, then the native services start normally.

The extra space holds factory, known-good, previous and a staged candidate.
When the release volume already has room for the new image, a 1 MiB margin,
and the splash staged at confirmation (an idle core up to 8 MiB, its sidecar,
and a temporary copy), staging writes and verifies it there and does not
delete anything.
When it does not, staging first verifies the upload on a different filesystem
that is not tmpfs or ramfs and has room for the image plus that margin. On
the kit that filesystem is the mounted FESDATA3 volume or a FogCast directory
bind-mounted from it. A truncated or corrupt upload removes only that
temporary file and leaves every release in place. A spill file left behind
when that write is interrupted is removed on the next stage, before free
space is measured. After verification, staging
deletes other release images oldest first, and only until the new image, the
margin, and the splash reserve fit, then copies the verified file into the
release store. It
keeps the factory image, known-good, previous, pending and trial selections,
the image named by `boot.json`, and every release image in the store that
currently backs a loop device. Staging does not change the boot selection and
does not remove bootloader files. If no qualifying filesystem can hold the
upload, or those retained images still leave too little free space, staging
fails with insufficient storage and deletes nothing. The conventional 256 MiB
layout is unchanged and is too small for this appliance update workflow.

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

Software goes before the image. Before uploading, `update` reads the image's
`/etc/fes/software-base.json` and refuses an image whose menu interfaces the
kit's selected software bundle does not accept; install and activate a bundle
that accepts them first. A bundle built for another base contract cannot go
first, so that update proceeds with a notice and the image runs its factory
software until a bundle built for it is activated
([image compatibility](kit-software-releases.md#image-compatibility)).

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
for repair. Updates never replace bootstrap, kernel, U-Boot, credentials or
saves, and they do not rewrite `u-boot.txt`. Confirmation copies the confirmed
image's sealed idle core to FAT `/idle.rbf`. Recovery assumes the stable boot
files and the card/watchdog hardware still work. A change to U-Boot still needs
new media ([bootable media](bootable-media.md#u-boot-and-the-fat-idle-core)). A
card provisioned with the earlier `core=menu.rbf` U-Boot keeps FAT `/menu.rbf`
and that boot layout until it is reprovisioned; updates do not delete it.

See the selected FogCast `docs/appliance-updates.md` for the target routes and
`docs/ARCHITECTURE.md` for the bootstrap/store/coordinator source entry points.
