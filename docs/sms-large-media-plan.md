# SMS larger-media integration plan

Status: implementation in progress; no larger-ROM hardware acceptance claimed.

## Integration checkpoint

- Shared stream definitions are merged in mister-packages #9 (`fdc4ece`).
- Runtime #24 (`3fe4b91`) and FogCast #256 (`b606ca0`) are merged. They implement
  the explicit stream path, including coordinator capability propagation and
  isolated status snapshots. Runtime tests and ARM cross-build, host tests/vet and builds,
  and parent consistency/host build have passed. Host browser/CDP coverage
  was skipped because Chrome was unavailable.
- Parent consistency checks cover 18 generated consumers, 15 fixture copies
  and four copied source pins. Parent Python tests passed; container-delegated
  cases passed in their respective container runs.
- SMS RTL #66 (`7028b84`) is a review candidate, not an accepted package.
  Review requires a CPU-executed upper-16-KiB diagnostic and preservation of
  legacy upload cancellation on Hold Reset. A fresh HIP seal and coherent
  exact-artifact hardware acceptance remain pending.

These are candidate revisions, not release or hardware-acceptance identities.
Do not deploy this checkpoint while its SMS review findings remain unresolved.

The first target is an open 32 KiB SMS diagnostic mapped contiguously at
`0x0000–0x7fff`. It must execute or validate distinct bytes above `0x3fff`;
padding a 16 KiB program is not sufficient evidence. This is not general
retail-cartridge, Mode 4, audio or mapper support.

## Implementation boundary

- Keep `fes.simple-computer` 1.0 and format-2 packages. New SMS packages require
  the distinct `fes.media.blob-stream` 1.0 interface as well as legacy blob 1.0.
- Keep legacy blob 1.0 operations and their 16 KiB limit unchanged.
- Put stream constants, ordered-transfer semantics and checksum fixtures in
  mister-packages. Runtime and RTL consume the same generated definitions.
- Query actual endpoint capacity; distinguish that observation from installed
  driver support and the offline package declaration.
- Stream through bounded host/agent staging into the existing owned runtime
  lifecycle. Validate generation before mutation; never replay an ambiguous
  transfer. Reset remains held until complete media has been acknowledged.
- Caster owns SMS RTL, the upper-address diagnostic and sealed HIP production.
  The FES integrator owns shared/runtime/host alignment and selected revisions.

The canonical wire specification belongs to mister-packages, not this plan.
The host's 32 MiB storage policy is not a claim about the SMS cartridge size.
The factory image's package set does not change merely to admit this package.

## Verification gates

1. Shared contract fixtures and unchanged legacy exchanges.
2. Runtime and host tests: capacity mismatch, ordering, odd tails, checksum,
   interruption, stale generation, cleanup, Stop and relaunch. ARM cross-build
   remains separate from host tests.
3. SMS simulation: upper 16 KiB execution/data, unused-tail behavior, incomplete
   and corrupted transfer rejection, reset interlocks and legacy operation.
4. One reviewed component combination with consistent generated files and
   runtime lock; then a sealed package built with the standard HIP router.
5. An exclusive kit slot with coherent host, agent and runtime revisions.
   Record exact package, diagnostic media digest and platform identities.
   Exercise library-selected load, HDMI/input observations, Stop, relaunch and
   interrupted-transfer recovery in a fresh evidence directory.

No existing hardware result is inherited by the new package or runtime.
No kit service, live library, rootfs or physical card is changed during the
software implementation phase. SG-1000's separate acceptance lane stays parked.
