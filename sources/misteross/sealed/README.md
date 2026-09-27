# Sealed board-firmware artifacts

Tracked copies of sealed RBFs that FES image assembly copies from this
in-tree path. Build outputs under `build/` stay gitignored; do **not** pin
`build/fes-splash/core.rbf` from git. Do **not** fetch these bytes from
standalone `https://github.com/DeanoC/misteross` — that repository is
retired for day-to-day work. FES `sources/misteross` is the source of truth.

## `fes-splash.rbf`

| Field | Value |
| --- | --- |
| Source recipe | `make build-fes-splash` (PLL lock-gate HDMI after reconfig; idle fpga2sdram cell with the `fes.memory.hps-ddr` layout U-Boot latches at boot) |
| Sealed | FES `808bd7c275e1a9934cbca15f9177c553e8c406c7` |
| sha256 | `43dc7e9db350dbdef87b290bfde61f8df37483957c93e81cbd753c30d4cd93b6` |
| size | 1963100 |
| Provenance | `sealed/fes-splash.build-summary.json` |
| Lane | OSS Yosys/nextpnr, `gpu-router=OFF` |

FES pins this path as `[splash_rbf]` and `[idle_rbf]`, with the same bytes:
U-Boot loads it as FAT `/idle.rbf` (`core=idle.rbf`), and Stop idle loads it
from the rootfs. IdleRecipe must omit Probe (`0x0014`) and HPS framebuffer
(`0x002f`).

Rebuild with `make build-fes-splash`, then replace these files and update
digests here and in the FES pin.
