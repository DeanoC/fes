# Appliance leftover-capacity expand

**Status:** implemented for the host expander and the FogCast agent bind helper.
The exact spare-card host apply is recorded in [the physical HIL record](../../validation/2026-09-10-appliance-first-boot-expand-hil.md).
The implement cut is recorded in [the 2026-09-11 note](../../validation/2026-09-11-appliance-first-boot-expand-implement.md).
A later board boot of the expanded spare remains a separate exclusive-lease HIL.
**Owner:** FES (layout, dry-run, physical-card authorization); FogCast (agent startup bind of cache/saves/core-data/launcher-cache/evidence).
**Live card:** do not mutate `0.2.0-dev.4`. CARD_HASH `67297042…`, ROOTFS `1a01b910…` is GREEN. The physical HIL used only the separately identified spare USB card.

The 2026-09-10 backlog asked for a first-boot expand of `de10-nano-appliance-1g-v1` so a ~1 GiB image uses a much larger SD (28.8 GiB class). This note records the current table, the grow-FAT vs extra-partition tradeoff, the lease/WRITE_GO safety model, and the guarded host apply that never opens NVMe or internal disks.

## Current partition table (`de10-nano-appliance-1g-v1`)

Derived in `scripts/appliance_media_inside.py` from locked `de10-nano-mister-v1` boot inputs. FAT is enlarged to 1 GiB; A2 is moved to sit immediately after it. Locked kernel, U-Boot bytes and `mmcroot=/dev/mmcblk0p1` are unchanged.

| Region | Start sector | Sectors | Bytes | Type | Role |
| --- | ---: | ---: | ---: | --- | --- |
| MBR + gap | 0 | 2,048 | 1 MiB | — | disk ID `0x46455331`, signature `55aa` |
| Partition 1 | 2,048 | 2,097,152 | 1 GiB | `0x0c` active | FAT32-LBA `FESDATA`; kernel root / `/media/fat` |
| Partition 2 | 2,099,200 | 2,048 | 1 MiB | `0xa2` | Cyclone V SPL + U-Boot |
| End of image | 2,101,248 | — | 1,075,838,976 | — | assembled `card.img` |

On a 28.8 GiB class USB reader the written image occupies the first 1,075,838,976 bytes. Everything after sector 2,101,248 is unpartitioned. That leftover is the expand target.

Boot contracts that any expand must preserve:

- The Cyclone V ROM finds SPL by MBR type `0xA2`, not by partition index.
- Locked SPL loads U-Boot at A2 start + `0x200` sectors. The whole 1 MiB A2 partition must be copied if A2 moves.
- Locked U-Boot env keeps `mmcroot=/dev/mmcblk0p1`. FAT must remain partition 1.
- FAT already holds bootstrap, kernel, idle RBF, factory/known-good/previous/staged releases, `agent.toml` and launcher config.
- First boot mounts FAT as the kernel root, then loop-mounts `/linux/linux.img` (bootstrap) and pivots into the selected rootfs. FAT32 is mounted before userspace policy runs.

This spike did not read the live `.4` card. Geometry above is from the assembler, `tests/test_appliance_media.py`, and the 2026-09-08 write receipt (same Generic STORAGE DEVICE class, 28.8 GiB, MBR after write: p1 2048/2097152, p2 2099200/2048). The 2026-09-10 live-card record observed a 1 GiB first partition and small A2 without rewriting the block device.

## Why leftover space matters

FogCast's default `cache_max_bytes` is 2 GiB (`internal/agentconfig`.DefaultCacheMaxBytes). Target cache, SNES saves, core-data, launcher-cache and flight evidence all live under `/media/fat/fogcast/…` on the same 1 GiB FAT as four 64 MiB release slots plus bootstrap/kernel. Staging already refuses insufficient FAT space. Unreferenced release images are retained; GC is a separate non-goal.

Growing the current FAT in place keeps the assembler cluster size of 512 bytes (`mkfs.fat -s 1`). Two FAT32 tables for a 28.8 GiB volume at that cluster size are ~482 MiB. Recreating FAT with 64 KiB clusters drops that to a few MiB. In-place `fatresize` would keep the 512-byte clusters.

## Option A — extra data partition (recommended)

