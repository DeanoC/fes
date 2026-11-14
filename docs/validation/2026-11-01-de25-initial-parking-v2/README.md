# Nano version-2 initial CPU parking trace

The bounded TRWU8CBL trace passed on implementation `87b2d7f4721c819b554212f2b80c193a07a2531e`:
the first CPU parking check reports `valid=1`, all 32 ordered trace records are
present, and the board unconditionally parks before DDR, storage or BL31. This
qualifies the initial check for the observed core-0 startup. It does not qualify
the later SD drain, secure firmware ownership handoff, native entry or core loading.
See the [evidence](evidence.json).

The private version-2 record adds CPU power-status words, MPU power
request/status and boot selection. It requires the selected executing EL3
primary, a sole CPU channel request/ack, zero untouched spare-controller words,
unreleased spare reset history and no pending CPU release. Cold boot kind,
bridge/master containment, source identity, owner/epoch and SD drain checks
remain mandatory. The executing primary's reset bit is no longer used as a
live-state indication: the [factory comparison](../2026-11-01-de25-cpu-inventory-reference/README.md)
showed it set with all four CPUs online. The
[boot guide](https://docs.altera.com/r/docs/813762/25.3/hard-processor-system-booting-user-guide-agilextm-3-and-agilextm-5-socs/boot-flow-overview-for-fpga-configuration-first-mode)
places non-boot CPUs in WFI. This is a trusted fresh-start parking contract;
zero status is not interpreted as a general power-off code or hotplug support.
The old six-register tuple and version-1 records/cookies cannot grant it.

The matched cold firmware denies CPU_ON in the public PSCI frontend before
framework bookkeeping. The lower platform callback returns the supported
internal-failure result and performs no writes or ownership mutation. A strong
release-lock function beside the frontend is required by the consumer. The
first v11 candidate used a callback result outside TF-A's internal contract;
it was rejected before programming. Final v12 source and linked fallback/lock
instructions are inspected. The frontend was optimized into the SMC dispatcher;
its denial was checked in actual transformed C, rather than claimed as a
standalone linked function. No BL31 or CPU_ON call ran in this board trace.

Four final focused C/UBSan tests cover the produced frontend/fallback, initial
and live CPU state, missing reads, ownership retirement, old records and sealed
SPL reads. The previous parent run passed 1,217 tests (40 skipped) before the
frontend/test delta. That delta has focused C coverage; the four failed/unrun
affected phases then passed on the final code, including launch/return and
failure paths, producer identity, firmware/RTL and 1,571 FPGA tooling tests
(75 skipped). A cumulative tenfoot test deadline was scoped to each catalog
case; its optional completion hook also supports the shared endpoint tests.
The original failed runs remain retained. No full parent rerun is claimed for
the final delta.

| Diagnostic artifact | SHA-256 |
| --- | --- |
| SOF | `00db03271b70c04e9e7a3010b1fe7d6b5e57a8ec8cad37f2a32de93e9ed22f9a` |
| SPL hex | `d90eed681c9b48ffb2e4b41e1ebf68ef0413fb7ec3739c7c6201816b6439f363` |
| UART capture | `113fdec3ca245c24dca3993042ebbe19e20bac8dcf40cff63f2870ea38373c81` |

Diagnostic `e22160f98b1b508a` was built against base
`92bb630ff62461dc155d50eaca1756ff00a32457` plus the exact producer bytes subsequently
committed as the implementation above. Every tracked producer byte was checked
against that commit before programming. The seed configuration, three startup
objects and both stop boundaries remain verified. The input SPL and observed
build identity bind the SOF; independent extraction of the embedded SPL is not
claimed. Query/program/observation/overall bounds were 30/90/40/240 seconds.

The first and pre-park power samples agree: CPU0 `90000021`, CPU1-3 zero,
MPU PCH request/status `23`, boot configuration `0070021e`. Both CPU reset
samples remain `f`, release `0`, cause `4`; the initial boot kind is `4`.
MPIDR is `81000000` at EL3 and reset timeouts are zero. The final park is observed. Original factory
SD remains inserted; no SD/flash write, hotplug, physical power cycle or factory
restore occurred. The exact post-query still reads `4362C0DD`. UART clients
are closed and verified absent; exact USB permissions are restored to
root:root `0664`. Under the user's exclusive-kit authorization, the diagnostic
stays parked and the continuing claim remains held. Other kits, JTAG clock,
shared broker and harnesses were untouched.

The matched v12 native/recovery files are prepared with the recovery prefix
and upload path preserved. Their native boot remains untested. The next
bounded step needs the designated spare card; the factory card stays preserved.
Exact artifacts, producer snapshots, software receipts, failed candidates,
raw captures and complete source bundle are retained at
`/home/deano/fes-retained/de25-nano-initial-parking-v2-20261101/`.
