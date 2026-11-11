# DE25 FPGA qualification and DE10 software HIL

The fresh e3f40059d MENU and ordinary ZX81 fits failed admission. Both retain raw reports on fes-build-3090 under `/data/fes-de25-pr1312-qualification-e3f-20261111`. MENU has pixel reset-recovery slack of -4.771 ns; the HDMI output setup/hold diagnostic is positive across all216 paths, with minimum +0.463 ns. The old output-warning inventory also disagrees with the now-constrained video pins. Neither failed candidate was exported or programmed.

The repair adds only output-reset release registers, keeps local reset timing required, and admits the reduced warning set only after a source/database-bound numerical proof for all27 video outputs and four models. Focused unit tests and both affected board simulations pass. Fresh complete producers, independent reproducibility, and physical DE25 acceptance remain required.

DE10 Kit A ran exact e3 userspace with its existing sealed ZX81 package. Launch, injected P/PRINT, F12/Escape and expected-session Stop passed. Linux software reboot returned the same candidate but missed the harness 120-second readiness bound while the agent verified its image; this bounded check remains failed. The named baseline bundle/image, all four process hashes, idle RBF, launcher, protected data, FES Home and free lease were verified before release. The supported title deletion retains one hidden owned parent row; no raw catalog writes or original-media deletion were performed. `de10-hardware.json` preserves the evidence boundary and receipt hashes. No DE25 kit, boot flash, OS image, shared host or capture service was changed.

The a92 reset repair passes all reported numerical timing and all216 video paths, but fresh first fits retain CLK-30028 (wrong implicit HDMI master) and RDC-50003 (the intentional separate output-reset chain); both candidates remain unexported and no independent repeat started. A corrected-clock diagnostic binds the actual pixel PLL output, emits no clock warning, and has setup/hold minima +8.608/+0.102 ns. That diagnostic is not qualification. The user subsequently approved only the source-bound terminal-reset review described in `output-reset-review-proposal.md` and `output-reset-approval.json`. New source corrects the clock constraint and requires exact fanout plus all208 reset paths for that one raw rule. Complete fresh production builds and independent equal-RBF repeats remain required.

The affected parent run found five cached-DE25 preparation errors after moving the codec under the board. Parent inspection/reconstruction now dispatches from the independently selected recipe board; DE10 still uses its existing codec and cross-board packages remain refused. All34 core-dev tests and22 bundle tests pass, including a source-closed DE25 candidate through the real parser. The original broad failure remains retained.

f9 MENU's first fit passed package timing admission with all216 video and208 output-reset paths and zero unreviewed findings. Its independent repeat was superseded by a required shared-source repair, so no qualified package is claimed. f9 ZX81 was correctly rejected because Quartus added `hdmi_active~DUPLICATE`, outside the approved26 physical loads. The next source marks only `hdmi_active` as nonreplicable and binds that attribute in the source guard. The permitted fanout and numerical checks are unchanged. Only owned superseded jobs were stopped.

The corrected full parent software lane passes1714 tests with two skips; generated consistency passes41 consumers and45 fixture copies. The frozen f9 native/AArch64 proof passes14 cases; its receipt retains f9 as producer. Subsequent attribute changes require a new source-bound proof and complete producer fits.

The subsequent main-baseline-only DE10 reboot control became ready after133.672 seconds, also outside120 seconds. Bundle/image stayed a275/179 at generation142. All four role hashes, idle/launcher, idle/free lease and FES Home were reverified before release. The readiness delay reproduces on the main baseline; the timeout is retained and no shared-init image boot acceptance is inferred.

The final single-readiness-register source25bc6ef78 passes the full14-case native/AArch64 combined proof (80 compiler and61 runtime source bindings) and the scanout board simulation's12 stopped-clock cases. Fresh full MENU/ZX81 producer qualification is still in progress at this record.


The completed25bc6ef78 normal MENU and ordinary ZX81 producers each pass the
original and independent repeat. Their RBFs are byte-identical per role. Each
of the four fits checks all85 signoff rules, all216 HDMI setup/hold paths and
all208 output-reset recovery/removal paths with the approved26 terminal loads.
There are zero unreviewed findings and zero Quartus waiver counts. Raw missing
HDMI input/output and terminal reset findings remain visible under the approved
source-bound reviews. `qualification.json` records exact package, RBF, record,
report hashes and minima. `qualified-local-readback.json` binds the transferred
packages, both canonical readers and all74/143 source inputs. No physical
acceptance of these new bitstreams is claimed.

The final FPGA software suite ran2079 tests with21 errors and82 skips. Twelve
DE25 identity fixtures omitted the new review helpers, five static-export tests
still selected the shared DE10 codec, and three DE10 fixtures copied unrelated
DE25 sources into broad fallback records. Those fixtures now select their own
board codec/inputs. One real RISC-V record exceeded64KiB (67490 bytes); its
producer now selects a dedicated31-root closure with the existing Python and
compiler-read guards. The64KiB limit is unchanged. This changes RISC-V functional
identity, so the previous shared-producer equality statement does not apply to
that producer. A normal source-bound timing/seal build remains required.

All93 affected fixture/producer/codec/closure tests pass after repair, plus a
real-module RISC-V regression confirms the bounded audited record, every pinned
input and exclusion of unrelated DE25 files. The prior full-suite failure is
retained; this is focused repair evidence, not a new full-suite result.

The bootstrap DE10 image assembled successfully but its old verifier expected
a machine-id file link. The producer already redirects the whole D-Bus directory
to `/run/dbus` for atomic sibling writes; verification now checks that exact
relative directory link. The rootfs contract test passes. New image/QEMU and
physical shared-init boot acceptance remain required. Standalone diagnostic
verifier invocations without the producer's package environment stopped before
verification; their logs are retained, and normal producer verification is next.
