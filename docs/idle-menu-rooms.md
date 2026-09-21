# Idle MENU → rooms (design lock)

**Status:** design lock (2026-09-21). This document records product decisions
Deano already made. It is not an implementation claim. Deano owns FES parent
merge.

**Audience:** FES parent, FogCast (host/tenfoot/rooms/kit), libmister-runtime
idle lifecycle, and later mister-packages / misteross ABI work. Read this
before replacing `LoadIdle()`, the locked idle RBF, `fogcast-kit`, or kit HDMI
chrome.

**Related:**

- Phase 2 execution (owners, slices, engineering questions only):
  [`docs/idle-menu-rooms-phase2.md`](idle-menu-rooms-phase2.md)
- Current idle artifact policy:
  [`image/build/native-inputs.toml`](../image/build/native-inputs.toml)
- Runtime idle sequence:
  [`sources/libmister-runtime/ARCHITECTURE.md`](../sources/libmister-runtime/ARCHITECTURE.md)
  (`LoadIdle()`, MENU identity, HPS framebuffer) and
  [`NativeHardware::LoadIdle`](../sources/libmister-runtime/src/native/hardware.cpp)
- Rooms product and Stop restore:
  [`sources/FogCast/docs/rooms-experience.md`](../sources/FogCast/docs/rooms-experience.md)
- Sofa tenfoot renderer, GPU park, FC2D software stub:
  [`docs/sofa-launcher-design.md`](sofa-launcher-design.md),
  [`sources/FogCast/docs/native-tenfoot-launcher/README.md`](../sources/FogCast/docs/native-tenfoot-launcher/README.md),
  [`sources/FogCast/ui/gfx/fpga_protocol.md`](../sources/FogCast/ui/gfx/fpga_protocol.md)
- Launch composition (slots, not idle):
  [`sources/FogCast/docs/launch-composition.md`](../sources/FogCast/docs/launch-composition.md)
- Native image: no Main process, Menu bitstream retained:
  [`docs/bootable-media.md`](bootable-media.md),
  [`image/buildroot/board/fogcast-target/native-post-build.sh`](../image/buildroot/board/fogcast-target/native-post-build.sh)

---

## Why this exists

The living-room product is rooms on the FogCast host/tenfoot renderer. The
kit HDMI path is still the sealed MiSTer MENU core plus `fogcast-kit` drawing
a separate catalog on the HPS framebuffer. Stop restores the room on the
sofa and restores MENU plus the kit grid on HDMI. That is two UIs, two idle
stories, and a leftover Menu bitstream after the Main_MiSTer process is
already gone.

This lock replaces that dual path with one rooms-driven idle through FES
ABI packages. Cold-boot splash stays board firmware. Attract becomes an ABI.
The kit does not grow a second offline catalog.

## What is true now

