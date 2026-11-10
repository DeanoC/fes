# Plan-bound native FAT file prototype

Implementation: `e984200f1d86f8a772f116b17ebde5759c97dcc7`, based on
`a0aa06ba8`. This extends the hardware-proven bounded raw-sector writer with a
separate [FAT file diagnostic](../../../platform/embedded/tamago/fatcheck/README.md).
Ordinary startup remains read-only. The existing in-process package installer
is not enabled by this change.

The diagnostic binds the real spare CID, capacity, MBR and protected recovery
files into its executable. It requires an absent unique diagnostic target,
creates 4096 bytes, updates to 1024 bytes, verifies both, contains/resumes the
same owned SD session, freshly mounts and rereads the updated file, rechecks
boot metadata/protected files and ends contained. Writes cannot leave the sole
FAT32 partition or touch its primary/backup boot sector. Failures retain
unresolved ownership without retries, rollback or automatic firmware stops.

Software tests join the actual C dispatcher, Go PIO/card model and FAT code,
including independent mtools readback. Refusal and failure cases cover wrong
card/MBR/protected files, existing targets, cancellation, uncertain creation
and update writes, failed initial/final stops, replacement cards and lost
resume replies. The native decoder requires an explicit expected plan, the
complete cold prefix, all six ordered records and exact content/ownership
generations. Interoperability consumes actual model output with an explicitly
synthetic cold prefix; it does not establish physical boot.

The [prepared result](prepared.json) records the source-checked SDK, native
entry, SD-only DDR secure firmware, U-Boot/FIT and HPS-first SOF. Embedded SPL
decoding and offline FAT readbacks pass. The live recovery selection is the
existing trace recovery kit; the packager's generated SD image is not selected
for a physical card write. The [software results](software-results.json) retain
the exact affected plan and bounded outcomes. An additional parser test checks
duplicate/unknown fields added to an otherwise valid plan.

All eleven selected commands pass: parent regressions (1,875 cases, three
existing skips, plus one ancillary case), SD/PIO/FAT/store race checks, host/UI,
runtime, contracts, generated consumers and FPGA software/model checks (1,595
cases, 79 tool/fixture-dependent skips), plus planner and whitespace checks.
The final parser test passes separately; production bytes match the compiled
implementation commit above. Skipped tools/fixtures supply no acceptance.

No UART, JTAG programming, firmware call, upload or physical SD write occurred
in this preparation. The last physical state remains the qualified recovery
prompt from the separate independent raw-sector readback. Native FAT
persistence, package installation, native networking, power-loss behavior and
full appliance acceptance remain open. No earlier failed capture or consumed
programming grant is reused.

Next: prepare a fresh bounded top-only procedure, revalidate the installed
spare/recovery files, perform one native file trial, then separately boot
recovery and read the exact updated file while checking protected media.
The original factory card stays safe, the bottom kit stays excluded, and
old-output cleanup remains paused.
