# FogCast Authorized macOS Capture Helper Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make FogCast's Darwin capture worker use an explicitly authorized, cgo-enabled macOS helper identity and fail early with actionable authorization diagnostics.

**Architecture:** Keep the existing AVFoundation/VideoToolbox video-only capture API. Add a small Darwin authorization bridge and a platform-neutral status/error mapper, then gate external-device enumeration/opening on authorized camera access. Add a standalone helper-bundle build script and Make target that embeds usage descriptions and requires an operator-supplied signing identity; leave the existing non-hardware `make build` target unchanged.

**Tech Stack:** Go 1.26.5, cgo Objective-C, AVFoundation, VideoToolbox, POSIX shell, macOS `codesign`.

## Global Constraints

- Do not modify target code, target images, public host/target protocol, Stage A scope, or `rtmp-services/`.
- Do not commit signing identities, certificates, credentials, private target configuration, or generated binaries.
- Preserve the existing `CGO_ENABLED=0` generic build; the new helper build is explicit and Darwin-only.
- Keep the FogCast media path video-only; raw CoreAudio remains diagnostic evidence, not a new product worker.
- Use a stable signed bundle identity for TCC authorization; fail if `FOGCAST_SIGNING_IDENTITY` is absent.

---

### Task 1: Add authorization status mapping tests

**Files:**
- Create: `internal/remotemedia/capture_authorization.go`
- Test: `internal/remotemedia/capture_authorization_test.go`

**Interfaces:**
- Produces `type CaptureAuthorizationStatus uint8`, constants for `notDetermined`, `restricted`, `denied`, and `authorized`, `captureAuthorizationError(status) error`, and `captureAuthorizationStatusName(status) string`.

- [ ] **Step 1: Write the failing tests**

```go
func TestCaptureAuthorizationErrorForUnresolvedStatuses(t *testing.T) {
	for _, tc := range []struct {
		status CaptureAuthorizationStatus
		want   string
	}{
		{captureAuthorizationNotDetermined, "not determined"},
		{captureAuthorizationRestricted, "restricted"},
		{captureAuthorizationDenied, "denied"},
	} {
		err := captureAuthorizationError(tc.status)
		if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "Camera") {
			t.Fatalf("status %v error = %v", tc.status, err)
		}
	}
}

func TestCaptureAuthorizationErrorAcceptsAuthorized(t *testing.T) {
	if err := captureAuthorizationError(captureAuthorizationAuthorized); err != nil {
		t.Fatalf("authorized status error = %v", err)
	}
}
```

- [ ] **Step 2: Run the focused test and verify it fails**

Run: `mise exec go@1.26.5 -- go test ./internal/remotemedia -run TestCaptureAuthorization -count=1`

Expected: compile failure because the status type and mapper do not exist.

- [ ] **Step 3: Implement the status type and mapper**

Use `uint8` constants and return an error containing the status name, `Camera`, and `System Settings > Privacy & Security > Camera`. Treat only `authorized` as success; return an `unknown` error for an unrecognized value.

- [ ] **Step 4: Run the focused test and verify it passes**

Run: `mise exec go@1.26.5 -- go test ./internal/remotemedia -run TestCaptureAuthorization -count=1`

Expected: PASS.

- [ ] **Step 5: Commit the test and mapper**

```sh
git add internal/remotemedia/capture_authorization.go internal/remotemedia/capture_authorization_test.go
git commit -m "feat: model Darwin capture authorization states"
```

### Task 2: Gate the Darwin adapter and expose actionable failures

**Files:**
- Modify: `internal/remotemedia/capture_darwin.h`
- Modify: `internal/remotemedia/capture_darwin.m`
- Modify: `internal/remotemedia/capture_darwin.go`
- Modify: `cmd/remote-play-spike/main.go`
- Test: `internal/remotemedia/capture_unavailable_darwin_test.go`
- Test: `cmd/remote-play-spike/main_test.go`

