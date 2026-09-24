# Coleco expansion bus diagnostic on the designated kit

This is a functional diagnostic of the merged Coleco shell and expansion
artifacts, using a temporary host and temporary agent/runtime binaries built
from this worktree. It is not acceptance of a newly assembled appliance image.

## Artifacts and setup

- FES source base: `1983135e457bf4b56859d81a78b49f8ad08d5408`, with the
  runtime manifest and target-agent status fixes in this change.
- Sealed shell package: `adca771d492ae1b8220aa3e1447735078bc8bdaef605d2dd38c89ff6cdce3b2b`.
- Diagnostic expansion: `931b9e0bb8c7115a308facd7e1f51ae64ffb6ad0114801a557774044fba8709b`.
- Linked RBF SHA-256: `f9d40544bed434656adb4e07314b10de32e080a3f74ddd9501dd3f0f852d8000`.
- Temporary ARM runtime SHA-256: `08b7539c4f30b398d54f5e83b2f3cf2219ff4a73e08acd5a453c5297a4423aef`.
- Temporary ARM agent SHA-256: `8c51b88857ed638866a6adf963ec07b8b4266978c444f647aa552b464def03e2`.
- Temporary host API SHA-256: `eaf882cd50fad85ba34b05bea5804b5a92e6efefa21ea0f02d7a01f9f68894f9`.

The isolated host imported the shell, expansion and a 1,145-byte original
diagnostic ROM (SHA-256
`d6ce9649257df534c1ed9d7078110c25771e8e0254adef0c06698d8e13505f6e`).
The ROM jumps over the existing BIOS-free Graphics I cartridge to a short
probe. It checks the expansion's `0x2000` reset value (`0x5a`), writes and
reads `0xa5`, arms the `0x2001` WAIT mask with `5`, waits for a read and
checks that the mask clears, then writes and reads the `0x2002` IRQ-enable
register as `1` and `0`. Any failed comparison loops before video setup;
only the passing path jumps into the existing Graphics I cartridge.

## Kit observations

Normal `POST /api/v1/session/launch` returned an active native FPGA session.
Host and runtime status reported composition
`d16d0884b32b96fdc0b2e51b1a1f1b637004968c2b2893813bed38e87033a8e9`
with the linked payload SHA-256 above, `fes.application` 1.0 and active
`fes.expansion.coleco-bus` 1.0. The linked probe painted the expected Graphics I
checkerboard: [linked-probe.png](coleco-expansion-bus-2026-09-24/linked-probe.png).
Its capture SHA-256 is
`63d10d0719a54b2696edec1adfd8123557d796d13b64252fa5a882eb12e09470`.

As controls, the unmodified Graphics I cartridge painted the same checkerboard
on the shell without the expansion. The probe ROM on that same shell without
the expansion stayed black:
[shell-probe.png](coleco-expansion-bus-2026-09-24/shell-probe.png), SHA-256
`5a6a3ec4c782533a00f0301f63fbaee50f3fca1542685f7f0da95f2f2e500ebb`.
Each retained image is the final frame of a 120-frame HDMI capture from the
ShadowCast 3 on `/dev/video0`; its first captured frame was stale black even
when the Graphics I control was running.

This comparison demonstrates that the visible pass path depends on the linked
expansion and that the tested memory/register reads returned the expected
values. The ROM checks WAIT-mask completion, not elapsed WAIT duration, and
IRQ-enable readback, not the physical IRQ line independently. Those remain
separate hardware checks if required.

The host Stop returned idle, and the kit lease was free. The temporary host
container and its private configuration were removed. The agent/runtime bind
mounts were removed, restoring the pre-existing target binaries and services;
target health was ready and the kit lease free after restoration. The installed
image still reports runtime commit `b3e0be71b94d2047dbc7f1b9d3f9de44de1dff47`.

## Integration defects found

The installed runtime rejected the Coleco bus because it knew only the ZX81
slot. After replacing it with current source, runtime admission rejected the
new canonical `boundary_patch` manifest field. The first patched runtime then
programmed the linked RBF, but the target agent rejected its composed status
because its status validator knew only the ZX81 ABI/socket pair. This change
adds the exact Coleco boundary-patch parser and ABI/socket status validation.
No parent image build or image-level acceptance was performed.
