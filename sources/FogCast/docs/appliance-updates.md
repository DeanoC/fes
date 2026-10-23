# Native FES appliance updates

This path requires a card provisioned with the stable FES bootstrap. Existing
direct-root cards need a one-time FES migration; copying an update over their
live `/linux/linux.img` is not this workflow. FES assembles the release from clean
selected commits and verified rootfs/kernel inputs.

Build the operator client in this checkout:

```sh
make build-fes-update
bin/fes-update --action status
bin/fes-update --action update --release /absolute/path/to/release-directory
bin/fes-update --action rollback
bin/fes-update --action confirm --release /absolute/path/to/release-directory
```

The client reads `~/.config/fogcast/config.toml`. `--config` selects another private
host configuration; `--target` selects a named enabled target with recorded
`target_id`. Tokens are never command-line arguments. A release directory contains
`release.json` and `rootfs.img`; the client verifies the complete local digest
before claiming the kit or uploading. Update then reads the image's
`/etc/fes/software-base.json` straight from `rootfs.img` and, after verifying the
target, compares it with the kit's selected software bundle. It refuses the image
when that bundle does not accept every menu interface the image requires: install
and activate a bundle that does, then update. A bundle built for another base
contract cannot go first; the client logs that the image will hold it back and
run its own factory software, and continues
([kit software releases](../../../docs/kit-software-releases.md#image-compatibility)).
Keep the client running through reboot and confirmation. The default update deadline is eight minutes plus six seconds per
MiB of the release image, rounded up to a minute (21 minutes for 128 MiB).
`--timeout` overrides it. The client logs transfer and confirmation progress to
stderr. If it exits during reboot or trial, rerun with `--action confirm` and
either `--release` (only `release.json` is read) or `--image-sha256` to confirm
that exact image. Confirm waits for a pending reboot or raw idle trial and does
not upload or activate a release. Rollback defaults to 10 minutes, confirm to
15 minutes, and status to one minute.

Updates stop the running game before reboot; successful Stop persists supported
SNES battery saves. The client acquires the existing kit lease and renews during
transfer. If another owner holds the kit, the operation fails without takeover.
After reboot it discards old authority and claims a fresh lease only when startup
cleanup has finished. The UI can resume normal use once confirmation succeeds.

Rollback selects the previous confirmed release as another bounded trial. Failed
or unconfirmed trials fall back to known-good, then factory if necessary. The
factory image, bootstrap, kernel and bootloader are retained. There are no rootfs
migrations of save/configuration formats in this release ABI.

| Target route | Request | Result |
| --- | --- | --- |
| `GET /v1/update` | Bearer authentication | Actual boot/image, trial, raw idle, good/previous/pending, state integrity, `fat_idle_sha256`, `idle_sha256`, and `splash_warning` when a non-trial splash repair fails |
| `POST /v1/update/stage` | Raw image, exact Content-Length, `X-FogCast-Release-Manifest` base64 JSON | Verified immutable staged manifest (201) |
| `POST /v1/update/activate` | JSON `image_sha256` | Persist selection, flush `rebooting:true`, reboot (202) |
| `POST /v1/update/rollback` | Empty body | Select previous, flush, reboot (202) |
| `POST /v1/update/confirm` | JSON `boot_id`, `image_sha256` | Durable exact trial confirmation (200) |

All mutations require `X-FogCast-Kit-Lease` in addition to bearer authentication.
Manifest format 1 is `manifest_format` `1.0` and binds `de10-nano`,
`fes-bootstrap-v1`, kernel/image SHA-256 and size, version, and exact
FES/FogCast/runtime commits. A manifest may omit `manifest_format`; that means
`1.0`. The same major with a higher minor may include unknown optional fields.
`fes-update` and `POST /v1/update/stage` ignore those fields and log their
names. An unknown major, a malformed `manifest_format`, a duplicate field, or
a missing or malformed required field is rejected. Readers also accept optional
`image_passes`, a positive integer, when a manifest already contains it. New
releases omit `manifest_format` and `image_passes`, so `release.json` stays on
the field set the deployed agent already accepts. A one-pass HIL release
records `image_passes` in `evidence.json` and uses a `-1p` version suffix. The
client keeps a present `image_passes` value in the manifest it uploads.
Uploads are raw ext4, with
no archive paths or scripts to extract.

`POST /v1/update/stage` returns `507` with code `UPDATE_NO_SPACE` and message
`insufficient release storage` when the release volume cannot hold the image.
`fes-update` prints that code and message. Other rejected uploads stay `422`
`UPDATE_INVALID`. When the release volume already has room for the image, a
1 MiB margin, and the splash written at confirmation (an idle core up to
8 MiB, an 80-byte sidecar, and a temporary copy of that core), staging writes
and verifies it there and does not delete anything. When it does not, staging
verifies the upload on another filesystem before deleting anything. That
filesystem must be a different device from the release volume, must not be
tmpfs or ramfs, and must have room for the image plus the 1 MiB margin. The
splash reserve stays on the release volume. The kit uses the mounted FESDATA3 volume
(`/run/fogcast/fesdata3`) or a FogCast directory bind-mounted from it. A
truncated or corrupt upload removes only that temporary file. A spill file left
behind when that write is interrupted is removed on the next stage, before
free space is measured. Only regular `.stage-spill-` files in that store's
own staging directory are removed. After the
upload passes the size, ext4, and digest checks, staging deletes unreferenced
images oldest first, only until the image, the margin, and the splash reserve
fit, then copies the verified file
into the release store. It keeps the factory image, known-good, previous,
pending, trial, the `boot.json` image, and every image in the release store
that backs a loop device. It does not change the boot selection and does not
touch `u-boot.txt` or FAT `/idle.rbf`. If no qualifying filesystem can hold
the upload, or the retained images still leave too little free space, staging
returns the storage error and deletes nothing. An unreadable `boot.json`,
corrupt selection state, or a failed loop-device scan refuses the prune.
Prune also deletes `splashes/<image sha256>.rbf` and its `.sha256` sidecar
with that image.

Confirmation copies the running image's sealed idle core onto FAT `/idle.rbf`.
The agent checks `/usr/share/mister-runtime/idle.rbf` against `idle_sha256` in
`/usr/share/mister-runtime/build-inputs` and stages that file as
`splashes/<image sha256>.rbf` in the release store. It records the image as
known-good, then replaces `/media/fat/idle.rbf` through a same-directory
temporary and rename, and only then records the confirmation the watchdog
reads. A failed or cancelled publish leaves the watchdog armed. The same boot
retries, and a later boot of that image republishes the splash from the
running image even if the staged copy was removed. An unconfirmed trial leaves
`/idle.rbf` unchanged, so a watchdog fallback still boots the previous image
with the splash from the last successful publication. Factory fallback does
not write FAT itself. When that image starts, the agent republishes its idle
core. U-Boot programs the splash that is on FAT at the following boot.
Confirming the restored image writes that image's splash back.
`GET /v1/update` reports `fat_idle_sha256` for the FAT file and `idle_sha256`
for the running image record. When a non-trial boot cannot publish that
splash, the agent logs the failure and the same response still returns the
boot state and both digests, with `splash_warning` set to the repair error.
A trial boot does not use that field: the watchdog stays armed until the FAT
publish succeeds. U-Boot, the kernel, `u-boot.txt` and `/menu.rbf`
are not rewritten.

If a response is lost, inspect status; do not blindly replay activation. A pending
selection after a failed reboot request remains visible and is attempted on the
next reboot. An unconfirmed trial blocks normal launch/input/cast and has a
180-second bootstrap deadline plus the hardware watchdog reset interval. A client
that disappears cannot silently promote it. Corrupt selection state boots factory
and rejects further update mutations until the card state is repaired.

If the card already has an ext4 `FESDATA3` partition, the native agent bind-mounts
cache, saves, core-data, launcher-cache and evidence from that volume during
startup. It copies missing FAT files onto p3 first and never wipes releases or
credentials. That bind is refused while status shows trial, pending, or corrupt
so a trial boot keeps using the 1 GiB FAT trees.

Before arming a trial watchdog, the bootstrap verifies the ARM DE10-nano device
tree and prepares the Cyclone V boot ROM for a warm reset. The locked U-Boot
enables booting from retained on-chip RAM; physical diagnostics showed that this
path did not recover after watchdog expiry. The bootstrap marks the completed
preloader valid (`SYSMGR+0xc8 = 0x49535756`) and disables retained-RAM boot
(`SYSMGR+0xe0 = 0`). Both writes require matching readback before the watchdog is
opened. Marking the preloader valid preserves its SD image selection across
repeated resets; it does not confirm the candidate system image. Unexpected board
identity, register policy, access errors, or failed readback prevent arming.
The [Cyclone V boot ROM flow](https://docs.altera.com/r/docs/683126/21.2/cyclone-v-hard-processor-system-technical-reference-manual/boot-rom-flow)
and [SoCAL register definitions](https://raw.githubusercontent.com/RTEMS/rtems/5/bsps/arm/altera-cyclone-v/include/bsp/socal/alt_sysmgr.h)
describe these controls. Kernel and U-Boot bytes remain unchanged.

Automatic fallback assumes readable stable boot files/factory and functioning
card/watchdog hardware. Hardware results belong to the exact FES artifact record;
the package tests and isolated root-switch test are not physical acceptance.

## Reboot backstop

For an appliance activation or rollback, the agent installs independent
backstops right before it requests a normal reboot, then never touches them
again. Shutdown has three reset layers:

- A detached userspace fallback (ignores TERM/HUP/INT) forces sysrq `b`, then falls back
  to `reboot -nf`, only when
  shutdown has stalled: no block-device I/O progress for 30 seconds, or a hard
  deadline of 150 seconds while I/O keeps progressing. It first runs an explicit
  sync bounded to 10 seconds; sysrq `b` skips device shutdown, while `reboot -nf`
  avoids another sync if sysrq fails or returns. The capped stop
  scripts and software supervisor wait at most 29 seconds without I/O in total, below the 30-second stall
  trigger (`image/scripts/tests/shutdown-budget_test.sh`). This covers a stall
  inside rcK. BusyBox init has no kill-all omit list: after its synchronous
  `::shutdown` actions it sends SIGTERM and SIGKILL to every process except
  pid 1, so the fallback helper cannot survive that phase.
- The last `::shutdown` action runs `fes-reboot-backstop` after `/bin/umount -a
  -r`, normally with filesystems read-only, and before init's kill-all and
  `reboot(2)`. Read-only state is not guaranteed if a remount fails. If
  `/proc/sys/kernel/hung_task_panic` is not writable, the action best-effort
  remounts `/proc` read-write and, if needed, mounts a fresh procfs before
  arming `kernel.hung_task_panic=1`, a 20-second
  hung-task timeout, a 2-second check interval and a 3-second panic reboot
  delay. The detector fires only for a task that stays in
  `TASK_UNINTERRUPTIBLE` without a context switch for the whole timeout, so a
  progressing sync that switches on each writeback wait does not trip it. A
  task already tracked as stuck (for example, one blocked since boot) can trip
  at the first scan after arming. A newly blocked task is recorded by its first
  scan and trips within about timeout plus check interval, then panic waits 3
  seconds. A normal reboot takes about 1-2 seconds; reset is about 3-30 seconds
  after the action, versus 180 seconds for the hardware watchdog. This covers
  hangs after kill-all or inside `reboot(2)`, such as a wedged USB disk blocking
  kernel `device_shutdown` while waiting for its async probe. Unbinding or
  deleting that SCSI device before reboot was rejected because
  `scsi_remove_device`/`sd_remove` and USB-storage disconnect wait on the same
  stuck async probe or control thread and could block the shutdown action
  itself. The `sda` device is neither registered nor mounted, so there is
  nothing to skip syncing.
- The Cyclone V hardware watchdog, after the warm-reset register preparation,
  set to 180 seconds and never petted. It is kept only if its read-back timeout
  leaves room for at least a 60-second fallback deadline plus the 10-second sync
  and a 20-second margin (90 seconds); the fallback deadline is shortened so it
  always ends 20 seconds before the watchdog. Shorter read-backs (60-89 s) are
  magic-closed and only the fallback remains.
- If a just-confirmed trial's bootstrap guard still holds the watchdog, the agent
  waits up to 12 seconds for its handover. A reboot request error disarms the
  watchdog first, then kills the fallback.
- On stable startup, the agent disarms a stale watchdog only when no reboot
  marker exists. Trial boots leave the watchdog untouched because the
  independent bootstrap trial guard owns its reset deadline.
