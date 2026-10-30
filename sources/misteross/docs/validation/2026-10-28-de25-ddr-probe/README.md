# DE25 bounded HPS/DDR probe

This record covers a default-idle, read-only DDR transport diagnostic and the
shared descriptor that precedes Linux-owned MENU DMA. Evidence is host-only:
source inspection, RTL simulation, generated-interconnect simulation and a
Quartus compilation. No board, kit, block device, FPGA or QSPI was accessed.
There is no new runtime hardware admission, Linux DMA driver, programming
profile, display submission or interactive MENU implementation.

> Historical snapshot: the SOF, timing reports and simulations below are bound
> to the 2026-10-28 producer and probe RTL hash
> `884512e8259f8ac1becdeae5dd2845de450ee6ec3dc5b977f67fb11bb676d498`. They predate the
> current same-edge CLEAR fault guard. See the [2026-10-30 refreshed evidence](../2026-10-30-de25-ddr-probe/README.md)
> for a Quartus build and simulations against the current RTL.

## Source and artifacts

The work is based on headless Linux commit
`9c155bb5fc31d293efcf325b676215b0bb4399e0`. The compiled producer is
`8f54e4f89e5b12ae18df70c542bc5846d0452ce6`. Later commit
`c51d15b57f` adds the generated transport harness, source review and a planner
test fix; it does not relabel the SOF's producer. The functional source bytes
in this historical snapshot remain bound by [inputs.json](inputs.json); that
receipt does not attest later RTL commits.

| Item | Identity |
| --- | --- |
| Compiler | Quartus Prime Pro 26.3.0 Build 114, SC Pro Edition |
| FPGA | `A5EB013BB23BE4SCS` |
| Terasic revision-B resource ZIP | `f79f0eff3b0f9d042fdb61ddcf43efec77b0ebc13248eea100b94e47a1189b66` |
| SOF SHA-256 | `9c45d9371d108f1f1134864bb5bfbb9182c2af853f4a062b5904224c3a97367c` |
| HPS handoff SHA-256 | `d2a479eefd310505d9426efaf3bf81b1b2e95cc75e1ff3e5f1626fda3e43f81b` |

The retained output is
`/home/deano/.codex/worktrees/c134/fes/out/dev/de25-hps-interface/out/de25-ddr-probe-8f54e4f89e/`.
Its `project/output_files/golden_top.sof` is a diagnostic artifact. The
[build receipt](build.json) binds the SOF, reports, SDCs and handoff. Vendor
HPS/EMIF parameters and boot inputs pass the builder's preservation checks.
The new handoff's recognized payloads match the earlier HPS build, while four
trailing bytes differ; [source review](source-review.md) records the exact
comparison and remaining firmware compatibility limit.

Reproduce from a clean checkout of the producer, using a fresh output:

```sh
python3 sources/misteross/boards/de25nano/hps/build.py build \
  --quartus-root /data/altera_pro/26.3 \
  --vendor-archive /absolute/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip \
  --output /absolute/new-private-output
```

## Interface and functional checks

The [shared contract](../../../../mister-packages/docs/de25-ddr.md) defines
snapshotted DMA bus bounds, generation/sequence, successful completion,
cancellation and accepted-read drain accounting. It adds the separate
`fes.fabric.de25-ddr-probe` 1.0 definition and two generated constants files.
Existing GP, fixed Cyclone V DDR, runtime and FogCast interfaces are unchanged.
The offline planner emits endpoint-relative writes and keeps allocation,
translation, firewall and boot pairing explicitly unqualified.

[generation.json](generation.json) verifies all six nonoverlapping lightweight
slave ranges, the new `0x10100`/128-byte endpoint, 100 MHz control/read clock,
128-bit read master, 32-bit bus address and two-bit response port. It retains
the existing JTAG route and unchanged 256-bit F2SDRAM adapter. The architectural
CPU candidate `0x20010100` remains unadmitted; the raw Linux DT has no matching
fabric resource or DMA stream binding.

