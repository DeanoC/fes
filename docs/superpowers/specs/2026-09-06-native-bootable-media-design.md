# Native Bootable Media Design

## Goal

FES will produce a deterministic raw disk image that can boot the selected
native system on a DE10-Nano from blank removable media. The disk image wraps
the existing verified native `linux.img`; it does not modify or replace that
root filesystem. The target boots the native runtime without the Main
executable as a production dependency. A provisioned image brings the target
agent to ready/idle; the default unprovisioned image remains at native idle
after the agent's bounded configuration wait exits.

The first image is a compact bootstrap artifact for development and acceptance.
It provides enough writable data space for configuration, the three selected
systems, small cartridge caches, save RAM, and one staged rootfs replacement.
Automatic expansion to the physical card and a general-purpose installer are
later milestones.

## Chosen approach

FES owns an outer-media assembler and verifier. It consumes a completed,
cold-build `linux.img`, the pinned MiSTer bootloader and combined kernel/DTB,
and the pinned idle FPGA artifact already present inside the rootfs. It emits a
two-partition MBR disk image whose FAT paths match the boot contract embedded in
the selected MiSTer U-Boot image. The exact geometry is an FES candidate until
the pinned bootloader source or a known-good card confirms it and cold-boot
acceptance passes.

This preserves the existing FogCast Buildroot recipe as the sole rootfs
producer during this milestone. Copying that recipe into FES would create two
authoritative builders; moving it is a separate migration. A first-boot
installer was also considered, but its destructive repartitioning and recovery
logic are unnecessary for proving the complete boot path. Updating an existing
MiSTer card was rejected as the primary artifact because it would not establish
that FES can boot blank media.

## Artifact boundary

The normal `native-integration-dev` profile gains two explicit commands:

```text
make media
make verify-media
```

`make media` requires current, intact cold-build `host.json` and `image.json`
receipts, `verification.json`, `reproducibility.txt`, and `qemu-smoke.log`.
Verification evidence must name the current rootfs digest, record passing
structural and QEMU checks, and bind both reproducibility-pass hashes to that
same digest. It does not accept the incremental `development.json` receipt and
does not rebuild the host, FPGA cores, compiler, kernel, or root filesystem. If
the prerequisite output is absent or stale, it fails with an instruction to
run `make build` and `make verify`.

The outputs are:

```text
out/native-integration-dev/media/current/fes.img
out/native-integration-dev/media/current/fes-media.toml
out/native-integration-dev/media/current/media.json
```

`fes.img` is the flashable raw disk. `fes-media.toml` is the closed external
manifest. `media.json` is the FES reuse receipt binding the current parent
fingerprint, input receipts, assembly recipe, manifest, and disk image. The
manifest stays outside the disk so its whole-image digest is not
self-referential. Each successful result is published as an immutable
`media/generations/<image-sha256>/` directory. Only after full verification does
the assembler atomically replace the `media/current` symlink with a relative
link to that generation. Existing generations remain available for rollback;
a failed assembly, verification, or republish cannot change `current`.

`make verify-media` does not modify published artifacts or hardware. It may
write logs and extracted files in a private disposable scratch directory. It
revalidates the current parent and rootfs receipts, the boot payload lock, the
external manifest, disk geometry, filesystems, embedded payload bytes, and
whole-image hash.

Assembly and publication hold a profile-scoped FES development lease so two
agents cannot race cache population or replace `current` concurrently. The
lease records owner, process, host and expiry, supports bounded stale takeover
through the existing explicit recovery interface, and is always released on a
normal or handled-error exit. Verification resolves `current` once and verifies
that immutable generation even if another process publishes later.

Historical profiles remain rootfs-only. The incremental `make dev` path remains
unchanged.

## Disk layout

The first candidate layout identifier is `de10-nano-mister-v1`. All sizes and
offsets are fixed in 512-byte sectors:

| Region | Start | Length | Meaning |
| --- | ---: | ---: | --- |
| MBR and alignment gap | 0 | 2,048 | MBR in sector 0; remaining sectors zero |
| Partition 1 | 2,048 | 524,288 | 256 MiB FAT32-LBA data partition, type `0x0c`, active |
| Partition 2 | 526,336 | 2,048 | 1 MiB raw Cyclone V boot partition, type `0xa2` |

