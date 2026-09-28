# Bootable native media

This guide publishes and verifies the native DE10-Nano disk image. FES owns the
artifact and its host-side evidence. The selected runtime owns physical FPGA
transitions, and the FogCast target agent owns the kit lease.

## Build and verify the artifact

Start with a current clean integration build. Media assembly consumes its cold
receipts; it does not accept `make dev` output.

```sh
make build
make verify
make media
make verify-media
```

The normal FES package route uses the authenticated HIP/nextpnr producers for
Pong, ZX81 and Coleco. If a future system has no supported nextpnr route, its
recipe documents an explicit Quartus oracle/check; do not set Quartus as an
automatic fallback for the integration build.

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

Each successful output lives in an immutable evidence directory:
`media/generations/<image-sha256>/<evidence-sha256>/`. The outer hash identifies
exact disk bytes; the inner hash is the SHA-256 of `media.json`, which binds the
manifest, current validated input evidence, and recipe. `media/current` is a
relative symlink to one evidence directory. Identical disk bytes can therefore
have multiple immutable records when valid cold QEMU evidence or the media
recipe changes. Earlier records and image copies are retained.

This is an explicit amendment to the original one-level image-hash layout:
external verification evidence can change without changing disk bytes, so disk
identity alone cannot identify immutable evidence. No prior evidence is edited
or silently accepted as current. `make verify-media` validates current cold
host/image receipts and source/boot inputs first. If the selected evidence is
stale, it performs two independent media assemblies with the current recipe,
requires both to match the retained disk SHA, and repeats the raw-media and
rootfs structural/QEMU checks before publishing a new evidence directory.
Provisioned refresh uses a private, hash-checked snapshot extracted from the
retained disk; it does not require the original local config file. Changed cold
inputs or policy that produce different disk bytes fail without moving current.
Unchanged evidence is fully reverified and reused. Legacy one-level outputs
can be read-only refresh candidates; new publication uses both hashes.

## U-Boot and the FAT idle core

The card boots one idle core. Media assembly copies the sealed misteross
splash to FAT `/idle.rbf`, and the rootfs holds the same bytes at
`/usr/share/mister-runtime/idle.rbf` for Stop. Image policy
(`image/build/native-inputs.toml`) and media assembly reject a FAT boot core
that differs from the rootfs idle core.

U-Boot is derived from the locked MiSTer `uboot.img`. That file is four 64 KiB
SPL copies, then U-Boot proper as a legacy mkimage image at `0x40000`. Its
compiled-in default environment says `core=menu.rbf`, and `fpgaload` programs
that FAT file before `bridge enable`. `scripts/media_uboot.py` changes the
value to `core=idle.rbf`, which is the same length, and recomputes the legacy
data and header CRC-32s. The SPL copies and all other bytes are unchanged.
`boot-media.lock.toml` pins the upstream bytes as `[uboot_upstream]` and the
derived bytes as `[uboot]`. `make media` verifies the upstream image, derives
U-Boot into `out/work/boot-media/`, and checks the derived hash and its
`core=idle.rbf` environment before writing partition 2. The manifest records
the written U-Boot as `uboot.sha256` and the upstream as
`uboot.upstream_sha256`. The FAT core keeps the `splash` manifest fields, with
`splash.fat_destination = "/idle.rbf"`.

