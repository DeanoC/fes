# Native Menu Shell Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Paint the existing on-kit library shell on the described native HDMI menu, without changing the host library or physical game lifecycle.

**Architecture:** `fogcast-kit` retains its existing `kitlauncher` model, controller handling, configured FogCast service, cached offline browse and `fbgrid` painter. A CGO-free `gfx.MenuDisplay` rasterizes to RGBA8888 and calls a bounded public local-socket client for runtime `status`, `menu_frame_begin` and `menu_frame_commit`; the runtime owns all DDR and FPGA access. Selection of the menu backend is explicit until FES selects and installs the menu package in a later product-image change. There is no host federation or alternate launch path.

**Tech Stack:** Go, Linux Unix sockets with SCM_RIGHTS, sealed memfd, existing FogCast software rasterizer and libmister-runtime protocol 2.

**Spec:** `docs/superpowers/specs/2026-09-27-native-menu-display-design.md` (staged delivery item 4), `docs/superpowers/specs/2026-09-27-menu-core-library-design.md` (mesh boundaries).

## Global Constraints

- Fixed 1280×720 RGBA8888 staging, 3,686,400 bytes; no physical addresses in the UI.
- The kit's configured FogCast host API remains the library source and executor selection authority. Offline cached rows remain browse-only.
- A local runtime menu generation gates every frame. Active gameplay never gets menu pixels.
- Keep linuxfb behavior and its explicit framebuffer admission intact.
- No FES image, boot or default package selection in this PR; the native menu package still needs product-image integration and acceptance.

## Review Focus

- A stale generation at Stop or game launch must discard the frame and retry only after fresh status.
- A split Unix response with an FD must be read without losing or duplicating the descriptor.
- A malformed status, missing descriptor, wrong geometry or nonzero underflow must fail closed and leave the kit shell running.
- A menu present failure must not permanently suppress painting an unchanged browse model.
- The configured remote service being offline may show cached titles, but cannot enable launch from cache.

---

### Task 1: Bounded runtime frame client

**Files:** Create `sources/FogCast/ui/menudisplay/client_linux.go`, `client_linux_test.go`, and a non-Linux stub if build selection needs it.

**Interfaces:** `New(path string) *Client`; `Status(ctx context.Context) (Status, error)` returns available, generation, fixed geometry and underflows; `Present(ctx context.Context, generation uint64, pix []byte) (Result, error)` carries exactly one sealed frame through begin/commit.

- [x] Write a local Unix server test for status, SCM_RIGHTS, sealed commit and displayed-sequence completion; add invalid-frame rejection.
- [x] Run focused test and verify red.
- [x] Implement the client with finite deadlines, exact response/FD validation and no DDR or MMIO code.
- [x] Run focused test. Commit with the complete shell slice.

### Task 2: Menu graphics backend and kit paint gate

**Files:** Create `sources/FogCast/ui/gfx/menu_display.go` and test. Modify `sources/FogCast/ui/kitlauncher/client.go`, `run.go`, `present_hdmi_test.go`, `sources/FogCast/cmd/fogcast-kit/main.go` and tests.

**Interfaces:** `gfx.NewMenuDisplay(path string)` returns a `gfx.Device` with `Config()` fixed at 1280×720; the backend renders through `gfx.Software` and retries after failed or changed generations. `kitlauncher.Client.SetMenuDisplay(bool)` admits the callback while not active, without changing `ShouldPaintHDMI`.

- [x] Test menu callback admission during idle, splash fallback and gameplay; test backend frame, failure, retry and generation-change behavior.
- [x] Run focused test and verify red.
- [x] Implement explicit `-menu-display` / launcher config selection and the backend. Keep the existing `fbgrid` paint path and local pad/session flow.
- [x] Run focused tests. Commit with the complete shell slice.

### Task 3: Integration evidence and stacked PR

**Files:** Update `sources/FogCast/README.md`, `docs/ARCHITECTURE.md`, `docs/kit-launcher.md`; add a dated validation record when exact hardware is exercised.

- [x] Run FogCast `go test ./...`, ARMv7 kit build, FES `make check`, and affected software checks on this branch.
- [x] Record host-only versus any exact-artifact kit result. Do not infer product-image acceptance.
- [ ] Commit, push, and open a PR targeting `feat/native-menu-scanout`.
