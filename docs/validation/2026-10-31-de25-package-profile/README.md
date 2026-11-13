# DE25 explicit package/profile and complete runtime archive, 2026-10-31

The metadata checker now requires an explicit NativePlatform selection.
DE10 remains the default; package metadata cannot select a hardware backend.
The shared DE25 registry declares only de25-hps-first-v1 with computer major1
on the board definition's SCS part. One generic profile-pair checker consumes
the generated platform rows. Platform-specific C++ row types allow both real
registries in one translation unit without duplicate type definitions.

DE25 preflight and live Identify share one contract validator: format2,
computer ABI1.0, a nonzero128-bit build and exactly required video/HID/tape1.0.
The one CoreDriverRegistry binds that profile only with an explicitly selected
DE25 platform and supplied driver. Mixed platforms/devices/profiles, contained
development, extra DDR interfaces and unavailable drivers fail before control.

This is metadata/driver binding, not production construction. NativeHardware
and the Linux factory still select DE10. DE25 startup/video/idle/restart adoption,
tape SAVE, MENU/agent/catalog and normal launch/stop/relaunch remain unfinished.
No new package was installed or physically activated.

## Producers and checks

- Profile/runtime producer: 90156cae51d298bdcee886c315733192998bae69.
  Native package/driver tests and Go pack/emitter tests pass. Generation
  plans460 consumers and updates2; a second generation changes0. make check
  passes5.5s. Tests include real DE10+DE25 C++14 co-inclusion and binding the
  DE25 device to the real board definition.
- Validation/environment repair: e0b2f28c37ed2558ffea646629427900e5e20ab0.
  The original34-command run fails after837.3s on one stale expected validation
  source list: 1279 parent cases, 1236 pass,42 skip,1 failure. The list is
  corrected without weakening validation; all11 consistency tests and4 runtime
  script tests pass. The29 previously unstarted commands run separately on e0;
  their result is pending and is not counted here as passing.
- Complete archive producer: 769bbeda6f57f68f1149465a6b88e38db4e55406.
  The existing JSON parser is built once into the library, satisfying the
  programmer dependency and the AArch production-hardware test. The daemon
  uses that same archive object. Exact archive28 membership, source whitelist
  and raw-I/O ownership explicitly include the parser and DE25 objects;
  a missing-json negative fixture keeps the guard strict.
- Native all/archive-audit/production-hardware/active-tree/support-truth
  checks pass at769 in126.1s.
- The standard scripts/de25_runtime.py recipe passes at769 in135.4s:
  full dynamic runtime/archive, archive-audit, agent build, all9 selected C++
  test binaries under cortex-a53 QEMU, maintenance-contract CLI and all5
  selected agent tests. runtime.json/checks.json record exact hashes and SDK
  identity. Classification remains userspace-only, hardware_acceptance=not-run.

The library source/include, FPGA, FogCast and shared-definition bytes are
unchanged between e0 and769; the archive repair changes build composition,
audits and the selected diagnostic tests. Earlier results remain source-bound;
this record does not claim one all34-command pass on the final producer.

## Retained failures and environment correction

The initial private AArch package test reaches the existing trusted-root /tmp
test and fails when QEMU uses the full SDK sysroot. That sysroot contains /tmp,
so prefix lookup redirects the directory while a newly created child path
falls back to the host filesystem. A library-only prefix preserves that
namespace. The first prefix attempt lacked lib64 aliases and fails loading
libstdc++; including the actual SDK aliases fixes it. The complete private
package/programmer/computer QEMU proof then passes29.4s at901.

The standard e0 AArch recipe then catches a real missing link dependency:
the production-hardware test links all library objects, and the AArch DE25
programmer references the JSON parser that was outside the archive.
The native build had optimized out that architecture-specific reference.
The repair puts the existing parser in the archive, rather than bypassing
the test. Separate archive-audit expectations were also stale (25 versus27
members and omitted programmer ownership); they now require the exact28
members and explicit owners. Guards remain enabled.

The standard recipe now uses the tested library-only prefix, including
lib64 aliases, and includes core-package and DE25 programmer tests.
All failed receipts are retained alongside the passing controls.

## Hardware and remaining goal

There is no kit, UART, HDMI, FPGA, reboot, card or flash operation for this
increment. Earlier physical programming and CPU-only diagnostic proofs retain
their own source/artifact identities. The bottom-board candidate claim and
recovery copies remain retained; there is no baseline promotion.

Startup needs platform-specific wiring in the existing single lifecycle and
factory. DE25 HDMI initialization is already RTL-owned; its READY output is
currently unobserved and must not be replaced by the DE10 Linux I2C recipe.
Physical DMA/DDR/SID/SMMU/firewall, global reset/drain and failed-transfer
qualification, secure-call/shared-mutex bounds and timing #1284 remain open.

Complete artifacts are independently retained at
/home/deano/fes/out/artifacts/de25-package-profile/769bbeda6/.
Original failed controls are retained in the sibling90156cae5 directory.
