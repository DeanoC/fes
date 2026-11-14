# Tenfoot model Play, return and failure recovery

Exploratory change from `70d7b2d1954ae8b9a824b22e3d9b30e757558c7a` on
`codex/de25-embedded-os`. The result is the commit containing this record.
Evidence is host software and native AArch64 EL1 QEMU bare metal. No physical
board, shared kit, FPGA programming, GPU or Powerboat process was used.

## Integrated path

`run.py play` boots the actual `cmd/tenfoot` App and software renderer, using
real xHCI USB input and RAMFB. Its explicit model boot configuration supplies an
in-process `http.RoundTripper` for the existing catalog, Play, session and Stop
client calls. No App fields or session state are patched by the test runner.
The normal launcher and its production host/target paths are unchanged.

One cooperative actor owns the lifecycle controller. One bounded preparation
worker retains a FAT32 archive, verifies the selected ROM source digests, links
through the existing package API, and submits the opaque result. The actor
requires the selected package and receipt identities and the C++ oracle's
exact-package compatibility decision before starting the transition. On TamaGo,
the worker yields at existing parser/linker context checkpoints so frame and
control work can progress.

The platform obtains a frame grant before drawing and completes it after
synchronous RAMFB copying. It skips scanout without a grant. The actor waits
for an outstanding frame before display drain/containment/programming. An active
session response follows identity verification and release; its fresh package
generation binds input and the UI's expected-session Stop body. Return performs
Save, drain, containment, idle programming/identity/release, then resumes the
same catalog selection. Save and hardware completions are simulated endpoints,
not real game persistence or GP observations.

The actor continues while tenfoot's own asynchronous Stop/exit cleanup finishes.
Close refuses an outstanding renderer frame rather than stealing it; accepted
close blocks new grants/input and drains the model before shutdown.

## Native evidence

The [complete native receipt](result.json) binds compiler, C++ oracle, host fixture
producer, native ELF, modules, source, generated cases, screenshots, QEMU commands
and unchanged FAT32 hash. The [system-header inventory](oracle-system-headers.json)
is separately hashed. The [console](boot-1/console.log) records actual USB keys,
UI snapshots and model command/status events. All seven cases pass:

| Case | Observed result |
| --- | --- |
| Normal Play/Back | Correct retained receipt becomes active; input accepted for its generation; return resumes the same card |
| Wrong ROM | Source-digest rejection before ownership changes or program commands |
| Wrong receipt | Admission rejection before ownership changes or program commands |
| Interrupted preparation | Canceled link fails before admission; current idle owner survives |
| Identity mismatch | No bad-package release; bounded recovery finishes idle before UI reports failure |
| Programming mismatch | No bad-package release; bounded recovery finishes idle before UI reports failure |
| Failed Save, then Back again | Core/package generation survives with input retired; retry saves and returns safely |

Late input and frame tokens reject after generation changes. The runner checks
that failed activations never issue a release publishing a running package.
Tenfoot processes its own Quit and idle release; `FES_MODEL_CLOSED PASS` and
`FES_TENFOOT_EXIT PASS` precede the runner reaping QEMU. This differs from simply
quitting QEMU after a status marker.

Eleven screenshots capture the real UI. For example, [the final returned card](boot-1/model-save-returned.png)
retains the selection after save retry, and [the retry frame](boot-1/save-retry.png)
records the error state. A model session renders no emulated game or FPGA video;
playing frames demonstrate renderer parking, not gameplay. PNGs are retained
here; large intermediate PPM files remain in the local run output.

## Host and control evidence

[Race checks](race.log) use public App commands and `RunPlatform` over the same
fixture inputs. They also check the complete ordinary command order, hold a
frame across admission, reject a stale Stop after relaunch, cancel a real HTTP
request during preparation, and cancel after admission before target release.
Close refuses held frames in both idle and running states. Existing lifecycle
and admission ownership regressions also pass.

[Focused tenfoot regressions](tenfoot-regressions.log) pass the existing platform,
keyboard Play and session park/Stop paths. Vet and repository dead-code checks
pass. The [network-backed browse control](browse-control.json) still boots twice,
scans the real private host catalog, navigates with USB, changes layout and
restores it after reboot. The [standalone admission control](admission-control.json)
passes its original 17 admission cases and three retained ROM golden-vector cases.

QMP control now drains guest serial data while assembling partial replies,
keeping the existing five-second deadline. A socket-pair regression forces
serial backpressure between two reply fragments and proves forward progress.
Live UI/event parsers consume complete newline-terminated records.

## Scope and limits

Only FES embedded adapters, tools, tests and documentation changed. FogCast App,
renderer, production session service, physical runtime, shared contracts and
producer locks are unchanged. The embedded module adds the existing Linux-only
UI test dependencies; SQLite and those host adapters are not selected into the
bare-metal image. The diagnostic catalog is a subset adapter, not a replacement
host service or production target coordinator.

These are synthetic Cyclone V ROM/RBF vectors and a software controller; no DE25
programming or hardware acceptance follows. Physical DMA revocation, board boot,
USB/DWC3, SD, network PHY and live FPGA identity remain hardware work. Durable
package/session installation, real game saves, expansion/parts receipts and
production host/target launch orchestration are not provided. Worker I/O must
return promptly or enforce its own deadlines. Board heap and concurrent latency
remain unqualified.

Reproduce with the [native guide](../../../platform/embedded/tamago/README.md#tenfoot-play-and-return-through-the-model)
and [host test instructions](../../../platform/embedded/tamago/modelbackend/README.md).

## Repository checks

The [affected-check receipt](test-changed.json) passes planner/whitespace checks,
all 1,066 parent regressions and generated-consumer consistency. This change is
FES-owned, so no production component or core-simulation lane was selected.
The [focused-check receipt](checks.json) records the actual UI/model races,
existing tenfoot regressions, QMP pressure regression, vet and dead-code checks.
Cold image builds and physical deployment/acceptance are outside this scope.