The disk contains 528,384 sectors (270,532,608 bytes, 258 MiB). It uses an MBR
partition table with disk identifier `0x46455331`, signature `0x55aa`, exactly
two partition entries, and zeroed unused entries. Both entries use the canonical
LBA-only CHS sentinel `fe ff ff` for their start and end. Partition 1 has FAT
serial `0xf35d0001` and label `FESDATA`. Partition 2 is zero-filled before the
exact locked `uboot.img` bytes are written at its first byte.

The FAT filesystem contains exactly these assembly-owned boot paths:

```text
/menu.rbf
/linux/zImage_dtb
/linux/linux.img
/fogcast/
```

`/menu.rbf` is copied byte-for-byte from
`/usr/share/mister-runtime/idle.rbf` in the verified rootfs. It remains the
pinned Distribution_MiSTer menu FPGA artifact used as native idle; this design
does not claim a new FES idle core. The selected bootloader cold-loads that
filename before Linux and then loads `/linux/zImage_dtb`. Its kernel command line
mounts partition 1 at `/media/fat` and uses `linux/linux.img` as the read-only
loop root. Mounting partition 1 at `/media/fat` is an expected kernel/userspace
contract that must be confirmed during cold-boot acceptance.

No Main executable, MGL file, conventional Main command FIFO, ROM, game cache,
save data, or credential ships in the default image. The empty `/fogcast`
directory is writable after boot.

## Boot payload authority

FES adds a closed `boot-media.lock.toml`. It pins:

- `https://github.com/MiSTer-devel/Linux_Image_creator_MiSTer` commit
  `8aba321b2162e54b56522aa30758b22d97eec8da`;
- `uboot.img`, size 515,141 and SHA-256
  `e2d46cf9fe1ec40ca2c9c7409870249f267e06f70e5736dc6d30b4e21fe62a64`;
- `zImage_dtb`, SHA-256
  `a6c7b1be0da9ba24a91bc1816737915d6a6cfba27c6c3025caded95167dc8dae`
  and size 7,380,857;
- the expected embedded U-Boot environment: FAT partition 1,
  `/linux/zImage_dtb`, `menu.rbf`, and the read-only `linux/linux.img` loop root;
- the layout identifier, geometry, FAT identifier, and assembly-tool identity.

The existing selected rootfs receipt and manifest remain authoritative for
`linux.img` and its installed idle artifact. The media assembler extracts the
idle bytes without mounting the ext4 image and checks them against the child
manifest and native build inputs before placing them on FAT.

Payload fetching uses a revision-scoped cache under `out/work/boot-media/`.
Every use rechecks repository revision, clean status, size, and SHA-256. Network
access is needed only to populate a missing cache. Assembly uses cached bytes.

## Deterministic assembly

Assembly runs unprivileged in a pinned Linux container and never attaches a
loop device or writes a host block device. The tool environment supplies FAT
creation and copy utilities; its container digest and reported tool versions
are part of the recipe fingerprint.

The assembler fixes the MBR identifier, FAT serial, label, timestamps, file
ordering, directory ordering, allocation order, padding, and total image size.
It creates two independent temporary images and requires identical SHA-256
digests before publication. The existing rootfs two-pass evidence remains a
separate prerequisite rather than being inferred from media determinism.

The normal build fingerprint is split so adding or changing the media assembler
does not invalidate an otherwise unchanged host or rootfs receipt. Media
fingerprints include the relevant parent profile, input receipt hashes, boot
lock, assembler/verifier sources, and tool identity.

## Optional local provisioning

The default artifact contains no `agent.toml`. A separate explicit invocation
may add one for a local kit:

```text
make media AGENT_CONFIG=/absolute/path/agent.toml
```

The config must be an absolute, regular, non-symlink file with bounded size. Its
contents are copied to `/fogcast/agent.toml`; they are never printed, committed,
or copied beside the output image. The manifest records only its SHA-256 and a
`provisioned = true` flag. The receipt fingerprint includes that digest, so an
unprovisioned result cannot be reused as a provisioned one or vice versa.

The config bytes are opened and snapshotted once into owner-only scratch before
hashing or either assembly pass, preventing a mid-build change from producing
mixed evidence. Provisioned scratch and generation directories use mode `0700`;
provisioned disk images and manifests use mode `0600`, including retained failed
outputs until cleanup. CI uses the unprovisioned mode and synthetic fixture
configurations. Documentation explains how to add or replace the FAT-side
configuration after flashing.

## Manifest and verification

The closed format-1 TOML manifest records:

