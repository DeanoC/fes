# Stage A0 exit decision — 2026-08-09

## Decision

Stage A0 is a **Software-tested checkpoint**, not a completed Stage A
acceptance. The reproducible Main baseline, durable software lock, independent
build gate, and first Overlord handoff are complete enough to stop verification
churn. The remaining work is two substantive gates: legal redistribution
disposition and bounded HIL evidence.

## What is closed

| Area | Evidence | Status |
| --- | --- | --- |
| Main source | Public `DeanoC/Main_MiSTer`, fork commit `d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d`, tree `efb9c24e8e27945a75d8c497b4b99ec249129075` | Closed |
| Deterministic build | Two fresh independent captures from the durable software lock, with network disabled, produced byte-identical `bin/MiSTer` (`f9e6fd64…`) and `bin/MiSTer.elf` (`51a9864b…`) | Software-tested |
| Policy/material closure | Six canonical `complete` policy documents plus a 17-record reviewed material catalog bind source, toolchain, container, policy files, and six prebuilt shared libraries to the lock | Software-tested; license review-required |
| Resource handoff | Overlord `a9fe9106dcc06db6d80eb22af0cd11facc8f7851` plus resources `e13a97d8324baff83dd1cb8d4531e8648284fad7` generate the Cyclone V slice, declared HPS/core windows, and hash-bound outputs | Software-tested / address-level topology |
| Build container | GitHub Actions run [31312562553](https://github.com/DeanoC/FogCast-POC/actions/runs/31312562553) published the digest-pinned image `ghcr.io/deanoc/fogcast-stage-a0-firstbuild@sha256:ed821006efd42153736b57caf44a4ed571b8949ac6ec47db42fd3fec9cccc1c5` (config `sha256:8e94815d34cd5522f5aba74ce5fc47ab3004f79ab27702c2d13b96766a703338`) | Durable publication and consumed by the local capture |

## What remains

1. **License disposition.** The lock records exact notices and corresponding
   source authorities but uses `LicenseRef-StageA0-ReviewRequired` and
   `review-required` for every material. Legal review must replace those
   dispositions before redistributing binaries or images.
2. **HIL/compatibility evidence.** No target behavior, FPGA launch, HDMI,
   audio, input, or save equivalence has been claimed. Stage A acceptance still
   needs a known-good comparator and a recorded run on the designated
   disposable `misterpi` kit.

The address-level resource graph deliberately does not claim that the complete
FPGA gateware core or HPS interconnect has been implemented. The independent
comparison remains `Software-tested`/local-only even though its lock and
materials are durably retrievable.

## Next work, in order

1. Complete the legal license/redistribution review without changing the
   immutable material identities.
2. Run the bounded HIL compatibility slice and classify Stage A as
   HIL-observed or Accepted only from that evidence.

Until those gates change, the correct status remains **Software-tested**. This
is an intentional stop at a meaningful checkpoint, not an unfinished
verification loop.
