# Fresh card inspection after the native write rejection

Base `90259ead7`; compiled inspector `a5d6027e9`, build
`72f88abee51877af`. The preceding writer remains failed, with an unknown
possible partial write and last observed active SD generation 3. Its capture,
two-second passive tail, exact original source and cleanup remain retained.
This new read-only attempt neither resumes that test nor assumes its original
tail contents were preserved.

The matched inspector includes the board-wiring repair and its eight passing
software checks. It has no write plan and ordinary storage remains read-only.
Complete SDK/entry/BL31/FIT/SPL/source validation and independent embedded-SPL
decoding pass. The new operator's actual decoder and single-upload checker
pass host models, including nine negative lineage/protection cases. The
frozen procedure covers 2,410 files and three eligible source trees. No cached
paired/USB native admission is adopted.

The physical sequence is a fresh qualified recovery boot, one fixed
`FES/UPLOAD.BIN` Ethernet upload plus a fresh SD readback, then the matched
native boot. Native reads the recovery manifest, MBR and final eight sectors,
reports a new candidate plan from the observed full CID/capacity and current
hashes, contains/resumes the same card, reads again and ends SD-contained.
The resulting plan requires independent review before a new writer is built.
No native sector write or FPGA data release is selected.

All earlier bounds remain: program 90 seconds, prompt 50, native inspection
100, each boot phase 240, outer experiment 700. The first failed gate stops
later stages and preserves a bounded receive-only tail. Every programming
operation requires a fresh forced vendor scan and a consumed-once grant in
this new directory; prior grants cannot be replayed. Full clean-source,
file/tool/claim/UART admission and a fresh 60-second USB stability check are
required before starting.

Only top TRWU8CBL and its installed disposable spare are selected. Its factory
card stays safe, factory restore remains waived and the bottom kit is excluded.
The normal user acquires the shared Quartus lock, then runs the bounded root
supervisor inside it. Vendor children run as uid 1000, using the qualified
3 MHz/auto-off transport and top-filtered private server. Exact original
clock/auto settings and USB permissions, private-server shutdown and UART
release must be independently checked afterwards. Old-output cleanup remains
paused. Preparation grants no live authority or hardware success.

Raw helpers and results are under `out/nano-sd-reinspect-trial-20261110/`.
This inspection cannot establish native write success, FAT persistence,
normal core installation or full appliance acceptance.

## Hardware result

[The fresh inspection passed](hardware-result.json): recovery boot, fixed FIT
upload/fresh readback, native programming, full cold/ATF/EL2/EL1 proof, native
manifest/MBR/tail reads, same-card contain/resume/re-read and final SD
containment all completed. The last observed state is now SD-contained
generation 7; CPU park was not observed. Host cleanup independently verifies
15 MHz/auto-on readbacks, exact USB permissions, private-server termination and
free UART. Both programmer operations completed; there are zero new failures.

[The new card/layout review](real-plan-review.json) observes the same full CID,
capacity and MBR and confirms that the current tail hash equals the pre-failure
hash. This establishes current contents, not whether the earlier failed test
issued any write. That original failed attempt remains failed with an unknown
write outcome. Its capture and grants are unchanged and not reused.

The reviewed scratch extent remains the last eight sectors, outside the sole
FAT partition and 144 MiB boot image. Both boot environments are volatile.
The native inspection wrote no sectors; recovery wrote only the fixed upload
file. A new matched writer can now be built from this fresh reviewed plan.
