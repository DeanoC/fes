# Fresh independent readback of the completed native SD write

Base `ff86569d0`. Native build `e4487fd3d4c10b80` already completed the full
eight-sector write/read/contain/resume/read/finalcontain sequence. Its whole
attempt subsequently failed on the independent host parser before reaching
the tail. The original capture, passive tail and 2,519-file/source closure
remain retained; no failed gate is resumed or replayed.

This new procedure programs only the qualified recovery loader once and
performs read-only verification. It does not launch native code, upload a
file, issue an SD write or reuse the consumed original-tail plan as write
authority. The new volatile boot marker and live LMB ledger start after that
fresh boot. The repaired captured-format parser binds MMC device, LBA,
requested count and completed count. Six selected host checks and eight
focused verifier tests pass; native executable bytes are unchanged.

Before any live operation, the actual retained native trace is decoded again
against its checked SDK/FIT/ATF descriptors and expected plan, and compared to
the original completed receipt. Operator models reject five failed/altered
proof cases. The actual programming wrapper also rejects native, initial
recovery and unknown operations: only logical `readback`, mapped to the public
recovery role, is permitted. The frozen input set covers 2,406 files and three
eligible source trees. The already completed native image has one historical
programming; none is selected in this new attempt.

The verifier checks the qualified version, fresh boot marker, live memory map,
card summary, partition layout, raw protected prefix, MBR, recovery FIT and
recovery manifest. It hashes the exact eight tail sectors against the pattern,
then checks protected media again. FAT loads have explicit length bounds.
Full CID is bound by the retained native proof; recovery `mmcinfo` supplies
only its recorded manufacturer/product/capacity summary. No stronger CID claim
is inferred. Every command retains the fresh console nonce/prompt checks.

The single recovery boot retains its 240-second bound and program/prompt bounds
of 90/50 seconds. Readback is bounded to 150 seconds, outer experiment to 450
and host supervisor to 600. The first failure stops outgoing board commands
and preserves a two-second receive-only tail when a console is open. The normal
user holds the shared Quartus lock around the root supervisor; vendor children
run as uid 1000. Qualified fixed 3 MHz/auto-off, top-only private service and
independent exact clock/auto/USB/server/UART cleanup remain mandatory.

Only top TRWU8CBL and the installed disposable spare are selected. The original
factory card remains safe, restoration is waived, bottom kit is excluded and
old-output cleanup remains paused. Fresh stable-USB and clean-source/file/tool/
claim/UART admission are still required. No readback success or FAT/ordinary
installation acceptance is predeclared. Helpers and raw results are under
`out/nano-sd-independent-readback-20261110/`.

## Hardware result

[The separate read-only verification passed](hardware-result.json). One fresh
qualified recovery boot read and hashed sectors 7,774,200–7,774,207 against the
native writer's pattern. Protected prefix, MBR, root recovery FIT and recovery
manifest checks pass before and after the tail read. All four fresh-marker/live
LMB observations pass; no raw read is treated as an invented LMB reservation.
Recovery observes the card summary; it does not claim a fresh full CID.

The native writer was not rerun, and this attempt issued no SD write, upload or
native launch. The earlier native phase's full write/read/contain/resume/read/
finalcontain proof and this separate fresh-boot verification establish the
bounded native SD PIO write/readback path. They do not establish general FAT
persistence, power-loss recovery, ordinary package installation or full appliance
acceptance. The previous failed whole attempt and parser-failed capture remain
unchanged; they are not retrospectively promoted to pass.

Independent cleanup passes: 15 MHz/auto-on readbacks, exact original USB
permissions, owned private service gone and complete free-UART inventory.
The single recovery programming completed with no new programmer failure.
The last observed board state is the qualified recovery prompt after the
successful read-only verification; CPU park was not observed. The continuing
top-kit claim and factory-restore waiver remain in effect.
