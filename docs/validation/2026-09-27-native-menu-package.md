# Native menu package build — 2026-09-27

Classification: sealed FPGA build and host simulation evidence; no hardware
acceptance or default image selection.

The described `fes.menu` package requires fixed video, HPS DDR and
`fes.video.menu-display` 1.0. It has no system or playable catalog entry.

- Source: `0d32e0b618b4d0368033a99bfc66a6467fe86333`.
- Package: `51dc2fe6028cf02cc62483905e76950feeaf47c4a33cb64114f619941f11b30d`.
- RBF SHA-256: `1e5ca116433897ba147f5842cf34bec2e3c54bb0cd4ee9043b1b7b9885f079a5`;
  2,013,321 bytes.
- Yosys: `b27035fcc1be6ec040df35a3adbe6d4149297cd8`.
- nextpnr: `da1ee82b7b019ff51d62fe41fd9447961f178e1a` (main's #266 repair).
- Mistral: `7ed06e21c18b047ec5c6d6a7e85e5ea2c8827039`.
- GPU 0, seed 4; legal routing. Pixel Fmax 84.56659698486328 MHz,
  target 74.25 MHz.

The closed producer records live under
`sources/misteross/build/oss/fes-menu-package`; the immutable export lives in
`sources/misteross/build/packages/<package-id>`. The producer checks the shared
DDR layout, inactive operations, electrical constraints, timing and exactly
one HPS GP resource before export. Original pattern/DDR diagnostic producers
retain their independent admission gates.

Actual GP/DDR and production board simulations cover explicit enable,
frame-boundary completion, delayed quiesce acknowledgement, restart, maximum
sequence/wrap rejection and coherent counter snapshots. Existing application
and Catch simulations passed. The misteross suite passed 879 tests with one
skip; the two additional producer guard/evidence cases passed the later
16-test focused run.

Runtime presentation and designated-kit acceptance are pending. Compiler
closure alone does not establish Linux memory reservation, DDR visibility,
underflow-free scanout or menu/game/Stop handoff.

## Rebuild after activation review

The independent presentation review found that GP Enable arriving during active
video could count underflows before initial prefetch. The delayed-enable test
reproduced that defect. Initial activation now waits for the first blank row;
underflow accounting begins when prefetch is armed and still detects real stalls.
All nine menu simulations pass, including enable during active video and two
cycles before the initial blank ends.

The corrected sealed build selects committed source
`8907788dde541788728778fdc5d2f2d2591b280b` with the same authenticated tools above.
GPU 0, seed 4 routes legally at 90.17132568359375 MHz against 74.25 MHz.
Package `0d1ecd3328237fb4ba93e69c69dee45f48b4251a063e54cc86e6f7a8c96b1cc2`
contains RBF `839b4084851c7be180fbcc6612c22dd2dab546bb5fcb583e87fe732b4f7dde28`,
2,012,379 bytes. Routed use is 1,201 combinational cells, 1,226 FFs and three
M10Ks. Closed producer records now describe this rebuild; the original immutable
package remains retained. Neither package has physical presentation acceptance.
