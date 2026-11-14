# Vendor programmer device-list comparison

The preceding [automatic API diagnostic](../2026-11-11-de25-jtag-enumeration/hardware-result.json)
passed on a fresh private service after the failed native programming. The
original failure therefore remains unexplained; neither the API result nor
its successful identity query authorizes another programming attempt.

This separate, single-use comparison invokes exactly the documented
`quartus_pgm -c <private top cable> -a` device-list command. The pinned installed
help text defines `-a` as displaying connected devices. No `-o`, image path,
UART, CPU-reset or SD command is permitted. Three injected orchestration
models exercise actual command construction, uncertain operations, cleanup
and refusal to retry. Existing qualified3MHz/auto0, private top-only server,
continuing claim, fresh USB stability, shared Quartus lock and exact
clock/USB/server cleanup remain required.

A successful command means device-list execution completed, not that an
image can be programmed. A failure stops the new procedure with no fallback.
The earlier native attempt and the API-only diagnostic remain independently
retained. This experiment changes no native firmware or core payload.


The [device-list result](hardware-result.json) at source `bd2738c58` passed.
The actual vendor command listed the one expected4362C0DD FPGA on the private
top cable and reported zero errors/warnings. It used3MHz/auto0 and restored
original15MHz/auto1. Host inspection confirms private-server exit and current
USB permissions matching the preceding independently verified baseline.
No image operation, UART, CPU-reset or SD command was requested.

The two independently admitted fresh-service enumeration results leave the
earlier shared-service programming failure unexplained. They do not authorize
programming or prove that an image can load. The next change will isolate each
image operation in its own fresh private service and add the actual vendor
`-a` device-list gate before reserving programming. A bounded image-loading
qualification must still pass before any native install/launch claim.

The [retention closure](retention-proof.json) covers126 files across both
scan-only procedures, archive readback/modes, independent source restoration,
strict fsck and pack verification. The prior failed native attempt and its
consumed grants remain separate and unchanged. Top-kit ownership stays with
this task; factory media and the bottom kit remain protected.
