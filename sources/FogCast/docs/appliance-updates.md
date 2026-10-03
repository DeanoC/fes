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
before claiming the kit or uploading. Keep the client running through reboot and
confirmation. The default update deadline is eight minutes plus six seconds per
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
| `GET /v1/update` | Bearer authentication | Actual boot/image, trial, raw idle, good/previous/pending and state integrity |
| `POST /v1/update/stage` | Raw image, exact Content-Length, `X-FogCast-Release-Manifest` base64 JSON | Verified immutable staged manifest (201) |
| `POST /v1/update/activate` | JSON `image_sha256` | Persist selection, flush `rebooting:true`, reboot (202) |
| `POST /v1/update/rollback` | Empty body | Select previous, flush, reboot (202) |
| `POST /v1/update/confirm` | JSON `boot_id`, `image_sha256` | Durable exact trial confirmation (200) |

All mutations require `X-FogCast-Kit-Lease` in addition to bearer authentication.
Manifest format 1 binds `de10-nano`, `fes-bootstrap-v1`, kernel/image SHA-256 and
size, version, and exact FES/FogCast/runtime commits. Uploads are raw ext4, with
no archive paths or scripts to extract.

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

For an appliance activation or rollback, the agent prepares the Cyclone V warm-reset
registers, arms the hardware watchdog, then requests a normal reboot. A detached
fallback requests `reboot -f` after 90 seconds if init shutdown stalls; the
watchdog resets the board after its 180-second timeout if shutdown still hangs.
If a just-confirmed trial's bootstrap guard still holds the watchdog, the agent
waits up to 12 seconds for its handover before arming. The fallback is
started independently, so it stays in place when the watchdog cannot be armed.
A reboot request error cancels the fallback and disarms the watchdog. On stable
startup, the agent disarms a stale watchdog only when no reboot marker exists.
Trial boots leave the watchdog untouched because the independent bootstrap trial
guard owns its reset deadline.
