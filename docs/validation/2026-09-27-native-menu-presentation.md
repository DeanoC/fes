# Native menu presentation software qualification — 2026-09-27

Scope: shared menu-display contract, described GP/DDR firmware, runtime-owned
reserved mapping and sealed staging, idle lifecycle generations, existing Unix
socket descriptor transport and diagnostic pattern client. No FogCast renderer,
factory selection or media provisioning changes.

Implementation source is `8907788dde541788728778fdc5d2f2d2591b280b`, based on
main `b94eb11fc9d6604a5979b484b8910bdb2ce4ee43`. The sealed FPGA rebuild is recorded
in [package evidence](2026-09-27-native-menu-package.md).

## Verification

The full runtime `make test archive-audit` passes, including 50 lifecycle,
30 native hardware, 25 protocol and 36 daemon cases, real socketpair descriptor
cleanup and the actual three-frame pattern client. Tests cover stale generation,
immutable file identity/seals/size, split metadata, multiple/truncated FDs,
five-second preparation timeout and disconnect after acceptance. Incremental
build, version, exact archive membership, raw-I/O and support-truth checks pass.
Parent `make check` and generated consistency pass (15 consumers, 32 fixtures).
Parent Python regressions pass 586 tests with 39 explicit skips.
The final affected-component pipeline passes all 34 selected commands from
source `8907788d`: parent, FogCast host/appliance/UI, shared linker, runtime,
contracts, FPGA producer tests and Apple II, Coleco, demo, menu, Pong,
SG-1000, SMS and ZX81 simulations. Local logs and the structured receipt
are retained under `out/validation/native-menu-presentation/`.

The retained Buildroot 2021.02.4 GCC 9.4.0 toolchain cross-builds the production
ARM daemon and diagnostic client with archive audit. The clean-source rebuild
explicitly sets runtime version `git-8907788dde54`; container metadata did not
expose the worktree git directory. Artifacts are retained in the worktree under `out/target-menu-runtime/`,
outside the canonical host-test build directory. They have not run on the kit:

- Daemon SHA-256: `fe5d1f6d00371066abf96036b15cbb1205d959efc4ebff2c23a2b2c90441bf56`.
- Pattern client SHA-256: `b855dfd3041d42c1436742dca1ce52b9f2bec59ac0fb21a1cdb529d06613500b`.

Independent whole-branch review found one important activation timing defect.
Its delayed-enable regression failed before the fix; all nine menu simulations
pass after safe blank-row arming. No other blocking findings were reported.

## Physical precondition check

Read-only inspection of the designated kit found kernel `5.15.1-MiSTer`,
`mem=511M memmap=513M$511M`, and actual System RAM `0x00000000–0x1fefffff`.
The complete `0x30000000–0x3fffffff` FPGA window is therefore excluded.
The installed runtime hash is
`d5776191048b112f852443db260962760cd14cce106acd2c068894cac6c07235`.
Its root loop image is
`0103b5f04cbe256ed84e97d589ec5cf108b1031e415820bd28c8bd3cb16b1a4d`.

FAT `/idle.rbf` and `/menu.rbf` both match the selected DDR splash
`43dc7e9db350dbdef87b290bfde61f8df37483957c93e81cbd753c30d4cd93b6`.
The [earlier DDR diagnostic](2026-09-27-hps-ddr-kit-diagnostic.md#fes-u-boot-loading-fat-idlerbf)
records the derived U-Boot installation and an explicitly seeded diagnostic
latch record; that record was removed during its cleanup. It explicitly leaves
first-runtime boot capture and cold power-cycle qualification untested.

`/run/mister-runtime-boot-hps-ddr` is absent. The boot-latched SDR layout
cannot be established from later configuration mirrors after FPGA activity.
No layout record was fabricated, no FPGA was programmed, and no services,
image, boot files or card were changed. The kit lease remained free.

Classification: host software, ARM compilation and sealed FPGA timing only.
Ten-minute underflow-free HDMI/DDR operation, CPU/latency measurements and
menu/game/Stop handoff remain untested. Physical diagnostics need the updated runtime installed before the first
FPGA program of a qualified DDR boot, with its startup latch record intact. Network runtime replacement alone
does not qualify U-Boot's latched layout; see [DDR boot requirements](../hps-ddr.md).
