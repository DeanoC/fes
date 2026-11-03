# Persistent native selected-program service

The Nano selected-program frontend now accepts successive launch, return and
control requests through a persistent native `ProgramService`. It uses the
existing retained store, guarded transfer engine, live computer identity checks,
control-only scope and owned SD return. Its selected mode no longer automatically
runs a fixed two-choice script. The application can reuse the boot image for
eligible selections from the external SD catalogue.

This checkpoint implements and cross-builds the new service. Its joined software
and failure tests pass; this revision has not been loaded on TRWU8CBL. The prior
[physical result](../2026-11-03-de25-selected-upload/README.md) applies to the old
two-cycle frontend and its recorded image. Different physical packages, ordinary
Nano runtime admission, native Tenfoot, physical input/video and native Ethernet
acceptance remain open. The goal of normal core loading remains active.

## Source and ownership

Implementation: `af37a76aed47e049b0a70909026dd55cce567b1a`.
Base: `7521a5f8a338f68c1d38ba978e129f31c3c02401`.
FES owns this experimental base; the service wraps the existing physical
transition implementation. No first-party component or shared protocol definition
changed. The QEMU model/Tenfoot path is unchanged. Ownership remains `lane:deano`;
no PR handoff or merge occurred.

A service claims its verifier once. Launch checks the current Ready generation
before file or firmware I/O. Each successful load creates a private session token;
control and return require that exact service/session. Copies share all mutable
state. Status can be read during an operation; concurrent mutations reject.
Old launch, input and return requests cannot act on a later core.

Content rejection leaves healthy SD usable and preserves the Ready generation.
After containment begins, an uncertain reservation, publication, transfer,
identity, command, closure or SD-grant failure retains resources and permanently
requires reboot. Cleanup closes an opened scope at most once and never uses an
uncertain return to grant SD. Normal Return publishes Ready only after owned
Hold/closure and SD resumption. Terminal Park returns a running session while
leaving SD contained; later activation rejects.

The real `nano_bridge_sip.c` rejects context reads during an SD device lease.
Service startup therefore derives only its application owner token from the
store's cached active receipt. After SD containment, the existing transfer path
still independently checks the actual secure boot context and owner before
reservation/START. Cached status and unsigned metadata confer no boot authority.
The joined service model explicitly rejects context reads while SD is active.

Configuration keeps its 20-second limit, checks time before/after calls, rejects
a backwards clock and preserves the one-millisecond BUSY wait. Computer commands
remain bounded by one second/64 polls. Accepted return cleanup has its own
five-second context and finishes even if the requester disconnects. Synchronous
platform hooks must return within their own bounds; context cannot interrupt a
stalled hook.

## Native caller

`fpgaload --selected-programs --computer-control --computer-reload
--computer-storage-return` now enters the persistent service. All three control,
rearm and SD-return contracts are mandatory for selected mode. The native loop
polls the board UART and accepts one JSON command per line, at most 1,024 bytes.
An overflowing line is discarded in full. Each response starts
`FES_NATIVE_PROGRAM_REPLY` and has scope `nano-contained-program-service-v1`.

Status supplies cached state, boot owner, local request generation and catalogue
indices. Launch supplies only an index and the current Ready owner/generation.
Return, Run, Hold, Keyboard and Activity use the active session generation.
Keyboard requires exactly nine HID rows. Park provides the terminal bounded-test
handoff. Unknown/duplicate/missing fields, arbitrary paths/addresses, invalid
indices and stale owner/session tokens reject before I/O. The adapter is a local
serial interface, not a network server or a new compatibility authority. Its
operations are documented in the [service guide](../../../platform/embedded/tamago/nativefabric/README.md).

The fixed-source low-level diagnostic remains separately selectable. The old
selected-mode automatic script was removed, including its unused loader branches.
Tenfoot still needs its native service adapter and qualified physical resources.

## Verification

