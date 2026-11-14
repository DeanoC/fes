# DE25 retained Secure reader hardware diagnostic

The bounded format-14 trial on TRWU8CBL passes the first native MENU handoff
after programming-source retirement, its drain and Ready Quit/Park, exact
original-FIT restoration and the complete 51-command baseline ZX81/ZX80
launch/return cycle. Independent raw-wire replay, completed-program witnesses
and owned-process checks pass. The test card remains installed and the last
verified board state is the original native baseline at final Park.

This is diagnostic evidence for the matched artifact. It does not authorize
source-memory reuse, repeatable retirement-path core loads or general appliance
acceptance. No physical monitor observation was collected. QSPI, the factory
card and the bottom kit were untouched.

## Exact artifact and procedure

Compiled firmware source: `ab9e10c00594a60cb8930323aa45e7e51202d568`.
Paired auditor implementation: `539df63a3`. Frozen execution checkout:
`71ec854f9d036130ffe29079fe60d115ea1f8ced`.
Chain `eb154268e7db9d882d3d429090289fc6b654f896e302fd990b3cbc217e9b2146`,
boot `35c6e069de206975`, candidate FIT
`23f2bc37a3b9b81250095f46cfee378624874fa9ce74f1865d0fa3e8b37a9878`.
The [matched build and source-bound reader policy](../2026-11-13-de25-retained-secure-reader/README.md)
remain separate from this actual trial.

Procedure
`9698b0326faf8db63178e9f066b2162d3058c5e82b0f73a47175c517560bea45`
freezes 30,832 files and permits one four-role transaction under the standing
human trial approval. Its fresh file checks include a 61-test controller,
binding/preflight/ownership/recovery tests, eight real admission-boundary checks
and the actual default CLI's two native-closure workers. The rollback role uses
the original baseline's source-bound installer navigation; the candidate uses
the current common paired auditor. The latest post-reset restoration is replayed
from its archived wire before reserving the new attempt.

## Observed result

All 1,708,032 source bytes transfer in two posts and two acknowledgements with
matching SHA-384 and no pending/outstanding work. Transfer takes 11,632,272 us.
Retirement completes in 402,698 us and retained continuation in 594,637 us,
within their original limits. Command consumption is observed at 97,166 us;
mandatory final observations complete at 118,235 us under the enclosing
one-second retirement scope. The 100 ms consumption bound is unchanged.

The separately admitted FPGA-status query succeeds. Both first-MENU reader
records match the sealed route digest: Secure stream 1, `ARUSER=0x3E0`,
`ARPROT=3`, Secure bank, Non-secure abort still set, source retained,
source generation 4 and control generation 5. Native MENU opening completes;
one UART Quit then verifies master stop, owned reader drain and Park generation
1. Independent replay reproduces that same startup/return proof. No core-launch
or configuration-reuse command is issued by the diagnostic.

Fresh recovery then passes, and the refresh/readback restores exact original FIT
`d79a8e7216962dcdb2a72214d8e937a39e4f7039a45600775bf2d6c1319e95f1`.
Both refresh directions verify all 17 expected files before and after their
single selected boot-file write. The original native boot completes all 51
baseline exchanges, USB STOP generation 3 and final Park generation 5, with
independent saved-wire replay. Four distinct transport epochs and their program
witnesses verify. The complete transaction takes 906.524 seconds; no physical
reset is needed.

The independent root audit checks all 22 recorded owned processes, including
checkers in root-only preflight directories. Their original PID/start-time pairs
are stopped. A separate root FD scan finds no UART handle. USB identity, inode
and permissions are restored, and each transport records restored JTAG auto
adjustment 1 / 15 MHz. [Hardware result](hardware-result.json),
[owned-process audit](owned-processes.json) and [retention](retention.json)
record these checks.

## Software and retention

The [complete selected software results](software-result.json) pass on the
unchanged execution checkout: 2,687 parent tests (15 skips), native runtime and
producer integrations, DE25 Avalon integration, firmware/identity/drain RTL and
2,181 FPGA software tests (88 skips). The first invocation stops at the 30 GiB
disk gate; after scoped cleanup, only the four uncompleted phases are resumed.
The eight passing earlier phases remain tied to their original terminal record.
The separate 233-test focused run has zero skips. No synthesis, fitting or timing
claim follows from these software checks.

The actual trial archive is independently read back at
`/home/deano/fes-retained/de25-secure-reader-physical-20261114-r1/physical-trial.tar.gz`,
SHA-256 `fb071148e3927472741b7f791bdf59836169607a1be3df30e41bbfa3b5dc66fd`.
Its receipt names the matched image and prepared-source dependency archives;
all are required to reproduce the complete closure. Raw native/recovery wire,
program logs, refresh receipts, approvals, independent collectors and software
terminal results are retained. [Cleanup](cleanup-summary.json) removes only
byte-identical obsolete vendor-source copies after canonical archive/hash/mode
readback; current candidate and all final boot/rollback images remain intact.

Next is generation-bound source release and SDM/SMMU rearm, with ownership,
late-response and failure tests, then repeated real menu/core/return cycles.
The retained arena and paired auditor currently reject a second configuration
before overwrite or transport. Removing those guards alone is not that repair.
PR #1791 remains `lane:deano`; no merge or Lander handoff is requested.
