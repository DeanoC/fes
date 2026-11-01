# DE25 managed Linux base and SDK preparation

Tested base producer: `e7cacb2a2f9d2558f712d69c62b2a2cafa1c52a6`. Kernel producer:
`59d3b43997b5827dda55125f0f651248a51e6ebf`. This is a real Buildroot
`managed-base-intermediate`, with source-bound files and an isolated developer
SDK proof. It is not a complete factory/card image or native board acceptance.
No UART, SSH, capture, allocation, FPGA, SD, installed software/kernel, QSPI or
power access/change was performed. The bottom claim remains retained; the top
board is untouched. Last authenticated native health remains 14:48 UTC on
2026-11-01 with owner ABI2. No ABI3 client was run on that kernel.

## Result

The existing DE25 image builder gains `managed-base` and `verify-base` actions.
It uses the pinned Buildroot source and authenticated native-image builder,
with the owner kernel's locked AArch64 SDK. The shared native configuration
supplies merged `/usr`, read-only root policy, all fourteen required library
paths, BlueZ/D-Bus, serial recovery, network/SSH scripts and Bluetooth firmware.
The existing Go producers build actual AArch64 agent/UI, storage, supervisor and
Bluetooth helpers. Their hashes are checked against the installed ELF files.

The owner kernel now requires 18 USB Bluetooth/HID/input selections built in,
including classic/LE Bluetooth, BTUSB, UHID and uinput. Five newly checked linked
symbols are present. No vendor or DE10 modules are reused. Kernel config,
Image/DTB, notes and the permanent-owner receipt remain separately bound.
The kernel C/DTS/profile/build-source files (26 files) match their kernel
producer; the shared image builder has changed, so this is not a claim that
every kernel-build input file is unchanged.

The base keeps `/.fes-bootstrap`, normal runtime admission closed and managed
software startup disabled. It contains no runtime, factory bundle, RBF or card
image. Factory assembly still needs the selected real runtime, sealed DE25 MENU
and existing bundle/base/package admission. The top-level image/media flow and
the kit-software SDK selector still need wiring to this managed-base receipt.

## Validation

| Check | Actual result |
| --- | --- |
| Matching owner kernel | PASS 690.7 s; required config and symbols checked |
| Real managed Buildroot base | PASS 858.2 s |
| Public `verify-base` command | PASS 4.2 s |
| Isolated SDK and QEMU userspace | PASS 13.8 s |
| Full affected checks | Six commands PASS 251.2 s; 1,251 parent tests, two skips, 239.624 s |
| Committed-source consistency | PASS 8.2 s; 40 generated consumers and 42 fixture copies |
| Source bundle restoration | PASS 3.6 s; exact tested tree |
| Artifact/proof retention | PASS 1.8 s after the self-audit repair described below |

The [actual rootfs check](rootfs-check.json) verifies 35 selected tar/ext4 files,
real ELF/library closure, six fresh executable hashes, serial policy,
bootstrap guard, missing factory/core/modules/SSH keys and required config.
Its base contract is `fes-kit-base-v1-4d08af30f26132bab500672c0bd227576cdaf11e108617966690620b22f494a9`.

The [isolated SDK proof](sdk-proof.json) mounts no original build volume and
requires `/build` to be absent. After relocation the compiler sysroot must stay
inside the extracted SDK. It compiles C++ against Imlib2, Bluetooth and zlib,
checks the real library/symbol closure and runs the result under AArch64 QEMU:
`DE25_MANAGED_SDK_PASS`. BusyBox reports `aarch64`; an ephemeral Dropbear server
returns `SSH-2.0-dropbear_2020.81`. SSH authentication, kernel/native boot and real
peripheral operation are not tested. The exact [proof script](sdk-smoke-complete.py)
and [executed command](sdk-job.json) are retained.

