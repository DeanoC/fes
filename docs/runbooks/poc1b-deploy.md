# POC 1B Deployment and Offline Recovery

This procedure is only for the dedicated complete MiSTer Pi development kit. The SuperStation One must be powered off and remain untouched. The two checkpoints deliberately change one boot artifact at a time:

- `binary-kernel` replaces only `/media/fat/linux/linux.img` and retains the accepted POC 1A kernel.
- `source-kernel` requires the accepted POC 1B development root, stores the reproduced modules in a versioned FAT directory, and replaces only `/media/fat/linux/zImage_dtb`.

The installers refuse hashes that are absent from the POC 1B lock, unexpected current artifacts, changed accepted FAT assets, altered one-time backups, extra package content, and out-of-order checkpoints.

## Physical recovery gate

Do not run either installation checkpoint until all of these are true:

1. A freshly prepared spare MiSTer Pi SD card or a verified full-card image exists and is readable from the MacBook.
2. The dedicated MiSTer Pi has HDMI, the accepted wired USB controller, and wired Ethernet connected.
3. The SuperStation One is powered off and will not be used as a recovery target.
4. The MacBook can physically remove and mount the dedicated MiSTer Pi SD card if boot networking fails.
5. The offline command `scripts/restore-poc1a-sd.sh /Volumes/MISTER` is visible in a local terminal or note before the first reboot.
6. The accepted stock menu and both test games have just been checked manually on the dedicated MiSTer Pi.
7. With that accepted POC 1A system still running, `scripts/capture-poc1a-lock.sh` and `scripts/verify-poc1a-lock.sh` have recorded and rechecked the `linux_root` artifact at `/media/fat/linux/linux.img` as well as the other accepted artifacts.

The committed pre-deployment lock predates the `linux_root` trust anchor, so the installer intentionally remains fail-closed until this physical baseline recapture is performed. Run it only after the recovery prerequisites above are satisfied:

```sh
export MISTER_TARGET=root@MISTER_IP
scripts/capture-poc1a-lock.sh
scripts/verify-poc1a-lock.sh
```

Review the resulting `linux_root` path and SHA-256 in `build/sources.poc1a.lock.toml` before packaging either checkpoint. Do not hand-edit or infer this hash from the root image being installed.

The target-side installer makes same-volume, one-time backups before the first replacement:

```text
/media/fat/linux/linux.img.pre-poc1b
/media/fat/linux/zImage_dtb.pre-poc1b
/media/fat/linux/poc1b-checkpoint.state
```

Never rename, edit, or replace those files. A repeated installation verifies them against the hashes in the checkpoint state and refuses any difference.

## Finish and verify local outputs

Task 7 records the already reproduced outputs in the source lock. Until `[outputs]` exists with all three lowercase SHA-256 values, the transport wrapper intentionally refuses to run.

```sh
mise exec go@1.26.5 -- make build-lock
bin/poc1b-lock record-outputs \
  --lock build/sources.poc1b.lock.toml \
  --prod build/output/poc1b/prod/linux.img \
  --dev build/output/poc1b/dev/linux.img \
  --kernel build/output/poc1b/kernel/zImage_dtb
mise exec go@1.26.5 -- make check
```

Review the lock before committing it. It must not contain a bearer token, a ROM path, or any machine-specific credential. Re-run the image and kernel verifiers before touching hardware:

```sh
POC1B_CONTAINER_RUNTIME=docker scripts/verify-poc1b-image.sh dev \
  build/output/poc1b/dev/linux.img \
  build/output/poc1b/dev/manifest.tsv \
  build/output/poc1b/dev/library-report.tsv
POC1B_CONTAINER_RUNTIME=docker scripts/verify-poc1b-kernel.sh \
  build/output/poc1b/kernel
```

## Checkpoint 1: reduced root with accepted binary kernel

Set the target without placing the agent token in shell history:

```sh
export MISTER_TARGET=root@MISTER_IP
POC1B_CONTAINER_RUNTIME=docker scripts/install-poc1b.sh binary-kernel
```

