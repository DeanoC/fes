# Rejected hint-only tape RAM candidate

Producer `786e6383b32617af8baeb32e9de6cf99231a19f9` generated the correct
HPS routes and exact clocks, but the full Quartus compile failed capacity.
Synthesis estimated 136,319 ALMs against 46,800 available and 154,544 registers.
The fitter required 230,235 LUTs against 93,600 available.

The synthesis report identifies the 131,072-bit tape `image` as uninferred
because its read-during-write behavior is unsupported. A memory hint alone
did not implement it in M20K. No SOF was produced; no timing or hardware
acceptance follows from this candidate. Its inputs and source remain frozen.
The later board-local banked adapter is a separate producer.

[receipt.json](receipt.json) gives original and retained hashes. Raw reports
and logs ending in `.gz` are compressed without changing their decompressed
bytes. [Synthesis summary](golden_top.syn.summary),
[synthesis report](golden_top.syn.rpt.gz),
[failed placement report](golden_top.fit.place.rpt.gz),
[compile log](compile.log.gz), [inputs](inputs.json) and
[generation checks](generation.json) are retained.
