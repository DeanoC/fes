# 2026-10-04: fes.riscv 0.1.0 kit A development load

Exact-artifact development diagnostic of the sealed package in the
[host seal record](2026-10-04-riscv-seal.md). It shows that bitstream running
on kit A. It does not accept an appliance image, a later build, or any other
kit.

## Artifact and path

| Item | Value |
| --- | --- |
| Package | `46c140ee0a9efdd1bb3a1d2a3e350897082029ef6650c590b28858e1aabb4c5e` (`.fcore` sha256 `865af298a59decf74f762c29cacb0b798f7b4bf2bb1f95df7b9138bcf46c053f`) |
| Build ID | `3547ac28dcaacae0f5bbcf3969e72230` |
| Kit | A (`dev` target, `http://fes-kit-a:8182`), lease free before and after |
| Path | the running Powerboat host, `fogcast core-load` (volatile development load, generation 4), HDMI by ShadowCast 3 at 1920x1080 |

Kit B rejected the configured credential (HTTP 401) and was not used. No
second host was started.

## Observed

- The load reported ABI `fes.application` 1.0, active interfaces
  `fes.gamepad` and `fes.video.fixed-720p60`, and the same package and build
  IDs.
- HDMI showed the firmware's picture at the correct 4:3 playfield: white
  border, the horizontal-blue to vertical-green gradient, the banner
  `FES RV32I` and a centred 12x12 box.
- The box colour changed between stills taken one second apart, alternating
  between two palette entries four steps apart, which matches the firmware's
  four timer-interrupt ticks per second.
- Host input `dpad-right` held for half a second and released moved the box
  from the centre to 31 framebuffer pixels right (279 captured pixels at
  9 per framebuffer pixel), one pixel per 60 Hz frame as the firmware does.
- Stop returned the host session to idle and the kit lease to free.

## Not established

Up, down, left and the border clamp on hardware, the execution-hold restart,
colour accuracy of the capture, sustained operation, the factory image, and
kit B. Package-only standing is unchanged.
