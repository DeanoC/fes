# FES ZX81 (planned)

This is planned work on parent branch `feat/zx81`. The selected image does
not include a ZX81 package.

FES Pong proved a custom GP ABI, a format-2 package and the
Yosys/nextpnr-mistral board shell. The next described core is a small
computer: **FES ZX81**, derived from the MiSTer Quartus ZX81 implementation.
The bring-up order is custom ABI, Quartus, then nextpnr/mistral.

Read the [design](superpowers/specs/2026-09-10-fes-zx81-design.md) and
[implementation plan](superpowers/plans/2026-09-10-fes-zx81.md) before
editing. Current described-core commands remain in
[core packages](core-packages.md).

## Worktrees

| Path | Branch / pin | Role |
| --- | --- | --- |
| `out/dev/zx81/fes` | `feat/zx81` @ FES `de2b917d` | Parent docs and later pin integration |
| `out/dev/zx81/mister-packages` | `feat/zx81` @ `82b78c4` | ABI, registry, generated consumers |
| `out/dev/zx81/misteross` | `feat/zx81` @ `4a8b863` | RTL, sim, Quartus then Mistral recipes |
| `out/dev/zx81/libmister-runtime` | `feat/zx81` @ `2bfff81` | Computer driver, keyboard, media |
| `out/dev/zx81/FogCast` | `feat/zx81` @ `cd70be1` | Package load and host keyboard |
| `out/dev/zx81/ZX81_MiSTer` | detached `9b24af6` | Read-only upstream Release 20260603 |

Keep root `sources/` at the indexed gitlinks. Component workers edit the
paths above. The integrator alone will select reviewed commits later.

## First slice

ZX81, 16 KB RAM, original ROM, 40-key matrix, one `.p` tape image, fixed
720p60 HDMI. No ZX80, colour, YM2149, turbo, joysticks or SDRAM.

## Validation

Use component tests and Verilator in the worktrees. Quartus 17.0.2 is the
first programmable artifact. Do not start the Mistral computer recipe until
the Quartus kit checks in the plan have passed. Hardware claims need dated
exact-artifact evidence on the designated kit.
