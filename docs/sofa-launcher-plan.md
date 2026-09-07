# Sofa launcher implementation plan

> **For agentic workers:** Use the executing-plans workflow task by task, with bounded delegated component scopes and review before integration.

**Goal:** Boot the native kit into a controller-operated launcher and complete Pong launch/play/Stop through existing ownership paths.
**Architecture:** Runtime owns framebuffer enablement. FogCast provides a restricted authenticated host listener and session-bound controller stream. The kit shell uses those services; its live catalog uses the shared `fbgrid` renderer while the SDL layout remains owned by the other UI task.
**Tech stack:** Go (CGO-free ARMv7), C++ native runtime, existing Buildroot/FES assembly.
**Spec:** [Approved design](sofa-launcher-design.md). Approved in conversation; other work is UI only.

## Global constraints

- Preserve clean pinned sources; all edits in out/dev/sofa-launcher component worktrees.
- No commits/push/PR without authorization. Leave reviewable diffs.
- One hardware operator with existing kit lease; target 192.168.10.84.
- Reuse unchanged compiler/Linux/FPGA inputs. Label modified images diagnostic.
- Keep browser loopback restrictions; authenticate only the explicit launcher listener.
- Do not duplicate the other task's visual layout work.

## Task 1: Native idle framebuffer

Owner: runtime worker. Files: src/native/video.{cpp,hpp}, Linux framebuffer primitive under src/native/linux/, src/linux/production_hardware.cpp, unit tests, ARCHITECTURE.md.

- [x] Add failing tests for mode validation and framebuffer SPI ordering/failure before idle publication.
- [x] Add injected framebuffer preparation to Menu bring-up; validate returned geometry/address/stride and enable HPS framebuffer in Menu.
- [x] Run runtime unit tests and cross-build changed daemon using retained toolchain.
- [x] Hand back diff and diagnostic sequence; root alone deploys/tests.

## Task 2: Restricted host connection and input

Owner: host worker. Files: cmd/fogcast-api/, internal/hostapi/, host/remote_input.go only if necessary, new launcher host configuration package.

- [x] Add failing HTTP tests for missing/wrong bearer, forbidden route, target mismatch, stale input session and disconnect neutralization.
- [x] Add explicit launcher listener configuration, keeping normal browser listener loopback-only.
- [x] Reuse host services for library/session launch and Stop. Restrict permitted routes.
- [x] Add session-bound controller stream feeding RemoteInput.SendEvent, timeout/disconnect neutralization and exclusive input source.
- [x] Run affected Go tests/race checks and provide exact client/config contract to root.

## Task 3: Kit input/application adapter

Owner: root. Files: new kit launcher package/command, new input adapter; avoid existing SDL/grid layout files.

- [x] Read host contract and add failing tests for physical USB mapping, one interface selection, virtual pad exclusion, Select+Start hold/release, session transition reset.
- [x] Implement evdev normalization and hotplug in a new package; support unsigned fixture axes using ioctl min/max.
- [x] Wire catalogue/launch/status/Stop and input stream into a minimal functional shell using existing graphics, with the shared `fbgrid` live catalog renderer. Keep the SDL renderer independently replaceable for the UI task.
- [x] Cross-build ARM binary and run focused tests without opening real devices.

## Task 4: Boot and provisioning

Owner: root. Files: FogCast native package/init recipes and FES scripts/provisioning tests/docs.

- [x] Test generated launcher config contains explicit host endpoint/target identity and secret without leaking secret to receipt.
- [x] Add supervised boot command after runtime/agent and auto-provision config from owner-only host file.
- [x] Document listener setup, configuration generation, component interfaces and checks.

## Task 5: Integration and hardware

Owner: root, after independent diff review.

- [x] Verify focused tests, diff checks and changed binaries before hardware mutations.
- [x] Claim kit, deploy retained-name diagnostic root image, verify framebuffer output and new daemon.
- [x] Run real Pong controller loop and settled capture; ask user for physical button presses when needed.
- [x] Verify Stop restores launcher, disconnect neutralization, and existing MD/SNES behavior as content permits.
- [x] Record exact artifacts and limitations; release lease. Final handoff records the merged UI integration, uncommitted diffs and remaining integration requirements.
