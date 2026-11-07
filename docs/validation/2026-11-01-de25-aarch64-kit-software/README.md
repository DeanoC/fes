# AArch64 kit software producer and base helpers

The normal kit-software producer now selects DE25 explicitly and compiles its
runtime with the authenticated SDK from the matching DE25 headless image. DE10
remains the default. This is build and CPU-emulation evidence; it installs no
software and does not establish managed-kit startup.

| Changed producer | Evidence |
| --- | --- |
| `199604662ea19a526e1884ad202fbca48e1c84d7` | Real common-producer build of the AArch64 runtime, agent and two launchers; actual ELF, library/symbol and interface checks; amd64 pack helper |
| Same producer | Cortex-A53 runtime argument refusal and launcher `--print-kit-ui` returning `tenfoot`; headless base refused by the real bundle packer because it has no factory software directory |
| Same producer | 22 focused producer tests pass; original aggregate software run remains **FAILED**, with the successful continuation described below |
| `51d872c220a6635188c351004b68e94a96ca1986` | Actual static AArch64 supervisor and Bluetooth helper; ARMv7 default rejects both outputs; supervisor invalid action runs under Cortex-A53 emulation before service startup |
| Same producer | Affected software checks pass in 253.1 seconds; focused appliance, platform and producer checks pass |
| `8a804e0531e1e7f229da5b964f59125901d3f39c` | Actual static AArch64 [`fes-kit-data`](storage-helper.json), Go 1.26.5, ARM64 v8.0, CGO disabled; Cortex-A53 invalid operation exits before any mount or device access, 14.4 seconds |

`make kit-software KIT_SOFTWARE_BOARD=de25-nano` requires the matching
`KIT_SOFTWARE_DE25_IMAGE` and an already managed, reviewed base image. The SDK
receipt, archive and Buildroot configuration are authenticated before private
relocation in the matched builder. This path selects no DE10 kernel window
module. The existing payload closure, base-system contract, typed board and
architecture checks and atomic publication rules remain active. See the
[operator instructions](../../kit-software-releases.md).

The real compiler completed before an initial orchestration failure looked for
the wrong QEMU executable. Subsequent failed wrappers are retained: the static
QEMU name, executable test-copy permissions, expected refusal text and private
build-output cleanliness were corrected without rebuilding or changing the
compiled payloads. The authoritative completed check is
[`producer-completion.json`](producer-completion.json). Both actual guest
assertions passed in the matched builder. No update bundle was published.

The original `199604662` aggregate run failed after 1,960.1 seconds. Host,
native runtime, contract and parent lanes passed. The FPGA lane ran 1,729 tests
with 76 skips; its failures were 12 assertion events in ten Makefile-entrypoint
methods. Passing `PYTHON` as a make override leaked that private executable path
into nested command-text assertions. Selecting the same environment through
`PATH` and clearing the inherited make/Python overrides resolves those ten
methods in 26.4 seconds. An initial focused wrapper also had import-path errors
and remains failed. Previously passing cases were not repeated. These are a
failed aggregate receipt plus a successful focused continuation, not a rewritten
green aggregate. The exact methods and both outcomes are retained alongside
the software receipts.

The real target binaries pass AArch64 ELF validation and the actual base's
dynamic library and symbol closure. The static base helpers have no interpreter
or dynamic segment and have executable entry points. Their early argument tests
exercise the CPU binaries; they do not mount storage, start services, control
Bluetooth or access the FPGA. The source-equivalence record binds the producer,
helper and runtime source scopes to their actual commits.

Independent retention at
`out/artifacts/de25-managed-software/199604662/retention.json` holds the compiled
workspace, original failures, two restored full source bundles and matched
builder archive. Its compiler/helper subset has 86 verified files. The later
storage helper is retained with the reviewed-input milestone at its own
`8a804e053` producer. The original cross-engine Docker transport failure remains
failed; the actual compile used the builder whose local image ID matched the
receipt.

Complete managed image assembly still requires a qualified sealed DE25 MENU,
the permanent-owner ABI3 kernel, FAT configuration and persistent FESDATA3
storage wiring. Native factory/MENU startup, agent/catalog launch-stop-relaunch,
software activation/restore and physical media behavior remain open. This batch
changes no installed kernel, SD, FPGA configuration or QSPI and promotes no
shared-kit baseline.