**Interfaces:**
- C produces `int mr_capture_video_authorization_status(void)` with values matching the Go constants and `int mr_capture_wait_for_frame(void *handle, int timeout_ms, char **error_out)`.
- Go keeps `ListCaptureDevices` and `OpenNativeCapture` signatures unchanged; both return the authorization error before touching an external device.

- [ ] **Step 1: Add failing Darwin test coverage for the preflight boundary**

Add a test that calls the new status-to-error mapper for a non-authorized status and asserts the error names Camera permission. Keep physical-device tests unchanged so they still validate “not found” after an authorized inventory.

- [ ] **Step 2: Run the Darwin-focused test before implementation**

Run: `CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 mise exec go@1.26.5 -- go test ./internal/remotemedia -run TestCaptureAuthorization -count=1`

Expected: FAIL because the C bridge and adapter gate do not exist.

- [ ] **Step 3: Implement the Objective-C status bridge**

Map `AVAuthorizationStatus` to the four stable integer constants in C. For a
`notDetermined` status, call `requestAccessForMediaType:completionHandler:`
only when the signed helper has `NSCameraUsageDescription`, with a bounded
prompt wait; a raw executable without the bundle usage description returns an
actionable error. Add a condition-variable wait helper that returns after the
first captured frame, or returns a timeout/runtime error without blocking
cleanup.

- [ ] **Step 4: Integrate the gate into Go**

Call the C status/request functions in `ListCaptureDevices` and `OpenNativeCapture` for physical UVC capture. Call the wait helper from `NativeCapture.Start` with a bounded two-second timeout. Skip physical inventory for the existing `screen` source, and preserve screen-capture behavior and existing close/error ownership.

- [ ] **Step 5: Run focused Darwin tests and the package tests**

Run: `mise exec go@1.26.5 -- go test ./internal/remotemedia -run 'TestCaptureAuthorization|TestOpenNativeCaptureRejectsUnknownPhysicalDeviceClearly' -count=1` and `mise exec go@1.26.5 -- go test ./internal/remotemedia -count=1`.

Expected: PASS; hardware-only tests may skip when the device is absent.

- [ ] **Step 6: Commit the Darwin gate**

```sh
git add internal/remotemedia/capture_darwin.h internal/remotemedia/capture_darwin.m internal/remotemedia/capture_darwin.go internal/remotemedia/capture_unavailable_darwin_test.go
git commit -m "feat: fail Darwin capture on missing authorization"
```

### Task 3: Add the stable signed helper bundle build

**Files:**
- Create: `resources/fogcast-host/Info.plist`
- Create: `resources/fogcast-host/Entitlements.plist`
- Create: `scripts/build-fogcast-host.sh`
- Create: `scripts/tests/fogcast-host-build_test.sh`
- Modify: `Makefile`

**Interfaces:**
- `scripts/build-fogcast-host.sh [output-app-path]` consumes `FOGCAST_SIGNING_IDENTITY`, `VERSION`, and `REVISION`; it produces a signed `FogCastHost.app` or exits before building when the identity is absent.
- `make build-fogcast-host` delegates to the script and remains separate from `make build`.

- [ ] **Step 1: Write the failing shell test**

The test must run the script with no `FOGCAST_SIGNING_IDENTITY`, assert a non-zero exit, and assert the error names the missing variable. It must also check the template contains `CFBundleIdentifier`, `NSCameraUsageDescription`, and `NSMicrophoneUsageDescription`.

- [ ] **Step 2: Run the shell test and verify it fails**

Run: `sh scripts/tests/fogcast-host-build_test.sh`.

Expected: FAIL because the helper script and template do not exist.

- [ ] **Step 3: Add the bundle template and build script**

Use bundle identifier `com.fogcast.host`, executable `fogcast-api`, and explicit usage strings. Build with `CGO_ENABLED=1 GOOS=darwin GOARCH=arm64`, copy the plist and camera entitlement, require `FOGCAST_SIGNING_IDENTITY`, and run `codesign --force --deep --options runtime --entitlements resources/fogcast-host/Entitlements.plist --sign "$FOGCAST_SIGNING_IDENTITY"`. Do not write certificates or identities to disk.

- [ ] **Step 4: Add the Make target and shell test**

