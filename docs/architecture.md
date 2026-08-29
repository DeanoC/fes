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

## Milestone boundaries

M0 establishes pinned, repository-local tools, environment setup, diagnostics,
and reproducibility metadata. M1 is only the `010_blinky` 50 MHz counter and
LED experiment.

M2 adds `020_linux_mailbox`. Its production RTL has one input clock, exactly
one `cyclonev_hps_interface_mpu_general_purpose` boundary, a 12-byte constant
message, and no external FPGA output. Its policy permits that one HPS general
purpose primitive while continuing to reject PLL, DSP, block-memory, LUTRAM,
SDRAM, video, audio, and unknown hard-block use. Simulation substitutes a
test-only HPS model that never enters either synthesis lane.

The M2 data flow extends the common build without changing M1 programming:

```text
shared RTL + policy
  -> Verilator protocol proof
  -> OSS and Quartus builds
  -> lane-specific manifests and canonical resource evidence
  -> semantic comparison
  -> private four-file development bundle
  -> dedicated FogCast dev transport
```

`scripts/dev_bundle.py` binds `top.rbf`, the compact artifact manifest,
canonical unsigned resource evidence, and `bundle.sha256` to one clean source
commit, lane, and run ID. `scripts/fogcast_dev.py` is a separate host transport
for FogCast preflight, volatile load, and deterministic recovery testing. It
uses argv-list subprocesses and direct remote `exec` commands; it does not
import or modify `scripts/program.py`.

The transport distinguishes three time domains: an absolute 120-second SSH
reconnect bound, a cumulative 30-second recovered FogCast/Main readiness
bound, and an absolute 10-second fault-inspection bound. A successful reboot
must expose a fresh session and strictly greater owner generation. A fault
cycle additionally requires the orphan stage, result, diagnostic, hook, and
socket to be absent after recovery.

M2's repository handoff is Software-tested. It proves source policy,
simulation, both compiler lanes, deterministic OSS bytes, semantic comparison,
bundle construction, and host-only dry-run transport. It does not claim FPGA,
HPS mailbox, reboot, or display behavior on a physical target. Those remain in
the separately authorized cross-repository HIL phase.
