# SMS larger-media integration plan

Status: selected stream integration implemented; derived diagnostic physical
checks and full-image software verification PASS, with distinct artifact scopes.

## Integration checkpoint

- Shared stream definitions are merged in mister-packages #9 (`fdc4ece`).
- Selected runtime is `a6d658cd305c4a84860afc1f8b00a2798ee6e4f4`; selected
  FogCast is `d9745ed746a1e8d0bde423151d08810248ce8815`. These include the
  stream-start reset ordering and SMS direction/Fire1 input corrections after
  the initial runtime #24 / FogCast #256 integration. They implement
  the explicit stream path, including coordinator capability propagation and
  isolated status snapshots. Runtime tests and ARM cross-build, host tests/vet and builds,
  and parent consistency/host build have passed. Host browser/CDP coverage
  was skipped because Chrome was unavailable.
- Parent consistency checks cover 18 generated consumers, 15 fixture copies
  and four copied source pins. Parent Python tests passed; container-delegated
  cases passed in their respective container runs.
- Selected misteross is `0825da5f277648009c2caa21c591e8b8bbca212a`, including
  the subsequent compiler/recipe fixes from #67/#68. SMS RTL #66 merged at
  `be3b0836fd18fa24dae8f2eaec152c6be4c8f7a3`.
  Fix `9fa3a02` adds CPU-executed upper-half diagnostics and preserves legacy
  upload cancellation on Hold Reset. The diagnostic package handoff from
  sealed tip `d647781` used HIP seed 1; its identities below are distinct from
  newly rebuilt packages using the selected compiler/recipe revisions:
  - package ID `6e172ee279a69d6c7326009c7a8a82ab496e56ce2cb5ac2f2e7e3252d7b194d7`
  - RBF SHA-256 `2a245d6e8a5d73b52f86c6b7c8a802019ecfd43d1ccf3ed29eb3d9d6d15d0090`
  - interactive 32 KiB ROM SHA-256 `411c33162658bf0bba55f5745565ee023c6bb6f5190a57f9a3b3ea5e2c484835`
  The exact diagnostic package/media subsequently passed the bounded physical
  checks recorded below; this does not accept newly rebuilt FPGA outputs.

The selected revisions and diagnostic hardware identities are separate evidence.
FES #76's standalone pin lacked the matching shared consumers; #75 now includes
that pin with all matching components, so neither build nor automated preparation
depends on #76 merging first.

The corrected derived diagnostic passed visible HDMI output, P1 directions/A
press-release, menu return and three consecutive exact-SMS UI relaunches.
The intervening missing `pong.rbf` launch selected Pong, not SMS. Earlier startup
and input failures remain recorded in the
[diagnostic acceptance record](validation/2026-09-18-sms-stream-start-diagnostic.md).

Full-image verification separately passed two-pass reproducibility, structural
checks and QEMU packaging (not FPGA emulation). The retained receipt is
`out/hardware/sms32k-inputfix-20260918.muS1pr/full-image.1yUF9p/verification.json`;
verified image SHA-256 is
`6538ca1b5d61729f287a5aaeb5a72c14cf7f8bd6ae03bd87bc874056c65c75d9`.
This does not establish physical acceptance of that full image or its rebuilt
FPGA packages, nor does it imply release publication. The diagnostic kit and
full-image verification remain separate acceptance scopes.

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
software implementation phase. SG-1000 evidence and acceptance are a separate lane.
