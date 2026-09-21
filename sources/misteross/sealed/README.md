# Sealed board-firmware artifacts

Tracked copies of sealed RBFs that FES image assembly copies from this
in-tree path. Build outputs under `build/` stay gitignored; do **not** pin
`build/fes-splash/core.rbf` from git. Do **not** fetch these bytes from
standalone `https://github.com/DeanoC/misteross` — that repository is
retired for day-to-day work. FES `sources/misteross` is the source of truth.

## `fes-splash.rbf`

| Field | Value |
| --- | --- |
| Source recipe | `make build-fes-splash` at misteross `20d0460346cd3c0dcab2d3b77748fb2000e65c10` (PR #80) |
| Sealed | misteross `a2af7fdd58d8e5d288892aeda38e8dc226aaed07` (PR #81) |
| sha256 | `f165fdb841c16cb75e27ad518ff689802890008422abe51b5a788b42d8bd33d6` |
| size | 1961783 |
| Provenance | `sealed/fes-splash.build-summary.json` |
| Lane | OSS Yosys/nextpnr, `gpu-router=OFF` |

FES pins this path as `[splash_rbf]` / `[idle_rbf]` (same bytes until a
second idle bitstream exists). FAT install stays `/menu.rbf`. IdleRecipe
must omit Probe (`0x0014`) and HPS framebuffer (`0x002f`).

Rebuild with `make build-fes-splash`, then replace these files and update
digests here and in the FES pin.
