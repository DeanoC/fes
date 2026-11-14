# Retained first-source setup diagnostics and a new matched image

Implementation `2c20c0c0f80a5d963c47320ff5713834e0680ba9` makes the [first-source setup failure](../2026-11-14-de25-dispatch-rearm-physical/README.md) diagnosable without retrying or weakening it. The actual older trial reports eight register-write intents, outer status 4, service error -5 and scope reason 27 before SDM START/DATA. That signature alone does not identify the failed queue check, callback or deadline. These diagnostics do not repair that hardware failure.

The supplier emits two failure-only records after initialization, preparation or arming returns an error. `FES_NANO_SOURCE_SETUP` records the stage, retained probe error, accepted clock history, read/write counts and last attempted callback/register observation. `FES_NANO_SOURCE_SETUP_QUEUE` preserves the nested queue error, stage, original clock/observed sample, polls, publication and completion history. A queue error is still visible when the outer probe maps it to -5. The observer adds no SMMU/device observation, timer sample, ownership callback, error acknowledgement, reset or retry; output uses the existing diagnostic console. It follows no retained callback/pointer. Success emits neither record. Source retention, sticky errors, existing clocks/deadlines and access gates remain unchanged.

## Completed off-board checks

The actual selected supplier/service/setup/queue sanitizer model passes eight cases: unchanged successful transfer/retirement, initialization refusal, prepare/readback failure, three distinct failures after exactly eight write intents, arm acknowledgement failure and an observer call with every callback poisoned. It checks that the two records preserve the actual retained cause, that logging leaves state and observation counters unchanged, and that later prepare calls cannot replay the failed attempt. In particular, nested poll exhaustion remains -110 while its outer error is -5. The three matching signatures are modeled counterexamples to inferring a hardware cause from the write count, not evidence that any one occurred on TRWU8CBL.

All twenty-six source/retirement tests pass with zero skips. The actual vendor AArch64 platform object compiles with its original flags and both diagnostic symbols present. Its compiler reports a 224-byte observer frame and a 688-byte preparation frame; these individual records do not qualify whole-EL3 stack usage. The initial compile harness checks are retained, including correction of its include path/flag selection; the final compile preserves the normal vendor recipe. See [compile scope](aarch64.json).

An additional explicitly configured native integration run passes thirty tests with zero skips using the pinned vendor SHA/mailbox/SiP/header sources, host Go and device-tree tools. This covers the strong cold provider, source/ROM/tape/USB/MMU control, parser and startup-admission checks unavailable to the default parent invocation. The first isolated startup invocation lacks a component import path; its failed log is retained. The corrected invocation supplies the same complete component paths as the normal callers. See [native controls](native-controls.json) and [completed checks](completed-checks.json).

## Matched image and exact source boundary

Both retained sealed FPGA projects pass fresh current-producer derivation. The prior SDK/entry copy is kept as a control: its old private project paths fail the new checkout's source admission. The selected image therefore rebuilds SDK, CPU entry, complete BL31 and SPL/U-Boot through the normal producers, then composes the image under `/tmp/lander-quartus-3090.lock`. No FPGA synthesis/fitting or physical device operation occurs. Both diagnostic format strings are present in the linked BL31.

The [complete matched chain and native file admission](matched-chain.json) pass on the exact clean implementation:

| Input/output | Identity |
| --- | --- |
| Source commit | `2c20c0c0f80a5d963c47320ff5713834e0680ba9` |
| Chain | `da3233d7ef350b50f16c42bc127e5e9d25c44b1b4ad0e071634141312cdd9171` |
| Native boot ID | `00ed5a4bbda33055` |
| FIT | `6e8b7d8c657db55a4c01c9e1eae8bd5f5d4f4e7736864dd5f2fc4594c12e85a1` |
| FIT size/bound | 8,542,928 / 16,777,216 bytes |

The core payloads remain unchanged. This is file-only admission: no hardware acceptance, ordinary activation or whole-EL3 stack qualification is claimed. The [matched archive](matched-retention.json) independently reads back 41,752 files at `/home/deano/fes-retained/de25-source-setup-matched-20261114-r1/matched-chain.tar.gz`, SHA-256 `29318ff0ca32a9dddd07987cde8fc20e07a1c3270c3c505c7fe98306399ebe2e`. Explicit canonical sealed-project dependencies remain required.

The [focused software archive](focused-retention.json) independently reads back source, compile artifacts and completed targeted checks at `/home/deano/fes-retained/de25-source-setup-focused-20261114-r1/software.tar.gz`, SHA-256 `03a5f768469b3b3655a3e0c6595e8e28fe68598df65b2b0da44494406290a75f`. It includes failed preparation/test-harness controls and the original broad campaign failure; it does not snapshot the running replacement phase as passed.

## Broader regression status remains separate

The default parent invocation passes 2,714 tests with fifteen skips. Native runtime/producer/firmware integration advances to the final selected FPGA software command. The eleven-phase campaign then hits its original 2,100-second host supervisor deadline. The original final unittest worker survives the signal long enough to finish its remaining tests; it reports 2,181 tests, eighty-eight skips and **one error caused by the supervisor SIGTERM interrupting the pc386 device bench**. That original campaign remains failed.

The supervisor's log hash covers its exact 1,036,447-byte prefix. Independent prefix reconstruction matches that hash; the settled full log and 91,540-byte late tail are retained separately. Liveness checks identify the original worker by PID/start/session and wait for its actual termination. No replacement begins while that session is live. The identical final selected command is subsequently launched as a separate bounded check after the original session is gone. Its terminal result is not yet observed at this publication checkpoint. This is a test/host qualification boundary, not a reason to relax firmware time bounds or claim the full campaign passed. See [exact failed campaign and pending check](broad-campaign.json). CI is a separate assessment.

## Hardware state and next step

The [post-reset restoration](../2026-11-14-de25-dispatch-baseline-restoration/README.md) independently verifies the exact original FIT and all fifty-one baseline launch/return exchanges, USB STOP and final Park. TRWU8CBL is on that original baseline, test card installed, all hardware handles closed and top claim held. The new diagnostic image above has never been programmed. QSPI, factory card and bottom kit remain untouched.

After software qualification, the next hardware work needs a fresh source-bound operator and bounded procedure selecting this new image. The consumed failed trial is never retried. The full repeated-loading path, independent drain, broader packages/devices and DE10-equivalent readiness remain open. Default Linux build/image behavior remains unchanged; the native route stays behind its explicit experimental selection.
