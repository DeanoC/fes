# Idle MENU → rooms — Phase 2 execution brief

**Status:** implementation brief (2026-09-21). Docs only. Does not reopen
the [design lock](idle-menu-rooms.md). Deano owns FES parent merge.

**Audience:** Bob / Herd / Caster executing Phase 2. Read the lock first.
This brief names owners, PR slices, and engineering questions only.

**Base:** FES `main` tip `392be4f9` (lock PR #102). Paths below exist in
that tree.

---

## Goal / non-goals

**Goal:** HDMI is honest while Linux and tenfoot start, and again after
Stop. Cold boot programs a distinct U-Boot splash RBF (logo + motion:
board firmware, not rooms, not a play package). `Stop` / `LoadIdle()` no
longer bring up stock MENU chrome. Stop idle is that splash or an
explicit non-Menu idle.

**Does not deliver:** attract ABI, rooms on kit HDMI, graphics
accelerator, FC2D hardware, kit-as-host, retiring `fogcast-kit`.

Phase 2 **may** still enable the HPS framebuffer if tenfoot / kit HDMI
has no other path. That is a temporary Stop-idle surface, not a MENU or
linuxfb product commitment.

Play packages (`fes.pong`, `fes.zx81`, `fes.coleco`) and launch
composition stay unchanged.

---

## What is true now (tip `392be4f9`)

There is still one sealed idle bitstream. Image policy
[`image/build/native-inputs.toml`](../image/build/native-inputs.toml)
pins MiSTer-devel `Distribution_MiSTer` `menu.rbf` (`idle_rbf.path`,
install `/usr/share/mister-runtime/idle.rbf`). Fetch/verify hard-require
that repository, commit, path, and install path
([`image/scripts/fetch-native-runtime-inputs.sh`](../image/scripts/fetch-native-runtime-inputs.sh),
[`image/scripts/verify-native-runtime-inputs.sh`](../image/scripts/verify-native-runtime-inputs.sh)).
Rootfs install is
[`image/buildroot/board/fogcast-target/native-post-build.sh`](../image/buildroot/board/fogcast-target/native-post-build.sh).

Media copies those same bytes to FAT `/menu.rbf` and **rejects** FAT ≠
rootfs idle
([`scripts/media_inside.py`](../scripts/media_inside.py)
`check_inputs`). U-Boot env is `core=menu.rbf`
([`boot-media.lock.toml`](../boot-media.lock.toml),
[`scripts/media_inputs.py`](../scripts/media_inputs.py)
`EXPECTED_ENVIRONMENT`). Receipt `idle.fat_destination` is `/menu.rbf`.
[`docs/bootable-media.md`](bootable-media.md) still requires both idle
hashes to match one pinned value.

[`NativeHardware::LoadIdle`](../sources/libmister-runtime/src/native/hardware.cpp)
opens `/usr/share/mister-runtime/idle.rbf` (production
[`src/linux/production_hardware.cpp`](../sources/libmister-runtime/src/linux/production_hardware.cpp)
`MISTER_RUNTIME_IDLE_RBF`), programs
`ProgrammingProfile::mister_v1`, then
[`MenuVideoBringup::BringUp("MENU")`](../sources/libmister-runtime/src/native/video.cpp):
SPI user-io reset, `CoreLoader::Probe` (command `0x0014`) **must**
observe `MENU`, ADV7513 720p, then HPS framebuffer over SPI `0x002f`
into reserved DDR (`ValidateMenuFramebuffer`: 640×480 at `0x22001000`).
Startup also emits a core-name change from `"MENU"`
([`Runtime` start](../sources/libmister-runtime/src/runtime.cpp)).
Sequence prose:
[`ARCHITECTURE.md`](../sources/libmister-runtime/ARCHITECTURE.md)
native idle admission.

Kit HDMI idle is that MENU overlay plus `/usr/sbin/fogcast-kit`
([`S60fogcast-kit`](../image/buildroot/board/fogcast-target/native-rootfs-overlay/etc/init.d/S60fogcast-kit)).
[`ui/kitlauncher/run.go`](../sources/FogCast/ui/kitlauncher/run.go)
skips `present` while `session.State == "active"` and keeps last menu
pixels until confirmed idle.

`config/core-recipes.toml` has play packages only. **No splash / idle /
logo recipe exists in misteross.** Do not invent one in this brief;
slice 3 is TBD.

---

## Workstreams (strawman owners)

Owners are component strawmen. Edit here if file ownership differs.
Agree files before parallel edits ([agent workflow](agent-workflow.md)).

| Workstream | Strawman owner | What Phase 2 changes | What it must not do |
| --- | --- | --- | --- |
| **A. Splash RBF source/build** | misteross recipe + FES seal into `native-inputs.toml` (and media lock). Not a format-2 play package. | New bitstream: logo + motion. Document observed core-ID (or that U-Boot does not probe). HIP/nextpnr unless a recipe documents Quartus-as-oracle. | Do not register it in `config/core-recipes.toml` as `fes.*`. Do not treat `fes-demo` / Pong / MENU as the splash. Source tree is **TBD / needs misteross recipe**. |
| **B. Image / media split** | FES `image/` + `scripts/media*.py` + `boot-media.lock.toml` | U-Boot `core=` (FAT splash) and runtime idle (rootfs `LoadIdle` artifact) **may diverge**. Fetch, verify, post-build, media receipt, appliance media (`scripts/appliance_media*.py`) name both classes. | Do not keep a hidden “they are the same bytes” check once splash exists. Do not reintroduce Main / `/dev/MiSTer_cmd`. |
| **C. `LoadIdle` contract** | libmister-runtime | Identity, programming profile, and video/fb bring-up become an explicit **defined idle**, not hardcoded MENU chrome. Startup, Stop, and failed-launch cleanup share that path. | Do not load attract-as-ABI (phase 4). Do not infer splash identity from a filename. |
| **D. Kit HDMI after Stop** | FogCast `fogcast-kit` / `ui/kitlauncher` | When MENU chrome is gone: HDMI shows splash / defined idle. Catalog grid is **not** the Phase 2 product. linuxfb overlay only if the idle bitstream still enables HPS fb. | Do not ship rooms on kit HDMI. Do not grow an offline catalog. Do not claim FC2D (`ui/gfx/fpga_protocol.md` remains a software stub). |
| **E. Acceptance / HIL** | FES parent; one kit operator via [kit sharing](kit-sharing.md) | Prove splash + defined Stop idle on an exact assembled image. Play launch+Stop still returns that idle. | Do not call it rooms-on-HDMI, attract ABI, or exact-artifact acceptance for a later image. Media receipt stays `hardware: not-run`. |

---

## Ordered PR slices

Smallest first. Slices 1–2 do not wait on splash RTL. Slice 3 is the
FPGA blocker. 4 needs 1+3; 5 needs 2+4 on a kit; 6 is the leased proof.

### 1. Name splash vs Stop-idle in FES policy

**Owner:** FES (`image/build/native-inputs.toml`, fetch/verify/post-build,
`scripts/media.py` / `media_inside.py` / `media_inputs.py`,
`boot-media.lock.toml` env check, `tests/test_media*.py`,
`image/scripts/tests/native-runtime-inputs_test.sh`).

**Do:** Two named artifact classes (splash for FAT/U-Boot, idle for
rootfs `LoadIdle`). Stop requiring FAT bytes == rootfs idle. Transitional
pin may still be today’s sealed Menu until slice 4.

**Success:** Parent tests pass with two slots; same-bytes pin still
assembles. Receipt can record distinct `fat_destination` vs
`rootfs_destination` hashes. No kit, no new RBF.

**Does not:** Remove MENU chrome from HDMI.

### 2. `LoadIdle()` without MENU chrome as the contract

**Owner:** libmister-runtime (`hardware.cpp` `LoadIdle`,
`video.cpp` `MenuVideoBringup`, `runtime.cpp` start/Stop `"MENU"`
emits, `production_hardware.cpp` idle path, unit tests in
`tests/unit/native_hardware_test.cpp` / `video_test.cpp` /
`runtime_test.cpp`).

**Do:** Idle identity, `ProgrammingProfile`, and HPS fb become parameters
of a defined idle, not `BringUp("MENU")` plus required `0x002f`. HPS fb
optional (slice 5 may still want it). Fakes prove a non-MENU idle
publishes `idle` without reboot_required.

**Success:** Unit tests: non-MENU idle programs; MENU-less probe does not
fail idle when the idle recipe says so; play-package cleanup still
`LoadIdle()`s. Focused runtime tests + `git diff --check`.

**Does not:** Change image pins; claim HDMI.

### 3. misteross splash recipe (TBD source)

**Owner:** misteross (new core tree + producer). FES only lists it in
`native-inputs` in slice 4.

**Do:** Logo + motion bitstream. Not rooms, not attract ABI, not
`config/core-recipes.toml`. Document: HDMI timing, whether it speaks
MiSTer user-io (`0x0014` name, `0x002f` fb), observed core-ID, compiler
route. **Source does not exist in this tree** — needs a new misteross
recipe; do not retarget `cores/fes-demo` or sealed `menu.rbf`.

**Success:** Sealed RBF + provenance; sim or documented visual check of
logo/motion. Identity string recorded for slice 2/4. Quartus only if the
recipe says oracle.

**Does not:** Format-2 package id; factory play-set change.

### 4. Seal splash and wire U-Boot `core=` vs rootfs idle

**Owner:** FES image/media (depends on 1 and 3). Appliance copies in
`scripts/appliance_media.py` / `appliance_media_inside.py`.

**Do:** Pin splash bytes for FAT / U-Boot; pin Stop-idle bytes for
`/usr/share/mister-runtime/idle.rbf` (same as splash or a second thin
bitstream — see questions). Update `core=` and FAT name, **or** keep
filename `menu.rbf` with new bytes (question below). Verify no longer
assumes Distribution_MiSTer `menu.rbf` as the only legal idle.

**Success:** `make check` / image verify: splash digest on FAT, idle
digest on rootfs; U-Boot env matches the FAT file it programs. QEMU
packaging still passes. No HIL required in this slice.

### 5. Kit HDMI after Stop without MENU chrome

**Owner:** FogCast `cmd/fogcast-kit`, `ui/kitlauncher` (and image init
only if the service must not paint). Depends on 2+4 on hardware.

**Do:** After confirmed idle, HDMI is splash / defined idle. If idle
still enables HPS fb, `fogcast-kit` **may** paint linuxfb as a temporary
surface (connecting/retry, last-good shelf) — not Menu’s file browser,
not rooms. If idle has no `0x002f`, skip present and leave FPGA pixels
alone (today already skips present while `active`).

**Success:** Host-only tests for present/skip. On kit (slice 6): after
Stop, no MENU file-browser chrome. Catalog-on-linuxfb is optional and
labelled temporary.

**Does not:** Phase 3 single-launcher / rooms-on-HDMI.

### 6. Leased HIL: splash + defined Stop idle

**Owner:** FES parent; one operator; existing target lease
([kit sharing](kit-sharing.md), FogCast `docs/DEVELOPMENT.md` kit).

**Do:** Cold image from slice 4. Cold boot: HDMI shows splash (logo +
motion), not MENU chrome, through Linux/`LoadIdle`. Launch a closed
play package; Stop returns the defined idle, not Menu. Record boot /
image identity. Classify **hardware diagnostic** vs **exact-artifact
acceptance** explicitly; do not inherit NES/Coleco records.

**Success:** Dated validation note: image SHA, splash SHA, idle SHA,
what HDMI showed at boot and after Stop, play identity, lease released.
`hardware: not-run` remains on the media receipt.

**Does not claim:** rooms on kit HDMI, attract ABI, accelerator, FC2D,
linuxfb gone.

---

## Open engineering questions (not product)

Prefer a strawman in the implementing PR. Do not fork a second lock.

1. **Stop idle vs splash bytes.** Reuse splash on `LoadIdle()`, or a
   second thin bitstream? Lock allows either. Strawman: **reuse splash
   bytes** until a second bitstream has a recipe. Divergence in policy
   (slice 1) still lands so a second file is possible.

2. **Does `mister_v1` stay for splash `LoadIdle`?** U-Boot programming
   has no `ProgrammingProfile`. Linux `LoadIdle` today always uses
   `mister_v1` (MiSTer GPO/reset in
   [`fpga_manager.cpp`](../sources/libmister-runtime/src/native/linux/fpga_manager.cpp)).
   A thin splash may not speak user-io. Strawman: keep `mister_v1` only
   if splash needs that reset recipe; otherwise a contained profile plus
   ADV7513 bring-up (`FixedVideoBringup` is the play-package HDMI path,
   not automatically correct for splash). Decide from the splash recipe,
   not from MENU.

3. **HPS framebuffer on Stop idle.** Needed only if kit linuxfb has no
   other HDMI path. If splash omits `0x002f`, HDMI is FPGA splash pixels
   — honest, and closer to the lock. Strawman: **do not require** HPS fb
   for idle success; enable it only when the idle recipe declares
   support.

4. **FAT filename vs U-Boot env.** `uboot.img` is locked; media verify
   requires each `boot-media.lock.toml` `environment` string to appear
   in those bytes. Changing `core=menu.rbf` → `core=splash.rbf` means
   **resealing U-Boot**. Strawman: **keep FAT `/menu.rbf` and `core=` as
   the filename**, replace bytes with splash, until a kernel/U-Boot
   change is already scheduled. Name in policy/docs is still “splash”,
   not Menu.

5. **Core-ID string.** `Probe` is MiSTer user-io. Splash ID is unknown
   until slice 3. Do not invent `SPLASH` / `FESIDLE` here. If splash has
   no probe, idle must not require one.

6. **HDMI during Linux start.** Today U-Boot programs Menu, then runtime
   `LoadIdle()`s the same bytes (possible blink). If splash ≠ Stop idle,
   `LoadIdle` at daemon start will reprogram. Strawman: accept one
   reprogram at runtime start; do not add a “leave U-Boot bitstream”
   skip unless measured.

7. **`fogcast-kit` present after Stop.** Phase 2 is not dual-UI kill.
   Strawman: if HPS fb exists, keep connecting/retry + last-good shelf
   as the temporary overlay; if not, do not blank-and-fail the service —
   log and leave splash visible.

---

## Pointers

| Doc | Why |
| --- | --- |
| [Design lock](idle-menu-rooms.md) | Product decisions; Phase 2 row; artifact classes |
| [Component boundaries](component-boundaries.md) | Host vs runtime vs image vs misteross |
| [Image assembly](image-assembly.md) | FES `image/` owns idle install |
| [Bootable media](bootable-media.md) | FAT `/menu.rbf` vs rootfs idle; no Main; HIL gates |
| [Kit sharing](kit-sharing.md) | Exclusive lease; no unleased programming |
| [Agent workflow](agent-workflow.md) | File ownership, worktrees, handoff shape |
| [Core packages](core-packages.md) | Play packages; locked idle is still Menu until slice 4 |
| [Sofa launcher](sofa-launcher-design.md) | Current MENU + kit-grid HDMI |
| Runtime [`ARCHITECTURE.md`](../sources/libmister-runtime/ARCHITECTURE.md) | Current `LoadIdle` MENU + HPS fb sequence |
| FogCast [`fpga_protocol.md`](../sources/FogCast/ui/gfx/fpga_protocol.md) | FC2D software stream; not this phase |

Phase 3+ stay in the lock. Do not start attract ABI or rooms-on-HDMI
work from this brief.

FES parent merges stay Deano’s.
