# #1581 S5: the pc386 sealer gates on a lockstep report

S5 of [`docs/design/rtl-frontend-lanes.md`](../../design/rtl-frontend-lanes.md) makes
`build_fes_pc386_quartus.py` require `--lockstep-report`. `make build-fes-pc386-quartus` requires
`LOCKSTEP_REPORT`. Before Quartus runs, the sealer refuses a report unless all of these hold:

- the run passed (`run.passed`);
- the run was the default suite (`run.default_suite`): every suite argument
  (`SUITE_ARGUMENTS` in `test_fes_i386_lockstep.py`) is at its default, and the run read every
  in-scope file of a full download of the pinned corpus (`run.complete_corpus`, below);
- the run was a `--netlist` run;
- the report's netlist `program_sha256`, `cpu_source_sha256` and Yosys identity (lock, lock sha256,
  router, tool) equal this build's;
- the simulator is the Verilator the i386 lock pins;
- a CPU-top netlist that the sealer regenerates with the same call has the report's
  `netlist_sha256`.

A sealer that accepts the report writes it to the build output as `lockstep-report.json`. It also
records a `lockstep` object in the build summary, holding:

- the report digest;
- the netlist, program and CPU source digests;
- the Yosys tool;
- the simulator.

## Runs

All runs were on Powerboat, on the clean branch tree `329c374cb` (main `7b33b38bf` plus S5). They
ran one at a time under a `flock` queue.

| Run | Command | Result |
| --- | --- | --- |
| lockstep RTL | `test_fes_i386_lockstep.py --jobs 10` | pass, 696 s |
| lockstep netlist | the same with `--netlist` | pass, 1,099 s ([`reports/lockstep-netlist.json`](reports/lockstep-netlist.json)) |
| refuse RTL | `make build-fes-pc386-quartus SEED=1 LOCKSTEP_REPORT=<RTL report>` | exit 1 before Quartus |
| refuse tampered | the same, with the netlist report's `cpu_source_sha256` set to zeros | exit 1 before Quartus |
| refuse S4 | the same, with S4's netlist report, which predates the `run` record | exit 1 before Quartus |
| seal | the same, with the netlist report | pass, 8,467 s |

The netlist report's `run` is `{"passed": true, "default_suite": true}`, with every suite argument
at its default.

### Refusals

```
FES pc386 (Quartus): lockstep report is not a --netlist run
FES pc386 (Quartus): lockstep report's netlist cpu_source_sha256 is '0000000000000000000000000000000000000000000000000000000000000000', this build's is '51903bb2f8b4bf6ed7212942b6127d8337446d01b7c279f549fd7404501db40f'
FES pc386 (Quartus): lockstep report did not pass
```

### Seal

The first attempt hit the job's 2-hour cap during the Quartus fit. That run was not used. Other
sessions' builds held Powerboat's load average near 55 on 24 cores. The rerun used a 5-hour cap and
passed ([`reports/build-summary.json`](reports/build-summary.json),
[`reports/build-inputs.json`](reports/build-inputs.json)):

- BUILD_ID `7a3fd7953ee3fba94adcd42eae225bfb`, seed 1, HIP;
- RBF sha256 `3ce9f8be541f2ed7e31102bd8ee16b16f297d94ad0b85a16b46c03b35611044c`;
- every timing analysis met:

  | Analysis | Worst slack |
  | --- | --- |
  | setup | +0.837 ns |
  | hold | +0.063 ns |
  | recovery | +6.625 ns |
  | removal | +0.266 ns |
  | minimum pulse width | +1.122 ns |

  Clocks: system 61.44 MHz, pixel 74.27 MHz, audio 12.29 MHz;
- 36,937 of 41,910 ALMs.

The summary's `lockstep` object:

| Field | Value |
| --- | --- |
| `report_sha256` | `c8053863…`, the sha256 of the report file given, and of the copied `lockstep-report.json` |
| `netlist_sha256` | `2fa5c771…`, the report's CPU netlist, which the sealer regenerated |
| `netlist_program_sha256` | `23ce90ec…`, the seal recipe's program, as in S4 |
| `cpu_source_closure_sha256` | `51903bb2…` |
| `yosys_tool` | `e240a465`, binary `be9f3be5…` |
| `simulator` | Verilator v5.054, i386 lock, slot `0a4c6c2a…` |

The summary's `netlist.machine_netlist_sha256` (`b5270e03…`) is the whole-machine netlist that
Quartus fit. As the design doc's "Binding to the seal" rule says, the gate never compares against it.

## What this does not cover

- No kit run. S5 changes the producer, not the RTL, and this seal is the gate's acceptance run.
- CI does not run the gate, because it needs Bochs and the corpus (design doc, Yosys lane). The
  unit tests in `tests/test_build_fes_pc386_quartus.py` (`LockstepGateTest`) cover each refusal
  with fixtures.
