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