`make -C sources/misteross/boards/de25nano/hps sim` passes the mailbox/reset
benches and thirteen probe groups. [rtl-sim.log](rtl-sim.log) covers descriptor
rejection, byte enables, immutable active snapshots, boundary splits, stalled
request stability, same-cycle responses, cancellation, errors and local drain.
The 21 source-preparation tests also pass.

The affected software runner passes all selected parent, host/UI, runtime,
contract and FPGA tooling lanes. It records 1,084 parent tests (two skipped)
and 1,397 FPGA tooling tests (35 skipped), with 15 focused descriptor tests.
[software-checks.json](software-checks.json) retains the exact commands,
selection, tested checkout/local paths and full-log identity. The working
helper/test bytes were subsequently committed in `c51d15b57f`; the final
documentation/evidence does not change those executable inputs. `make check`
passes with 25 generated consumers and 37 fixture copies. The HIL decision is
`NO_DEPLOY_CHANGE` for this standalone diagnostic.

The generated transport helper passes ten cases with the real probe and 114
unchanged generated RTL/header inputs. [interconnect-receipt.json](interconnect-receipt.json)
binds source, wrapper, compiler, executable and per-case AXI traces. It uses
16-byte narrow AXI transfers on the 256-bit bus, preserves upper/lower lane
order and confines the AXI read footprint even for a 16-byte grant.
SLVERR/DECERR reach the probe and drain without successful DONE. AR stalls and
RVALID gaps are exercised. R-channel backpressure, generated-path reset and
cancellation are outside those ten cases. The cut point is the vendor
adapter's AXI subordinate boundary; HPS, MPFE/TBU, translation, firewall,
EMIF and physical memory are outside the model.

The independent source/receipt review verifies every captured generated hash
still matches the project after full compilation, and the real probe/headers
match the producer's Git blobs. The earlier helper setup attempts remain in
private outputs ending `-01` and `-02`. The original vendor Qsys map lacks
materialized address-space metadata; the final builder checks the elaborated
SOPCINFO map instead of guessing slave spans from raw source.

## Compilation and remaining acceptance

IP generation completes with 0 errors/118 warnings. Full compilation completes
with 0 errors/308 warnings. The final fit uses 10,893 ALMs, 21,655 registers,
131 RAM blocks/2,098,688 RAM bits, no DSP blocks, one HPS and two PLLs. Retained
[fit](golden_top.fit.summary) and [timing](golden_top.sta.summary) summaries
describe this artifact.

All 23 constrained timing rows have nonnegative slack and zero TNS. Minimum
setup is 3.581 ns, hold 0.000 ns, recovery 7.567 ns, removal 0.155 ns and
minimum pulse width 0.220 ns. The 35 clock identities/types/periods/frequencies
match the earlier HPS reset build; all reported unconstrained clock, input,
output and path counts are zero. These results do not establish whole-board
timing or physical acceptance.

Overall design closure remains FAIL. The signoff report contains four failed
rule classes: missing output delay, reset polarity conflict, multiple reset
synchronizers and intra-clock false-path reset synchronizers. The polarity
finding is new and is traced separately to generated datapath lane selection;
it remains visible and unwaived. See the [timing review](timing-review.md) for mapped
source context, reset/MTBF coverage and exact report hashes. No constraints,
frequencies, tool failures or acceptance gates were relaxed.

Before activation, the board path still needs a compatible firmware/SOF pair,
a Linux resource and actual DMA stream-domain binding, physical backing and
firewall coverage, and a stop/drain/warm-reset lifecycle retaining allocations
across failed recovery. The first board test is serial boot of the separate
headless image, DHCP/SSH and reboot, followed by an allocated, bounded DDR probe.
Interactive MENU then needs the combined HPS/display design, driver ownership,
ordered publication, pixel-clock crossing and frame-boundary completion.
