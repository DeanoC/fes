# FES Apple II: kit re-check of the general cart-merge fix (FES #250)

On 2026-09-27 the designated MiSTer Pi re-ran the full Apple II kit sequence
from the [kit diagnostic](2026-09-27-apple2-kit-diagnostic.md) against a shell
and cards built with the fixed nextpnr. The probe cards went back to a plain
**inferred** `$C800` RAM, the shape that read zero before the fix. All four
cards passed their self-check on hardware. This is a **hardware diagnostic**,
with the same classification and restore as the earlier record.

## Toolchain and artifacts

- nextpnr `0259c6dc1c46dd46fe79f3923a17ad36d2513421`
  ([DeanoC/nextpnr#87](https://github.com/DeanoC/nextpnr/pull/87), on main
  `a93fe013`). Every traced cart clock now maps onto the socket clock, and
  cart inputs on undriven nets are rejected. It is pinned in
  `toolchains/apple2.lock` (shared slot `da248d6c…`); Yosys and Mistral are
  unchanged. The HIP build of that slot passes
  `mistral/tests/fes_cart_dual_clock_m10k.py`.
- Shell resealed from FES `3f00871e`: package
  `fff7fac146c3df99f8729d75fccbb9dad9ec449820c3fa1a7b7523947b9a50e0`, archive
  `85a754f0…`, `core.rbf` `4e50860784d46e6caf5d56cac7114b982b6fe8118c544a519ce8f2903e833bc0`,
  BUILD_ID `edd2cb4e5b6adf6c0bebfdeb8aff3281`. Clocks: system 57.43, pixel
  93.47, audio 161.03 MHz (targets 52.224 / 74.25 / 12.288). The package ID
  changed because the toolchain is part of the build identity. It supersedes
  the pathfinder seal's `e02ec042…` for this lock.
- Probe cards, each with an inferred `MISTRAL_M10K` (`CFG_DUAL_CLOCK=1`) whose
  `CLK1` and `CLK2` are both on `system_clock.clocks[0]` in the routed design.
  All CRAM changes are inside the socket, and the producer's clock guard
  passed:

  | Slot | Expansion ID | CRAM bits inside |
  | --- | --- | --- |
  | 2 | `73d06b5cbb6f523e119a4901d73568406d9af590c992797e820fb70f1573b316` | 28,918 |
  | 4 | `962108d391fbe2056c9a8342ee20a19cc11399953e85ddb5e0614a2421b35192` | 28,924 |
  | 5 | `067bffc528a4a12c1b38bccd26fa20375aa029c54437851d4ff9b8210f97721b` | 28,726 |
  | 7 | `40e27a6552b2e9c5060f55e468dd42aaa3e2c23e26047b110b692c94fab41109` | 28,396 |

- Firmware `d97a495d…` (the diagnostic with `D`) and the diagnostic disk
  `d7033489…`. Composition
  `1d820b56cfbc6ecdf94baa59d47fc8d539b20b6bd21e35e4bc4656c8dfa19a03` and
  programmed RBF `d8fc1efb3a47d8d74e36ecb321590b7c4ce08e3f58a5a675940d38f5bffb979f`,
  identical offline (`fes-slot-link`) and in the kit session.
- Kit binaries: runtime `de4d016c…` and agent `b95d1574…`, from the
  `make dev` image of FES `3f00871e`. They were bind-mounted with the lease
  free, then removed; the image's runtime `d5776191…` and agent `d9d2cf5a…`
  were restarted afterwards. The kit was left idle with the lease free.

## Result

Every step of the sequence passed, with 0 differing text cells on each text
screen:

- boot and self tests;
- lores, hires and mixed (mean abs 8.6 / 7.5 / 7.2);
- HID echo;
- `S` slot scan: `PROBE CARD OK IN SLOT 2/4/5/7`;
- `D` dump: each card `FESPROBE`, ID `A2`, counter `06`, scratch `5A` and
  `$C800` `C33C`, with the vacant slot 1 row reading slot 7's RAM as modelled;
- live insert, boot and eject;
- Control+F12;
- re-insert and a second boot;
- Stop.

The decoded screens, the dump capture and the run log are in
[apple2-cart-clock-recheck-2026-09-27](apple2-cart-clock-recheck-2026-09-27/).

Other locks still pin nextpnr `a93fe013`; their current carts are not
affected. The follow-up decisions are in the
[Apple II next-iterations plan](../superpowers/plans/2026-09-27-apple2-next-iterations.md).
