# DE25 native computer control — 2026-10-29

FES producer `fe624de4f160c9bd50667b7934b4979431d9ced6` cross-builds the
native C++14 computer-register client and passes its twelve scripted-register
test groups under AArch64 QEMU. The canonical archive/daemon, seven C++ unit
binaries, agent maintenance CLI and five Go agent/runtime cases all pass the
existing off-board SDK lane. No board or physical register access occurred.

The client requires an explicit nonzero selected 128-bit build identity,
exact bridge/ABI/capabilities and ZX81 tape limits before controls. Each
exchange submits one ordered atomic 32-bit GPO word, verifies its held payload
and coherent ACK/fault state, and refuses all later commands after uncertainty.
It implements hold/run, complete HID rows with settling, and bounded CRC32/
chunked tape delivery and eject. Known acknowledged rejection remains a healthy
link and is not retried automatically. Tests cover missing and late successful
ACKs, stopped/backward clocks, post-write/read faults, post-ACK faults/busy,
reserved/signature corruption, wrong selected identities, odd tape words,
maximum payload/chunks, ready confirmation and serialized callers.

The [runtime receipt](runtime.json), [unit checks](checks.json) and
[source binding](source-binding.json) retain exact SDK, builder, source tree,
archive/daemon/unit hashes and tool identities. All sixteen named receipt files
were rehashed. The native protocol archive member is ELF64 AArch64. The same
committed source also cross-builds an ELF32 ARM hard-float daemon/archive using
the existing pinned GNU Arm 10.2 toolchain; these ARMv7 binaries were not run.
The archive has exactly 23 production members and unchanged raw-I/O owners.
`make check` verifies 32 generated consumers and 37 fixture copies; no shared
YAML, computer ABI, FPGA source or production factory was changed.

The retained SDK is from image producer
`3ede3890895aad82be69dfbe3e4c4cbbb1c02c6c`; its archive SHA-256 is
`e4c09c2ebe908a4fa1077b98122a9b561712345435ae1b2fe7d4c563d1ac12c7`.
The runner builds clean committed source in its existing isolated builder,
uses QEMU Cortex-A53 with the SDK sysroot, and does not start a production
daemon. The source/build receipt is software portability evidence, not a
physical bus-access deadline or DE25 qualification.

The affected run against base `ff73fe2173861b216ef16512340c3adfe4f54147`
completed 1,090 parent cases (1,088 passed, two skipped), consumer consistency
and the host functional suite, then **failed** in the unchanged FogCast tenfoot
race test `TestHomeCoreDetailsOpenInTheHeroAndBackClosesThem`, waiting for the
Coleco room. The [original failure excerpt](test-changed-failure.log) and full
log hash are preserved. No cause is established. The exact test passed in
separate `-race -count=1` runs on a clean base snapshot and the candidate;
FogCast source bytes are identical. Those narrow passes do not relabel the
original aggregate run as successful.

The five host commands skipped after that failure were then run separately:
focused race, appliance race, expansion functional/race and UI tests all passed.
The full native `make -j2 test` also passed, including the twelve new groups,
existing runtime/driver/protocol/server cases, incremental/version checks,
archive/active-tree checks and support truth. [Software checks](software-checks.json)
retain each command/result, source identity and log hash. Focused parent checks
passed eleven consistency and four SDK-runner cases. No broad successful suite
was repeated, and no UI code or timeout was changed.

Independent peer review caught a late-ACK deadline gap; the checked producer
rejects a successful GPI read or completion validation that returns after the
100 ms exchange limit. The directed late-ACK test proves that an already applied
command is not repeated. All functional bytes remain those of producer
`fe624de4f160c9bd50667b7934b4979431d9ced6`; the evidence commit adds records only.

[Strict HIL](hil-plan.json) classifies this archive change as
`OVERLAY mister-runtime`. The existing runtime bundle install/smoke/restore
check remains open. The client is not constructed by production: there is no
physical MMIO backend, device resource, programming profile, daemon command or
DE25 admission. The ARMv7/exact-DE10 guard remains unchanged. An admitted bounded
register backend, exclusive hardware/reset lifetime, firmware/device binding
and first-board HID/tape/HDMI evidence remain future gates. No kit or harness access, image activation, SD write or QSPI programming was
performed.