| Artifact | SHA-256 |
| --- | --- |
| Rootfs ext4, 256 MiB | `82b14d8cd6f974f46ae27ec17721a7e838cb1b0f1bca5b384de1daa9b1be2537` |
| Rootfs tar | `36b74e0229689ffc7022fa8a5d8b0eaff1aa70a116d3196a520f71dfac18f41d` |
| Exported SDK | `d632a493aa5f51bb4c3e218ace4bb3e609ab26adda3776871c7e9685d64d6209` |
| Kernel Image | `28201c24220c9ed11ac5943aa2583f647904d2f694796407d66a7728c1c1456b` |
| Kernel configuration | `2395baf1c813f4b2c18f5069f8df32bf4ffc38914b9e48fcf68ef5d82a15d467` |

The input compiler SDK is separately pinned as
`7912076c08d93c8d8c28c0ef81bd406abf6b1acb9e01ccac37d44307e39759a7`; it differs from the newly exported SDK.
The base recipe is `f8e9ae0f2aa4934dadd44cd6bbbf7ca83b4093e8f62ededefe9db8e54525f3b9`.
[Base receipt](base.json), [kernel receipt](kernel-build.json),
[kernel audit](kernel-audit.json), [affected report](affected.json),
[all checks](checks.json) and [source scope](source-scope.json) retain exact
producers and limits. All 374 canonical MENU, 59 combined-compiler and 58
runtime-test input hashes remain unchanged. Earlier RTL/CPU proofs are reused
only for those unchanged inputs; no new Quartus or physical acceptance is claimed.

## Preserved failures

The first base at `f4bf39806` compiled its packages but failed final assembly
in 827.0 s: Buildroot's Python 3.9.6 shadowed the builder's Python and lacked
`tomllib`. The hook now explicitly uses `/usr/bin/python3`; the regression
executes it with an incompatible Python ahead in `PATH`. Constructed-wrapper
and source inspection also found compiler links outside exported `HOST_DIR`.
The corrected recipe places the locked compiler under `HOST_DIR/opt`, so
Buildroot emits relative links and wrappers. The isolated SDK proof above
validates the resulting export. No successful first-base receipt was produced.

The first isolated SDK probe failed in 14.5 s because its C++ source omitted
`stddef.h` before headless Imlib2. Only the probe changed; the SDK/rootfs bytes
are identical for the passing run. The original script, command and failed log
are retained. A source-restore attempt omitted the previous publication bundle
(0.7 s failure); explicit authenticated prerequisite replay completed in 3.1 s.
The first retention audit counted its own live processes (0.9 s failure); the
corrected audit excludes only itself and preserves that failed record. A
superseded kernel attempt was stopped after incorrect UHID/uinput linkage names
were noticed; the corrected kernel build above is separate. None of these are
native-board failures, retries or rewritten green records.

## Retention and open work

Artifacts, original failures, commands, full logs, kernel, SDK and source bundle
are under `/home/deano/fes/out/artifacts/de25-managed-base/e7cacb2a2/`.
The prerequisite failed-source archive is under sibling `f4bf39806/`.
The [retention manifest](retention.json) binds files and restored source;
[source restoration](source-restoration.json) records prerequisite hashes.
The independently verified proof archive is `52b1114db1cf81c5c81261972870ee617f4b58ac74ffd54dd9ef4b642bff172b`
(919854 bytes). Completed own processes/groups and containers
are absent. These checks do not promote, restore or release a kit baseline.

Next connect this real base/SDK to normal kit-software and complete managed
factory/card assembly. Keep the sealed-MENU gate. Native PID1/FAT/root switch,
Ethernet/SSH authentication, peripherals, reboot/watchdog, owner ABI3/SID/SMMU/
firewall/coherent DDR and firmware behavior remain unqualified. Normal
MENU/catalog ZX81 launch/stop/return/relaunch, keyboard, LOAD/RUN/SAVE/reload,
persistence, restart/adoption/update/restore and whole-call/security bounds
still need their real hardware paths.

Only the previously approved two HDMI asynchronous-input exceptions apply.
No additional PCB files are available. HDMI output and whole-reset timing,
independent eligible identical-RBF rebuild and exact-artifact hardware acceptance
remain required. No PCB delay, clock or aperture is invented or relaxed.
No physical action is requested by this record. Future QSPI/boot-configuration
deployment needs new explicit approval after a concrete artifact is prepared.