U-Boot, the kernel and FAT `/idle.rbf` change only when a card is
reprovisioned with new media. A network or appliance update replaces only the
system image. It does not rewrite U-Boot or FAT boot files, and it does not
delete them. A card provisioned before this change keeps U-Boot `core=menu.rbf`
and FAT `/menu.rbf` until it is reprovisioned. It still boots, but the
runtime offers `fes.memory.hps-ddr` only after a boot that latched the correct
port layout (see [HPS DDR](hps-ddr.md#boot-requirement)).

## Select and reverify a previous generation

Choose the retained disk/evidence hash pair from its directory and media.json:

```sh
make rollback-media GENERATION='<previous-image-sha256>/<previous-evidence-sha256>'
```

The command holds the same exclusive media lease as build and verification
across candidate validation, evidence refresh when needed, atomic selection,
directory sync, and failure rollback. A competing media operation fails while
that lease is held. If validation, sync, or SIGTERM/SIGINT/SIGHUP interrupts
selection, the previous current link is restored before the lease is released.
Use this command instead of replacing the symlink from a shell script.

Rollback must still satisfy the current selected cold inputs and media policy;
a historical directory alone does not authorize acceptance. If its bytes no
longer match the selected inputs, restore and verify the matching integration
state first. The command changes only the local published selection, never a
card or kit. Do not edit retained image or evidence files in place.

## Provision a local image

`make media` creates a ready-to-boot local image by deriving the target
`/fogcast/agent.toml` from the owner-only host configuration at
`~/.config/fogcast/config.toml`. Set `FES_HOST_CONFIG=/absolute/path/config.toml`
to use another private host file. The host file is read once, and only the
target token and fixed MiSTer runtime paths are copied; host library paths and
other settings never enter the image.

```sh
make media
```

The generated target file is embedded in the FAT partition, so inserting the
resulting card starts the target agent without a hand-created file. FES records
only its SHA-256 digest in the manifest and receipt. The host configuration and
the provisioned disk image stay private and are never printed or committed.

### Preserve the kit's discovery identity

With a discovery-capable FogCast host and agent, prepare the selected target's
identity in FogCast target settings before assembling its card. FogCast saves
an opaque `target_id` in that target's private host configuration. FES copies
the selected ID into `agent.toml`, allowing the host to find the same provisioned
kit after its network address changes. Keep the existing agent credential.

The ID is a canonical lowercase UUID. It is separate from the target's name,
address and boot ID, and stays unchanged across repeated builds and reboots.
For legacy host files, the optional top-level `target_id` is supported too.
Media assembly reads the host configuration without rewriting it or generating
an identity. Configurations without an ID retain their existing output; a
discovery-capable agent can persist its own ID and the host can learn it through
the configured address.

Prepare a separate target identity for each kit; cloning one provisioned card
onto two running kits makes discovery ambiguous. Explicit address configuration
remains available when the local network blocks multicast. The identity fields
require a matching FogCast version; this parent provisioning change alone does
not add discovery to an older agent image.

For a deliberately unprovisioned artifact, use the explicit CI/recovery mode:

```sh
make media FES_UNPROVISIONED=1
```

An explicit target configuration remains available for a nonstandard kit:

```sh
make media AGENT_CONFIG=/absolute/private/path/agent.toml
```

That path must be absolute, regular, non-symlink and owner-only. `CI=true`
also suppresses automatic provisioning so credentials cannot be picked up from
a runner's home directory.

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

## Per-card Ethernet MAC

Every card starts with the same U-Boot default Ethernet address, so two kits
flashed from one image would collide on the LAN. The rootfs init script
`/etc/init.d/S15fes-ethaddr` gives each card its own address on first boot. It
runs after `S10mdev` and before `S20mister-network` starts DHCP; the FES
bootstrap has already moved the writable FAT to `/media/fat`.

- If FAT `linux/u-boot.txt` already has an `ethaddr=` line, with any value, the
  script changes nothing. That is the manual override: write
  `ethaddr=<mac>` there yourself and it wins forever.
- Otherwise it reads the SD card CID from `/sys/block/mmcblk0/device/cid`. A
  readable, non-empty, non-zero hex CID gives `02:46:43:xx:xx:xx`, with the low
  three bytes being the first three bytes of SHA-256 of the trimmed CID string.
  The `02` first octet is locally administered and unicast.
- Without a usable CID it draws three random bytes from `/dev/urandom` once.
- It appends `ethaddr=<mac>` to `u-boot.txt`, creating the file when it is
  missing, preserving existing lines and adding a missing final newline. It
  writes a temporary file beside it, renames it into place and syncs. The file
  is the only persisted state, so a random address stays stable from then on.
  If the FAT is mounted read-only, the script remounts it read-write for the
  write and restores read-only afterwards.

The address follows the SD card, not the board: one SD card per kit is the
intended deployment. Moving a card moves its address. Because the CID is fixed
in the card, a reflashed card derives the same address again. Reflashing
replaces the FAT, which removes a manual `ethaddr=` line (and a random one), so
re-add an override after reimaging when you need a specific address.

On the first boot after flashing, U-Boot has already applied its default
address. The script therefore also sets the new address on `eth0` while the
link is still down (`ip link set dev eth0 down`, `address`, `up`), before
`S20mister-network` runs `udhcpc`, so the first DHCP lease already uses it. If
`eth0` is already up or missing, it is left alone and the persisted address
takes effect from the next boot, when U-Boot applies it. Nothing blocks boot:
a failed write keeps the address for this boot only and logs a warning.

The script logs one line such as
`fes-ethaddr: mac=02:46:43:a9:6a:37 source=cid persisted=yes eth0-set` to the
console and syslog, and writes the same line to `/run/fes-ethaddr`. The
`source` is `cid`, `random` or `existing`. The host test
`image/scripts/tests/ethaddr-init_test.sh` covers these cases through path
overrides and a no-op `ip`.

The same CID could later seed a per-card `target_id` or credential; that is
not implemented here.

## What verification proves

`make verify-media` reads the published artifact and checks the MBR, partition
geometry, FAT filesystem and payload bytes. It checks the embedded rootfs
against the cold image and reruns the rootfs structural and QEMU packaging
checks. It also verifies the manifest, receipts and the two matching assembly
hashes. The resulting `media.json` records these host checks as passing and
records hardware as `not-run`.

`make media`, `make verify-media`, and `make rollback-media` never write a block device, deploy to
a target, reboot a kit or claim a hardware lease. QEMU validates target
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
  (`/media/fat/linux/zImage_dtb`), FAT boot idle core (`/media/fat/idle.rbf`,
  which U-Boot programs as `core=idle.rbf`),
  and installed runtime Stop-idle artifact
  (`/usr/share/mister-runtime/idle.rbf`) with `rootfs.sha256`, `kernel.sha256`,
  `splash.sha256`, and `idle.sha256` in `fes-media.toml`. The FAT and rootfs
  copies are the same sealed core, so `splash.sha256` equals `idle.sha256`.
- Compare the installed rootfs, agent, runtime, kernel, splash, idle artifact and all
  selected package payloads before exact-artifact acceptance. Run:

  ```sh
  sha256sum /media/fat/linux/linux.img /media/fat/linux/zImage_dtb \
    /media/fat/idle.rbf /usr/share/mister-runtime/idle.rbf
  sha256sum /usr/sbin/mister-agent /usr/sbin/mister-runtime \
    /usr/share/mister-runtime/core-packages/*/core.rbf
  ```

  Compare the first line to the external `fes-media.toml` (`splash.sha256` for
  FAT `/idle.rbf`, `idle.sha256` for rootfs idle); compare the agent,
  runtime and each package digest to the retained cold `manifest.tsv` bound by
  that generation's `image.json`. Do not use a manifest from a different
  source revision, recipe or cold receipt.
- `/media/fat` is writable and the loop-mounted root is read-only.
- The selected `fes.pong`, `fes.zx81` and `fes.coleco` packages each launch
  through the normal host API, report the expected package identity, and Stop
  returns the system to idle. Use the package-runtime smoke target for this
  check; it does not substitute for separate input, video, audio or persistence
  acceptance for a changed package artifact.
- No Main process or `/dev/MiSTer_cmd` is present.
- Reboot produces a new boot ID and reaches ready/idle again.
- The final Stop succeeds and the kit lease is released.

Hardware remains `not-run` in the immutable media receipt. Store separate
dated acceptance evidence for the exact artifact instead of changing that
receipt or treating earlier image evidence as current acceptance.

## Prepare the on-kit launcher

The launcher uses the workstation's library and session service. Prepare the
selected target's stable identity in FogCast first, then run:

```sh
python3 scripts/prepare_launcher.py --host-address 192.168.10.2
```

Replace the example with the workstation's reachable LAN IPv4 address or DNS
name. `--host-config /absolute/path/config.toml` selects a nondefault private
FogCast configuration. The command writes `launcher-host.json` and
`launcher.json` beside that configuration, both mode 0600. They contain a
separate shared launcher token and the selected target UUID. Repeating setup
preserves the token and updates the host address; it rejects incomplete pairs
and identity changes. Keep the pair private. Setup prints no credentials.

Configure the FogCast host launcher listener with `launcher-host.json`. The
listener is `0.0.0.0:8789`; the kit companion points to the explicit workstation
address on port 8789. The existing browser listener remains separate. The host
must be running and reachable for browsing and launch services.

Normal local `make media` automatically embeds the prepared companion as
`/fogcast/launcher.json` beside the generated agent configuration. When using a
nondefault host configuration, pass the same absolute `FES_HOST_CONFIG` to media
assembly. Assembly requires the launcher and agent target identities to match.
`CI=true`, `FES_UNPROVISIONED=1`, and explicit `AGENT_CONFIG` do not discover a
launcher companion. Without a prepared companion, existing agent-only media
behavior is preserved.

Only the launcher's SHA-256 enters the closed media manifest and receipt. Media
verification checks the exact FAT path and embedded bytes, including when
refreshing retained generations; it does not consult today's host companion to
change a retained card. Generated images contain credentials and remain private.
