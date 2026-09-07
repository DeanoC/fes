# Appliance release candidate: host verification complete

Version `0.2.0-dev.1` is a locally built candidate for the native three-system
appliance. Source and artifacts have not been published. The complete card has
not yet been installed or booted; update and fault-fallback acceptance remain
pending. The separate [watchdog diagnostic record](2026-09-08-appliance-watchdog.md)
documents the physical reset and confirmation-close primitives that passed on
the existing card.

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

All artifact hardware fields remain `not-run`. The card directory is mode 0700,
with card/evidence files mode 0600 because provisioning includes credentials.
Do not commit or share the private card.

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

## Physical migration still required

The old card's 256 MiB FAT partition is too small for bootstrap, factory,
retained old image and update candidates. A private verified backup retains the
existing identity, configuration, cache and both SNES save files. Installation
requires the kit card in the USB reader, exact-device identification and the
existing kit-sharing procedure. At artifact completion another task held the
kit lease (`nextpnr-m10k-dual`); it was not displaced.

After the owner releases the kit, install the generated disk, preserve the
relevant backed-up data, and verify the new boot's actual bootstrap/rootfs
identity. Then exercise update, confirmation, rollback, failed-init and
unconfirmed/hung-candidate fallback, plus native idle/Pong and save preservation.
An HTTP 200 from the old working card is not acceptance of this artifact.
