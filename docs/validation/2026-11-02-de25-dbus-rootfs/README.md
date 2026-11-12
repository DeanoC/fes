# DE25 read-only D-Bus startup repair

The shared native rootfs now redirects the entire D-Bus state directory to
`/run/dbus`. Previously only `machine-id` was a symlink: `dbus-uuidgen --ensure`
still created a sibling temporary file in the read-only `/var/lib/dbus`
directory and failed. Preparation replaces both the old directory and the
previous final-file alias, including on warm trees, without generating an ID
in the image.

Image producer: `dfaffa8696fb164bf30576a6f7a1d7cd1844dd48`.
Final software-check source: `620fd59b0d14803a61d5553dc2686fb4afcc5ac4`.
The latter changes one test assertion only; all image code and recipe bytes
match the actual producer. Publication changes documentation only.

The shared boot smoke now requires a valid runtime machine-ID and a successful
system-bus `GetId` reply with a five-second bound. Both DE10 and DE25 log
validators require the new readiness marker. The DE25 tar/ext4 verifiers also
check the parent-directory alias and absence of a generated image machine-ID.
Existing root-read-only, volatile tmpfs, kernel-failure, exact-input and bounded
process cleanup requirements remain enforced.

The unchanged old image is retained. Booting it through the stronger check
reproduces the actual read-only write failure and is refused; no successful
boot report is published. That negative control is an expected refusal, not a
boot pass. A clean rebuild produces a new actual rootfs and SDK. Its full-system
AArch64 virtual boot passes the filesystem and responsive D-Bus checks without
the original error, using the same real owner kernel.

| Check | Result |
| --- | --- |
| Focused Python plus shared DE10/DE25 rootfs/warm-tree fixture | PASS, 25 Python cases, 6.6 s total |
| Actual old-image negative control | Expected CLI refusal, 48.7 s; original bytes preserved |
| Clean managed-base build | PASS, 561.9 s |
| Public base verification | PASS, 3.5 s |
| New actual image/kernel virtual boot | PASS, 47.4 s |
| Actual SDK, kernel/library/source audit | PASS, 2.7 s |
| Corrected complete affected plan | PASS, all six commands, 255.3 s |

The final affected run selects 1,325 parent tests with 45 skips in 248.638 s,
and checks 40 generated consumers/42 fixture copies. The original run remains
**failed**, 247.0 s: its sole failure floors the full test count before a
strict-half comparison. With the new test there are 1,283 selected tests in the
plan under examination; 641 is less than half, but not less than its floored
integer 641. Comparing `2 * selected < total` preserves the exact strict-half
rule. No production selector or limit changes. The focused correction passes
in 1.8 s. [test-rounding.json](test-rounding.json) and the retained original
aggregate distinguish that correction from image verification.

[checks.json](checks.json), [boot.json](boot.json), [audit.json](audit.json)
and [old-control.json](old-control.json) bind exact artifacts, producer and
consumer identities, executed commands and evidence scopes. The actual SDK
authenticates against its committed image producer. All six kernel files and
the complete base ABI contract match the earlier base. Of 54 actual shared
library payloads, 52 match byte-for-byte; GObject and GIO differ and their
individual hashes are retained. The newly built image passes its own complete
ELF/library/symbol closure and virtual boot, without borrowing the old image's
boot result. All 374 canonical MENU, 59 combined-compiler and 58 runtime-test
inputs are unchanged; no Quartus or combined RTL/CPU proof is rerun.

[retention.json](retention.json) binds independently restored source, the real
new base/kernel/SDK, both virtual observations, original failed checks and their
completed continuation. No original receipt or failure is rewritten green.
Independent restoration and archive verification pass in 7.5 s. The restored
tree is `bea3e8ace423eaf3a0b173f2d75b18200bd9aad9`; all 57 regular archive
members are verified. The archive is 258,852,561 bytes, SHA-256
`02975444e2bec446afc81e8d2a899d0bb4921232e547357ffc06a97a491787c7`.
All 20 completed owned process/process-group identities and both named virtual
boot containers are checked absent.

This qualifies a real managed intermediate's filesystem and virtual D-Bus
startup. The virtual machine has no physical Ethernet, persistent FAT, FPGA or
Bluetooth adapter. It provides no factory admission, native firmware/CPU/DMA,
peripheral, watchdog, managed MENU/catalog ZX81 or update/recovery acceptance.
Complete factory/card production still requires sealed MENU and the high-level
DE25 receipt-selection integration. Output/whole-reset timing, firmware/native
qualification and the independent eligible FPGA rebuild remain open.

No board, UART, SSH, capture, SD, installed software/kernel or flash is accessed.
The bottom board's claim and last authenticated ABI2 baseline remain unchanged;
no ABI3 clients run against it. The top board is untouched. The full normal-kit
goal stays active.
