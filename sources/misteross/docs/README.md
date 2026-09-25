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

- [OSS place-and-route testing](oss-pnr.md) — `make sim` / `make oss` /
  `make oracle` / `make compare`, including freeze-scaffold compose.
- [Cores](cores.md) — simulate, seal, and the boundary with the FES recipe
  registry and factory image.
- [Architecture](architecture.md) — functional identity, lanes, compilers,
  freeze-scaffold mechanism, per-core build contracts, package boundary.
- [Quartus reference lane](oracle-method.md) — optional experiment oracle.
  Not the product build.
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
| [SG-1000 OSS gap ladder](validation/2026-09-16-sg1000-oss-gap-ladder.md) | OSS gaps and a synth-only HIP note. Not a current seal or parent-recipe claim. |
| [SG-1000 Quartus bring-up](validation/2026-09-16-sg1000-quartus-bringup.md) | Quartus oracle bring-up. |
| [SMS 32 KiB fixed map](validation/2026-09-17-sms-32k-fixed-map.md) | Host simulation of the fixed map. |
| [SMS 32 KiB diagnostic](validation/2026-09-17-sms-32k-p2-diagnostic.md) | Host simulation of the diagnostic and HoldReset abort. |
| [SMS OSS gap ladder](validation/2026-09-17-sms-oss-gap-ladder.md) | OSS gaps as of that date. |
| [SMS Quartus bring-up](validation/2026-09-17-sms-quartus-bringup.md) | Quartus oracle bring-up. |
| [Coleco SGM cart placement](validation/2026-09-25-coleco-sgm-cart-placement.md) | Placement-only feasibility of the SGM v2 cart in the socket rectangle (issue #203): placer fixes, capacity bounds and measured LAB footprint. Not a route, seal or hardware claim. |