Declare `build-fogcast-host` phony, invoke the script, and validate the missing/ad-hoc identity paths, shell syntax, static plist/entitlement fields, and (on Darwin) a successful fake-sign bundle layout. Keep generated app output under ignored `bin/` by default.

- [ ] **Step 5: Run the shell checks**

Run: `sh -n scripts/build-fogcast-host.sh scripts/tests/fogcast-host-build_test.sh` and `sh scripts/tests/fogcast-host-build_test.sh`.

Expected: PASS.

- [ ] **Step 6: Commit the helper build**

```sh
git add resources/fogcast-host/Info.plist scripts/build-fogcast-host.sh scripts/tests/fogcast-host-build_test.sh Makefile
git commit -m "build: add signed FogCast macOS capture helper"
```

### Task 4: Document authorization and verification workflow

**Files:**
- Modify: `docs/POC6-DEVELOPMENT.md`
- Create: `docs/runbooks/fogcast-authorized-capture.md`

**Interfaces:**
- The runbook documents the exact helper build, System Settings grant, launch command, code-signature inspection, and first-frame verification without recording private identities.

- [ ] **Step 1: Add the failing documentation checks**

Extend the shell test to require the runbook strings `FOGCAST_SIGNING_IDENTITY`, `NSCameraUsageDescription`, `System Settings`, `codesign`, and `captured_frames`.

- [ ] **Step 2: Run the documentation check and verify it fails**

Run: `sh scripts/tests/fogcast-host-build_test.sh`.

Expected: FAIL because the runbook does not yet exist.

- [ ] **Step 3: Write the runbook and POC6 cross-reference**

Document that Genki permission is not inherited by FogCast, that `make build` remains cgo-disabled for generic checks, and that the signed helper must be authorized before `bin/fogcast-api` is used for physical capture. Include a redacted verification example requiring non-zero `captured_frames`; do not include target addresses, signing identities, or secrets.

- [ ] **Step 4: Run documentation checks**

Run: `sh scripts/tests/fogcast-host-build_test.sh`, `git diff --check`, and the repository Markdown local-link check.

Expected: PASS.

- [ ] **Step 5: Commit the workflow**

```sh
git add docs/POC6-DEVELOPMENT.md docs/runbooks/fogcast-authorized-capture.md scripts/tests/fogcast-host-build_test.sh
git commit -m "docs: document authorized FogCast capture launch"
```

### Task 5: Full verification and host smoke test

**Files:**
- Inspect: all files changed in Tasks 1–4

- [ ] **Step 1: Run formatting and focused tests**

```sh
gofmt -w internal/remotemedia/capture_authorization.go internal/remotemedia/capture_authorization_test.go internal/remotemedia/capture_darwin.go
mise exec go@1.26.5 -- go test ./internal/remotemedia ./cmd/remote-play-spike ./cmd/fogcast-api
sh scripts/tests/fogcast-host-build_test.sh
```

- [ ] **Step 2: Run the full verification suite**

```sh
mise exec go@1.26.5 -- go test ./...
mise exec go@1.26.5 -- go vet ./...
git diff --check
```

- [ ] **Step 3: Build the cgo-enabled helper on macOS**

With `FOGCAST_SIGNING_IDENTITY` already set in the local environment to the
operator's stable signing identity:

```sh
test -n "$FOGCAST_SIGNING_IDENTITY"
make build-fogcast-host
codesign --display --verbose=4 bin/FogCastHost.app
```

Do not record the identity string in tracked files.

- [ ] **Step 4: Run the authorized host smoke test**

Grant Camera/Microphone to `com.fogcast.host`, launch the helper from its bundle executable, run the named `ShadowCast 3` capture while Sonic 2 is active, and require a report with `captured_frames > 0`. Preserve only redacted counters and hashes in ignored artifacts.

- [ ] **Step 5: Review and hand off**

Inspect `git diff`, verify only host code/build/docs changed, request Vega review of the exact diff, and report any remaining inability to run the signed smoke test as an external signing/permission blocker rather than claiming the shell fix is proven.