Leave p1 FAT and p2 A2 exactly where they are. Create p3 in the aligned leftover (type `0x83`, start 2,101,248 on the 28.8 GiB model, ~27.8 GiB).

Keep on FAT p1:

- `/linux/linux.img`, `/linux/zImage_dtb`, `/menu.rbf`
- `/fogcast/agent.toml`, `/fogcast/launcher.json`, `/fogcast/target-id`
- `/fogcast/releases/` (factory, known-good, previous, staged, `state.json`, `boot.json`)

Bind-mount p3 ext4 over:

- `/media/fat/fogcast/cache`
- `/media/fat/fogcast/saves`
- `/media/fat/fogcast/core-data`
- `/media/fat/fogcast/launcher-cache`
- `/media/fat/fogcast/evidence`

Copy existing files before the bind. Never wipe known-good, credentials, or release images.

Tradeoffs:

- Does not move A2 or rewrite the mounted FAT. Safe as a first-boot MBR edit plus `mkfs` of unused space.
- No bootstrap ABI change: `fes-boot` still selects images from FAT.
- FogCast path strings stay the same if bind-mounted.
- Release slots still compete for 1 GiB FAT. That is enough for the current four 64 MiB images plus several extras; cache/saves are the 2 GiB pressure, not the release store.
- Needs an ext4 mkfs helper and fstab/bind policy in the replaceable rootfs (FogCast image recipe), or a host-side format of p3 before first insert.
- New MBR with a populated third entry is no longer byte-identical to `card.img`. That already matches today's cache/saves overlay: the immutable hash describes the assembled artifact, not the live card.

## Option B — grow FAT (not first-boot)

A2 is the last partition, so FAT cannot grow without moving A2 to the end of the physical card, updating the MBR, then growing the filesystem.

Tradeoffs:

- One filesystem, no bind mounts, MiSTer-like paths stay native.
- Requires an unmounted FAT. At first boot FAT is the kernel root; FAT32 cannot be grown online. A ramdisk/expand-boot would be a new bootstrap ABI and a brick path if A2 copy or MBR update tears.
- In-place grow keeps 512-byte clusters (see above). A host-side recreate with larger clusters is a new layout, not a grow.
- Moving A2 is a boot-ROM-visible mutation. Copy the full 1 MiB, then atomically update the MBR. A crash between copy and MBR update is recoverable (old A2 still valid). A crash after MBR update and before a complete copy is a brick.
- Must not run during a trial boot or network update.

Host-side grow on a spare, unmounted USB card is possible later. It is not a first-boot operation.

## Recommendation

1. Do not expand the live `.4` card.
2. Prefer **extra p3 ext4** for FogCast mutable data. Keep releases and boot files on the 1 GiB FAT.
3. Prove the MBR mutation on a sparse file (this spike) then, only after Bob ping + Deano GO, on **spare** media in the USB reader.
4. Treat on-target first-boot as a follow-up once spare HIL has booted the expanded table. The 64 MiB rootfs / 32 MiB bootstrap do not currently ship `parted`/`fatresize`; a helper would be new FogCast work.
5. Defer grow-FAT until there is a host-side unmounted rewrite that also recreates FAT with a sane cluster size, under a new layout id.

## Safety model

Three independent gates. None of them is implied by a successful `make appliance-media`.

| Gate | What it covers | What it does not cover |
| --- | --- | --- |
| FES media file lease (`scripts/media.py`) | Concurrent assembly of `card.img` under `out/` | Any block device |
| `kit.py` exclusive lease | Target-agent mutations while the kit is up | A card sitting in a USB reader with the kit powered off |
| Exact-device + `WRITE_GO` + Bob ping + Deano GO | Physical USB writer admission | Live `.4` in the DE10 slot |

HIL rules for any later physical step:

