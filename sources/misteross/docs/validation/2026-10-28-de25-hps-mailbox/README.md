# DE25-Nano HPS mailbox: host compile, 2026-10-28

The later [reset and clock cleanup](reset-sequencing/README.md) records the
current diagnostic and its remaining gates. This record preserves the original
mailbox producer and artifact before that cleanup.

The committed HPS mailbox diagnostic generated all project IP and compiled
successfully with Quartus Prime Pro 26.3.0 Build 114. This is host-only
compiler/simulation evidence. No board, FPGA, QSPI or block device was touched.
Timing, DDR, boot-firmware/DT compatibility and physical acceptance remain
unqualified.

## Source and exact artifact

- FES producer: `4b7b88775ca41e5cd680f1510419d1a368d057dd`.
- Parent diagnostic base: `2061cce66fc8389b99795fdd58cde96b732f5b64`.
- Device: `A5EB013BB23BE4SCS`; two CPU workers.
- Tool: `26.3.0 Build 114 10/14/2026 SC Pro Edition` at
  `/data/altera_pro/26.3`.
- Vendor resource ZIP SHA-256:
  `f79f0eff3b0f9d042fdb61ddcf43efec77b0ebc13248eea100b94e47a1189b66`.
- SOF SHA-256:
  `3bd8ff20634b3401a15a0d5458e49590c539bb1e9e6d7c5fe4999a6703f49250`.

The source snapshot is
`out/work/de25-hps-4b7b88775ca41e5cd680f1510419d1a368d057dd/`.
The complete private build is
`out/de25-hps-final-4b7b88775ca41e5cd680f1510419d1a368d057dd/`
under `/home/deano/.codex/worktrees/c134/fes/out/dev/de25-nano-bringup/`.
The SOF is `project/output_files/golden_top.sof` within that output.

[inputs.json](inputs.json) and [build.json](build.json) bind that committed
revision, the eight copied FES source files, the artifact and generated
SDC/handoff identities. [vendor-inputs.json](vendor-inputs.json) records the
original extracted vendor files. [generation.json](generation.json) records
the generated top-level HPS lightweight-bridge connection to the mailbox,
its relative offset `0x100a0` and its 100 MHz clock.

The build was invoked from the retained clean snapshot:

```sh
python3 sources/misteross/boards/de25nano/hps/build.py build \
  --quartus-root /data/altera_pro/26.3 \
  --vendor-archive /home/deano/.codex/worktrees/c134/fes/out/dev/de25-linux-assessment/out/de25-linux-cache/DE25-Nano_revB_v.1.0.0_ResourcePackage.zip \
  --output /home/deano/.codex/worktrees/c134/fes/out/dev/de25-nano-bringup/out/de25-hps-final-4b7b88775ca41e5cd680f1510419d1a368d057dd
```

The full generation, integration, clear-generation and compiler logs remain
in that output. [Full report hashes](full-report-sha256.txt) identify those
logs and retained full timing/fitter/DRC reports. The first successful
working-copy run is separately retained at `out/de25-hps-build-06/`; it is
diagnostic evidence, not the committed producer above. Earlier failed or
incomplete preparation/generation attempts remain in their numbered outputs.

## What was verified

- Seventeen builder tests and the real Verilator mailbox transaction bench
  passed through the FPGA test wrapper: 18 tests. The bench covers every
  endpoint byte address, all byte-enable masks, read latency, consecutive
  requests, simultaneous scratch read/write and reset blocking/cancellation.
- Eleven parent consistency tests and seven generator regressions passed.
  `make generate` updated exactly two new Verilog/C++ consumers from the
  shared fabric definition; documentation links and whitespace checks passed.
- After source freeze, `make check` passed with 23 generated consumers,
  37 fixture copies and zero source pins. The selected parent, host, runtime,
  contract, consistency and FPGA-producer software jobs passed for the same
  source in [CI run 40681550229](https://github.com/DeanoC/fes/actions/runs/40681550229).
- Platform Designer regenerated source IP with zero errors and 118 warnings.
  Cached vendor synthesis outputs/stamps were excluded and generation
  directories were explicitly cleared. The vendor custom `f2sdram_adapter`
  is found through the explicit copied `custom_ip` search path.
- Full compilation completed with zero errors and 310 warnings. The
  [fitter summary](golden_top.fit.summary) reports successful SCS fitting:
  8,332 ALMs, 16,775 registers, one HPS and two PLLs.
- Original HPS and packaged LPDDR4 module parameters were unchanged after
  generation. A separate [postcompile check](postcompile-preservation.json)
  verifies they remained unchanged through compilation, all retained vendor
  boot/software sources stayed identical, and every copied FES input matches
  its committed Git blob. This does not qualify boot compatibility.

## Timing and remaining report gates

All 23 rows in the [timing summary](golden_top.sta.summary) have nonnegative
slack and zero TNS. [timing-rows.json](timing-rows.json) preserves those
reported rows; the worst values by check are:

| Check | Worst slack (ns) |
| --- | ---: |
| Setup | 3.484 |
| Hold | 0.000 |
| Recovery | 7.613 |
| Removal | 0.156 |
| Minimum pulse width | 0.220 |

The complete clock inventory and transfers remain in the full report. The
100 MHz fabric, 50 MHz PLL reference, 166.6666 MHz EMIF reference, vendor
internal reset-control clock and generated memory clocks remain selected.
The vendor PLL SDC already creates the 20 ns reference clock on `CLOCK2_50`,
so the inherited `MAIN_CLOCK` alias is ignored. No additional clock alias,
false path, frequency reduction or warning waiver was introduced.

The report's constrained Timing Closure is Pass; overall Design Closure is
Fail. [Selected report rows](report-excerpts.txt) retain the observations:

- High `RES-50001`: inherited configuration reset directly resets 89 fabric
  registers, with a debounce counter as the sample endpoint. The vendor top
  directly resets its debounce/heartbeat logic. The new mailbox instead
  receives the generated peripheral reset controller's synchronized reset.
- High `TMC-20012`: `LED[7]` has no output-delay constraint. Medium rules also
  flag the duplicate clock target/ignored alias and multiple reset chains in
  the same domain.
- The vendor top retains PIO width mismatches, unused/undriven peripheral
  ports, unused pin assignments and I/O electrical-default warnings.
- STA finds 41 synchronizer chains, includes 24 in its model and excludes
  17. Its modeled `1e9`-year value for included chains is not a whole-design
  physical MTBF claim.

The reviewed EMIF constraint repair changes only the obsolete port name
`DDR4_REFCLK_p` to the connected `LPDDR4A_REFCLK_p`, retaining the pinned
166.6666 MHz value. Four exceptions naming absent legacy nodes are removed;
they are not applied to active LED/reset paths. All original and repaired
constraint identities remain in the receipts.

## Board and boot boundary

The SOF is a standalone compiler diagnostic. It is not combined with the
vendor U-Boot SPL or packaged as a QSPI image, and it is not a replacement
for the headless Linux BSP's factory boot firmware. No Linux physical bridge
address, FES runtime profile or package admission is inferred from the relative
mailbox offset. A later identified-board test must establish safe firmware/
handoff/DT pairing, actual bridge access, DDR behavior and recovery, alongside
the retained reset, electrical and external-timing gates.
