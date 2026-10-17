# Persistent kit software: final acceptance audit

Audit baseline: FES `1f1316c5584e35192987745a49b65511d162e036` (main after
PR #756). This record combines the source/evidence audit with its implementation and
designated-kit qualification follow-up. Exact source and artifact identities are
kept separate below.
FES owns this integration assessment; FogCast retains network/session ownership
and libmister-runtime retains physical lifecycle ownership.

The requested modular release architecture is implemented and its ordinary
update workflow has designated-kit evidence. The original three implementation
slices and subsequent integration fixes are merged. The follow-up adds process-crash tests and qualifies the base-supervisor
hardening from [PR #746](https://github.com/DeanoC/fes/pull/746). The working
bundle remains installed; tests do not restore a stock selection. The final
update and failed-candidate recovery passed on the new base. Pong HDMI/input passed, but the settled launcher exposed an Installed cores script-budget failure; its correction is in progress.

## Requirement and evidence map

| Original requirement | Source and recorded evidence | Assessment |
| --- | --- | --- |
| One immutable agent/runtime/two-launcher bundle, assets, canonical content identity and provenance | `appliance/software/{manifest,archive,store}.go`, `image/scripts/kit-software.py`, `scripts/kit_software.py`; [producer and image qualification](2026-10-16-kit-software-workflow.md) | Implemented. Dependency and versioned-symbol validation use the explicit base contract; source equality is not required. |
| Verified FESDATA3 ext4 storage, separate cores, retained factory/selected/previous, no GC | `appliance/software/persistent_linux.go`, `platform/internal/kitsoftware/{repository_linux,factory}.go`; [adoption and persistence](2026-10-15-kit-software-releases.md) | Implemented; published releases are retained. Corrupt/missing established state fails closed rather than resetting selection. |
| Base supervisor and early storage preparation; OS trials preserve the data gate | `S40fes-kit-software`, `platform/cmd/fes-kit-software/main_linux.go`, `kitsoftware/reconcile.go`; [confirmed adoption without a second reboot](2026-10-15-kit-software-releases.md) | Implemented. Factory trial startup and post-confirmation reconciliation have host and earlier-artifact hardware evidence. |
| Install while busy; one durable, idempotent, generation-checked queue; explicit cancellation and requester/wait status | `kitsoftware/{server,state}.go`, `cmd/fes-software`; [supported lifecycle runner](2026-10-16-kit-software-acceptance.md), [merged local-control bundle](2026-10-16-merged-local-control.md) | Implemented and observed, including requester exit, conflicts, cancellation and queue persistence across reboot on the recorded earlier artifact. |
| Maintenance wins at the free boundary without taking over execution; serialize appliance/diagnostic operations | `internal/kitlease/idle_maintenance.go`, supervisor admission/server gates; [held active game and queued activation](2026-10-16-multi-host-kit.md) | Implemented. Reservation acquisition checks free state and zero admitted references under the manager lock; existing owner renewal continues. |
| Journal before stopping, one resolved release, reverse stop/ordered start, exact process and stable health | `kitsoftware/{engine,process_linux}.go`; [startup/recovery qualification](2026-10-16-kit-software-workflow.md) | Implemented: runtime initialization precedes agent/launcher; readiness requires exact executable identities, bundle identity, native idle and launcher identity for ten seconds within the shared sixty-second startup budget. |
| One durably spent recovery attempt; interrupted activation never retries candidate; stale completion cannot undo newer work | `engine.go` phase transitions, `state.go` operation/generation checks, fsynced repository; interruption/recovery and acknowledgment tests | Implemented and covered by focused host tests and failed-candidate recovery on the exact rebuilt supervisor below. An earlier failed recovery remains explicitly recorded in its original record. |
| REST/client/status and diagnostics; remove routine binary restore and paired-source requirements | [release guide](../kit-software-releases.md), [sharing guide](../kit-sharing.md), FogCast `AGENTS.md`, managed guards in `scripts/kit_manifest.py` | Implemented. Managed kits reject file swaps/restores before mutation. Unmanaged adoption tooling remains intentionally available; it is not a parallel production selection path on managed kits. |
| Independent hosts, local launches, controller/HDMI and core persistence | [generation-bound local Stop](2026-10-16-merged-local-control.md), [two workstations](2026-10-16-two-workstation-soak.md), [same-process recovery](2026-10-17-same-process-host-recovery.md), [physical controller](2026-10-17-physical-controller.md) | Recorded coverage closes these bounded workflow checks. The physical check is operator-reported launch/movement/return on the exact installed bundle. |

All `appliance/...`, `internal/kitlease/...` and `cmd/fes-software` paths above
are beneath `sources/FogCast/`; supervisor paths are beneath `platform/`.
Bundles contain application payloads, not migration programs. Compatibility of
persistent configuration/data remains a producer obligation; no test can prove
that arbitrary future application code performs no irreversible writes.

## Process-crash campaign

The supervisor adds one instance-local, nil-by-default repository observer for
crash tests. Production selection behavior and external contracts are unchanged.
The test kills separate processes with SIGKILL at actual state-file creation,
write, file sync, close, rename and directory-sync boundaries. The process death
also releases the real flock; no deferred cleanup runs.

The final matrix contains **104 cases**: 38 acceptance, 50 recovery, eight
cancellation and eight blocked-recovery cases. They cover operation publication,
selection/receipt commit, and modeled stop/start/admission-release boundaries.
Reopening uses the real repository and checks exact selection, previous release,
generation and operation identity. Durably cancelled operations start nothing;
interrupted candidates never replay; a spent recovery remains blocked; a newer
accepted operation rejects stale completions. Cancellation interrupted before
publication leaves the old durable queue intact, so callers must observe an
uncertain cancellation rather than assume it succeeded.

Lifecycle controllers and admission in this matrix are modeled. Real service
processes and admission are exercised separately by the supervisor integration
tests and kit diagnostic. Initialization-marker interruption/corruption has
existing repository fixtures; this is not an exhaustive campaign over every
filesystem syscall or loss of volatile storage caches. Physical card power-loss
qualification remains unclaimed.

Production/image source is `6641b58fa36754e629d3e9666f35b8674178b2b6`.
The later 16 cases and final stronger assertions change tests only; final test
source SHA-256 is
`a15973e2e546e8dd55d45ea1f36b837d201c743de792244325c8c9e05380c751`.
The 104-case race run passed in 2.713 seconds. Independent review approved it.

## Image adoption and supervisor qualification

Both independent 192 MiB image passes produced
`06066d298bfbf97ba7a00b8c01566dd742b7958c6a17d700baa81b50e16b050e`.
Structural/package checks, QEMU packaging smoke and versioned release export
passed. The first parent build returned 143 after publishing the image and
receipts; a subsequent clean parent build returned zero, reusing those verified
outputs. The recorded two-pass hashes, rather than that interrupted invocation,
establish image reproducibility. QEMU does not emulate the FPGA.

The compatible fence bundle `00160c48…` first installed and accepted on the old
base at generation 16 without reboot. The leased appliance updater then staged,
activated and durably confirmed the exact new image. Boot changed from
`fe3774a0-6fa0-4aaa-9bf1-5eaec17962d5` to
`457a8b1a-54af-46c0-9e6e-351c08467496`. During confirmation handoff, observations
included the factory trial/data gate, transient service unavailability and
recovery-required admission. Read-only observation showed these settle without
intervention: persistent `00160c48…` accepted at generation 17, storage/service
faults cleared, native idle and free admission. No second reboot was needed.

Under an exact diagnostic maintenance reservation, the reviewed helper used
pidfds to kill each accepted child once. A launcher failure replaced only the
launcher, preserving runtime and agent identities. Agent and runtime failures
each replaced all three children. The same supervisor process and boot survived
all three faults. The existing maintenance operation/generation/token survived
agent replacement; an explicit resume of that exact reservation was recorded
where startup left it blocked. Each stage observed ten seconds of stable native
idle/readiness before continuing. The supervisor itself was never killed.

A separate queued reselection remained behind an ordinary owned lease, refused
a competing claim while that owner held it, and accepted after release at
generation 18. The hardware competing-claim sample proves held-owner refusal;
atomic priority at the free boundary remains a focused host-test result.
The helper was removed and final status was ready/idle/free, with no pending,
unreleased, storage or service fault. These are **idle-maintenance child-failure
hardware diagnostics**, not active-game crash or physical power-loss tests.

The running supervisor's `/proc/PID/exe` and installed file both matched the
compiled artifact SHA-256
`12305944bc1666ea2106ee9fa8ff01e8e0e1ffa0b8ee7a3135cfa041fb0381d6`.
All six installed-core files matched the fresh pre-adoption baseline through
bundle activation, appliance adoption, child faults and queued reselection.
[Public receipts](2026-10-17-kit-software-final-audit/evidence-scope.json) retain
build identities separately from hardware observations. Build receipts keep
`deployed: false` and release evidence keeps its original build-time hardware
status; neither is rewritten into an acceptance result.

## Subsequent update, recovery and HDMI

The supported API runner accepted changed bundle
`c2d732ccb23363424f3bdcef0daf6a5e9d99e75835b6864aa3591d855f40ea95`
at generation 19 on the same confirmed base and boot. Held-lease installation,
competing-client refusal, duplicate/conflicting/stale requests, cancellation and
requester exit passed. An explicitly constructed diagnostic candidate with a
valid ARM agent that exits immediately failed startup; its matching durable
receipt recorded recovery to the newly accepted bundle. Ten seconds of stable
readiness followed both acceptance and recovery. API receipts establish one
matching durable recovery result, not a count of individual process starts.
The successful selection remains installed. The six original installed-core
files still match the pre-adoption baseline after these operations.

Two private-host attempts stopped before launch while the release-list read
exceeded its 60-second timeout. Both shut down cleanly and produced no input or
captures. A direct bounded read verified 18 retained releases in 77.276 seconds;
listing currently authenticates every payload. The reviewed diagnostic's private
request limit was extended to 240 seconds within a 480-second global deadline,
without changing shared host configuration or ownership/cleanup guards.

The subsequent probe launched exact Pong package `6efd4054…`, acknowledged
software-delivered up/down input and completed owned Stop. Captured frames show
the left paddle at the top after up and at the bottom after down. No physical USB
controller claim follows. The immediate launch and post-Stop frames were blank; the settled launcher frame instead showed “Installed cores: script
exceeded 48ms budget”. API status was ready/idle/free at generation 19, but that
is not successful menu acceptance. This newly exposed failure is being corrected
before the PR is made ready.

No broader legacy purge is required for this slice. Host auto-update, independent
per-service updates, unrelated compatibility cleanup, additional core execution
qualification and automatic game replay remain outside the agreed scope.
The image update is justified by an actual base-supervisor change; it does not
make image rebuilds part of routine compatible bundle development.

## Host validation and retained failures

- Final 104-case journal crash matrix: race pass, 2.713 seconds.
- Complete supervisor/command race suites: pass (38.320 s, 1.015 s), before the
  additional test-only extension. The production observer is identical.
- Appliance software/store, kit lease, HTTP and target-client race suites: pass.
- Recovery-fence agent tests: race pass (1.172 s, 1.076 s).
- Static ARMv7 supervisor and both immutable application bundles: built.
- Final `make test-changed --base 1f1316c5`: pass, including 945 parent tests
  in 281.940 seconds with 38 skips, policy checks and whitespace checks.
- `make check`: 18 generated consumers and 37 fixture copies matched.

Earlier aggregate attempts failed first for an absent pinned media-tool
container, then because two bootstrap tests encountered the concurrently held
parent-build lock. Provisioning the pinned tools and rerunning after the build
resolved those failures; they are not counted as passes. Release export initially
refused uncommitted test-only files and missing locked boot payloads. The reviewed
104-case extension was preserved outside the checkout while exporting the frozen
source, then restored. Locked payloads were shallow-fetched from an existing
exact-commit checkout and passed the standard hash checks. An unnecessary full
upstream-history clone was stopped; no kit mutation was repeated.

No shared ABI, REST schema, base compatibility contract, kernel, bootloader,
host autostart or installed-core selection was changed by this follow-up.
