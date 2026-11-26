# Bare-metal Block Storage & Sector Cache

`platform/embedded/tamago/block` defines the fixed 512-byte sector `Device`
interface, VirtIO-block DMA/GICv3 driver (`virtio.go`), host file device
(`block.go`), lock-free I/O telemetry wrapper (`metrics.go`), and bounded LRU
write-through sector cache (`cache.go`).

## Sector Cache (`block.Cache`)

`NewCache(dev, CacheConfig{Capacity, MaxPopulateSectors})` wraps any `Device`
with a pre-allocated, zero-allocation LRU sector cache:

- **Coalesced misses:** contiguous uncached sector runs are issued as a single
  underlying `ReadSectors` call, preserving multi-block VirtIO-block and SDHCI
  transfer throughput.
- **Streaming bypass:** single requests or consecutive sequential miss/write
  streams exceeding `MaxPopulateSectors` (default `min(Capacity, 16)` = 8 KiB)
  do not allocate new cache slots — including sector-at-a-time streams such as
  `fat.file.Read` with interleaved FAT-sector hits — so bulk sequential I/O
  cannot evict hot FAT32 directory and FAT-chain sectors; overlapping cached
  sectors on writes are still updated in place.
- **Write-through & fail-closed:** every `WriteSectors` writes synchronously to
  the underlying device first. Any write error invalidates all cached sectors in
  the attempted range before returning so partial writes can never leave stale
  cached data. `Flush` delegates directly to the underlying device.
- **Explicit invalidation, warmup & telemetry:** `Invalidate()` and
  `InvalidateRange(lba, count)` drop cached entries on demand (e.g. across
  storage ownership handoffs), `Prefetch(lba, count)` / `Contains(lba)` warm and
  inspect resident sectors without disturbing hit/miss or streaming-bypass state,
  and `Stats()` (`CacheStats`) reports hits, misses, bypasses, in-place write
  updates, flushes, and device error counts. `cmd/tenfoot` mounts FAT32 over a
  128-sector (64 KiB) `block.Cache`.
