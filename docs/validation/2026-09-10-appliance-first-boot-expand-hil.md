# Appliance first-boot expand — spare-card physical HIL

Status: `GREEN` for the exact spare-card media operation and read-back
verification. This is host-side physical-media acceptance of the recommended
extra `p3` ext4 layout; it is not FogCast first-boot bind-mount acceptance.

Validation ran on 2026-09-10 from `ai-dev-mac` through `powerboat`. The
living-room `0.2.0-dev.4` card was not reseated, read, rewritten, or otherwise
mutated. No mister boot was needed: the current appliance keeps p1 and A2 as
its boot contract, while FogCast's p3 bind policy remains a later component
change.

## Exact device gate

The only admitted target was:

```text
/dev/disk/by-id/usb-Generic_STORAGE_DEVICE_000000000819-0:0
```

The final pre-write gate on `powerboat` resolved that by-id to `/dev/sda` and
reported:

| Check | Observed |
| --- | --- |
| `lsblk SIZE` | `58.2G` |
| sysfs sectors | `122138624` |
| exact byte capacity | `62534975488` |
| model | `STORAGE DEVICE` |
| vendor | `Generic` |
| serial | `000000000819` |
| removable | `1` |
| udev bus/type | `usb` / `disk` |
| mounted target partitions | none |

Any missing by-id, capacity mismatch, serial mismatch, non-removable target,
raw `/dev/sdX`, NVMe, internal disk, or mounted target is rejected by the
physical path. `WRITE_GO=1` is required for both physical commands.
Remote negative probes with `WRITE_GO=1` rejected raw `/dev/sda` and
`/dev/nvme0n1` before opening either device.

## Base image

The available base was the exact remote appliance artifact:

```text
/home/deano/fes-release-0.2.0-dev.4/out/native-integration-dev/appliance/card-0.2.0-dev.4/card.img
size: 1075838976 bytes
SHA-256: 67297042698dc5be832e700a0eef887bf63454d489c0f4e470f6f9300bd92065
```

The tool validated the locked `de10-nano-appliance-1g-v1` MBR before writing,
then wrote only the exact USB by-id and read back all 1,075,838,976 base bytes.
The source and target read-back hashes both matched the SHA-256 above.

The guarded command was:

```sh
sudo -n env WRITE_GO=1 python3 appliance_expand_dry_run.py \
  --lock boot-media.lock.toml write-base \
  --image /dev/disk/by-id/usb-Generic_STORAGE_DEVICE_000000000819-0:0 \
  --base-image /home/deano/fes-release-0.2.0-dev.4/out/native-integration-dev/appliance/card-0.2.0-dev.4/card.img \
  --base-sha256 67297042698dc5be832e700a0eef887bf63454d489c0f4e470f6f9300bd92065
```

The base/apply commands were run from a temporary copy of the reviewed FES
tool and lock on `powerboat`; that copy hashed to
`f74a566885835f2304e80e1ad279a5f6318abdf0d0021f4ec7ecd90d3449e6af`. A final
post-format identity check and an early exact-path/missing-device gate were
then added to the local tool, copied to the same temporary directory, and the
read-only `verify` command was rerun. The current local/remote tool hash is
`6dac87a032813a5ce16a0e8612de71664013cb57b1677857326be2bd899f758a`.

## Physical expand

The recommended 58.2G plan was:

| Partition | Start sector | Sectors | Bytes | Type | Contract |
| --- | ---: | ---: | ---: | ---: | --- |
| p1 | 2048 | 2097152 | 1073741824 | `0x0c` active | FAT32 `FESDATA` |
| p2 | 2099200 | 2048 | 1048576 | `0xa2` | SPL + U-Boot |
| p3 | 2101248 | 120037376 | 61459136512 | `0x83` | `FESDATA3` ext4 |

The physical command was:

```sh
sudo -n env WRITE_GO=1 python3 appliance_expand_dry_run.py \
  --lock boot-media.lock.toml apply \
  --image /dev/disk/by-id/usb-Generic_STORAGE_DEVICE_000000000819-0:0 \
  --mode extra-partition
```

It revalidated the two-entry base table, wrote the MBR p3 entry, rescanned
the kernel partition table, and formatted only `/dev/sda3` with
`mke2fs -t ext4 -F -m 0 -L FESDATA3`. Physical `grow-fat` is rejected.

The tool's read-back result was:

| Evidence | Value |
| --- | --- |
| MBR SHA-256 after p3 | `84929643238562491f6c2a8893e733ead8341465d4b6ba1650d4e8f75abe943b` |
| p2/A2 SHA-256 | `59cebe7a86626ff09cdec623f173208047885e7af04f888203d78bbc4f96cca5` |
| p3 device | `/dev/sda3` |
| filesystem | ext4, 4096-byte blocks |
| filesystem label | `FESDATA3` |
| filesystem UUID | `ddd67f0e-7b5f-4923-8f78-a0b8df439e05` |
| ext4 block count | `15004672` |
| ext4 superblock SHA-256 | `c64a6dc82518e03afe54102f3f265e10925b4eec69e7bbcbd9e8f4e7e56fee34` |

Independent `busybox fdisk -l` and `lsblk` showed p1 as active 1G FAT32,
p2 as 1M type `a2`, and p3 as 57.2G ext4 type `0x83`. The base and target
SHA-256 for sectors 1 through 2,101,247 (the image excluding the changed MBR)
both matched:

```text
6a23098e42ffc8c38bd5842cb71ab25084c3a0b0cbda5f3c7a356c8641658cae
```

The A2 hash also matched the base image independently. No target partition
was mounted during or after the apply.

## Implementation and checks

`scripts/appliance_expand_dry_run.py` now provides:

- exact udev/sysfs USB identity and capacity validation;
- `write-base` with locked-MBR validation, `WRITE_GO`, raw write and full
  base-image read-back;
- physical `apply` for the extra p3 ext4 layout only;
- read-only `verify` for the expanded MBR and ext4 superblock; and
- continued regular-file dry-run coverage for both expand plans.

Focused verification passed:

```text
python3 -m unittest tests.test_appliance_expand_dry_run -v
Ran 12 tests ... OK
```

The kit lease was observed `free` read-only and no `kit.py` mutation or mister
boot was performed. The p3 is ready for a later FogCast bind-mount/helper
change; that follow-up must retain the same exact-device and kit-sharing
boundaries.
