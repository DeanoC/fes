# Bootable native media

This guide publishes and verifies the native DE10-Nano disk image. FES owns the
artifact and its host-side evidence. The selected runtime owns physical FPGA
transitions, and the FogCast target agent owns the kit lease.

## Build and verify the artifact

Start with a current clean integration build. Media assembly consumes its cold
receipts; it does not accept `make dev` output.

```sh
QUARTUS_ROOTDIR=/absolute/path/to/intelFPGA_lite/17.0 make build
make verify
make media
make verify-media
```

`make media` requires intact current `host.json`, `image.json`,
`verification.json`, `reproducibility.txt` and QEMU evidence. If one is absent
or stale, rebuild and verify before retrying. It publishes:

```text
out/native-integration-dev/media/current/fes.img
out/native-integration-dev/media/current/fes-media.toml
out/native-integration-dev/media/current/media.json
```

`media/current/fes.img` is the flashable raw disk image. `linux.img` alone is
only the target root filesystem and cannot boot a blank card. The manifest is
kept outside the disk so it can record the whole-image hash without making that
hash self-referential.

Each successful output lives in an immutable content-addressed directory under
`media/generations/<image-sha256>/`. `media/current` changes atomically only
after assembly and verification succeed. A failed assembly or verification
leaves the previous current generation intact.

## Select and reverify a previous generation

Use this host-only rollback only after identifying the prior generation SHA-256
from its retained `media.json`. It changes the local `current` symlink; it does
not write a card or contact a kit.

```sh
media=out/native-integration-dev/media
generation=<previous-image-sha256>
test -d "$media/generations/$generation"
test -f "$media/generations/$generation/media.json"
ln -s "generations/$generation" "$media/.next-current"
mv -Tf "$media/.next-current" "$media/current"
make verify-media
```

Do not edit a generation in place or skip the final verification. A retained
generation is usable only with the matching selected sources, media recipe and
cold-build receipts (`host.json`, `image.json`, `verification.json`,
`reproducibility.txt`, and QEMU evidence). `scripts/media.py rejects stale
evidence`: if those inputs no longer match the generation receipt,
`make verify-media` fails. Rebuild and verify the matching integration state
rather than treating an old image directory as accepted.

## Provision a local image

The default media image is intentionally unprovisioned. It can reach runtime
idle, but it contains no target-agent credentials and therefore cannot reach
agent ready/idle. For a designated local kit, create an owner-only private
configuration file and build a separate provisioned generation:

```sh
make media AGENT_CONFIG=/absolute/private/path/agent.toml
```

The configuration path must be absolute and private. FES snapshots it locally,
copies it into the disk at `/fogcast/agent.toml`, and records only its digest in
the manifest and receipt. Do not commit, print or share the configuration or
its contents. A provisioned generation is distinct from an unprovisioned one;
keep the generated disk image private as well.

### Add or replace configuration on a flashed card

For the approved local-kit flow, configuration can instead be added or replaced
after the image has been flashed. With the card's FAT data partition already
mounted at the local `FAT_MOUNT` directory, copy the private file into the
existing `fogcast` directory, then flush and unmount it before booting the kit:

```sh
FAT_MOUNT=/run/media/$USER/FESDATA
config=/absolute/private/path/agent.toml
test -d "$FAT_MOUNT/fogcast"
sha256sum "$config"
cp -- "$config" "$FAT_MOUNT/fogcast/.agent.toml.new"
mv -f -- "$FAT_MOUNT/fogcast/.agent.toml.new" "$FAT_MOUNT/fogcast/agent.toml"
sync
sha256sum "$FAT_MOUNT/fogcast/agent.toml"
umount "$FAT_MOUNT"
```

The two digests must match. The target sees this FAT-side file at
`/media/fat/fogcast/agent.toml`. Record its non-secret digest with the
acceptance evidence. This local card change is outside the immutable generated
image: `make verify-media` verifies `media/current/fes.img`, not a card whose
configuration was changed after flashing. Never treat the post-flash card as
byte-identical to an unprovisioned `fes.img`; bind acceptance to its base image
SHA-256 and the recorded configuration digest.

## What verification proves

`make verify-media` reads the published artifact and checks the MBR, partition
geometry, FAT filesystem and payload bytes. It checks the embedded rootfs
against the cold image and reruns the rootfs structural and QEMU packaging
checks. It also verifies the manifest, receipts and the two matching assembly
hashes. The resulting `media.json` records these host checks as passing and
records hardware as `not-run`.

Neither `make media` nor `make verify-media` writes a block device, deploys to
a target, reboots a kit or claims a hardware lease. QEMU validates target
userspace packaging; it does not emulate the DE10-Nano bootloader, FPGA, video,
audio or input hardware.

## Physical-card and hardware acceptance

Writing a physical card is a separate operator action. Before it, resolve and
display the exact device, obtain explicit authorization for that device, and
claim the designated kit through the existing [kit-sharing workflow](kit-sharing.md).
A build or successful media verification does not authorize a write to any
device. Follow the selected FogCast [development guide](../sources/FogCast/docs/DEVELOPMENT.md)
for the kit designation and lease operation.

Record acceptance against the exact provisioned `fes.img` SHA-256 and the
non-secret configuration digest. On the first cold boot from a newly written
card, verify all of the following before releasing the lease:

- Before calling the result exact-artifact acceptance, compare installed bytes
  with the retained manifests for this exact cold receipt. Compare the installed
  rootfs (`/media/fat/linux/linux.img`), kernel
  (`/media/fat/linux/zImage_dtb`), FAT idle artifact (`/media/fat/menu.rbf`),
  and installed runtime idle artifact
  (`/usr/share/mister-runtime/idle.rbf`) with `rootfs_sha256`, `kernel_sha256`,
  and `idle_sha256` in `fes-media.toml`. The two idle hashes must both match
  the pinned idle value.
- Compare the installed rootfs, agent, runtime, kernel, idle artifact and all
  three cores before exact-artifact acceptance. Run:

  ```sh
  sha256sum /media/fat/linux/linux.img /media/fat/linux/zImage_dtb \
    /media/fat/menu.rbf /usr/share/mister-runtime/idle.rbf
  sha256sum /usr/sbin/mister-agent /usr/sbin/mister-runtime \
    /usr/share/mister-runtime/cores/megadrive.rbf \
    /usr/share/mister-runtime/cores/pong.rbf \
    /usr/share/mister-runtime/cores/snes.rbf
  ```

  Compare the first line to the external `fes-media.toml`; compare the agent,
  runtime and all three core digests to the retained cold `manifest.tsv` bound
  by that generation's `image.json`. Do not use a manifest from a different
  source revision, recipe or cold receipt.
- `/media/fat` is writable and the loop-mounted root is read-only.
- Pong, Mega Drive and SNES each launch, accept input, emit audio, and Stop
  returns the system to idle.
- An SNES save survives a full reboot.
- No Main process or `/dev/MiSTer_cmd` is present.
- Reboot produces a new boot ID and reaches ready/idle again.
- The final Stop succeeds and the kit lease is released.

Hardware remains `not-run` in the immutable media receipt. Store separate
dated acceptance evidence for the exact artifact instead of changing that
receipt or treating earlier image evidence as current acceptance.
