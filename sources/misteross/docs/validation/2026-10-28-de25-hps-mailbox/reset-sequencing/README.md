# DE25-Nano reset and clock cleanup: host validation

Producer `4ab2472b0b22b64d8756fd7c6e83d117e794b214` generated IP with zero
errors/118 warnings and compiled with zero errors/308 warnings using Quartus
Prime Pro 26.3.0 Build 114. This is host-only compiler/simulation evidence.
No board, FPGA, QSPI or block device was touched.

The diagnostic SOF is retained at
`out/de25-hps-reset-4ab2472b0b/project/output_files/golden_top.sof` in the DE25
bring-up worktree. SHA-256:
`2d93db15c2a525a8416916fab8f7018f44488be27a2aaf7507324cdf7e8c663c`.
[Inputs](inputs.json), [build receipt](build.json), [handoff](generation.json)
and [full report hashes](full-report-sha256.txt) identify the result.

## Change and source preservation

The board-local two-register reset asserts asynchronously during configuration
or PLL unlock and releases after two 100 MHz edges. Its output feeds the SoC
reset input, debounce and heartbeat. Generated subsystem controllers remain in
place and perform their existing domain-specific release. The PLL reset remains
`ninit_done`, avoiding a lock/reset dependency loop. Both registers are preserved
without forcing each vector member to become a separate synchronizer head.

The redundant 50 MHz `MAIN_CLOCK` declaration is removed; generated PLL SDC
still owns that clock. Exactly two synchronizer clear pins and four verified
human-visible LED ports receive narrowly scoped timing exceptions. Stage data
and released functional reset paths remain timed. The original 166.6666 MHz
EMIF clock frequency is unchanged.

[Postcompile preservation](postcompile-preservation.json) confirms all copied
FES bytes match the producer commit, original HPS/EMIF parameters are unchanged
and original vendor boot files are identical. All 23 generated RTL files bound
by the [earlier protocol audit](reset-domain-review.md) also remain identical.
This establishes source preservation, not boot/firmware compatibility.

## Timing and mapped-path evidence

All 23 [constrained timing rows](timing-rows.json) have nonnegative slack and
zero TNS. All 35 clock identities, types, periods and frequencies match the
original diagnostic. STA reports zero unconstrained clocks, inputs and outputs.

| Check | Worst slack (ns) |
| --- | ---: |
| Setup | 2.982 |
| Hold | 0.000 |
| Recovery | 7.534 |
| Removal | 0.157 |
| Minimum pulse width | 0.220 |

Separate routed-netlist queries find both local reset stages, all 89 top reset
consumers and the two generated synchronous reset-controller input registers.
The one-slow-model [path review](path-review.json) records stage setup/hold
slacks of +9.574/+0.107 ns and sampled functional release recovery/removal of
at least +7.534/+0.321 ns. These are real returned paths, rather than empty
collections or false-pathed stage data.

The generated synchronous-controller SDC retains its vendor +100/-100 ns
asynchronous-input bounds. An additional in-memory diagnostic applies tighter
10/0 ns bounds only from the last FES stage to those two controller inputs.
Both setup and hold queries return both paths; reported relationships confirm
10/0 ns took effect, with minimum slacks +7.652/+0.296 ns. The
[query scripts](reset-paths.tcl) and [bound probe](controller-bounds.tcl) are
retained. Every compiler report hash remained unchanged after the queries.
This extra probe is a routed-path diagnostic, not production or physical signoff.

## Remaining report and hardware gates

The original unsynchronized-reset and duplicate/ignored-clock failures are
absent. The intermediate forced-chain/multiple-source findings are also absent.
[Closure inventory](closure-review.json) and [report excerpts](report-excerpts.txt)
retain the remaining findings with zero waivers:

- High TMC-20012 still requests an output delay on LED[7]. The board output
  is an asynchronous human-visible indicator without a clocked receiver;
  its explicit port exception leaves zero unconstrained outputs. No fictitious
  output delay is supplied.
- Medium RDC-50003 groups 12 heads: the common top chain, two subsystem
  controllers and nine JTAG chains. Four unchanged HPS adapter circuits leave
  this classification after their source becomes synchronized. Their absence
  from the group supplies no new reset-quiescence or physical proof.
- Low RES-50101 reports two same-clock reset transfers into retained vendor
  controllers whose generated SDC cuts asynchronous clear pins. Altera documents
  this as conservative [IP-preservation classification](https://resources.altera.com/quartushelp/24.3/da_rules/res_50101.htm).
  Those vendor exceptions are preserved; no blanket waiver is introduced.

Constrained Timing Closure passes; overall Design Closure remains Fail.
MTBF analyzes 24 of 38 identified chains and excludes 14; the shortest chain
has two registers. Its model value does not qualify the excluded chains or
whole physical design. Electrical, DDR, identified-board behavior, firmware/
handoff/DT pairing, external traffic/reset coordination and recovery remain open.
The SOF remains independent of the Linux BSP's factory QSPI boot artifact.

## Software checks and retained candidate

The corrected producer passes 21 focused tests: 19 preparation/guard/receipt
tests and actual mailbox/reset RTL benches. The reset bench exercises all
four configuration/PLL combinations, startup, held/stopped clocks, independent
assertion causes, interrupted release and two-edge relock.

[Affected checks](test-changed.json) pass against this exact producer: 870
parent tests (one skipped), 1,395 FPGA software tests (35 skipped), planner
regressions and generated consistency. [Consistency](consistency.txt) verifies
23 consumers/37 fixtures/zero source pins; the [deployment plan](hil-plan.json)
is `NO_DEPLOY_CHANGE`. Full software/path log hashes are retained separately.

The [first reset candidate](candidate-6cb01a9123/README.md), its failures,
source identity, SOF and full output remain preserved. It is not relabeled
as the corrected artifact above.
