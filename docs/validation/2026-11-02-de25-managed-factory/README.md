# DE25 managed factory assembly action

The existing DE25 image builder now has `managed-factory` and `verify-factory`
actions. They select AArch64 image artifacts, admit MENU through the normal
package selector, install the selected runtime through the existing Buildroot
recipe and use the shared native factory post-build/software-bundle assembler.
The compiler and matching owner kernel remain pinned and independently checked.
No alternate package, bootstrap, update or hardware coordinator is introduced.

The implementation is `36438ad3ad426a1cb6ce16d4a62b7b6f3e7a6fde`.
`0cb6a68e4e4b9c574e8f61712b668212ae9ad39b` changes only the shell fixture to
select its CPU explicitly and test AArch64 installation/hash failure. The
[source scope](source-scope.json) verifies that difference and all unchanged
FPGA/ZX81 inputs. Subsequent publication changes documentation only.

The factory step now installs its current S40 supervisor startup script after
the software bundle passes assembly. This supports the intermediate base that
deliberately omits S40. The shared runtime-input verifier also selects the
AArch64 executable for an explicitly chosen DE25; a staged ARMv7 executable
cannot satisfy that check. Factory build keys bind source, kernel and selected
artifact identities. Fetch alone has network access; build and verification
remain unprivileged and offline.

**Complete factory assembly has not been run.** Its required admitted MENU
package is unavailable: the retained canonical candidate remains unsealed.
There is no new factory rootfs, card image, installation or hardware pass in this
record. The builder retains closed normal admission and labels its future
outputs as uninstalled candidates. No timing/seal exception or bypass was added.

## Executed checks

| Check | Result and scope |
| --- | --- |
| Focused managed-image Python tests | 16 cases pass at the implementation source, including nine factory configuration, cache, provenance, environment, network and refusal cases |
| Shared DE10/DE25 factory shell fixture | Pass; real Go bundle packer with explicit readelf, package-selector, executable and RBF stand-ins; includes omitted S40, rejected bundle staying disabled and successful assembly installing the current script |
| Corrected runtime-input fixture and consistency | Pass in 7.1 s at `0cb6a68e4`; AArch64 selection, absent/foreign/digest refusal, actual prebuilt-runtime Make recipe and failed hash preserving the installed file; 40 generated consumers and 42 fixture copies |
| Actual pinned Buildroot configuration | Pass in 5.7 s at `36438ad3a`; real 2021.02.4 source, pinned builder/compiler and generated AArch64 factory configuration, with shared read-only policy and BlueZ/D-Bus |
| Real AArch64 runtime installation | Same 5.7 s job; actual previously built daemon passes the normal package Make hash check and installation, then ELF64/AArch64 inspection; this is installation, not a new compilation or execution |
| Existing managed SDK reuse | Pass in 3.4 s at `0cb6a68e4`; authenticate actual `cad04bd65` producer, current kernel policy, all retained artifact hashes and archive containment |
| Full affected plan | All six commands pass in 264.1 s at `36438ad3a`; parent suite runs 1,307 tests with 41 skips in 258.462 s; consistency passes 40 consumers/42 copies |
| Source/proof restoration | Pass in 1.6 s; independently restored tree `34be7ccf40811a95dd454afb4797d0b353b8ac9a`, proof archive members verified |

The real configuration proof invokes the existing SDK relocation and
`prepare_buildroot` path. It does not supply or admit any FPGA package and does
not build a rootfs. Its isolated source/cache mounts and retained script are in
[config-proof.py](config-proof.py) and [config-inside.py](config-inside.py).
The compiler reports `aarch64-buildroot-linux-gnu`. The original toolchain
archive is pinned to SHA-256
`7912076c08d93c8d8c28c0ef81bd406abf6b1acb9e01ccac37d44307e39759a7`.
The installed real runtime remains SHA-256
`05c25c74d9c53a073625d56c36e04ef63a407c71c823a25e4e50448c6325dffd`,
from the [normal SDK/runtime proof](../2026-11-02-de25-managed-sdk/README.md).
No C++, Go, kernel or FPGA implementation bytes changed in this increment.

The first aggregate focused job **failed** after 5.2 s: its older Make fixture
omitted `BR2_arm=y`, so the existing production architecture guard correctly
refused it. Python and shared factory checks had passed before that failure.
The fixture correction selects the CPU and adds AArch64 install/tamper cases;
it does not relax the guard. The original job/log are retained alongside the
successful focused continuation. The full affected result belongs to the
unchanged implementation at `36438ad3a`, with the later shell-only correction
tested separately; neither is presented as exact-publication-head CI/review.

## Retention and remaining acceptance

[checks.json](checks.json) records the scopes; [retention.json](retention.json)
records the independently restored source and proof identities. Actual local
artifacts and original logs are retained at
`/home/deano/fes/out/artifacts/de25-managed-factory/0cb6a68e4/`.
The proof archive SHA-256 is
`279fb1ce580646ee08091db8548f5343d54b45cd0ffea81926eba2fd37fd6395`.
The archive verifies 27 regular members. The complete earlier managed-base/SDK
publication is an explicitly hashed prerequisite; it was not modified.
All completed owned job identities were checked absent, and the isolated
configuration container completed and was removed. Its named compiler volume
is retained. No board UART/SSH/capture, allocation, FPGA, SD, installed software,
kernel, QSPI or power access occurred.

All 374 canonical MENU, 59 combined-compiler and 58 runtime-test inputs remain
unchanged. No Quartus, HDL model or CPU execution proof was repeated. HDMI
output and whole-reset signoff, native firmware/disabled-F2H operation and an
eligible independent identical-RBF rebuild remain open. Only the two explicitly
approved HDMI asynchronous-input findings have reviewed exceptions. No
additional PCB files are available; no board delay was invented.

Next, execute the complete factory action with a qualifying MENU, then bind its
real output into the existing release/bootstrap/media path and capacity-checked
DE25 card layout. Top-level `make image`, image `export` and complete media
still select DE10. Native PID1/FAT/root switching, SSH/peripherals,
watchdog/reboot recovery, owner ABI3/SID/SMMU/coherent DDR/firmware security,
MENU/catalog ZX81 launch-stop-return-relaunch, HID, BASIC LOAD/RUN/SAVE/reload,
persistence, update/restore and whole-call bounds remain required for the
[normal-kit goal](../../design/de25-linux-platform.md).

The bottom `TRWU8J8G` claim is retained and the top `TRWU8CBL` is untouched.
Last authenticated native health remains 2026-11-01 14:48 UTC on owner ABI2;
never run ABI3 clients against it. This increment restores, promotes and
releases no baseline. A future QSPI/boot-configuration deployment needs fresh
explicit approval after the concrete artifact is ready; no physical action is
requested by this record.
