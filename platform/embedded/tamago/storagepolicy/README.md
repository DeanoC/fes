# Trusted and appliance FAT mounts

`storagepolicy` supports two mount modes over partition-confined FAT32 storage:

1. **Pinned mode (`Mount`, `Plan`):** Compiled platform configuration binding
   exact card CID, capacity, MBR SHA-256, and the sizes/hashes of `u-boot.itb`
   and `FES/RECOVERY.JSON`. The strict bounded parser rejects unknown or
   duplicate fields.
2. **Appliance mode (`MountAppliance`, `AppliancePolicy`):** Swappable-SD mode
   for game/appliance builds without compile-time CID or MBR pins. It still
   enforces valid non-overlapping MBR partition geometry, sole FAT32 partition
   selection (optionally alongside non-FAT32 partitions such as `0xa2`),
   primary/backup boot sector verification (unless `SkipBootBackupCheck` is
   set), optional non-empty file checks (`ProtectedFiles`), and mid-session
   card-swap detection (`CardChanged` / `Check` / `Remount`).

In both modes, writes are confined to the active FAT32 partition and cannot
overwrite the MBR, outside sectors, primary boot sector, or backup boot sector.
Errors issue no stop, repair, retry, or rollback. The caller owns and
serializes storage access and holds any required storage lease.
