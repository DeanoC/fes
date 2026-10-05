# 2026-10-04: fes.riscv 0.1.0 kit A development load

Exact-artifact development diagnostic of the resealed package in the
[host seal record](2026-10-04-riscv-seal.md). It shows that bitstream running
on kit A. It does not accept an appliance image, a later build, or any other
kit.

## Artifact and path

| Item | Value |
| --- | --- |
| Package | `e5c4bd79c0ebf627563eea1040a846626d9bc3ca72fec9966d680e168f1839f4` (`.fcore` sha256 `17feb5ae7ad537a1c3fc9512c232592fd6abc970e7e2483ad14e032b16d35fe2`) |
| Build ID | `55de600f44d9b309472ce998d4e298f4` |
| Kit | A (`dev` target, `http://fes-kit-a:8182`), lease free before and after |
| Path | the running Powerboat host, `fogcast core-load` (volatile development load, generation 5), HDMI by ShadowCast 3 at 1920x1080 |

Kit B rejected the configured credential (HTTP 401) and was not used. No
second host was started. The same checks were first run on the pre-review
package `46c140ee…` (build `3547ac28…`, generation 4); review then found two
defects and the package was resealed, so this record covers the resealed
bytes and repeats every observation below on them.

## Observed

- The load reported ABI `fes.application` 1.0, active interfaces
  `fes.gamepad` and `fes.video.fixed-720p60`, and the same package and build
  IDs.
- HDMI showed the firmware's picture at the correct 4:3 playfield: white
  border, the horizontal-blue to vertical-green gradient, the banner
  `FES RV32I` and a centred 12x12 box.
- The box colour changed between stills taken 1.4 s apart, alternating
  between the magenta and yellow palette entries four steps apart, which
  matches the firmware's four timer-interrupt ticks per second.
- Host input `dpad-right` held for half a second and released moved the box
  from the centre to 31 framebuffer pixels right (279 captured pixels at
  9 per framebuffer pixel), one pixel per 60 Hz frame as the firmware does.
- Stop returned the host session to idle and the kit lease to free.

## Not established

Up, down, left and the border clamp on hardware, the execution-hold restart,
colour accuracy of the capture, sustained operation, the factory image, and
kit B. Package-only standing is unchanged.
