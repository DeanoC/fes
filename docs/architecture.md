# Repository Architecture

The repository targets the Terasic DE10-Nano/MiSTer Cyclone V SoC FPGA
`5CSEBA6U23I7` (UFBGA 672, speed grade 7). Small RTL, constraints, scripts,
manifests, and reduced regressions are versioned; generated source trees,
tool installations, logs, reports, netlists, and bitstreams stay under ignored
build or controlled third-party paths.

## Execution lanes

`sim` is the dependency-isolated logical lane and uses Verilator. `oss` is the
open FPGA lane: Yosys synthesis, nextpnr-mistral place-and-route, Mistral RBF
generation, and explicit openFPGALoader programming. `oracle` is an optional,
explicit Quartus Prime Lite 17.0.2 reference lane. The OSS lane must not
discover, invoke, link to, or shell out to Quartus or another Intel/Altera
executable.

The common flow is:

```text
RTL + constraints -> simulation -> synthesis -> place/route -> RBF -> manifest
```

Every build preserves command lines and complete logs. Build targets never
program hardware implicitly. Hardware work is limited to volatile FPGA
configuration; flash, HPS storage, and SD-card contents are not modified.

## Initial boundary

M0 establishes pinned, repository-local tools, environment setup, diagnostics,
and reproducibility metadata. M1 is only the `010_blinky` 50 MHz counter and
LED experiment. Raster/video, PLLs, HPS, BRAM/M10K, LUTRAM, DSPs, SDRAM,
audio, and MiSTer framework integration are deferred.
