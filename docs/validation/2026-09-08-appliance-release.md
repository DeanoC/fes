# Appliance release candidate: physical acceptance complete

Version `0.2.0-dev.1` is a locally built candidate for the native three-system
appliance. Source and artifacts have not been published. The prepared card was
installed in the designated kit and accepted on 2026-09-08. It booted the
stable bootstrap, completed a network update and confirmation, rejected an
unbootable candidate before candidate init, fell back to the confirmed image,
and completed a rollback to the factory release. The separate
[watchdog diagnostic record](2026-09-08-appliance-watchdog.md) documents the
physical reset and confirmation-close primitives that passed before this card
was installed.

## Exact inputs and outputs

| Item | Revision or SHA-256 |
| --- | --- |
| FES cold/assembly revision | `d12b6a6302c099d9884041a36df36486fe0ebc24` |
| FogCast | `980be19710a5e1ab3d5f98ab74d01eb51509844d` |
| libmister-runtime | `bdf56abd16afcc29bdca45cdafafeb5b0b56a3c8` |
| Rootfs, 64 MiB | `2607d27a70ced3fa6c9cc97cd0ad4b1114d374472d9ee426ee6cbfce98ea0f6c` |
| Stable bootstrap, 32 MiB | `b228e50961fc01a9df3d32189a72d867446c6f580d18bbe141497190c03cc1c7` |
| Static bootstrap executable | `de3b72f2cca1b91ebb671cff73c87009b129919168cbff9aec1a53fb2cd74c24` |
| Locked kernel | `a6c7b1be0da9ba24a91bc1816737915d6a6cfba27c6c3025caded95167dc8dae` |
| Card, 1,075,838,976 bytes | `670337b0c2a1d964b01363b12dd3a76dc1925146dd14edb00ba939ef78a11fea` |

The disk uses `de10-nano-appliance-1g-v1`: 1 GiB FAT at sector 2048, followed
by the unchanged A2 bootloader partition at sector 2099200. Kernel, U-Boot and
the previously validated source-built FPGA bundles retain their locked bytes.

## Completed checks

- FES regression suite: 173 tests, 36 expected host/container skips. Real pinned-
  container bootstrap and card suites execute through their host drivers.
- Full final FogCast Go suite, focused race tests, vet and static ARM compilation
  pass. Independent reviewers checked store durability, trial admission,
  bootstrap isolation, operator retries, artifact provenance and reset MMIO.
- Parent consistency validates package YAML, seven generated consumers, three
  copied core-source pins and selected component/runtime locks.
- Both cold rootfs passes produced the rootfs hash above. Structural checks and
  QEMU packaging smoke passed. QEMU does not emulate the MiSTer FPGA.
- Release export and source-selected bootstrap assembly passed. Both independent
  bootstrap assemblies matched.
- `make appliance-media` produced matching independent complete disk assemblies.
  `make verify-appliance-media` separately reconstructed them and matched the
  retained card and its external evidence.

The assembly evidence retains `hardware: not-run`; that field describes the
reproducible artifact and is separate from the live-card acceptance below. The
card directory is mode 0700, with card/evidence files mode 0600 because
provisioning includes credentials. Do not commit or share the private card.

## Local artifact locations

These paths are beneath the isolated FES worker
`/home/deano/fes/out/dev/appliance-release/fes`:

```text
out/native-integration-dev/appliance/
  releases/2607d27a70ced3fa6c9cc97cd0ad4b1114d374472d9ee426ee6cbfce98ea0f6c/
    0f56a06112d7701aac9c0cce8e712fed33dea7dfe68a64148a3513375f777092/
      rootfs.img, release.json, evidence.json
  bootstrap/9019a6d0a912f71c9496f35fcf8f840086ebb49889083be8107da77407782b59/
    linux.img, evidence.json
  card-0.2.0-dev.1/
    card.img, evidence.json
```

The card automatically includes agent and launcher configuration. Comparison
with the existing kit backup preserved all existing agent values and all launcher
values; the added explicit `target_id` matches the saved kit identity. No manual
configuration-file creation is required.

## Physical installation

The old card's 256 MiB FAT partition is too small for bootstrap, factory,
retained old image and update candidates. A private verified backup retains the
existing identity, configuration, cache and both SNES save files. Installation
requires the kit card in the USB reader, exact-device identification and the
existing kit-sharing procedure. The migration used the exact removable device
described below; no system disk was touched and the backed-up state was restored
only under `fogcast/cache` and `fogcast/saves`.

## Card write receipt

On 2026-09-08 the verified image was written to the removable device
`/dev/disk/by-id/usb-Generic_STORAGE_DEVICE_000000000819-0:0` (USB model
`STORAGE DEVICE`, serial `000000000819`). The device was 28.8 GiB, unmounted,
and its pre-write MBR matched the old FESDATA/A2 layout. The write used `dd`
with a 4 MiB block size and an `fsync` completion. It transferred 1,075,838,976
bytes at 16.8 MiB/s.

The written region was then compared byte-for-byte against `card.img`; the
comparison passed. The new MBR has disk ID `0x46455331`, FAT partition 1 at
sector 2048 with 2,097,152 sectors, and A2 partition 2 at sector 2,099,200
with 2,048 sectors. No system disk was touched. The backed-up user state was
then overlaid under `fogcast/cache` and `fogcast/saves`; all seven restored files
were byte-verified against the private backup. The immutable source image hash
above still describes the generated artifact; the physical card intentionally
differs in those mutable state paths after restoration. The card was flushed and
unmounted cleanly.

## Live-card acceptance

All transitions used the normal authenticated `fes-update` client and a fresh
kit lease. The client never replayed an activation after a lost response.

| Check | Result |
| --- | --- |
| Initial boot | Boot ID `423d9fb8-a115-4c28-a8f9-6d4879a34685`; bootstrap `b228e509…`; root `2607d27…`; health ready and native idle |
| Good network update | Diagnostic image `f277c11c…` (`0.2.0-dev.2-diagnostic`) uploaded once, activated once, rebooted as `ea470ffe-cc3d-4737-b08d-c48df8ba8029`, and was durably confirmed; previous became factory `2607d27…` |
| Invalid-init fallback | Candidate `b3795759…` (`0.2.0-dev.3-bad-init`) was a valid ext4 upload with a non-executable `/sbin/init`; activation returned a new boot `6be52daa-e80a-45aa-b59f-e1135f783813` on confirmed `f277c11c…`; the client refused to confirm the rejected candidate |
| Rollback | Previous factory `2607d27…` was selected, rebooted as `62a3031e-235f-46b3-95bc-f6f259c5606e`, and durably confirmed |
| Final state | `/v1/update`: good factory, no pending/trial/corruption, raw idle ready; `/v1/status`: idle; root read-only with `/.fes-bootstrap` retained; all five cache files and two SNES saves present |

The invalid-init result demonstrates fallback before candidate init and network
startup. The diagnostic images remain content-addressed but unreferenced on the
card for post-test inspection; they do not replace the factory release. Native
game-launch and UI acceptance remain the separate runtime/UI checks.
