# DE25 DDR probe evidence refresh

This record rebuilds and resimulates the existing DE25 HPS/DDR diagnostic
against the current probe RTL. It is host-only evidence: no board, kit, FPGA,
block device or QSPI was accessed. It does not qualify DMA admission, physical
DDR access or hardware timing.

## Producer and artifact

| Item | Identity |
| --- | --- |
| Producer revision | `ae47a408588a41dddfb45ea0df4003a5ec3485db` |
| Probe RTL SHA-256 | `4afe3497bca4bb991e855d1c40bd7e8e1d97ea9b943d40083f44eddede7c2d52` |
| Compiler | Quartus Prime Pro 26.3.0 Build 114, SC Pro Edition |
| FPGA | `A5EB013BB23BE4SCS` |
| Terasic revision-B resource ZIP SHA-256 | `f79f0eff3b0f9d042fdb61ddcf43efec77b0ebc13248eea100b94e47a1189b66` |
| Diagnostic SOF SHA-256 | `e9ca0b6e7d138e73d9e20c9ba0707aa0c96481e1460e909d3700cac7f38cb1b2` |
| Top system handoff SHA-256 | `bbcb85bd91fc2693558d25ff72ada05bcd42299d994faea907c5ed939a63e904` |

Quartus IP generation completed with zero errors and warnings. Full compilation
completed with zero errors and 308 warnings. The SOF remains in the producing
checkout at `out/fixjob-fes-1205-de25/quartus-build/project/output_files/`;
`build.json` binds its hash, the selected source inputs, generated handoff and
compiler reports. The receipts and compact summaries are retained here.

The current probe guards CLEAR when a fault arrives on that same clock edge.
`inputs.json` records the source hash above, and the RTL simulation below and
the generated-interconnect receipt use the same bytes.

## Simulation

`make -C sources/misteross/boards/de25nano/hps sim` passes the mailbox and reset
benches and all 15 DDR probe groups. `rtl-sim.log` is the captured command
output. The two same-edge CLEAR cases preserve prior result registers while
latching the unsolicited-response fault, both with and without an earlier
response error.

The generated-interconnect simulation passes all ten transport cases using
the regenerated Quartus project. Its receipt binds 114 generated source files,
including the current probe RTL hash, and records no hardware access. See
`interconnect-receipt.json`.

## Timing and remaining gates

The [timing review](timing-review.md) records all 23 final STA rows, resource
use, zero-count coverage rows and the four unwaived Design Assistant rule
classes. Setup, hold, recovery, removal and pulse-width rows pass their timing
checks; overall design closure remains FAIL. The compiler timing model is not
a whole-board or physical timing qualification.

The [source review](source-review.md) records the source binding and simulation
boundary. The diagnostic remains read-only and idle by default. No physical
DDR address, Linux allocation/translation permission, reset/drain lifetime
contract or runtime admission is established. The historical
[2026-10-28 record](../2026-10-28-de25-ddr-probe/README.md) remains bound to
its earlier SOF and source hash.

## Reproduction

From the repository root, with the locked resource ZIP available:

```sh
python3 sources/misteross/boards/de25nano/hps/build.py build \
  --quartus-root /data/altera_pro/26.3 \
  --vendor-archive /absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip \
  --output /absolute/new-private-output
make -C sources/misteross/boards/de25nano/hps sim
python3 sources/misteross/boards/de25nano/hps/interconnect_sim.py \
  --project /absolute/new-private-output/project \
  --output /absolute/new-interconnect-simulation
```
