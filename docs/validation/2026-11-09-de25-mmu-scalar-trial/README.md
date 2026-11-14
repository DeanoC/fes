# Scalar MMU scope hardware trial

Candidate `38c6b2fe551dcd88` is built from selected firmware source
`479bc3756f5f0297391f778f10075d2275536f70`. It removes repeated live MMU
callback-table copies, preserving the secure NC lifecycle state, all six
production translation tables, ownership/CPU/USB/register guards and the
original one-second tape deadline. The linked wrapper is eight instructions
and32 bytes. See the adjacent scalar-scope record for current integration
checks and explicit limits of the extra remote FPGA campaign.

The preceding `bd103b1a199bc2be` image reached Tenfoot Ready, closed/drained
the menu and configured ZX81, then failed initial tape transfer at512/917
bytes with LIMIT(-6). Its actual rejecting branch was not recorded. That
image and all preceding operation grants remain consumed and retired.

This is one fresh paired recovery/native procedure on the exclusively
claimed top TRWU8CBL. The spare card remains installed and the factory card
is preserved; the user waived factory restoration. The bottom kit is
excluded. Fixed3MHz/auto0 programming must restore15MHz/auto1 and exact USB
permissions. Fresh single-use reservations and scans precede programming.
The procedure preserves13 network uploads/fresh SD readbacks, complete
native boot proof, seven configurations, three launches and four menu
visits. Native450s, outer800s, host890s and initial Ready100s remain; the
first failure is terminal with a two-second passive tail. No physical key
press is requested. Independent device cleanup is required afterwards.

This trial does not establish ordinary hardware acceptance. Ordinary verified-package
admission, visible display/input behavior and full DE10 appliance equivalence
remain unproven. Cleanup of old host build data stays paused at the user's
request.

The physical trial is now terminal and retired. Recovery/native programming,
all13 network uploads/fresh SD readbacks, MMU/loader/EL1/SD/FAT startup,
Tenfoot Ready in58.37s, four submitted menu frames with zero reported
underflows, drained menu closure and complete ZX81 configuration/activation
passed. Initial tape still stopped with LIMIT(-6),512/917 completed chunk
progress and6915 reads/431 writes. The last accepted clock was near1s; the
exact rejecting clock or poll branch was not captured. Failure containment
reportedstatus0, with reboot_required/resourcesheld and no runningcore or
normalcore return. The first failure stopped outgoing commands; the original
two-second passive tail is saved. Host detachment does not prove CPU park
or firmware return.

Ownership samples averaged63.71us versus the prior67.16us, about5% lower;
sampling includes instrumentation and does not establish a speedup for every
operation. Inspecting the actual ELF shows remaining144-byte live scope and
112-byte observer stack frames. The observer retains ten register values
across a nested CPU-lock check; reducing these saves is a possible next
repair, subject to preserving the read order and every guard/failure check.
No benefit or new hardware acceptance is claimed for that proposal.

Independent device cleanup passed: original15MHz/auto1, exact USB permissions,
only the owned private server gone and UART free. Historical programming
failures stay2 with no new programmer failure. Both new grants and this
image remain consumed; no retry is authorized for them.

The complete admitted file set, raw terminal evidence and independently restored
pinned source bundle passed archive/file/mode readback:5906 files. See
`hardware-retention-proof.json`. The original source/result archive remains
unchanged; this publication only corrects the rounded comparison and records
retention.

The one-second tape timeout originated in the exploratory fixed-window secure
loader on8 November (`2c5daf36a`); it is a software policy rather than a
demonstrated DE25/FPGA requirement. It includes the entire descriptor/hash
prologue and media upload before CPU release. The current917-byte failure
shows it can reject legitimate loading; the declared ZX81 payload limit is
16375bytes. This is not a gameplay-duration limit. The next timeout design
should separate bounded stalls/individual firmware steps from a size-aware
overall transfer budget, preserving owner/identity/integrity checks and
containment. Merely forcing every valid file through within1s is not the
appliance requirement. No timeout implementation has changed yet.

An unselected direct-store observer prototype passes all four actual cold
platform integration groups. With the existing production compiler/options,
its observer frame shrinks112 to32bytes; the same ten system-register reads
and CPU-lock call remain in the same order. It has no matched boot chain,
measured performance gain or board acceptance. Timeout policy is being
reconsidered alongside this optimization.
