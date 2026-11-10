# Bounded native FAT file persistence trial

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