- target and layout identity;
- FES revision/profile and media recipe hash;
- rootfs path, size, SHA-256, image-receipt SHA-256, and child manifest hash;
- each boot payload's repository, revision, path, size, SHA-256, and destination;
- idle artifact source identity and both rootfs/FAT destinations;
- sector size, disk size/identifier, both exact partition entries, FAT label and
  serial;
- provisioned state and optional configuration SHA-256;
- output image size and SHA-256;
- both assembly-pass hashes;
- separate structural-media, assembly-reproducibility, rootfs-structural and
  rootfs-QEMU statuses. Hardware status is not part of the build claim and is
  recorded as `not-run` when a generation is first published.

The verifier rejects unknown or duplicate manifest fields, noncanonical paths,
unexpected partitions, overlaps, out-of-bounds regions, nonzero alignment
padding, a changed active flag/type/geometry, malformed FAT metadata, missing or
extra assembly-owned boot paths, changed payload bytes, a truncated image,
unexpected nonzero boot-partition tail bytes, stale receipts, or changed recipe
and tool identities.

It reads the embedded `linux.img` back from FAT and checks byte identity with
the existing output. It stages the extracted copy under the child verifier's
allowed `/work/build/output/target-image/` namespace inside the disposable
workspace, then invokes the selected FogCast structural verifier and QEMU
rootfs smoke. QEMU must use the already verified VExpress kernel cache created
by `make verify`; `verify-media` fails if that cache is absent or stale rather
than building a kernel. These checks retain their existing meaning: QEMU
exercises userspace and does not claim to emulate the DE10-Nano bootloader or
FPGA.

## Tests

Most parent tests use small fixture payloads while exercising the same code
paths. At least one sparse-image test uses the complete production geometry and
a valid FAT32 filesystem; reduced fixtures must also remain FAT32 rather than
silently exercising FAT12 or FAT16. They cover:

- exact MBR encoding, FAT metadata, paths, hashes, padding, and raw boot write;
- deterministic independent assembly;
- rejection of stale or incremental receipts and changed rootfs inputs;
- rejection of changed locks, boot payloads, idle bytes, geometry and manifests;
- config omission, safe config inclusion, symlink/path/size rejection, and no
  secret text in logs or manifests;
- initial publication, republishing over an existing valid generation, atomic
  `current` replacement, and preservation of the last valid generation after
  injected assembly or verification failure;
- extraction and delegation to the existing child rootfs verifier;
- command help, profile restrictions, and receipt reuse.

The full parent suite, `make check`, clean `make build`, `make verify`, two-pass
media assembly, and `make verify-media` run before hardware work.

## Hardware acceptance

Hardware acceptance uses a newly written disposable card or image-backed test
medium and the designated kit lease. Existing rootfs deployment evidence does
not establish blank-media bootability.

Acceptance is a separate evidence record bound to the exact provisioned
disk-image hash and non-secret configuration hash; it changes hardware status
for that hash without mutating the immutable media generation. It verifies:

1. cold boot from the newly written media reaches native ready/idle without a
   Main process or `/dev/MiSTer_cmd`;
2. the installed rootfs, agent, runtime, kernel, idle artifact and three core
   hashes match the manifest;
3. `/media/fat` is writable while the loop root remains read-only;
4. Pong, Mega Drive and SNES launch, accept input and emit HDMI audio, with Stop
   returning to idle under one lease;
5. SNES save creation/restore survives a full reboot;
6. the reboot produces a new boot ID and returns to ready/idle;
7. final Stop succeeds and the lease is released.

The physical media device is resolved and displayed before any write. Writing a
block device requires a separate explicit operator command and existing device
authorization; `make media` and `make verify-media` never write hardware.

## Failure and recovery

All fetch, assembly and verification operations fail closed. Temporary outputs
remain outside the published directory and are removed after failure. An
existing valid published image is retained. No command automatically falls back
to stale rootfs receipts, incremental outputs, upstream Main, alternate boot
payloads, a different target device, or relaxed validation.

Boot-media hardware testing keeps a recoverable copy of the previously accepted
target image and save directory. Kit lease expiry and explicit operator takeover
remain the recovery mechanisms for abandoned sessions.

## Deferred work

This milestone does not include automatic partition expansion, an installer UI,
in-place updates of arbitrary existing MiSTer cards, general system provisioning,
rootfs recipe migration from FogCast, building U-Boot from source, a new idle
FPGA core, more systems, SNES enhancement chips, or production release signing.
