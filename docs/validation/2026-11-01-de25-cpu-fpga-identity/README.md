# DE25 CPU FPGA identity diagnostic: stopped, 2026-11-01

The first bounded TRWU8CBL attempt stopped after the secure control-only
opening. The FPGA identity read did not complete within the 45-second native
observation window. No endpoint identity or control reclose is accepted.
Native artifacts were built on `fe58a6dc9a2348f9df4b65bd0cc185874136c213`;
software and hardware validation used `1f886dce4efcc2ebd411ba581ef8a93a6288636b`. The only later source change
is the host SD wire test's service dependency and lease checks; preflight
rechecks all compiled source digests and that exact test-only difference.

## What passed and where it stopped

The qualified recovery loader booted. Ethernet downloaded the 5,234,772-byte
FIT, checked its RAM hash, overwrote only `FES/UPLOAD.BIN`, and independently
read it back. Protected boot files and the recovery manifest were unchanged.
The matched HPS-first/native loader verified the FIT before parsing, brought
up 1 GiB DDR and reached its manual prompt. Exactly one `fesboot` passed both
closed owner observations, EL2 cleanup, EL1 entry and the secure source-context
digest check.

Firmware reported `phase=open`, generation 1, reset mask `f`; then
`phase=read`, generation 2, reset mask `d`, base `20010400`. These are the last
accepted checkpoints. That establishes the managed control-only transition.
The read marker precedes ownership validation and endpoint MMIO; it does not
prove which individual register access was reached. No `close`, `closed`,
identity response, error-return cleanup or final Snapshot was observed. The
host stopped and retained the first trace; no retry or reset followed.

The outer cleanup restored and read back the original 15 MHz cable clock and
exact USB permissions. An independent privileged scan verified the UART was
free. The stopped diagnostic, spare card and exclusive claim remain retained;
the original factory card stays safe outside the board. Control reclose and
the final internal CPU state remain unproven.

| Identity | Value |
| --- | --- |
| Native boot | `e50bd4671ed2c929` |
| Package | `6a730fa546d4d33ff94ab403881adb98803a51ca40212f88cacb60787de00b63` |
| FPGA build | `e25bb65a1d14a2bbbddf4c770ea033e5` |
| HPS source context | `94da3557f175eae05144d70937c3afceda20c6c9f9ca9d25155535da3e0814fc` |
| Compiled control map | `bc13d4774a2f80683d42d70e8440d487c20b232da4281d5a1706c337ba176796` |
| Composed SOF | `60305d3539c089a0364edbd4c2debe8a76365764c12c4314d0265cec59de81b0` |
| Native FIT | `a8cce5cb8e19b7a0a32fe5fb9a6660d9e836bd6c6a21fe993309af497f0cf0a9` |
| Unchanged core | `415cc2d68955755d2de406b233e34fa2cd6cce755089ba842f10f62e6945320c` |

## Software evidence

All 12 exact affected commands passed on the validation revision, with
independent phase bounds. Parent regressions, generated consumers, native SD
lease/PIO, native firmware/RTL launch-return, producer identity, shared
contracts and FPGA regressions are recorded in [software.json](software.json).
The parent suite passed 1,244 tests. The actual sealed package also passed
simulated CPU/TF-A/RTL and tenfoot lifecycle checks; the production runtime
oracle still reports `unsupported_target`. The native command and wrapper
passed 24 and 21 injected cases respectively.

The first broad run exposed the stale generated-consumer count of 33 after
the added firmware C header raised it to 34. A later SD wire test lacked the
new service object. Both failures are retained; the final plan passes with
the count corrected and real service/SD lease tests linked. An unprivileged
physical preflight could not inspect protected process descriptors; it made
no board change. The privileged preflight verified the full UART ownership
gate before this attempt. Earlier developer fixture/CLI setup failures are
also retained, not accepted as passing evidence.

## Evidence and next diagnostic

[Native serial](native-console.log), [native receipt](native.json),
[stopped-state audit](hardware-stopped.json), [outer cleanup](experiment.json),
[upload/readback](upload.json), [control map](hps-control.json) and
[file hashes](files.json) preserve the exact boundaries. Matched builds and
incremental native/qualification source bundles are independently verified at
`/home/deano/fes-retained/de25-cpu-fpga-20261101`; see [build retention](build-retention.json)
and its predecessor's [restoration chain](../2026-11-01-de25-hps-context/README.md).

Next distinguish a pre-MMIO ownership stall from an endpoint transaction
stall, inspect the actual generated control clock/reset route, and compare a
bounded FPGA-side identity read against the stopped CPU path before preparing
another native boot. Do not replay this consumed attempt or infer a safe
reclose from the host timeout. A CPU abort or hung MMIO/SMC can prevent the C
error-return cleanup from running.

Generic cold bridge mutation and configuration remain denied. Only the
dedicated source-bound diagnostic selects the one-shot probe; ordinary images
have no probe provider. There is no runtime core admission, active fabric or
device drain acceptance, live HPS I/O measurement, runtime programming, native
SD coexistence, USB/network resume or HDMI qualification. `HPSContextVerifier`
does not implement `BootHandoffVerifier`.


## Follow-up inspection before another boot

A separate read-only top-device inventory sees the exact TRWU8CBL JTAG target,
but no master, jtag_debug or SLD services. System Console refuses to link the
matched image metadata because the visible SLD agents differ. No master was
claimed, no endpoint register was accessed, no clock changed and no reset or
programming occurred during this inventory. The exact USB permissions were
restored. [Service inventory](jtag-service-inventory.log) and
[link rejection](jtag-link-mismatch.log) retain the observations.

The [actual generated reset route](reset-route-inspection.json) derives control
reset from configuration/PLL reset and includes the correct inversion into the
HPS IP's active-high lightweight reset. This is structural inspection, not a
measurement of the running clock or reset. No polarity repair is inferred.

The missing services are consistent with fabric not yet configured. The
[Agilex 5 HPS-first boot guide](https://docs.altera.com/r/docs/813762/25.3/hard-processor-system-booting-user-guide-agilextm-3-and-agilextm-5-socs/hps-boots-first)
describes a separate core/fabric configuration step after HPS boot. That is a
working diagnosis, not a measured SDM state. The next candidate queries SDM
configuration status before any control opening, rejects incomplete/unready
status while closed, and adds individual access markers if fabric is ready.
It sends no configuration command and grants no runtime programming permission.
