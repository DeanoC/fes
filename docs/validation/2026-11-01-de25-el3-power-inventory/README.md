# Nano initial and pre-park EL3 power inventory

The one bounded TRWU8CBL diagnostic passed: all 32 records, both power
snapshots, the original first/fresh reset samples and unconditional park were
captured. The three unused CPU power-status words are zero and the request/
status CPU-channel masks identify only the active core. These are inputs for
qualifying trusted startup parking. The existing native ownership check still
rejects; no native policy changed. See [evidence](evidence.json).

## Frozen source and containment

Tested source: `3ce34748808cd3593067ef6e6928be72c4cfed3b`.
Diagnostic ID: `cee0d4c723b05b09`.

| Artifact | SHA-256 |
| --- | --- |
| Volatile SOF | `0b52d2f163c4a2aeca0390b33ae56491bbac3cee399262e4c8468170318e6e32` |
| SPL hex | `3d379518d186c3e18aecadce5aa41200329289e7ef089ce28d542d6854d7afc5` |
| SPL ELF | `cb55466d36bd4f2c6ef1d43afbf5b68a5da0096f93a7ccad308069cb25c7518b` |
| UART capture | `44edbf76c86230dbab1891a3e82fff8555ea0157a688aa355af5fa719fddecf1` |

The existing 304-byte OCRAM record now holds exactly 32 events: 18 lifecycle
checkpoints, seven fixed power/request/boot words after first C entry and before
the cold sample, and seven immediately before park. A compile-time capacity
check protects that bound. The target helper's disassembly has one aligned
32-bit load per sampling-loop iteration. No power request or reset write is
introduced. Values are retained as observations and never grant admission.
The decoder requires complete ordering, one build identity and one MPIDR/EL;
missing power rows fail completion. Console-control mode has only the later
seven power words and cannot claim a first-C sample.

The seed configuration is retained except for identity. All three startup
objects' text match the seed. Both initializer stops, absent watchdog-start/
firmware-entry symbols and non-sleeping park remain checked. Four focused
C/UBSan and decoding tests plus all six affected checks passed. Actual C tests
exercise the generated park path, sample ordering, before/after value retention,
read-only addresses, UART guards/timeouts and record overflow. The producer
receipt binds prior HEAD plus exact hashes; those bytes were committed as the
tested source before programming. No firmware ABI or native gate changed.

## Physical observation

The attempt ran from `2026-11-01T10:16:31.936758+00:00` to
`2026-11-01T10:16:39.958493+00:00`. Before programming, the original factory boot had
been recovered and authenticated with all four CPUs online after the
[reference hotplug failure](../2026-11-01-de25-cpu-inventory-reference/README.md).
That exact boot/card were rechecked and Linux shut down cleanly.

Exact-cable identity `4362C0DD`, volatile programming and the observed SPL/trace
ID passed. First-C and all later trace records carry `MPIDR=0x81000000`, EL3.
The power snapshots agree:

| Observation | First C, before cold proof | Report complete, before park |
| --- | --- | --- |
| cpu0-pwrstat | `0x90000021` | `0x90000021` |
| cpu1-pwrstat | `0x00000000` | `0x00000000` |
| cpu2-pwrstat | `0x00000000` | `0x00000000` |
| cpu3-pwrstat | `0x00000000` | `0x00000000` |
| mpu-pchctlr | `0x00000023` | `0x00000023` |
| mpu-pchstat | `0x00000023` | `0x00000023` |
| mpu-bootconfig | `0x0070021e` | `0x0070021e` |

The pinned SDK's CPU-channel mask is `0x1e`; both request and status mask to
`0x2` here. The full values also include non-CPU bits, which are not interpreted
as secondary CPU requests. Initial and post-console `CPUINRESET` remain `0xf`,
`CPURSTRELEASE=0`, cause `4`, boot kind `4`; reset timeouts are zero. The initial
native check still reports `valid=0`. The final line and checkpoint confirm
`FES_NANO_CPU_DIAG PARK no-ddr no-storage no-bl31`.

No DDR initialization, SD/FIT load, BL31/native entry, bridge programming or core
launch occurred. No SD/flash write, CPU hotplug, unchanged-image retry, card swap
or physical power cycle occurred in this trace. Bounds were 30 seconds for
query, 90 for programming, 40 for observation and 240 overall. The compiled SPL
was not independently extracted from the SOF; input SHA and observed identity
are the artifact binding used by this diagnostic.

## Contract implications and retained state

The [reference comparison](../2026-11-01-de25-cpu-inventory-reference/README.md)
showed `CPUINRESET=1` with all four CPUs online and unchanged when CPU1 was
logically offlined. The [official boot guide](https://docs.altera.com/r/docs/813762/25.3/hard-processor-system-booting-user-guide-agilextm-3-and-agilextm-5-socs/boot-flow-overview-for-fpga-configuration-first-mode)
describes non-boot CPUs waiting in WFI. A replacement contract must establish
trusted initial parking, selected-primary identity and continued prevention of
secondary release. It must preserve cold-entry, bridge/master, source-chain and
SD drain proofs. Matching this observed tuple alone is insufficient; runtime
CPU lifecycle support and native handoff remain unqualified.

Under the user's exclusive-kit/optional-restore authorization, the diagnostic
stays loaded and parked with the original factory card inserted. A separate
read-only query still reports `4362C0DD`. The host UART/programmer clients are
closed, no UART owner remains and exact top USB permissions are restored. The
continuing claim is held; factory restoration after this trace was unnecessary
and omitted. Other kits, shared JTAG service/clock and harnesses were untouched.

Exact artifacts, transformed sources, compiled startup comparisons, software
results, raw serial/programmer receipts and source bundle are retained with
verified checksums at
`/home/deano/fes-retained/de25-nano-el3-power-inventory-20261101/`.
