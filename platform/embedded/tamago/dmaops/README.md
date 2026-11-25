# Bare-metal ARM64 DMA buffers and cache coherency

`platform/embedded/tamago/dmaops` provides small, direct ARM64 DMA coherency
primitives, aligned buffer pools with fail-closed poison semantics, and fixed
descriptor rings for single-core bare-metal EL1 drivers on QEMU `virt` and
DE25-Nano (Agilex 5).

## Model

- **Hardware coherency (`Coherency`, `Native()`):** Reads `CTR_EL0.DminLine`
  for the hardware data cache line size, provides `DMB SY` / `DSB SY`, and
  issues Point-of-Coherency `DC CVAC`, `DC IVAC`, and `DC CIVAC` maintenance
  loops. Partial head/tail cache lines on `InvalidateRange` always use
  `DC CIVAC` so dirty neighbor bytes are never discarded.
- **Explicit arenas (`Pool`, `Buffer`):** Manages aligned, coalesced spans over
  caller-supplied memory outside Go's heap (`qemuvirt.DMAPool()` on QEMU `virt`
  or `de25nano.NewUncachedPool(base, size)` over an `arm64map`-verified window
  on DE25-Nano). In `Cached` mode, every allocation is aligned and padded to
  `CacheLineSize()` so distinct buffers never share a cache line.
- **Fail-closed poisoning:** Calling `Buffer.Poison(err)`, `Ring.Poison(err)`,
  or `Pool.Poison(err)` permanently pins the affected physical spans until
  reboot. Poisoned resources reject `Free`, `Close`, `Sync*`, and descriptor
  reads/writes rather than recycling memory with uncertain hardware ownership.
- **Descriptor rings (`Ring`):** FIFO `Acquire` → `Publish` →
  `SyncDescriptorForCPU` → `Retire` over a contiguous descriptor table and
  optional per-slot data buffers (or descriptor-only tables such as SDHCI ADMA2),
  with `Available()` and cumulative/high-water telemetry via `Stats()` (`RingStats`).
  `NewRing` requires `DescSize` to be a multiple of `CacheLineSize()` on `Cached`
  pools so adjacent descriptors never share a cache line; concurrent hardware
  rings with smaller descriptors (such as 16-byte XGMAC/VirtIO descriptors) must
  use an `Uncached` pool.

## Usage

```go
pool, err := dmaops.NewPool(physBase, backing, dmaops.Uncached, dmaops.Native())
if err != nil {
    panic(err)
}
buf, err := pool.Alloc(4096, 64)
if err != nil {
    panic(err)
}
copy(buf.Bytes(), payload)
if err := buf.SyncForDevice(dmaops.ToDevice); err != nil {
    panic(err)
}
// Kick device with buf.PhysAddr(); on timeout, call buf.Poison(err) instead of buf.Free().
```

## Verification

Host unit tests run with `-race` against a deterministic recording `Coherency`
provider:

```sh
GOWORK=off go -C platform/embedded/tamago test -race -count=1 ./dmaops
```

`cmd/probe` runs an EL1 self-test (`probeDMA`) verifying `CTR_EL0` line-size
discovery, real `DC CVAC` / `DC IVAC` / `DC CIVAC` round-trips, pool
coalescing, ring wrap-around, and fail-closed poisoning on both QEMU and
DE25-Nano.
