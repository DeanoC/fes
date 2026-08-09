# Stage A0 exit decision — 2026-08-09

## Decision

Stage A0 is a **Software-tested checkpoint**, not a completed Stage A
acceptance. The implementation work for the reproducible Main baseline and
the first Overlord handoff is complete enough to stop verification churn. The
remaining work is four substantive closure gates; another byte-comparison
run does not advance the status until those gates change.

## What is closed

| Area | Evidence | Status |
| --- | --- | --- |
| Main source | Public `DeanoC/Main_MiSTer`, fork commit `d1a3a4e65c2dbee1f23eb5a890d8f29e6448c30d`, tree `efb9c24e8e27945a75d8c497b4b99ec249129075` | Closed |
| Deterministic build | Two fresh captures from distinct roots, with network disabled, produced byte-identical `bin/MiSTer` (`f9e6fd64…`) and `bin/MiSTer.elf` (`51a9864b…`) | Software-tested |
| Policy observation | Six canonical policy documents and a material catalog are regenerated from the reviewed capture; the ELF closure now binds sysroot dependencies to the lock's `toolchain` material ID | Candidate only |
| Resource handoff | Overlord `1a358e5222d9b4cecfb9d18dca0d3db2a4b41a` plus resources `cfa6b1ecbbbaac0ae0da0ed1686795eb2f0792b3` generate a Cyclone V slice and hash-bound outputs | Software-tested / partial |
| Build container | GitHub Actions run [31312562553](https://github.com/DeanoC/FogCast-POC/actions/runs/31312562553) published and pulled the digest-pinned image `ghcr.io/deanoc/fogcast-stage-a0-firstbuild@sha256:ed821006efd42153736b57caf44a4ed571b8949ac6ec47db42fd3fec9cccc1c5` (config `sha256:8e94815d34cd5522f5aba74ce5fc47ab3004f79ab27702c2d13b96766a703338`) | Durable publication; not yet consumed by the local capture |

## What remains

1. **Material and license closure.** The candidate lock still has empty policy
   bindings and placeholder license IDs. The bundled/prebuilt libraries,
   copied headers, logo, Arm toolchain, Debian build utilities, and container
   need exact notices, SPDX expressions, corresponding-source locators, and
   redistribution decisions. High-confidence upstream matches are recorded by
   the audit, but they are not silently treated as legal closure.
2. **Promoted lock and independent gate.** The two successful captures use the
   disposable local image and the candidate authority. A valid final lock must
   bind the durable container, reviewed policies, and material records before
   the independent-build command can issue Reproducible evidence.
3. **Hardware/resource topology.** The generated slice does not yet model the
   complete 16 MiB `/dev/mem` aperture, Main shared/core windows, or a real
   HPS-to-FPGA/core graph. The current graph deliberately does not invent those
   connections.
4. **HIL/compatibility evidence.** No target behavior, FPGA launch, HDMI,
   audio, input, or save equivalence has been claimed. Stage A acceptance still
   needs a known-good comparator and a recorded run on the designated
   disposable kit.

## Next work, in order

1. Review and commit the component-level material/license catalog.
2. Replace the candidate lock with the reviewed durable lock, regenerate the
   six policies, and run two fresh builds from that lock.
3. Complete the real Cyclone V aperture/core topology and regenerate the
   Overlord outputs.
4. Run the bounded HIL compatibility slice and classify Stage A as
   HIL-observed or Accepted only from that evidence.

Until step 1 changes, the correct status remains **Software-tested**. This is
an intentional stop at a meaningful checkpoint, not an unfinished verification
loop.
