# Native SD scratch diagnostic and read-only inspection candidate

Base: `1213c7fed`. This connects the real native writer to the owned PIO session
without enabling writes in the ordinary core-loading image. The new `sdwrite`
command requires a compiled card plan: CID/capacity, MBR SHA-256 and the original
SHA-256 of the final eight sectors. The native workflow independently checks
these values, supported MBR partition bounds and write protection before its
single write batch. It rejects a protective GPT, unsupported partition types,
overlapping partitions, occupied tail sectors and replay of the written pattern.
The trusted spare-card layout must independently establish that these sectors
contain no required unpartitioned firmware data.

The workflow flushes and checks its pattern and MBR, contains SD through secure
firmware, resumes the same card and checks again, then ends contained. It checks
receipt ownership and exact generation increments at every stage. Any failure
stops the workflow without retry, rollback or automatic stop. The written
pattern remains available for independent recovery readback. This establishes
neither FAT write semantics nor power-loss durability nor normal installation.

`storage --sd-scratch-inspect` is the read-only precursor. It prints candidate
plan JSON through the existing SD ownership and contain/resume diagnostic. The
write plan must then be reviewed and compiled into a separate matched `sdwrite`
boot chain. A read-only observation does not authorize the later write.

Host tests execute the actual C firmware dispatcher and Go PIO driver against
an independent register/card model. They verify byte-for-byte preservation
outside the scratch region, exactly eight ascending sector writes, two firmware
stops, same-card resume/readback, malformed/stale card and content expectations,
partition failures, replay rejection, uncertain partial writes and failed stop.
Read-only inspection produces the expected plan without writes or stops.

Build evidence is under `out/nano-sd-scratch-20261110/`. The writer build uses
**synthetic CID/content expectations** and must not be deployed. The read-only
inspector has its own native entry, SD-only DDR BL31/U-Boot and composed SOF/FIT,
with embedded SPL decoding and independent FAT file readbacks. The generated
card image is an offline packaging product, not a proposal to replace the
installed recovery card. A live test must retain the installed qualified trace
recovery and transfer only its admitted native FIT.

The first BL31 invocation rejected ROM-specific OCRAM parameters; the SD-only
candidate uses the existing DDR firmware contract. An initial U-Boot build
failed because the wrapper replaced its recorded Python dependency path; the
corrected environment builds successfully in a fresh directory. Packaging
rejected the trace-only recovery record and an obsolete recovery base. Its
qualified base was traced from current recovery provenance and validated
before composition. Every failed output remains retained; no guard was relaxed.

`result.json` records software and artifact outcomes. No board, card, firmware
call, programming cable or serial port was accessed. The next hardware action
is a fresh bounded **read-only inspection** of TRWU8CBL, followed by reviewing
its real plan, building the matched writer and arranging independent readback.
Native SD writes, FAT persistence, network installation and ordinary appliance
acceptance remain open. The active kit is still the previously observed returned
menu; preparation does not imply containment or CPU park.

All eight selected software checks pass, including 1,853 parent regressions
(three existing skips), generated consumer consistency, actual C/Go SD lifecycle
and package-store tests, and the complete firmware/RTL/native/Tenfoot integration.
The added command-only planner regression passes with all 41 planner tests;
unchanged suites were not repeated after that test-only addition. Both final
native builds bind unchanged production source hashes. The read-only candidate
FIT is 2,615,096 bytes under the unchanged 8,388,608-byte limit. No live hardware
admission or programming grant has been created.
