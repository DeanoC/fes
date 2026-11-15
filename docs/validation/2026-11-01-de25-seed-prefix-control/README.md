# Nano seed-configuration console control

The bounded TRWU8CBL comparison passed as a **contained hardware diagnostic**.
The exact SPL banner, all three post-console checkpoints, six bracketed register
reads and unconditional park were observed. The CPU reported EL3 and
`MPIDR=0x81000000`. This establishes execution of this SPL through console and
park; it does not provide an initial trusted ownership proof or native/core-load
acceptance. See [machine-readable evidence](evidence.json).

The later [initial trace](../2026-11-01-de25-initial-trace/README.md) restores
the early sample and all 18 checkpoints successfully with the preserved seed
configuration. It replaced this control; initial native admission still rejects.

## Artifact and containment

Frozen source: `a7b045916e969390b8eb73e940c337ff5dd67ee2`.
Diagnostic ID: `eeff478768a18617`.

| Artifact | SHA-256 |
| --- | --- |
| Volatile SOF | `f9747d52dc2b2b35496c37587b361233f14d5f098653640c70cc7051721af899` |
| SPL hex | `c46f1cc87e56b75ae18ede0120fc199527f6e1d73c466fa04bf62579ab92e39a` |
| SPL ELF | `473b5d0e117bc1a125e479e5a61874d8a616e85cdd7771fe19af951ffa7a404f` |
| UART capture | `27dd0ec6dbb44beaaa0422640570904f3101ee150c5286feaf9f1df56168e63f` |

The [previous console control](../2026-11-01-de25-console-control/README.md)
omitted cold proof but still stripped drivers and traced before console setup.
This changed control retains the seed configuration exactly except for its
build identity, with no pre-console proof or trace. Its startup source and the
three compiled startup objects' text match the seed. Both watchdog-start
paths are excluded. The first initializer stops after the diagnostic report;
the later SPL initializer independently stops on entry, and firmware invocation
is also removed. Drivers may remain linked, but these initializers cannot
continue into DDR probing, SD loading or payload entry. The actual linked
watchdog-start, cold-proof and handoff/ATF-entry symbols are absent.

The first preparation exposed a later `initr_watchdog` reference and failed
before SOF construction or hardware programming. That failed generated source,
ELF and logs remain in `out/nano-seed-prefix-prep-20261101/candidate-v1`.
The corrected `candidate-v2` passes the artifact guards. Four focused tests,
including 24 compiled C cases and the independent late-entry stop, and all
six affected checks pass for the implementation. The build receipt binds
previous HEAD plus exact input hashes; those bytes were committed as the
frozen source above before programming.

## Physical observation

The single changed-image attempt ran from `2026-11-01T09:03:15.384659+00:00` to
`2026-11-01T09:03:23.106620+00:00`. The exact top cable reported `4362C0DD` and volatile
programming succeeded. The observed scope is **after console initialization**;
checkpoints `10`, `11`, `12` mean console done, report begin and park. They carry
the same build ID, MPIDR and EL. Register reads completed in this order:

| Observation | Value |
| --- | --- |
| MPIDR / EL | `0x81000000` / `3` |
| Reset cause | `0x00000004` |
| Peripheral reset 0 | `0xff7fbfff` |
| Bridge reset | `0x0000004f` |
| CPU reset | `0x0000000f` |
| CPU release | `0x00000000` |
| SDM boot kind | `4` |

The final line was `FES_NANO_CPU_DIAG PARK no-ddr no-storage no-bl31`.
No initial cold proof, DDR calibration, SD/FIT load, BL31, native command or
core launch occurred. There were no SD/flash writes, no unchanged-image retry,
card swap, reference-image restore or physical power cycle. Bounds remained
30 seconds for query, 90 for programming, 40 for observation and 240 overall.

These changes restored output but changed both driver configuration and
pre-console instrumentation; the comparison does not isolate a single root
cause. The next trace should retain this seed configuration and stops while
restoring the initial observation path. The CPU register still reports all
four bits set despite the observed active CPU. Its semantics and an independent
secondary-core inventory need qualification; later samples do not replace a
failed first proof or justify relaxing native admission.

## Retained board and evidence

Under the user's exclusive-kit authorization, this diagnostic stays loaded
and parked with the original factory card inserted. The continuing claim is
held. A subsequent read-only exact-cable query still reports `4362C0DD`.
UART/programmer clients are closed, there is no UART owner and the exact top
USB permissions are restored. The shared JTAG broker/clock, other kits and
harnesses were untouched. Factory boot was not restored or freshly verified.
The prior authenticated baseline/clean shutdown remain historical evidence.

Source bundle, exact SOF/SPL/ELF, configuration, transformed sources, startup
comparisons, software results and raw live receipts are retained with verified
checksums under
`/home/deano/fes-retained/de25-nano-seed-prefix-control-20261101/`.
