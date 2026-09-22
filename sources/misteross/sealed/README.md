# Sealed board-firmware artifacts

Tracked copies of sealed RBFs that FES image assembly copies from this
in-tree path. Build outputs under `build/` stay gitignored; do **not** pin
`build/fes-splash/core.rbf` from git. Do **not** fetch these bytes from
standalone `https://github.com/DeanoC/misteross` — that repository is
retired for day-to-day work. FES `sources/misteross` is the source of truth.

## `fes-splash.rbf`

| Field | Value |
| --- | --- |
| Source recipe | `make build-fes-splash` (PLL lock-gate HDMI after reconfig) |
| Sealed | FES `2609827b8de1397b6d6a2fc877be56e657e1ba11` |
| sha256 | `feb0a66a3384d56a234fcdbd4ee2665fe366310946b206f5aefbfa6ad6e6e83f` |
| size | 1962648 |
| Provenance | `sealed/fes-splash.build-summary.json` |
| Lane | OSS Yosys/nextpnr, `gpu-router=OFF` |

FES pins this path as `[splash_rbf]` / `[idle_rbf]` (same bytes until a
second idle bitstream exists). FAT install stays `/menu.rbf`. IdleRecipe
must omit Probe (`0x0014`) and HPS framebuffer (`0x002f`).

Rebuild with `make build-fes-splash`, then replace these files and update
digests here and in the FES pin.
