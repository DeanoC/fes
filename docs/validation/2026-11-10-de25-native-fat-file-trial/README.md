# Bounded native FAT file persistence trial

The [hardware result](hardware-result.json) passes on TRWU8CBL, operator source
`918b5bfc3`, native build `11f99d3a93c5f9cc` compiled from `e984200f1`.
All six native records bind the real spare CID/capacity, selected path/content
and same owner at generations 3/3/3/4/6/7. The native phase created 4096 bytes,
updated to 1024, read them, contained/resumed SD, freshly mounted and reread,
rechecked protected media and ended SD-contained at generation 7.

A separately consumed recovery programming/boot then read the exact updated
file and verified its SHA-256, protected prefix, MBR and root recovery FIT and
manifest. Four fresh-marker/live-LMB observations pass, including the target's
real 1024-byte file-load reservation. Recovery checked the card summary; full
CID was enforced natively and was not reobserved in recovery. No SD write or
network command occurred in that independent readback. All three programming
operations completed with zero new programming failures.

Independent [host cleanup](cleanup-evidence.json) confirms 15 MHz/automatic
adjustment on, exact original USB permissions, the owned server gone and UART
free. The last observed board state is the qualified recovery prompt after
file verification; no native lease or CPU park is inferred across reboot.
The full [2,560-file/three-code-tree closure](retention-proof.json) has archive
readback/mode/membership checks and independent base-plus-delta source
restoration, strict fsck and pack verification. External tools are explicitly
identified dependencies; the archive grants no live authority.

All [six selected software commands](software-results.json) pass, including
1,881 parent cases with three existing skips and one ancillary case, plus the
14 focused observer checks. The private operator and programming-gate models
also pass. Earlier raw-write failures/captures remain unchanged.

This proves bounded native FAT creation/update and clean recovery-boot
persistence. Ordinary startup stays read-only. Physical package installation,
native networking, power-loss behavior and full appliance acceptance remain
open. The successful absent-target plan is consumed: this file now exists and
must not be silently overwritten or reused for another trial. The following
retains the original prepared procedure.

Next integration is the existing owned package installer, which publishes its
catalogue after content readback. The board adapter still uses the conservative
400 kHz transport; the separate default-off 25 MHz driver prototype needs owned
session wiring and physical qualification before relying on larger packages
within the existing operation bounds.

Prepared candidate: `11f99d3a93c5f9cc`, compiled from `e984200f1`. The separate
[prototype record](../2026-11-10-de25-native-fat-file/README.md) retains its
matched SDK/entry/secure firmware/U-Boot/SOF and software checks. The new
recovery file observer is `1f29d152b`; it does not change the native payload.
This record initially describes preparation, not a hardware pass.

The [frozen procedure](operator-source.json) selects only top TRWU8CBL and its
installed spare card, with the existing qualified trace recovery loader.
The last observed state is its recovery prompt after independent raw-sector
readback. No native SD lease or CPU park is inferred across that reboot.
The original factory card remains safe; restoration is waived and the bottom
kit is excluded. Old-output cleanup remains paused.

One fresh recovery boot verifies a fixed `FES/UPLOAD.BIN` upload and fresh SD
readback. One matched native boot then checks the complete cold/CPU proof and
runs the compiled card/file plan: absent target, create 4096 bytes, update to
1024, verify, contain/resume, fresh mount/readback and final containment.
Only a complete native pass permits the separately consumed recovery program
and fresh boot followed by an independent read of the updated file. Each of
the three programming operations has its own fresh scan and consumed record;
the first failed gate ends the attempt without replay, rollback or retry.

The new read-only observer shares the existing prefix/MBR/protected-file,
fresh volatile marker, card-summary and live LMB checks. It bounds the target
file load to exactly 1024 bytes and verifies the deterministic updated digest;
missing/old/oversized/corrupt files refuse. The file load adds its actual LMB
reservation, unlike raw MMC reads. Its contents are hashed before the private
buffer is reused for a later MBR check. Recovery does not reobserve the full
CID; the native phase enforces that independently expected CID/capacity.
No network, SD write, repair or persistent environment command is issued by
the independent readback.

Fourteen focused recovery-observer tests pass. The injected operator checks
also pass the actual private decoder, upload lineage and native/recovery
receipt failures, plus the separate programming namespaces. These are host
models. They do not establish physical file persistence or ordinary core
installation.

Device bounds are unchanged: recovery/native phases 240 seconds, programmer
90, prompt 50, native file check 100 and independent readback 150; outer 850
and supervisor 890. The shared Quartus lock, qualified 3 MHz with automatic
clock adjustment off, private top-only server, normal-user vendor tools and
exact clock/USB/server/UART cleanup remain required. Fresh source/tool/claim,
USB stability and complete UART-owner admission must pass before hardware.
No cached paired-core admission or previous live grant is adopted.