All eight affected software scopes pass on the implementation commit. They cover
planner/runner policy, whitespace, parent regressions, generated consumers, native
SD/PIO/store lifecycle and actual patched firmware/RTL/model launch integration.
The exact parent selection has 1,345 tests: 1,343 pass and two explicitly skip
for unrelated environment prerequisites. [The skip record](environment-skips.json)
names the locked U-Boot cache and macOS-only statfs layout.

The race-tested service/console cases execute real Go retention, preflight,
arena publication, transfer, computer and owned-return code against simulated
firmware/CSR state. Three distinct canonical software packages (different core,
build and payload sizes) complete request-driven launches and returns. Coverage
includes stale/foreign sessions, copied services, concurrent requests/status,
content rejection, changed source/lease, partial reservations, publication/write/
completion/open/identity/Hold/close/grant failures, deadline/backwards-clock/late
return/cancellation, malformed serial commands and one-way Park. The actual
retained sealed FPGA archive also passes the explicit producer-export preflight.
These are software evidence, not different-package physical acceptance.

The matched SDK, entry, BL31, U-Boot and local PFG composition all pass.
All 121 selected Git blobs and 1,298 pinned external SDK/module inputs match the
implementation. Actual ELF variables contain the selected/rearm/SD-return flags
and compiled platform policy; the selection/metadata contents stay external.
The linked main calls the new service, and the service calls the actual board
UART polling assembly. [Linked service proof](service-sdk-proof.json) records
those actual symbols and disassemblies. Pinned vendor startup assembly, six
linked startup/relocation intervals, contained console, raw hashing, CPU mapping
and strong per-load firmware provider checks also pass.

Initial off-board test fixture compile/declaration errors, one invalid TOML
fixture, and a tool invocation missing its recorded shared-library environment
are preserved with the final passing runs. They caused no board attempt. The
full eight-scope verification ran after the functional commit was frozen.

## Candidate and next physical check

Candidate: `out/nano-program-service-20261103/native-composition/`.
Native boot ID: `916b895d55afba6e`.
FIT: 6,823,844 bytes, SHA-256
`432f06edbbedf6ee2cf89362be688f3720da907e9088908bd49af2b7b3eaa7fd`.
SOF SHA-256:
`f70325574150e873cdacb399479e736d4a28cb6eb129bc89f2182ef802685aa3`.
The FPGA core payload is unchanged:
`e9255d3dc149e7db17f012c3032f8c1da245d913ded3947a2342034eb1927ac0`.
No new FPGA compile, 3090 job or board action occurred. Any future 3090 Quartus
job must hold the existing shared lock throughout.

The [next bounded test plan](hardware-plan.json) calls for three request-driven
sessions in one boot, with stale/malformed request rejection, live fetch
observations, owned returns and final Park. It keeps the configuration/control
and outer bounds. A checked command/reply runner and off-board runner tests are
still needed before executing it; the old automatic two-cycle runner cannot test
this service. Both example catalogue entries currently select the same sealed
package, so this planned run will not qualify different physical packages.

The last independently verified board state is still boot `145213088b9b4dbb`
after owned return generation 11, with SD contained and data held. The exclusive
claim and spare card remain retained; the factory card is safe. JTAG was restored
to 15 MHz, exact USB permissions restored, private service stopped and UART free
in that prior audit. No current board-state refresh or hardware acceptance is
claimed here. Future recovery must use the spare's matched
`out/nano-version-suffix-20261103/recovery-kit/network-recovery.sof` and current
`phy-prepare` FIT/manifest, preserving the rollback files.

[Software/build summary](software-and-build-summary.json) records exact artifacts
and evidence. Full source restoration/fsck and member-verified artifacts are
retained under `/home/deano/fes-retained/de25-program-service-20261103/`, with
explicit previously verified frozen-input dependencies. Next: implement/test the
bounded command runner and qualify this service on TRWU8CBL, then different
eligible physical packages and native Tenfoot/input/video/ordinary integration.
