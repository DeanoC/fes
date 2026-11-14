# Contained CCU decode logging before native boot

The Linux-free Nano still stops during the vendor firmware's NOBITS cache
maintenance, before native startup. The previous physical trace captured an
asynchronous SError while both architectural RAS records and the CCU detection
controls were zero. That does not identify the originating transaction or
establish that the memory is healthy. This milestone implements and validates
a contained diagnostic to collect better evidence; it does not fix or qualify
ordinary core loading.

Implementation `b95d8d6261e3e6128a51b0d74a225fa7d4eef8a6` enables only
`DSU.CAIUUEDR.DecErrDetEn` (bit3 at `0x1c000100`) around the original NOBITS
`inv_dcache_range` call. The retained Altera CS/R1 revB0 RC28 map identifies
this RW field separately from memory/ECC detection and interrupt enable.
The diagnostic requires the reviewed A55 r2p0, primary affinity, EL3/MMU/cache
state, fresh CPU/master reset and power containment, and zero detection,
interrupt and status baselines. It writes8, checks the readback, and later
writes0 with another readback. Interrupt control stays zero; error status is
never acknowledged and must match across restoration.

The stack-free helper preserves arguments and retained registers. Its lifetime
register is initialized before installing private exception vectors. The actual
linked CPU-ops table, reset-time errata entries, early callees and original
56-byte cache leaf were checked for lifetime-register preservation. On a fault,
the existing handler captures the original exception, 43 fixed observations and
the qualified RAS inventory before attempting guarded restoration. An exception
during restoration cannot start it again. A failed or missing readback leaves
restoration unproven. The decoder retains these outcomes but always rejects boot
following an exception, including one with successfully restored logging.

Validation passed:

- 66 ARM logging cases cover normal/status-preserving restoration, CPU and
  ownership rejection, every MMIO read site, write faults and ignored writes.
- Five ARM cases connect the actual full exception handler and fixed/RAS-skip
  inventories to cleanup, including retained status and interrupted restoration.
- 79 ARM vector, containment, UART-bound and fixed-inventory cases also pass.
  CPU identity/MMIO windows are injected; these 150 cases are off-board evidence.
- All ten affected software steps pass: 1,540 selected parent tests with two
  environment skips, 1,581 FPGA-tooling tests with 75 tool/fixture skips, generated
  consumers and native C/RTL/producer integration. The shared ROM linker runs
  with Go1.26.5. A subsequently added fatal-cleanup decoder regression also passes.
- The actual rebuilt BL31, SPL/U-Boot FIT and HPS-first file composition pass
  source, instruction, allocation and final-branch audits. Composition held the
  shared Quartus lock. The 1,428-byte early guard and vendor cache operations
  remain intact; no status, interrupt or ECC-control writes were introduced.

The matched FIT is 8,183,808 bytes. Its chain is
`df4c0edbc8eddf6bdbcd26cb904fb2eac44ef7519b3134fa96636d0a133dd9fc`.
The native application/entry from `31d716957759c3dede3330877dac2ce7bcd2d643`
remain byte-identical, with all 2,098 selected input hashes rechecked. The sealed
FPGA core payload is unchanged. Exact inputs and checks are in [result.json](result.json).

Failed preparation evidence is retained: a duplicate fixture label, an unaligned
MMU-off fixture access, a missing fixture include path, the production CPP
include-order/link failure, two audit-script assumptions, and an SDK-only input
count assertion. Corrections sent no board commands or widened any limits.
The include-order failure has a permanent preprocessing regression test.

No hardware attempt, programming, upload or SD write occurred in this milestone.
Deano reported an accidental power interruption during the host work. USB serial
`TRWU8CBL` is visible again, but boot/card/CPU/FPGA state is unverified after that
interruption. Previous parked-state observations remain historical. The exclusive
claim and spare card are retained; the factory card and other board were untouched.

Evidence is retained with verified member readback and source-pack import at
`/home/deano/fes-retained/de25-decode-log-20261106-r1/`, using the preceding
`de25-ras-banks-stop-20261106-r1` source/evidence prerequisite.

Next, freeze and fixture-check a bounded operator for this new chain. Its passive
fault drain must capture the decode restoration outcome after RAS END/SKIP while
preserving all current byte/time/poll limits and stopping active commands at the
first failure. Fresh recovery/boot/card/reset admission is required after the
power interruption. Only then run one bounded test on TRWU8CBL. ROM16-byte,
400-million-tick and 64-poll bounds remain unchanged; neither this build nor a
successful diagnostic qualifies general ROM/package admission or DE10-equivalent
core loading.
