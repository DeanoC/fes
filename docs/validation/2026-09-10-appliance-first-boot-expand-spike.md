# Appliance first-boot media expand spike

Status: `GREEN` for design-plus-host-dry-run. No physical card, live `.4`, NVMe, or system disk was written. On-target first-boot remains future work and still needs Bob ping + Deano GO before any HIL write.

Follow-up: the separately authorized spare-card physical HIL completed later on
the same date; see [the exact-device HIL record](2026-09-10-appliance-first-boot-expand-hil.md).

## Scope

Spike the leftover-capacity expand of `de10-nano-appliance-1g-v1` after live FES `0.2.0-dev.4` went GREEN. Owner is FES (layout, dry-run, card authorization) with FogCast implications for cache/saves/core-data. Live card identities from the 2026-09-10 live-card record were treated as read-only context:

| Item | Value |
| --- | --- |
| Live card artifact | `67297042698dc5be832e700a0eef887bf63454d489c0f4e470f6f9300bd92065` |
| Release rootfs | `1a01b9100b7a0fa70f8f69a7c2d18ecffed22be9396e2496b1ae805304781c32` |
| FES pin | `de2b917d7b5bff2809f2cd0b868f7ad9445a55a3` |

This run did not mount `/media/fat`, claim `kit.py`, or read the live block device.

## Current table (from assembler + prior write receipt, not live `.4`)

`de10-nano-appliance-1g-v1`: FAT p1 at sector 2048, 2,097,152 sectors (1 GiB, type `0x0c` active); A2 p2 at sector 2,099,200, 2,048 sectors (type `0xa2`); disk file 1,075,838,976 bytes. On a 28.8 GiB class reader the remainder is unpartitioned.

This Mac (`ai-dev-mac`) had only the internal NVMe (`/dev/disk0` / APFS). No USB Generic STORAGE DEVICE was attached, so there was no spare card to inspect.

## Proposal

Recommended path: extra p3 ext4 in leftover space; keep boot files and `/fogcast/releases` on FAT; bind-mount cache/saves/core-data/launcher-cache/evidence from p3. Grow-FAT is rejected for first boot because A2 must move and FAT32 cannot be grown while it is the mounted kernel root. Current 512-byte FAT clusters would also waste ~482 MiB on a 28.8 GiB volume if grown in place.

FogCast default `cache_max_bytes` is 2 GiB, which cannot fit on the 1 GiB FAT.

Draft: [appliance leftover-capacity expand](../superpowers/specs/2026-09-10-appliance-first-boot-expand-design.md).

## Dry-run

`scripts/appliance_expand_dry_run.py` synthesizes a sparse 30,923,764,224-byte model, inspects the locked 1 GiB table, plans both modes, and applies MBR (and A2 copy for grow-FAT) only to that regular file. `WRITE_GO=1` still cannot write `/dev/disk0`, NVMe, raw `sdX`/`diskN`, or USB-by-id: physical apply is unimplemented pending Bob ping + Deano GO.

## Checks

```sh
python3 -m unittest tests.test_appliance_expand_dry_run -v
```

10 tests, OK. CLI `inspect --image /dev/disk0` and `WRITE_GO=1 apply --image /dev/disk0` both refused with `raw block node; USB admission is /dev/disk/by-id/usb-* only`. Host-only. Hardware: not exercised.

## Next integration step

The spare-card HIL is recorded separately; it did not reseat or rewrite live
`.4`. The next integration step is the optional FogCast bind-mount/helper for
the p3 data partition. A future board boot of the spare remains a separate
acceptance decision and must use the exclusive kit lease.
