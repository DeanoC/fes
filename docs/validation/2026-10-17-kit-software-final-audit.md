# Persistent kit software: final acceptance audit

Audit baseline: FES `1f1316c5584e35192987745a49b65511d162e036` (main after
PR #756). This is a source and recorded-evidence audit, with fresh focused host
checks. No kit was contacted, updated, restarted or rebooted during this audit.
FES owns this integration assessment; FogCast retains network/session ownership
and libmister-runtime retains physical lifecycle ownership.

The requested modular release architecture is implemented and its ordinary
update workflow has designated-kit evidence. The original three implementation
slices and subsequent integration fixes are merged. This is not blanket hardware
acceptance of every byte now on main: the newer base-supervisor hardening in
[PR #746](https://github.com/DeanoC/fes/pull/746) still needs an image build and
exact-artifact hardware qualification. That is the next integration step.

## Requirement and evidence map

| Original requirement | Source and recorded evidence | Assessment |
| --- | --- | --- |
| One immutable agent/runtime/two-launcher bundle, assets, canonical content identity and provenance | `appliance/software/{manifest,archive,store}.go`, `image/scripts/kit-software.py`, `scripts/kit_software.py`; [producer and image qualification](2026-10-16-kit-software-workflow.md) | Implemented. Dependency and versioned-symbol validation use the explicit base contract; source equality is not required. |
| Verified FESDATA3 ext4 storage, separate cores, retained factory/selected/previous, no GC | `appliance/software/persistent_linux.go`, `platform/internal/kitsoftware/{repository_linux,factory}.go`; [adoption and persistence](2026-10-15-kit-software-releases.md) | Implemented; published releases are retained. Corrupt/missing established state fails closed rather than resetting selection. |
| Base supervisor and early storage preparation; OS trials preserve the data gate | `S40fes-kit-software`, `platform/cmd/fes-kit-software/main_linux.go`, `kitsoftware/reconcile.go`; [confirmed adoption without a second reboot](2026-10-15-kit-software-releases.md) | Implemented. Factory trial startup and post-confirmation reconciliation have host and earlier-artifact hardware evidence. |
| Install while busy; one durable, idempotent, generation-checked queue; explicit cancellation and requester/wait status | `kitsoftware/{server,state}.go`, `cmd/fes-software`; [supported lifecycle runner](2026-10-16-kit-software-acceptance.md), [merged local-control bundle](2026-10-16-merged-local-control.md) | Implemented and observed, including requester exit, conflicts, cancellation and queue persistence across reboot on the recorded earlier artifact. |
| Maintenance wins at the free boundary without taking over execution; serialize appliance/diagnostic operations | `internal/kitlease/idle_maintenance.go`, supervisor admission/server gates; [held active game and queued activation](2026-10-16-multi-host-kit.md) | Implemented. Reservation acquisition checks free state and zero admitted references under the manager lock; existing owner renewal continues. |
| Journal before stopping, one resolved release, reverse stop/ordered start, exact process and stable health | `kitsoftware/{engine,process_linux}.go`; [startup/recovery qualification](2026-10-16-kit-software-workflow.md) | Implemented: runtime initialization precedes agent/launcher; readiness requires exact executable identities, bundle identity, native idle and launcher identity for ten seconds within the shared sixty-second startup budget. |
| One durably spent recovery attempt; interrupted activation never retries candidate; stale completion cannot undo newer work | `engine.go` phase transitions, `state.go` operation/generation checks, fsynced repository; interruption/recovery and acknowledgment tests | Implemented and covered by focused host tests. Recorded hardware recovery passes for an earlier supervisor artifact; an earlier failed recovery remains explicitly recorded. |
| REST/client/status and diagnostics; remove routine binary restore and paired-source requirements | [release guide](../kit-software-releases.md), [sharing guide](../kit-sharing.md), FogCast `AGENTS.md`, managed guards in `scripts/kit_manifest.py` | Implemented. Managed kits reject file swaps/restores before mutation. Unmanaged adoption tooling remains intentionally available; it is not a parallel production selection path on managed kits. |
| Independent hosts, local launches, controller/HDMI and core persistence | [generation-bound local Stop](2026-10-16-merged-local-control.md), [two workstations](2026-10-16-two-workstation-soak.md), [same-process recovery](2026-10-17-same-process-host-recovery.md), [physical controller](2026-10-17-physical-controller.md) | Recorded coverage closes these bounded workflow checks. The physical check is operator-reported launch/movement/return on the exact installed bundle. |

All `appliance/...`, `internal/kitlease/...` and `cmd/fes-software` paths above
are beneath `sources/FogCast/`; supervisor paths are beneath `platform/`.
Bundles contain application payloads, not migration programs. Compatibility of
persistent configuration/data remains a producer obligation; no test can prove
that arbitrary future application code performs no irreversible writes.

## Remaining integration and validation work

1. **Qualify the merged base supervisor.** PR #746 adds accepted-process restart,
   recovery admission fencing, bounded retry and tmpfs logging. Its PR explicitly
   reports host-only validation and requires a base-image update. The later
   physical and same-process receipts still identify base `ed721b84…` and bundle
   `0b6b7d81…`, generation 15. Those observations cannot qualify this newly
   compiled supervisor. Build and verify an image containing the change and a
   compatible bundle containing its agent-side fence, then adopt it under the
   designated kit lease. Preserve core bytes and record exact image/bundle
   identities. Exercise accepted launcher death, agent/runtime group recovery,
   queued-maintenance priority and readiness/failure status; confirm ordinary
   launch/input/return afterward. A later compatible bundle update should then
   demonstrate that routine updates still require no OS reboot.
2. **Close the strongest crash-injection wording before claiming full signoff.**
   Current tests construct interrupted `applying`/`recovering` states, verify a
   spent attempt never starts again, cover interrupted initial publication,
   corrupt/missing state, lost responses and selected journal/audit failures.
   Source uses file sync, atomic rename and directory sync. This audit found no
   systematic process-crash campaign at every state-file write/rename/sync and
   selection/start boundary. Add a bounded test campaign covering those actual
   boundaries, including acceptance committed before admission release and
   recovery consumed before prior-service start. Verify no candidate replay,
   at most one recovery attempt and no stale completion changing a newer
   selection. This is an acceptance-evidence gap, not an observed corruption bug.

Physical card power-loss testing is also unclaimed. It is useful additional
storage qualification, distinct from the original host crash-injection checks;
ordinary reboot and modeled interrupted-state tests do not establish it.

No broader legacy purge is required for this slice. Host auto-update, independent
per-service updates, unrelated compatibility cleanup, additional core execution
qualification and automatic game replay remain outside the agreed scope.
The remaining image qualification is justified by an actual base-supervisor
change; it does not make image rebuilds part of routine bundle development.

## Fresh audit checks

Focused race suites cover immutable storage, lease admission, HTTP software
integration, target-client ownership and the supervisor. Parent consistency and
focused producer/maintenance/acceptance-driver tests were also rerun. All completed successfully:

- `go test -race ./software ./softwarecontrol` in the appliance module:
  software passed (3.430 s); softwarecontrol has no tests.
- `go test -race ./internal/kitlease ./internal/httpapi ./targetclient` in
  FogCast: all three passed (1.509 s, 2.040 s, 7.249 s).
- `go test -race ./internal/kitsoftware ./cmd/fes-kit-software` in platform,
  using a temporary workspace with the local appliance module replacement:
  passed (37.382 s, 1.012 s). Initial direct invocations lacked the required
  local-module workspace/replacement and failed setup; no dependency file was
  changed to work around them.
- `make check`: 18 generated consumers and 37 fixture copies matched.
- `python3 -m unittest tests.test_kit_software tests.test_kit_maintenance
  tests.test_kit_software_acceptance`: 69 tests passed (32.148 s).

The documentation-only affected-test check and whitespace check also passed. No ARM build, image build, deployment or
hardware acceptance was performed for this documentation-only audit.
