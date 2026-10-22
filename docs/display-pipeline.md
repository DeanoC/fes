# Display pipeline

Every FES core drives one HDMI output. This page describes the shared path
from machine pixels to the HDMI pins, and how the launcher overlay (for
example a disk picker over a running machine) and a surround (a TV or
cabinet frame around the machine picture) fit into it.

Historically each shell grew its own variant: fixed 720p generators,
Direct/Scanlines video-part sockets on Coleco and Atari ST, and a ZX81-only
opaque launcher plane. The pipeline below replaces those variants with one
order of stages that every shell follows.

## Stages

```
machine source ─► video part (Direct | Scanlines) ─► display plane ─► HDMI pins
  (shell)           CRAM-linked socket, per load      (shell)
                                                         ▲
                                       HPS DDR: premultiplied RGBA 1280×720
                                       drawn by FogCast (surround + overlay)
```

| Stage | Owner | Clock | Contract |
| --- | --- | --- | --- |
| Machine source | Core shell | System clock, crossed to the pixel clock | `fes.fabric.video.native-pixels` or `fes.fabric.video.raster-rgb888` request |
| Video part | Linked part archive | 74.25 MHz pixel clock | Part response word: RGB888, DE, HS, VS, CE |
| Display plane | Shell, shared `fes_display_plane` | 74.25 MHz pixel clock | `fes.fabric.video.display-plane` 1.0 behind `fes.video.session-display` 1.0 |
| HDMI pins | Shell | 74.25 MHz pixel clock | `fes.video.fixed-720p60` |

The display plane sits after the video part, outside the part's CRAM fence:

- It needs the HPS fpga2sdram atoms, which belong to the shell, not to a part.
- It must work whichever part the household selected, including when a shell
  has no socket.
- Scanlines and later CRT effects darken the machine picture, not the
  launcher's text or the surround.

A package with the plane declares both `fes.video.session-display` 1.0 and
`fes.fabric.video.display-plane` 1.0 as required interfaces. Kit software
without the plane therefore refuses the package at compatibility, before
programming, and software with the plane refuses a session display that lacks it.

## One plane for surround and overlay

The surround and the overlay are both content of a single 1280×720 plane.
FogCast draws them on the HPS and the plane blends that picture over the
machine pixels. The surround is plane content with alpha 0 over the machine
picture and opaque art around it. An overlay, such as the hardware room's tape
or disk picker, is drawn on top of the surround with whatever alpha the UI
chooses. A second hardware plane would double the DDR read load (one plane
scans 221 MB/s) for no gain, because the HPS already composes the launcher
picture.

The plane uses premultiplied alpha. For each machine pixel `M` and plane
pixel `P` with alpha `a`:

```
out = saturate(P + (M × (256 − a − (a >> 7))) >> 8)
```

Alpha 0 returns `M` exactly and alpha 255 returns `P` exactly. Three
unsigned 9×9 DSP products compute it. Go's `image.RGBA` and the tenfoot
software renderer already produce premultiplied RGBA when they draw onto a
cleared transparent frame. Today's launcher draws opaque frames, which replace
the machine picture as before.

Each shell reports where its machine picture lands in the 1280×720 raster
(x, y, width and height). FogCast uses that rectangle to cut the surround's
window, so one piece of art can fit cores with different picture sizes.

A disabled, faulted or not-yet-visible plane returns machine pixels
unchanged. A display failure never stops, resets or reprograms the machine.

## Fixed latency

The plane receives the part's complete response word: RGB together with DE,
HS and VS. It derives raster coordinates from the DE and VS edges of that
word and delays sync together with the blended RGB, so it adds a constant
three pixel clocks to the entire word. A shell therefore never has to keep its
own coordinates aligned with the plane. The plane composites only after a
whole frame matches the fixed 720p timing; losing that lock while visible is a
display fault.

## Where each core is

| Shell | Machine source | Part socket | Display plane |
| --- | --- | --- | --- |
| ZX81 | Shell 720p raster | No | Yes (`fes_display_plane`) |
| Coleco | Native pixels | Yes | Planned |
| Atari ST | Shell raster | Yes | Planned |
| SMS, SG-1000, Spectrum, C64, Apple II | Shell 720p raster | No | Planned |
| Pong, Catch, RISC-V, RAM test, demos | `fes_video_720p` | No | Planned |
| Idle MENU | `fes_menu_video` (whole screen is the plane) | No | Not applicable |

Planned rows are not implemented. Moving a shell is per-core work: it
needs fresh synthesis, timing closure and kit evidence for that core's
package, like any other shell change.

## Focus and visibility

The runtime's `session_display` operation sets the plane's visibility and
launcher input focus separately. Every request names both:

| `visible` | `focus` | Shows | Input |
| --- | --- | --- | --- |
| `false` | `machine` | Machine pixels | Machine |
| `true` | `machine` | Plane over the machine (surround) | Machine |
| `true` | `launcher` | Plane over the machine (overlay) | Launcher |

A closed plane with launcher focus is rejected. Taking launcher focus posts a
neutral keyboard first. Moving focus on a shown plane keeps its display
generation, grant and frames. A close or display failure that cannot prove
the plane drained keeps launcher focus, so machine keys stay suppressed until a
later request succeeds. Focus is a runtime and FogCast concern; the FPGA plane
composites the same way whoever has input.

## References

- Contract: [`sources/mister-packages/docs/session-display.md`](../sources/mister-packages/docs/session-display.md)
- Video parts: [`sources/mister-packages/docs/video-parts.md`](../sources/mister-packages/docs/video-parts.md)
- HPS DDR: [hps-ddr.md](hps-ddr.md)
- Runtime presentation: [`sources/libmister-runtime/docs/menu-display.md`](../sources/libmister-runtime/docs/menu-display.md)
