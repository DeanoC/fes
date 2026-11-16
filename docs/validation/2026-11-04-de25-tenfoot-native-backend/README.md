# Tenfoot over the native program lifecycle

Base: `981d1ab4e62035b3de246591450baf3ae4284a39`. The source hashes in
[result.json](result.json) identify the tested working-tree implementation.

`nativefabric.ProgramBackend` maps a bounded, privately copied catalog into the
existing Tenfoot HTTP contract. It invokes the actual ProgramService retained
load and owned return paths, publishes the verified package and service
generation, and rejects a stop captured before a replacement launch. Cached
reads remain available during mutations without touching SD or firmware.
The constructor transfers exclusive frontend ownership of the Ready service.

Validation passed:

- Full Tenfoot App, software renderer and RunPlatform: three launch/Stop/return
  cycles through the actual Go service, loader, store and transfer code.
- Distinct package cycles, stale stop rejection without firmware calls,
  pre-containment missing-content recovery, malformed request rejection,
  and terminal reserve/write/identity failures.
- Full App launch and return failures: uncertain ownership stays contained,
  is visible in the UI, and later requests cannot replay hardware operations.
- Ten repetitions under the Go race detector, ARM64 TamaGo library cross-build,
  and all seven affected checks, including parent regressions, generated
  consistency and the existing patched TF-A C/Verilated RTL integration suite.

The new adapter tests use modeled secure calls and FPGA registers. The separate
C/RTL integration regression does not turn those tests into physical evidence.
Development failures and their corrected results remain under
`out/nano-tenfoot-backend-20261104/`; no physical trial was attempted.

The native boot command does not select this backend yet. The existing
fixed-window personality is a CRC reader diagnostic, not a display grant for
computer sessions. Native boot integration, owned HDMI across launch/return,
physical hub input, ordinary ROM/parts/media admission and native Ethernet
remain required before DE10-style core-loading readiness. Shared protocols,
module selection and firmware/FPGA producers were unchanged.
