# DE25 managed SDK reuse and normal runtime build

Tested source: `cad04bd65d82c98f0e375de927051d215dd63af1`.
The production changes are `5951d0c9f` (SDK authentication/export) and
`cad04bd65` (bootstrap directory). Publication adds documentation only.
This connects the real managed SDK to the existing kit-software runtime
builder. It produces a real managed-base rootfs/SDK and AArch64 runtime,
with CPU-model and ABI checks. Complete factory/card assembly and native
normal-kit acceptance remain open.

No UART/SSH/capture, FPGA allocation/programming, installed software/kernel,
SD, QSPI or power access/change occurred. Bottom claim retained; top untouched.
Last authenticated native health remains 14:48 UTC on 2026-11-01 with owner
ABI2. No ABI3 client was used on it. Hardware admission stays closed.

## Changes

`scripts/kit_software.py` accepts a typed managed-base SDK through its existing
explicit DE25 path. It requires a regular receipt, rejects ambiguous headless
and managed receipts and authenticates the exact producer commit in Git.
It materializes that commit's image/script files as data and recomputes the
producer recipe; it never executes old producer code. Full artifact hashes,
current kernel/SDK policy, builder identity, board, AArch64 C++ configuration
and archive containment remain required. A later software consumer can reuse
a compatible immutable base without forcing the base to match its Git head.
Tests use actual producer/later-consumer Git commits and reject tampering,
foreign boards, missing producers and bad/ambiguous receipts. Headless SDK
selection retains its existing strict profile and no silent fallback is added.

The normal consumer correctly rejected the previous `e7cacb2a2` SDK for three
absolute links, which its narrower isolated C++ proof had not exercised.
The export now makes NTFS sysroot aliases relative within their own sysroot,
and makes the Buildroot gettext alias point to the installed SDK share tree.
Unknown absolute paths and escaping links are refused. No old receipt or SDK
hash was edited. A real-data transformation preflight at `5951d0c9f` passes in
36.7 s, preserves all 16,488 regular-file payloads and metadata, and repairs
only those three links. The export function is byte-identical at the tested
base producer; [source scope](preflight-source-scope.json) binds that reuse.
The final rebuilt SDK is independently accepted by the normal consumer.

The normal FES PID1/factory path requires `/.fes-bootstrap` to be a mount
directory. The earlier intermediate's file placeholder would block that path.
The new base creates a real directory and checks its type in both tar and
ext4, refusing files or symlinks. Managed startup remains disabled; no runtime,
factory bundle, RBF or card is added to the base. This fixes a native boot
prerequisite; it is not evidence of a native boot.

## Actual validation

| Check | Result |
| --- | --- |
| Real corrected Buildroot base | PASS 730.3 s |
| Public `verify-base` entry point | PASS 4.2 s |
| Normal SDK selection/runtime builder plus CPU checks | PASS 41.3 s |
| Isolated SDK/library/QEMU proof, no producer build volume | PASS 10.5 s |
| Full affected checks | Six commands PASS 260.3 s; 1,256 parent tests, two skips, 251.690 s |
| Committed-source consistency | PASS 7.1 s; 40 generated consumers/42 fixture copies |
| Independent source restoration | PASS 1.7 s |
| Artifact/proof retention | PASS 1.0 s |

The [normal-builder proof](runtime-proof.json) calls the existing
`kit_software.build_runtime`, including its `de25_sdk` selector, private SDK
copy/relocation, pinned-container checks and production Makefile. It builds
the real archive and daemon, not a fixture. The archive audit requires all
29 production members, with no duplicates. Actual compile logs retain the
normal `-O2 -std=c++14 -Wall -Wextra -Werror -pthread` flags and active asserts.

Five actual AArch64 executables pass under QEMU Cortex-A76 with the direct
SDK loader and an identity host path namespace:

- `de25_computer_test`: 18 groups using scripted registers, explicitly no
  hardware admission.
- `de25_fpga_manager_test`: fake filesystem/programming lifecycle cases.
- `de25_menu_test`: menu/ownership models; checked successful exit, no stdout.
- `de25_pr_control_test`: identity, freeze/configure/resume, sequencing and
  terminal-failure controls.
- `production_hardware_test`: production construction/refusal controls.

