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


RISC-V's full authenticated production build at e9981c96d passes the unchanged
74.25MHz timing requirement at79.4723MHz on seed2 and exports sealed package
3df4dc23c0fb1789d581a0ac4f551f77d6a7ee53e0d23dc706186c0993796ec0.
Seed1's timing miss remains retained. The actual record is7068 bytes, covering
43 inputs under31 roots; compiler-read guards and source-history verification
pass. Directory and archive readers agree. `riscv-qualification.json` binds the
exact record/package/RBF and timing. A core-only KitA claim was refused by its
held lock, so no hardware access or acceptance is claimed for this new package.

The full DE10 MENU simulation target passes (261.16 seconds), including DDR,
reset/drain, board gating and full-frame blitter cases. The broader standard
ZX81 target remains running. The repaired image fixture also passes after its
fake SDK source cache was isolated from the warm real build cache.

The normal e998 bootstrap image and native host builds pass. The QEMU kernel
boot reaches read-only root, writable volatile state, real D-Bus machine-id and
system-bus checks, and the ready sentinel. Normal verification nevertheless
fails because its exact D-Bus line matcher rejected the serial console's CRLF.
The parser now accepts only the exact sentinel with LF or CRLF, preserving the
raw capture. Tests reject missing and malformed sentinels, and the repaired
parser accepts the retained real log. `qemu-serial-parser.json` preserves the
failed parent result. A fresh normal image/verification from the repaired
recipe remains required; no successful parent receipt or hardware boot is
inferred. VExpress has no DE10 Ethernet device, so its boot is no DHCP/SSH proof.


The new sealed RISC-V package passes physical KitA import, compatibility,
exact live package/build/target identity, a visible FES RV32I firmware scene
and owned Stop. This used an isolated e998 host with the existing main a275
bundle/image179. `riscv-hardware.json` and `riscv-hardware.png` record that scope;
gamepad and long-term animation were not tested. KitA was released with the
same generation154 and boot, all four baseline executable hashes, idle/launcher,
original catalog and protected paths, and FES Home. The one new empty
SHA256(fes.riscv) data directory was removed using rmdir after proving it absent
before and empty after. The initial restoration mismatch remains retained.
The earlier admission attempt used the kit launcher proxy and timed out before
any FPGA load; the isolated host completed admission; the earlier timeout’s cause remains unproved. A nonconforming host
container was also refused by its UID/GID gate before startup. Neither refusal
was bypassed. The private host, credential and tunnel are retired.

The broad `make sim-fes-zx81` job reached its1800-second outer cap and was
terminated. Its standard session/display, variants, expansion and many BASIC
regressions passed beforehand, but the full target is not claimed passing.
This is an incomplete simulation campaign, not a physical-kit result or a
waived bound. The complete MENU simulation passes.

The normal4ac image, native host and verification pipeline passes, including
structural and actual VExpress QEMU boot/packaging checks. The exported release
binds root744789c9, the unchanged locked kernel a6c7b1be and the tested e3
userspace. It records one-pass assembly; two-pass reproducibility is not claimed.
The rootfs is byte-identical to the earlier image; the parser was the repaired
input. `de10-image-preparation.json` records hashes and terminal results.
The release was subsequently tested natively under the exact user-approved plan below. All80/61
combined-proof inputs,74/143 DE25 package inputs and43 RISC-V inputs remain
unchanged; their original producer revisions are preserved. The newly qualified
DE25 packages still have no fresh physical acceptance.


The approved network-only image744/e203 trial on KitA is complete. Actual ARMv7
Linux verifies Ethernet/DHCP address and route, SSH, loopback, configured hostname,
the running-kernel Bluetooth module path, writable D-Bus machine-id and a real
system bus, read-only root and volatile tmpfs mounts. Persistent foreground DHCP
is running without the quit flag; timed lease renewal was not observed. All four
running candidate hashes match. MENU is visible. The existing sealed ZX81 package
imports and launches normally with its existing user ROM, exact live identity and
a clear black K prompt on white, then owned Stop returns to MENU. Input and BASIC
were not repeated in this image trial.

A normal idle software reboot returned the same image/bundle healthy and free
after129.673 seconds, missing the unchanged120-second readiness requirement. The
earlier main baseline also missed at133.672 seconds; no cause or timeout waiver
is inferred. Original bundlea275 was fully activated before normal image rollback
to179. Final checks verify generation160, all four original process hashes,
idle/launcher/kernel, original catalog rows and media chunks, protected paths,
ready/idle/free and visible FES Home. The owned private host and credential were
retired and the claim released. No card swap, physical power cycle, kernel,
U-Boot, QSPI, JTAG or shared service change occurred. Normal update history now
retains744 as the previous image. `de10-native-image-hardware.json` binds sanitized
results to private receipts; the PNG shows this trial’s existing ZX81 K prompt.
This supplies actual shared-init native DE10 evidence, not fresh DE25 acceptance.
