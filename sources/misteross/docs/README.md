# misteross documentation

Two jobs. Read one of them before the architecture or a dated note.

| You are… | Read |
| --- | --- |
| Proving a Cyclone V primitive or freeze-scaffold cart with Yosys/nextpnr | [OSS place-and-route testing](oss-pnr.md), then the design's `experiments/<name>/expected.md` |
| Changing or adding a described core, or the splash bitstream | [Cores](cores.md), then that core's `README.md` |
| Checking a build contract, identity, or package byte rule | [Architecture](architecture.md) |
| Looking up what an old experiment proved | [OSS experiment catalog](oss-experiments.md) |

[The module README](../README.md) is the short map. It is the right page to
open first.

## Current instructions

- [OSS place-and-route testing](oss-pnr.md) — `make sim` / `make oss`,
  including freeze-scaffold compose.
- [Cores](cores.md) — simulate, seal, and the boundary with the FES recipe
  registry and factory image.
- [Architecture](architecture.md) — functional identity, lanes, compilers,
  freeze-scaffold mechanism, per-core build contracts, package boundary.
- [Linux mailbox experiment](linux-mailbox-development.md) — worked example
  for `020_linux_mailbox` only.
- [Media stream 1.0](contracts/media-stream-1.0.md) — stream contract used by
  cores that enable it. Not a place-and-route guide.

## Dated records

Files in [validation/](validation/) record a particular day's evidence. They
are not current build instructions. A gap ladder that calls itself the
schedule described that tree, not this one. Do not revive a "later job" from
those notes without checking [Cores](cores.md) and the producer script.

| Record | What it recorded |
| --- | --- |
| [RAM-test local timeout/enable remap](validation/2026-09-28-ramtest-timeout-partition.md) | Shallower local cones and earlier arrivals at all 110 selected ENA pins, but changed placement regresses whole-design memory to 106.92 MHz; baseline remains 116.44. Host-only. |
| [RAM-test ABC9 area recovery](validation/2026-09-28-ramtest-abc-area-recovery.md) | Disabling recovery rounds shortens two control paths and passes full boundary equivalence, but regresses memory to 105.49 MHz; rejected. Host-only. |
| [RAM-test fitted Quartus control cones](validation/2026-09-28-ramtest-quartus-control-cones.md) | Passing 130 MHz fit uses broader ready-enable mapping, three-level timeout logic and different D/ENA choices; matched paths and decoded fitted functions. Host-only. |
| [RAM-test fixed analytical HPS anchor](validation/2026-09-28-ramtest-hps-fixed-anchor.md) | Fixed-only and fixed-plus-pin-offset flows regress to 90.80/95.01 MHz despite better physical locality; exact disabled control. Host-only. |
| [RAM-test analytical HPS pin geometry](validation/2026-09-27-ramtest-hps-pin-geometry.md) | Physical pin offsets in analytical HeAP regress to 96.52 MHz; exact disabled control and unchanged-logic proof. Host-only. |
| [RAM-test paired placed composition](validation/2026-09-27-ramtest-placed-composition.md) | Matched one-cell relocation/composition probes regress versus 116.44 MHz; rejected candidate and HPS endpoint-geometry lead. Host-only. |
| [RAM-test generic enable replication](validation/2026-09-27-ramtest-enable-replication.md) | Opt-in timing-driven enable replication preserves placements and reaches 116.44 MHz memory. Host-only; producer lock unchanged. |
| [RAM-test enable locality](validation/2026-09-27-ramtest-enable-locality.md) | Matched HPS-ready Quartus path and isolated post-placement enable replication probe. Host-only; producer lock unchanged. |
| [RAM-test placement options](validation/2026-09-27-ramtest-placer-options.md) | Ignored HeAP options fixed, preserved-default control, and improved combined FSM/placement timing. Host-only; producer lock unchanged. |
| [RAM-test FSM routing](validation/2026-09-27-ramtest-fsm-routing.md) | GPU initial-route convergence fix, FSM isolation controls and remaining timeout/control-path limit. Host-only; producer lock unchanged. |
| [RAM-test initialized FSMs](validation/2026-09-27-ramtest-fsm-init.md) | Yosys initialization-preserving FSM candidate, equivalence proofs and full-flow experiment. Host-only; producer lock unchanged. |
| [RAM-test Quartus/OSS timing](validation/2026-09-27-ramtest-timing.md) | Matched-source compiler comparison, FSM control and routing probes for issue #264. Host-only; no new timing closure or hardware acceptance. |
| [SG-1000 OSS gap ladder](validation/2026-09-16-sg1000-oss-gap-ladder.md) | OSS gaps and a synth-only HIP note. Not a current seal or parent-recipe claim. |
| [SG-1000 Quartus bring-up](validation/2026-09-16-sg1000-quartus-bringup.md) | Quartus oracle bring-up. |
| [SMS 32 KiB fixed map](validation/2026-09-17-sms-32k-fixed-map.md) | Host simulation of the fixed map. |
| [SMS 32 KiB diagnostic](validation/2026-09-17-sms-32k-p2-diagnostic.md) | Host simulation of the diagnostic and HoldReset abort. |
| [SMS OSS gap ladder](validation/2026-09-17-sms-oss-gap-ladder.md) | OSS gaps as of that date. |
| [SMS Quartus bring-up](validation/2026-09-17-sms-quartus-bringup.md) | Quartus oracle bring-up. |
| [Coleco SGM cart placement](validation/2026-09-25-coleco-sgm-cart-placement.md) | Placement-only feasibility of the SGM v2 cart in the socket rectangle (issue #203): placer fixes, capacity bounds and measured LAB footprint. Not a route, seal or hardware claim. |
| [Coleco SGM enlarged socket](validation/2026-09-25-coleco-sgm-expanded-socket.md) | Host-only vacant-shell and full-cart routes, timing, CRAM containment and Python/Go byte comparison for the larger development v2 rectangle. Not a seal or hardware claim. |