Present tense, FES tip `306d0e77` (includes #101). Paths below exist in this
tree.

There is no separate FES idle-RBF product. Image policy in
`image/build/native-inputs.toml` pins MiSTer-devel `Distribution_MiSTer`
`menu.rbf` and installs those bytes as `/usr/share/mister-runtime/idle.rbf`.
Media assembly copies the same bytes to FAT `/menu.rbf`. U-Boot still boots
with `core=menu.rbf` (`scripts/media_inputs.py`, `boot-media.lock.toml`).

`NativeHardware::LoadIdle()` programs that locked idle RBF with
`ProgrammingProfile::mister_v1`, requires core identity `MENU`, runs Menu
video bring-up, and enables the 640×480 HPS framebuffer over SPI so Linux
`/dev/fb0` can overlay HDMI. Play packages use `fes-gp-v1` and the format-2
ABI registry. Idle is the remaining `mister_v1` MENU path.

Native image assembly deletes `S40mister-main` and the Menu blanking helper.
Verify rejects `/dev/MiSTer_cmd` / Main / MGL init wiring
(`image/scripts/verify-target-image.sh`). Bootable-media acceptance requires
no Main process. The Menu *core bitstream* remains the idle and U-Boot core.

Host tenfoot/rooms never program the FPGA. They talk to the host API; the
target agent asks `mister-runtime`. Kit HDMI idle is MENU plus
`/usr/sbin/fogcast-kit` (`cmd/fogcast-kit`, `ui/kitlauncher`) presenting a
linuxfb catalog (`S60fogcast-kit`). That shell yields HDMI while a session is
`active` (it keeps last menu pixels and does not present over a live core)
and paints again after confirmed idle.

After Stop: sofa tenfoot restores the same room, location, and parent stack
(`rooms-experience.md` task 7) and unparks GPU cover work. Kit HDMI returns
to MENU plus the kit grid, not the room.

`gfx.NewFPGA` records an FC2D command stream and rasters in software.
`IsStub()` stays true. That is not a programmed 2D core and not HDMI FPGA UI.

## Decisions

Locked 2026-09-21. Do not reopen in implementation PRs.

### 1. Cold boot / U-Boot RBF

There is a loading U-Boot RBF: a logo plus some movement that shows the
board is working. It is board firmware / splash. It is not the rooms
product and not a play package.

Today’s U-Boot `core=menu.rbf` and the runtime idle RBF are the same sealed
Menu bytes. That coincidence is current assembly, not the product split.

### 2. Attract / screensaver

Attract is an **ABI** (demo-scene style): a described FPGA package the
runtime can load through the ordinary FES package path, not MENU chrome and
not a host video playlist painted on `/dev/fb0`.

Factory ships a **default** attract package. Rooms may later supply their
own attract package. Room-authored **custom RBF** is an explicit future
consideration, not v1.

This attract ABI is not FC2D and is not the graphics-accelerator ABI.

### 3. Kit without host

Prefer **one thing**, not a sofa host plus a separate kit-cache runner.
With Linux, a kit may **run the same host** as a deployment mode. Do not
design a second offline catalog UI.

Strawman default (edit here if wrong): the usual living-room topology stays
a sofa/PC FogCast host plus a kit that is the FPGA/target. When there is no
sofa/PC, the kit may run that same host/tenfoot process locally. That is a
deployment mode of the existing host, not a new catalog product.
`fogcast-kit`’s on-kit grid is the path that retires toward rooms, not a
permanent offline UI.

### 4. Video ownership

Long-term ideal: rooms use a **graphics-accelerator FPGA ABI** that owns
the framebuffer. Linux `/dev/fb0` is **debug-only**. Today’s HPS overlay on
MENU is flaky (cursor flash on MENU). Long-term compositor / window-stack
design is out of scope.

Near term: tenfoot keeps its custom renderer. Rooms already use it. It
**yields when a core runs** and **restarts on Stop/exit**, the same
lifecycle as today’s sofa GPU park
(`sources/FogCast/docs/native-tenfoot-launcher/README.md` GPU park;
`ui/kitlauncher/run.go` skips present while `session.State == "active"`).

Do not claim the accelerator or FC2D HDMI path is implemented.

## Visible contract

What HDMI / the sofa should show. “Today” is tip `306d0e77`. “Target” is
the locked product, reached across the phases below.

| Moment | Today | Target |
| --- | --- | --- |
| **Cold boot** | U-Boot programs FAT `/menu.rbf` (sealed Menu). Linux then `LoadIdle()`s the same bytes as `/usr/share/mister-runtime/idle.rbf`. | U-Boot loading splash RBF: logo + movement. Board firmware, not rooms, not a play package. |
| **Idle / attract** | MENU core + HPS framebuffer. Sofa: tenfoot rooms (and optional host attract playlist). Kit HDMI: `fogcast-kit` cover grid / kit attract stills on linuxfb. | Factory default attract ABI package on the FPGA. Rooms may later override with their own attract package. Not stock MENU chrome. |
| **Between games / Stop** | Runtime `LoadIdle()` → MENU + HPS fb. Sofa restores the room and unparks tenfoot. Kit HDMI returns to MENU + kit grid, not the room. | Same rooms surface as before launch, through one tenfoot renderer. Runtime Stop idle is a defined idle (splash or attract), not Menu’s file browser. Renderer yields during play and restarts after Stop. |
| **Host-down** | Kit needs the workstation host for catalogue/launch (`sofa-launcher-design.md`). HDMI still has MENU; launcher shows connecting/retry. Sofa without a kit is the host UI only. | No second offline catalog. Sofa topology: connecting/retry on the existing renderer, not a kit-cache UI. Kit-as-host topology: the kit *is* the host, so “host-down” is that process down — splash/defined idle, then the same tenfoot once it is back. |
| **Debug fb** | Production kit UI is `/dev/fb0` over MENU (`LinuxFramebuffer`, `gfx.OpenLinuxFB`). Overlay is flaky. | Near term: tenfoot may still use linuxfb where HDMI UI has no other path. Long term: `/dev/fb0` is debug-only once a graphics-accelerator ABI owns the framebuffer. |

## Artifact classes

Keep these names distinct. Do not collapse them into “the idle RBF.”

| Class | What it is | v1? | Notes |
| --- | --- | --- | --- |
| **Boot splash RBF** | U-Boot / board-firmware bitstream: logo + motion | Yes (phase 2) | Not a format-2 play package. Not rooms. Not attract. |
| **Attract ABI package** | Factory default described core; demo-scene screensaver | Yes (phase 4) | Ordinary FES ABI package path. Rooms may override later. |
| **Play packages** | Today’s `fes.pong` / `fes.zx81` / `fes.coleco` / … | Already | Unchanged by this lock. Launch composition stays the slot model. |
| **Room custom RBF** | A room ships its own bitstream | No (phase 5) | Explicit future. Do not design v1 launch around it. |
| **Graphics-accelerator ABI** | FPGA owns the framebuffer; rooms draw through that ABI | No (phase 5) | FC2D today is a software-recorded command stream (`fpga_protocol.md`), not this ABI and not HDMI UI. |

The sealed Distribution_MiSTer `menu.rbf` is none of the above. It is the
current idle/U-Boot stand-in to be retired.

## Process model

One FogCast host, one tenfoot renderer, rooms as the product UI.

```text
Today, sofa topology
  tenfoot/rooms (SDL, host machine) --HTTP--> FogCast host
                                            --network--> mister-agent
                                            --socket--> mister-runtime
  kit HDMI: MENU + fogcast-kit linuxfb grid (second UI)

Today, kit HDMI after Stop
  LoadIdle() -> MENU; fogcast-kit paints the catalog again

Target, sofa topology (usual living room)
  tenfoot/rooms (same renderer) --HTTP--> FogCast host --> agent --> runtime
  kit HDMI: splash / attract ABI / play package; no kit catalog UI

Target, kit-as-host (no sofa/PC)
  same host + tenfoot binaries run on the kit
  still one catalog: rooms, not fogcast-kit
```

`fogcast-kit` (`cmd/fogcast-kit`, `ui/kitlauncher`, image `S60fogcast-kit`)
is the current on-kit catalog. It retires toward rooms. Do not extend it
into an offline cache UI or a second library.

Kit-as-host is optional. It does not replace the sofa host for the usual
living-room setup. It exists so a kit alone still runs **the same** host
and renderer rather than a distinct product.

Physical transitions stay in libmister-runtime. Network/session
coordination stays in the FogCast agent. Image assembly stays in FES
`image/`. Attract and play packages are described cores; splash is board
firmware selected by FES media/U-Boot policy.

## Phases

Same decisions in every phase. Later phases do not invent a second idle
product.

| Phase | Delivers | Does not deliver |
| --- | --- | --- |
| **1 — this document** | Locked split: splash vs attract ABI vs play vs (later) room RBF vs (later) accelerator. Visible contract. Process model. | Code, new RBF, ABI bytes, kit time |
| **2 — shrink idle** | Distinct U-Boot splash RBF. Stop/`LoadIdle()` no longer brings up stock MENU chrome. Defined Stop idle (splash or an explicit non-Menu idle) so HDMI is honest while Linux/tenfoot start. Execution: [`idle-menu-rooms-phase2.md`](idle-menu-rooms-phase2.md). | Attract ABI, rooms on kit HDMI, accelerator, FC2D hardware |
| **3 — single launcher** | Rooms through one tenfoot renderer on sofa and on kit-as-host. `fogcast-kit` catalog path retires toward that renderer. Stop restores the room on the surface that launched, including kit HDMI. | A second offline catalog; room custom RBF; claiming linuxfb is gone |
| **4 — default attract ABI** | Factory default attract package on a described ABI. Runtime loads it as idle/attract through the package path. Room override hooks exist so a room can select a different attract package later. | Room-authored custom RBF as a general bitstream; graphics accelerator |
| **5 — later** | Graphics-accelerator ABI owns the framebuffer; `/dev/fb0` debug-only; room custom RBF allowed as an explicit rooms feature | A compositor stack; treating FC2D software replay as done hardware |

Phase 2 may still enable a framebuffer if tenfoot has no other HDMI path.
That is a temporary Stop-idle surface, not a commitment to MENU or to
linuxfb as the product.

Phase 3 is the dual-UI kill. If kit-as-host is not ready, sofa rooms plus
defined kit idle (splash/attract) is still better than MENU plus a second
grid — but the locked end state is one renderer, not a permanent
host-grid / kit-grid split.

Phase 4’s attract ABI is specified in mister-packages and built in
misteross when that work is scheduled. This lock only requires that it is
an ABI package, factory-defaulted, and overridable by rooms.

## Non-goals for v1

v1 means through phase 4 unless a later PR reopens the table.

- Room-authored custom RBF
- Graphics-accelerator ABI, compositor, or “Linux is just a hypervisor”
- Claiming FC2D or `gfx.NewFPGA` is HDMI FPGA UI
- A second offline / kit-cache catalog UI
- Host + separate on-kit runner as the product architecture
- Replacing play-package launch composition (firmware/expansion/media slots)
- Treating display name, package path, or a raw RBF as attract/splash identity
- Reintroducing the Main_MiSTer process or `/dev/MiSTer_cmd` as idle UI
- Changing U-Boot/kernel/bootstrap in this docs PR

## Open questions

Prefer the strawman. Change the sentence in this document rather than
forking a parallel design.

**Single thing / kit-as-host.** Default: sofa/PC host remains the usual
living-room topology; a kit without that host may run the same FogCast
host and tenfoot as a deployment mode. Not a trimmed kit-only catalog.
Deano can rewrite this paragraph if the kit should never host.

No other product question is left open by this lock. Attract ABI opcodes,
splash RBF source tree, and the exact phase-2 Stop bitstream are
implementation follow-ups inside the classes above.

## Pointers

| Doc | Why |
| --- | --- |
| [`docs/README.md`](README.md) | Index (Proposals) |
| [`docs/idle-menu-rooms-phase2.md`](idle-menu-rooms-phase2.md) | Phase 2 execution brief (does not reopen this lock) |
| [`docs/component-boundaries.md`](component-boundaries.md) | Host vs runtime vs image vs FPGA builder |
| [`docs/image-assembly.md`](image-assembly.md) | FES `image/` owns idle install; FogCast supplies `fogcast-kit` |
| [`docs/core-packages.md`](core-packages.md) | Described play packages; locked idle RBF is current assembly |
| [`docs/bootable-media.md`](bootable-media.md) | FAT `/menu.rbf` vs rootfs `idle.rbf`; no Main process |
| [`docs/sofa-launcher-design.md`](sofa-launcher-design.md) | Current kit HDMI grid + MENU HPS framebuffer |
| [`sources/FogCast/docs/launch-composition.md`](../sources/FogCast/docs/launch-composition.md) | Launch slots; not idle |
| [`sources/FogCast/docs/rooms-experience.md`](../sources/FogCast/docs/rooms-experience.md) | Rooms UI; Stop restores room on the sofa |
| [`sources/libmister-runtime/ARCHITECTURE.md`](../sources/libmister-runtime/ARCHITECTURE.md) | `LoadIdle()` MENU + HPS fb sequence |
| [`sources/FogCast/ui/gfx/fpga_protocol.md`](../sources/FogCast/ui/gfx/fpga_protocol.md) | FC2D software stream; not implemented accelerator |

FES parent merges stay Deano’s.
