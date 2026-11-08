# Private ownership-check inlining candidate

The retained7abe hardware baseline booted into the menu but failed its original
1s tape deadline. Inner sampled means were13.702us before-state,19.801us USB,
0.703us reset-registers and13.857us after-state. These identify private software
validation as a useful target; they include instrumentation and are not a
disjoint timing partition or performance qualification.

The new ce2e firmware changes only four private helper inline qualifiers:
computer_tape_state, computer_tape_sample_state, composition snapshot and
shared_valid. Removing only the qualifiers and their explanatory comments
reconstructs the complete two original files byte-for-byte. Predicate bodies,
callers, per-access checks, state/ownership policy, MMIO widths/barriers,
cache/MMU settings and the1s deadline are unchanged. There is no permission
cache or new successful-state shortcut. Callback addresses remain ordinary
runtime bindings, with out-of-line copies where their addresses are used.

Actual cross-built ELF inspection confirms the two tape helper symbols are
inlined away and the USB service contains its checks directly. The BL31 binary
grows8192 bytes; individual caller stack accesses are not a complete dynamic
cost model. No speed improvement is inferred from source or assembly. Strong
C ownership/USB/tape/failure tests pass, as do the parent/native integration
checks. The broad FPGA sweep's original300s timeout is retained; its completed
module prefix plus26 separately completed suffix modules cover the catalog
without increasing a job cap.

The matched TF-A/SPL/FIT/HPS-first image passes source/file/segment validation,
with original7360-byte OCRAM headroom. The native boot ID is a29dbbcf72ae64b1.
Runtime SDK, entry, FPGA core packages, ROM/firmware/media files and the917-byte
tape stay exact; the new FIT is the only changed uploaded payload. A fresh full
candidate admission covers5533 files/eight source/project trees, followed by
fresh membership/hash verification. Such file evidence grants no hardware
permission.

The frozen top-TRWU8CBL test permits one recovery and one new native operation,
each with a fresh source-controlled scan, exact transport rechecks and durable
one-use consumption. Thirteen uploads and thirteen fresh reads precede native
boot. The original full menu/core/menu exercise runs if it succeeds; first
failure stops commands with only a2s passive tail. Original phase limits and
the800s overall/890s host bounds remain. One prior transport programming failure
remains, two the maximum before repair. Old native/diagnostic reservations are
immutable and retired; the7abe image is not repeated.

The spare card remains installed, recovery boot files/factory rollback are
preserved, factory restore stays waived, and the bottom kit is untouched. No
physical key action is requested; uncommanded startup input remains terminal.
Independent cleanup must verify15MHz restoration, exact USB permissions, the
owned service gone and UART free. Prepared/result archives retain the complete
source/input/check and observation closure with readback and Git verification.

A successful programming operation or timing diagnostic is not normal return,
fabric drain, visible HDMI or ordinary DE10-equivalent core-load acceptance.
The comparison must establish the candidate's actual behavior before it is
retained as a performance improvement.
