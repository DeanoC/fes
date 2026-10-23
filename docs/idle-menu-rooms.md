# Idle MENU → rooms (design lock)

**Status:** idle and room design lock (2026-09-21), with the host-ownership
decision updated on 2026-10-22. It records the target product, not the
currently deployed topology. The kit-as-host implementation remains open under
[#863](https://github.com/DeanoC/fes/issues/863). Deano owns FES parent merge.

**Audience:** FES parent, FogCast (host/tenfoot/rooms/kit), libmister-runtime
idle lifecycle, and later mister-packages / misteross ABI work. Read this
before replacing `LoadIdle()`, the locked idle RBF, or kit HDMI chrome.

**Related:**

- Phase 2 execution (owners, slices, engineering questions only):
  [`docs/idle-menu-rooms-phase2.md`](idle-menu-rooms-phase2.md)
- Current idle artifact policy:
  [`image/build/native-inputs.toml`](../image/build/native-inputs.toml)
- Runtime idle sequence:
  [`sources/libmister-runtime/ARCHITECTURE.md`](../sources/libmister-runtime/ARCHITECTURE.md)
  (`LoadIdle()` programs the sealed splash through `development-contained-v1`) and
  [`NativeHardware::LoadIdle`](../sources/libmister-runtime/src/native/hardware.cpp)
- Rooms product and Stop restore:
  [`sources/FogCast/docs/rooms-experience.md`](../sources/FogCast/docs/rooms-experience.md)
- Sofa tenfoot renderer, GPU park, FC2D software stub:
  [`sources/FogCast/docs/native-tenfoot-launcher/README.md`](../sources/FogCast/docs/native-tenfoot-launcher/README.md),
  [`sources/FogCast/ui/gfx/fpga_protocol.md`](../sources/FogCast/ui/gfx/fpga_protocol.md)
- Launch composition (slots, not idle):
  [`sources/FogCast/docs/launch-composition.md`](../sources/FogCast/docs/launch-composition.md)
- Kit-as-host is the product since 22 October 2026 (#863), with the same
  host binary: [`docs/mesh-kit-host.md`](mesh-kit-host.md). The older mesh
  drafts [`docs/mesh-lan.md`](mesh-lan.md) and
  [`docs/mesh-node-protocol.md`](mesh-node-protocol.md) are historical for
  the kit side.
- Native image: no Main process; splash installed as `idle.rbf`:
  [`docs/bootable-media.md`](bootable-media.md),
  [`image/buildroot/board/fogcast-target/native-post-build.sh`](../image/buildroot/board/fogcast-target/native-post-build.sh)

---

## Why this exists

The living-room product is rooms on the FogCast host/tenfoot renderer. When
this lock was written, the kit HDMI path was the sealed MiSTer MENU core plus
a separate `fogcast-kit` grid catalog on the HPS framebuffer. Stop restored the
room on the sofa and MENU plus the kit grid on HDMI. That was two UIs, two idle
stories, and a leftover Menu bitstream after the Main_MiSTer process was
already gone. The grid has since been removed (#728).

This lock replaces that dual path with one rooms-driven idle through FES
ABI packages. Cold-boot splash stays board firmware. Attract becomes an ABI.
The kit does not grow a second offline catalog.

## What is true now

Idle and splash are one in-tree seal, `sources/misteross/sealed/fes-splash.rbf`.
[`image/build/native-inputs.toml`](../image/build/native-inputs.toml) pins that
file twice: FAT `/idle.rbf` (`splash_rbf`, which the derived U-Boot programs as
`core=idle.rbf`) and rootfs `/usr/share/mister-runtime/idle.rbf` (`idle_rbf`,
the Stop / `LoadIdle()` artifact). [Image assembly](image-assembly.md) copies
that seal into both places. It does not fetch standalone `DeanoC/misteross`,
and it does not install Distribution_MiSTer `menu.rbf`.

`NativeHardware::LoadIdle()` programs that rootfs idle RBF through
`development-contained-v1`
([`idle_recipe.hpp`](../sources/libmister-runtime/src/native/idle_recipe.hpp)).
The sealed splash has no MiSTer user-io and no HPS framebuffer. `mister_v1` is
not a runtime programming profile. Play packages use `fes-gp-v1`.

`fes.menu` is a separate factory package: the image idle display, not this
splash and not a playable library entry. The rooms design below is not what
the image installs. Verify rejects a Main init service and `/dev/MiSTer_cmd`
wiring (`image/scripts/verify-target-image.sh`).

Host tenfoot/rooms do not program the FPGA. They talk to the host API; the
target agent asks `mister-runtime`.

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

### 3. Kit host ownership

Product decision (Deano, 18 October 2026; #863): every kit runs the same
FogCast host and tenfoot renderer on the kit. A sofa or PC is a remote client
and library/content source, not the kit's owner. Do not design a second
offline catalog UI. The on-kit grid launcher has been removed (#728); tenfoot
is the only kit UI.

This is the approved target, not deployed behavior: #863 remains open with no
implementation or acceptance evidence. The existing separate-host path stays
the current implementation until its slices land.

### 4. Video ownership

Long-term ideal: rooms use a **graphics-accelerator FPGA ABI** that owns
the framebuffer. Linux `/dev/fb0` is **debug-only**. Today’s HPS overlay on
MENU is flaky (cursor flash on MENU). Long-term compositor / window-stack
design is out of scope.

Near term: tenfoot keeps its custom renderer. Rooms already use it. It
**yields when a core runs** and **restarts on Stop/exit**, the same
lifecycle as today’s sofa GPU park
(`sources/FogCast/docs/native-tenfoot-launcher/README.md` GPU park;
tenfoot pauses menu-display presents while a core runs).

Do not claim the accelerator or FC2D HDMI path is implemented.

## Visible contract

What the kit HDMI / tenfoot surface should show. The left column is the
historical software snapshot at tip `306d0e77` (before #728), not the current
implementation. “Target” is the locked product, reached across the phases
below.

| Moment | Snapshot at `306d0e77` | Target |
| --- | --- | --- |
| **Cold boot** | U-Boot programs FAT `/menu.rbf` (sealed Menu). Linux then `LoadIdle()`s the same bytes as `/usr/share/mister-runtime/idle.rbf`. | U-Boot loading splash RBF: logo + movement. Board firmware, not rooms, not a play package. |
| **Idle / attract** | MENU core + HPS framebuffer. Sofa: tenfoot rooms (and optional host attract playlist). Kit HDMI: `fogcast-kit` cover grid / kit attract stills on linuxfb. | Factory default attract ABI package on the FPGA. Rooms may later override with their own attract package. Not stock MENU chrome. |
| **Between games / Stop** | Runtime `LoadIdle()` → MENU + HPS fb. Sofa restores the room and unparks tenfoot. Kit HDMI returns to MENU + kit grid, not the room. | Same rooms surface as before launch, through one tenfoot renderer. Runtime Stop idle is a defined idle (splash or attract), not Menu’s file browser. Renderer yields during play and restarts after Stop. |
| **Host-down** | In this snapshot the kit needs the workstation host for catalogue/launch. HDMI still has MENU; launcher shows connecting/retry. | No second offline catalog. The kit's own host and tenfoot run on the kit. If that host is down, show splash/defined idle, then resume the same tenfoot when it returns. Sofa/PC sources are optional; their absence only makes their content unavailable. |
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

Distribution_MiSTer `menu.rbf` is none of the above, and it is not the
installed idle. The image pins `sealed/fes-splash.rbf` as `/idle.rbf`.

## Process model

One FogCast host per kit, one tenfoot renderer, rooms as the product UI.

```text
Historical software snapshot (tip 306d0e77; before #728 and #863)
  tenfoot/rooms --HTTP--> workstation FogCast host
                           --network--> kit mister-agent --socket--> runtime
  kit HDMI: MENU + legacy fogcast-kit grid

Target product after #863 implementation (not deployed yet)
  each kit: tenfoot/rooms --HTTP localhost--> its own fogcast-api
                                              --> its agent --> runtime
  desktop clients use the authenticated launcher listener
  sofa/PC/NAS: library and content sources, not owners
```

The base software supervisor `S40fes-kit-software` starts `fogcast-tenfoot`
as the only on-kit UI; the `fogcast-kit` grid catalog was removed (#728). Do
not add an offline cache UI or a second library on the kit.

Kit-as-host is the approved target (Deano, 18 October 2026; #863), not the
currently deployed topology. Every kit will run **the same** host and renderer
on the kit itself; a sofa or PC is a library source and a remote client, not
the kit's owner. Implementation and acceptance remain open. See
[`docs/mesh-kit-host.md`](mesh-kit-host.md).

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
| **3 — single launcher** | Rooms through one tenfoot renderer on sofa and on kit-as-host. The `fogcast-kit` grid catalog is removed (#728). Stop restores the room on the surface that launched, including kit HDMI. | A second offline catalog; room custom RBF; claiming linuxfb is gone |
| **4 — default attract ABI** | Factory default attract package on a described ABI. Runtime loads it as idle/attract through the package path. Room override hooks exist so a room can select a different attract package later. | Room-authored custom RBF as a general bitstream; graphics accelerator |
| **5 — later** | Graphics-accelerator ABI owns the framebuffer; `/dev/fb0` debug-only; room custom RBF allowed as an explicit rooms feature | A compositor stack; treating FC2D software replay as done hardware |

Phase 2 may still enable a framebuffer if tenfoot has no other HDMI path.
That is a temporary Stop-idle surface, not a commitment to MENU or to
linuxfb as the product.

Phase 3 removes the dual-UI paths and makes each kit's renderer use that
kit's host. Until #863 lands, the existing separate-host path remains the
current implementation; that transition delay does not change the target.

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

**Single thing / kit-as-host.** Rewritten 22 October 2026: the kit always
runs its own FogCast host and tenfoot, and sofa/PC hosts are library sources
and remote clients (#863, [`docs/mesh-kit-host.md`](mesh-kit-host.md)). Not a
trimmed kit-only catalog: it is the same host.

No other product question is left open by this lock. Attract ABI opcodes,
splash RBF source tree, and the exact phase-2 Stop bitstream are
implementation follow-ups inside the classes above.

## Pointers

| Doc | Why |
| --- | --- |
| [`docs/README.md`](README.md) | Index (Proposals) |
| [`docs/idle-menu-rooms-phase2.md`](idle-menu-rooms-phase2.md) | Phase 2 execution brief (does not reopen this lock) |
| [`docs/component-boundaries.md`](component-boundaries.md) | Host vs runtime vs image vs FPGA builder |
| [`docs/image-assembly.md`](image-assembly.md) | FES `image/` owns idle install; FogCast supplies the kit binaries |
| [`docs/core-packages.md`](core-packages.md) | Described play packages; locked idle RBF is current assembly |
| [`docs/bootable-media.md`](bootable-media.md) | FAT `/menu.rbf` vs rootfs `idle.rbf`; no Main process |
| [`sources/FogCast/docs/launch-composition.md`](../sources/FogCast/docs/launch-composition.md) | Launch slots; not idle |
| [`docs/zx81-tape-media.md`](zx81-tape-media.md) | Mid-session ZX81 tape (Phase 3); not idle |
| [`sources/FogCast/docs/rooms-experience.md`](../sources/FogCast/docs/rooms-experience.md) | Rooms UI; Stop restores room on the sofa |
| [`sources/libmister-runtime/ARCHITECTURE.md`](../sources/libmister-runtime/ARCHITECTURE.md) | `LoadIdle()` MENU + HPS fb sequence |
| [`sources/FogCast/ui/gfx/fpga_protocol.md`](../sources/FogCast/ui/gfx/fpga_protocol.md) | FC2D software stream; not implemented accelerator |
| [`docs/mesh-kit-host.md`](mesh-kit-host.md) | Kit-as-host decision record; the kit runs this lock's same host binary |
| [`docs/mesh-lan.md`](mesh-lan.md) | Earlier mesh draft; its kit-side placement and pairing are historical |

FES parent merges stay Deano’s.