The new runtime's real ELF/library/symbol closure matches the extracted
managed base, with contract
`fes-kit-base-v1-4d08af30f26132bab500672c0bd227576cdaf11e108617966690620b22f494a9`. Its ELF menu-interface section
contains the DE25 scanout, HPS DDR and fixed-720p/menu-display declarations;
these are software declarations, not hardware qualification. The existing
pack/update path still requires authenticated factory metadata and sealed
MENU requirements. No complete factory bundle is produced.

The [rootfs check](rootfs-check.json) verifies 34 selected regular tar/ext4
files plus the real bootstrap directory, real AArch64 ELF/library closure,
six freshly built helper hashes and retained startup policy. The isolated
[SDK proof](sdk-proof.json) has no original `/build` volume, relocates its
compiler/sysroot inside the extracted SDK, compiles and runs C++ against
Imlib2/Bluetooth/zlib, checks BusyBox `aarch64` and receives
`SSH-2.0-dropbear_2020.81` from an ephemeral QEMU Dropbear. It tests no SSH
authentication, kernel/native boot, peripherals, SDM, DDR or HDMI.

| Artifact | SHA-256 |
| --- | --- |
| Rootfs ext4 | `588421ad2ca56d9b4dae256772f46260068d680f0cdb5387faf3e1754206a9aa` |
| Exported SDK | `5a7fa4bf99cfc660bd157d3ffc79b25807cf93eaa49dab894b579c7c52ea30e4` |
| Runtime daemon | `05c25c74d9c53a073625d56c36e04ef63a407c71c823a25e4e50448c6325dffd` |
| Canonical runtime archive | `9d4718937aa52c64826d2be46c39ec219a3c74697ecc533e9bde6c9666fed21f` |
| Matching kernel Image, unchanged producer `59d3b4399` | `28201c24220c9ed11ac5943aa2583f647904d2f694796407d66a7728c1c1456b` |

The earlier matching kernel's 18 required Bluetooth/HID/input selections and
five linked symbols are reused only with its identical Image/configuration.
All 26 kernel C/DTS/profile/build-source files match that producer; the shared
image builder has changed. All 374 canonical MENU, 59 combined-compiler and
58 runtime-test source hashes remain unchanged. Earlier RTL/CPU evidence is
reused only for those input sets; no new Quartus or physical result is implied.

## Retention and remaining work

The first `5951d0c9f` base job was deliberately stopped after 108.1 s when the
bootstrap mount-point mismatch was found. Its original failed record and
separately passing 214.5 s affected check remain preserved. It produced no
successful base receipt. Earlier `e7cacb2a2` evidence remains unchanged and
continues to describe its narrower SDK smoke and file placeholder. The real
export preflight does not fabricate a replacement base receipt for that SDK.

Artifacts, full logs, five CPU test binaries, canonical archive/daemon, SDK,
kernel, source bundle and preserved superseded run are under
`/home/deano/fes/out/artifacts/de25-managed-sdk-integration/cad04bd65/`.
The [retention manifest](retention.json) binds every retained file. The proof
archive is `df208c7db206c5d3ef7f98b58ab48dfe28053ae7cbddc7f6a0e4f6ba9aaad364` (553321 bytes), with each
regular member independently hashed. [Source restoration](source-restoration.json)
replays the preceding publication bundle and checks the exact tested tree.
All completed own processes/groups are absent; no owned container remains.

Next complete normal factory/card assembly from this verified base/runtime,
including the selected sealed MENU and normal factory/software metadata.
Top-level image/media board selection, native PID1/FAT/root switching,
Ethernet/SSH authentication, peripherals, reboot/watchdog, owner ABI3/SID/SMMU/
firewall/coherent DDR and firmware behavior still need their real paths.
MENU/catalog ZX81 launch/stop/return/relaunch, keyboard, LOAD/RUN/SAVE/reload,
persistence, restart/adoption/update/restore and security/whole-call bounds
remain part of the normal-kit objective.

Only the previously approved two HDMI asynchronous-input exceptions apply.
No additional PCB files are available. HDMI output/whole-reset timing, an
eligible independent identical-RBF rebuild and exact-artifact hardware
acceptance remain required. No clock, PCB delay, aperture, SMMU or seal check
is relaxed. No physical action is requested. Future QSPI/boot-configuration
deployment needs explicit approval after a concrete artifact is prepared.