1. Live `.4` stays in the kit. Use spare media in `/dev/disk/by-id/usb-*` only (historical writer: `usb-Generic_STORAGE_DEVICE_000000000819-0:0`).
2. If the kit is up, `python3 sources/misteross/scripts/kit.py --config … status` must be `free`. Claim `owner=bob@ai-dev-mac` (or the authorized operator) with purpose `appliance-expand-dry-run` or `appliance-expand-WRITE_GO`. Do not takeover a foreign owner. Do not expand during trial/update.
3. Host-side USB writes happen with the spare card in the reader, not the booted kit card. `kit.py` cannot lease a powered-off board; the operator still follows kit-sharing so nobody reseats `.4` into the writer.
4. `WRITE_GO=1` is required for physical `write-base` and `apply`. The tool admits only the exact spare by-id after udev/sysfs checks for USB bus, model, serial, removable flag and capacity; regular-file dry-runs remain available without the gate.
5. Denied regardless of `WRITE_GO`: `/dev/disk0`, NVMe, `mmcblk`, mapper, md, raw `/dev/sdX`, raw `/dev/diskN`.
6. No `@codex` on host/frontend for this work. Quiet toward Deano unless HARD_NEED.

## Dry-run and physical apply

Host tool: `scripts/appliance_expand_dry_run.py`.

```sh
python3 scripts/appliance_expand_dry_run.py synthesize --output /tmp/fes-expand-sparse.img
python3 scripts/appliance_expand_dry_run.py inspect --image /tmp/fes-expand-sparse.img
python3 scripts/appliance_expand_dry_run.py plan --mode extra-partition
python3 scripts/appliance_expand_dry_run.py plan --mode grow-fat
python3 scripts/appliance_expand_dry_run.py apply --mode extra-partition --image /tmp/fes-expand-sparse.img
sudo -n env WRITE_GO=1 python3 scripts/appliance_expand_dry_run.py write-base --image /dev/disk/by-id/usb-Generic_STORAGE_DEVICE_000000000819-0:0 --base-image /path/to/card.img --base-sha256 <sha256>
sudo -n env WRITE_GO=1 python3 scripts/appliance_expand_dry_run.py apply --mode extra-partition --image /dev/disk/by-id/usb-Generic_STORAGE_DEVICE_000000000819-0:0
sudo -n python3 scripts/appliance_expand_dry_run.py verify --image /dev/disk/by-id/usb-Generic_STORAGE_DEVICE_000000000819-0:0
```

`synthesize` / regular-file `apply` operate on a sparse file sized to the 28.8 GiB model (30,923,764,224 bytes). Physical `write-base` writes and reads back the fixed image only on the exact admitted USB. Physical `apply --mode extra-partition` writes the MBR p3 entry, rescans the table and formats p3 with ext4; it proves p1/A2 unchanged. Physical `grow-fat` is rejected. Regular-file `apply --mode grow-fat` copies the 1 MiB A2 region and rewrites the MBR; it does **not** run `fatresize`.

The exercised 58.2G spare produced p3 at sector 2,101,248 with 120,037,376 sectors. Exact commands and read-back hashes are in the [physical HIL record](../../validation/2026-09-10-appliance-first-boot-expand-hil.md).

## Implementation status and follow-up

FES:

- Keep assembling the fixed 1 GiB `de10-nano-appliance-1g-v1` artifact. Expanded cards are a live mutation, like today's cache/saves overlay.
- The guarded host USB expander creates p3, formats `FESDATA3` ext4, and verifies the exact base read-back plus the unchanged A2 region.
- Dated validation notes stay separate from assembly receipts.

FogCast:

- Agent startup bind-mounts p3 over cache, saves, core-data, launcher-cache and evidence. Existing files are copied first; p3 files are never overwritten.
- Bind is refused while `/v1/update` shows trial, pending, or corrupt.
- Expand is not in `fes-boot`; releases stay on FAT.

A board boot of the expanded spare is still a later exclusive `kit.py` lease decision. libmister-runtime: no change.

## Non-goals

- Mutating live `.4`.
- Automatic GC of unreferenced release images.
- Expanding during trial boot or network update.
- Changing locked kernel/U-Boot.
- Writing NVMe, internal APFS, or any raw `sdX`/`diskN` node.

## Validation

Parent unit tests for the dry-run tool passed (12 tests). The exact spare-card
physical apply and read-back are `GREEN` in the [physical HIL record](../../validation/2026-09-10-appliance-first-boot-expand-hil.md).
The implement cut lands that expander plus the FogCast bind helper; see
[the 2026-09-11 implement record](../../validation/2026-09-11-appliance-first-boot-expand-implement.md).
No board boot or live `.4` mutation is part of that cut.
