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
after assembly and verification succeed. To roll back, select a previously
verified generation through the approved integration procedure; do not modify
a generation in place. A failed assembly or verification leaves the previous
current generation intact.

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