The Mac wrapper verifies the locked development image, scans the exact transport payload for secret assignments, and uploads it to a fixed installer-owned path on the FAT volume rather than consuming target RAM under `/tmp`. It then streams the target installer over SSH as root. The target verifies the accepted Main, Menu, root image, binary kernel, Mega Drive core, SNES core, and controller map. It creates or validates the one-time root/kernel backups, identity-checks and stops the POC 1A/POC 1B supervisors plus their children, moves the verified extracted image to `linux.img.poc1b.new`, and renames it over `linux.img` on the FAT volume. It does not replace the kernel, bootloader, Menu, Main, cores, ROMs, or controller map.

Cold power-cycle the MiSTer Pi. Health must become ready within 45 seconds. If it does not, stop the checkpoint: do not attempt checkpoint 2. Power off, remove the SD card, mount it on the MacBook, and follow the offline restore procedure below.

Before the checkpoint-1 HIL run, use development SSH only to verify the read-only/volatile mount policy and accepted kernel identity. Stop Dropbear, confirm its process and port are absent, close SSH, and run the unchanged POC 1A HIL sequence. Do not use SSH during gameplay acceptance.

## Checkpoint 2: reproduced source kernel

Checkpoint 2 is allowed only after checkpoint 1 has passed and the current files still match the accepted POC 1B development root and POC 1A binary kernel:

```sh
export MISTER_TARGET=root@MISTER_IP
POC1B_CONTAINER_RUNTIME=docker scripts/install-poc1b.sh source-kernel
```

The target stores the verified module archive and kernel manifest beneath:

```text
/media/fat/linux/modules.poc1b/5.15.1-MiSTer/
```

It does not delete or overwrite accepted modules. It then verifies `zImage_dtb.poc1b.new` and renames it over `zImage_dtb`. If power or transport is lost after that rename, rerun the same `source-kernel` command: the installer re-verifies the reproduced kernel, versioned modules, manifest, backups, and root before reconciling checkpoint state. Repeating an already completed checkpoint is also safe. Cold power-cycle again and require health within 45 seconds. On failure, perform offline recovery before further diagnosis.

## Recorded POC 1B hardware evidence

On 2026-08-02 the dedicated MiSTer Pi completed the source-kernel checkpoint after a cold reboot. The running kernel reported `5.15.1-MiSTer`; `/` was read-only, `/media/fat` was writable, the development root hash was `e038679bc82623b2911ef0e3876233ed95c6b3d546e77320cd6db2992647faa7`, and the reproduced `zImage_dtb` hash was `cb66e22edb04a44d883e82f62fa7eeca0d7d2b715b08ab72ad2dc5bc2a3178c5`. The unchanged HIL suite passed all 34 checks, including Mega Drive and SNES HDMI video/audio, controller playability, agent restart reconciliation, invalid requests, alternating launches, and black idle output. Local evidence is `artifacts/hil/poc1b-source-kernel.json` (SHA-256 `f0d62113c24a6aee371e9163d9202c2d9c5cd808fc6a341cb4be8f37bfd74a32`).

## Offline restore on the MacBook

Power the dedicated MiSTer Pi off, remove its SD card, and mount the FAT volume. Confirm its actual mount name under `/Volumes`; do not guess it and do not use a path outside `/Volumes`.

```sh
scripts/restore-poc1a-sd.sh /Volumes/MISTER
```

The script resolves the mount path, requires both precise `.pre-poc1b` backups and the checkpoint state, prints their recorded hashes, verifies them, copies them to same-volume temporary files, and renames the verified copies over `linux.img` and `zImage_dtb`. It does not format the card, recursively delete anything, or remove the backups.

Eject the card normally, reinstall it in the dedicated MiSTer Pi, and cold power-cycle. Manually confirm the stock menu, HDMI video/audio, wired controller, and both games before attempting any new POC 1B work. If the offline restore cannot verify both backups, stop and use the prepared spare SD or verified full-card image instead.
