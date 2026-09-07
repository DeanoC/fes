# Target discovery implementation plan

> **For agentic workers:** Use superpowers:subagent-driven-development to implement and review the bounded tasks below. User approved the linked spec; do not ask again.

**Goal:** Reconnect the configured kit after reboot/address change without restarting FogCast.
**Architecture:** Agent advertises a persistent opaque ID through DNS-SD. Host validates matching candidates and adopts endpoints through the existing lifecycle and lease clients. FES copies stable identity into private media configuration.
**Tech Stack:** Go, DNS-SD, existing HTTP APIs, Python media assembly.
**Spec:** ../specs/2026-09-07-target-discovery-design.md

## Global constraints

- Work only in out/dev/target-discovery/FogCast and out/dev/target-discovery/fes.
- No commits, publishing or parent pin changes until authorized.
- Never replay hardware mutations or override leases during discovery.
- Keep existing address-only configuration compatible.
- No FPGA or runtime changes; preserve exact-image acceptance distinctions.
- Write regression tests first and observe failure before implementation.

## Task 1: Agent identity and DNS-SD transport

Own internal/discovery/, internal/agentconfig/, cmd/mister-agent/, protocol/types.go and internal/httpapi health option; go.mod/go.sum.
- [x] Add tests for persistent identity, malformed IDs, ambiguous DNS-SD results, cancellation and advertisement contents.
- [x] Implement discovery.ValidID(string) bool, NewID() (string,error), Resolve(ctx context.Context, id string) ([]string,error), Advertise(ctx context.Context,id string,port int) error. URLs from Resolve have explicit HTTP scheme/port. Duplicate endpoints are deduplicated; host rejects multiple distinct matches.
- [x] Add TargetID string to protocol.Health and agentconfig.Config with target_id TOML; return configured/persisted ID through authenticated health.
- [x] Integrate advertisement startup/shutdown into agent without making multicast availability a prerequisite for HTTP availability. Persist fallback ID in /media/fat/fogcast/target-id.
- [x] Verify focused race tests and ARM build. Document transport decisions and test results in task report.

## Task 2: Host identity, resolution and reconnection

Own fogcast/, host/, internal/hostapi/ except agent-facing internal/httpapi, host/tenfoot/. Task 1 supplies discovery functions and Health.TargetID.
- [x] Test identity config round-trip and preparation action; preserve private mode and unrelated settings.
- [x] Add TargetID to TargetConfig and corresponding private/public target representations. Expose target preparation through existing settings API; write random ID once.
- [x] Test service with fake discovery plus real HTTP peers for changed endpoint, boot, duplicate/wrong identity, failed authentication, cancellation and concurrent launch/status.
- [x] Implement bounded lookup/backoff and adopt endpoints consistently for lease/input clients. Invalidate stale handles on reboot without cleanup against the new boot; never retry ambiguous mutations.
- [x] Expose connection state separately from game state and wire browser and tenfoot. Legacy explicit-address mode remains usable.
- [x] Verify affected race and UI tests; update canonical architecture and usage documentation.

## Task 3: FES identity provisioning

Own scripts/media.py, tests/test_media.py (or existing provisioning test file), docs/bootable-media.md and parent plan/spec.
- [x] Add failing tests that selected target_id is copied unchanged and malformed IDs are rejected.
- [x] Extend existing private host snapshot conversion, preserving deterministic bytes and no side-effect host writes. Legacy no-ID config remains valid and uses agent persistent fallback.
- [x] Document preparing identity before assembly; verify parent tests.

## Task 4: Review and integration acceptance

- [x] Independently review tasks 1-3 against approved spec; fix actionable findings with regression coverage.
- [x] Run combined affected Go race tests, target cross-build, parent tests, and diff checks.
- [x] Claim designated kit if free; use preserved diagnostic artifact and prove discovery/read-only reconciliation, reboot/address change, persistent Pong launch/Stop, and free final lease. If kit unavailable continue host tests and record precise hardware blocker.
- [x] Record exact changed paths, tests, diagnostic evidence, remaining acceptance limits and publication requirements.

## Validation record

Implementation and independent review are complete. Full Go tests, affected
package race tests, ARMv7 and host builds, browser JavaScript tests, and parent
regressions pass. Browser CDP checks require Chrome, unavailable locally.

Agent-only diagnostic hardware tests demonstrated automatic advertisement after
boot before Ethernet readiness, persistent identity across reboots, active visible
Pong during a controlled address change, Stop at the new address, and host
reconnection after reboot without restarting the host. These results do not
establish reproducibility or exact-parent-image acceptance.

The designated kit was restored to its verified baseline after diagnostics.
Offline card repair reclaimed orphaned allocations from replacing the live root
image, preserving existing files. Restoration and network readiness were verified.
Future root updates must retain the running image under a backup filename until
after reboot, then delete the backup; never unlink the running image's last name.

Publication selects the discovery-capable FogCast commit in FES alongside the
provisioning change. Complete image verification uses that selected revision;
private credentials, images, and local diagnostic records stay outside Git.
