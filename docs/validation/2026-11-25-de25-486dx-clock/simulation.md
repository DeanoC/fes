# 486DX functional replay

The [#2194 method](../2026-11-24-i486dx-s1/method.md) defines G2, G4 and G5.
Run the two scripts against separate clean worktrees of the same candidate.
Use fresh output directories on each run.

```bash
bash sim_lockstep.sh "$lockstep_checkout" "$lockstep_output" "$toolchain_cache" "$testfloat_gen"
python3 sim_games.py --source "$game_checkout" --output "$game_output" \
  --cache-root "$toolchain_cache" --drives "$census_image_directory"
```

G2 compares directed and random lockstep results at levels 3 and 4 and
runs the unit, transcendental and TestFloat benches for the shared x87
unit. Lockstep measures architectural behavior in clock cycles. The
candidate revision binds the selected RTL and its DE25 clock record.

G4 compares the shared CPU and the derived DX at the same clock with
#2194's level-3 oracle and fixtures. The DOOM comparison uses the second
half of the one-second RTL window after flip 40. The 3DBench window
uses 8.0 to 11.5 seconds, with snapshots of the screen for checking the
fly-through. Each comparison requires matching handover cycle, site,
registers and control bits. The DOOM ratio has the original DX-33 bound
of 2,062 times the ratio at most 4,682 realtics.

G5 enables the 387 on the DX bench and transfers execution at the DOS
prompt. Verify the captured frames reach the demo, and retain the RTL
counters and stop status from the 20-second window. The replay uses the
same level-3 oracle and 387 wiring as #2194.

The game runner authenticates the locked Verilator, verifies the census
disk hashes and records the firmware and linked Bochs inputs. It sets
the machine and audio device rates from the candidate. The generated
copies of the simulation bench and audio link use the candidate's PLL
clock, audio ratio and corresponding system-to-pixel scheduling. The
CPU runs with the governor disabled as in #2194. The game bench uses
#2194's DDR latency model. Quartus and the physical board provide
separate timing and hardware evidence.

The runner writes `analysis.json` with the handovers, windows, CPI
ratios and Quake's execution verdict. Use `--analyze-existing` with
the same source checkout and output directory to regenerate it from
the logs and profiles. The captured frames are evaluated separately.

The [C0 = 34 receipt](sim-c34.json) records the completed gates and
their evidence digests. On Powerboat, verify the retained candidate
worktrees, bench binaries and listed artifacts with:

```bash
python3 sim_evidence.py /data/projects/de25-486dx-clock-2263/simulation/c34 --verify sim-c34.json
```

For a signoff-only candidate, run `python3 sim_compare.py "$run_root"
"$candidate_checkout"`. It checks the recorded lockstep and compiler
inputs, regenerates the DX and game bench sources, and compares the
exact clock and guest rates. A matching result binds the retained
simulation evidence to the candidate's exercised inputs.

The [C34 signoff review comparison](sim-c34-repair-match.json) found
the same exercised inputs at `8dd207f19cae2dbe3669c2d65c36bd73531187a8`
as the retained C34 run at `3df878912962ede8557166d56c93ff8896869943`.
The matching inputs include 160 tracked files, the regenerated DX and
timed bench sources, all guest-rate ratios and the retained game disks
and command arguments. The successful G2 and G4 results apply to that
signoff-only candidate.

The [repaired C34 comparison](sim-c34-e255d4e3-match.json) checks
`e255d4e3a09fd29ad2cfeed465bf49a15e1c17ce`. It records the changed
`pc386/build.py` bytes. Removing only the reviewed Quartus compile
adapter and normalizing its board assignment and command-line entry
point gives the original module syntax tree. The replay setup calls
`model_clock` and `guest_rates` and does not call `compile_board`.
The regenerated inputs, clock and workload stimulus match the retained
run, including the game disks and parameters. G2 and G4 remain applicable
to this revision through the exercised inputs.

The [registered proof comparison](sim-c34-f9844c1b-match.json) checks
`f9844c1bf7aa0ad9f8ae6eaa63b839dc8515b030` from a clean checkout apart
from the fitter. The changed `pc386/build.py` adds the proof callbacks
in the compile adapter and includes the two proof files in the producer's
source capture. The checked projection removes those exact changes.
Replay setup again calls `model_clock` and `guest_rates`, without calling
`compile_board` or executing the proof module. Both proof files are
recorded by digest. The regenerated CPU and bench, clocks, device rates,
commands and game media match. G2, G4 and the rendered G5 demo milestone
apply through those exercised inputs.

The [explicit Pro lease comparison](sim-c34-d18a5a45-match.json)
checks `d18a5a45b91236abc4efac7607c84e97735eab07` in the separate
`wt-d18-proof` checkout. Its projection normalizes the exact import
block and the reviewed registration changes. Runtime tracing found the
same 18 setup source paths, with only the projected `build.py` bytes
changed. Both setups load `quartus_tools`. Replay setup calls neither
`quartus_lease` nor the retiming proof. The regenerated sources, rates
and game stimulus match the retained C34 run.

The [structural proof comparison](sim-c34-b6310cd4-match.json) checks
`b6310cd4cb7f9eb9f7c5cb4e1462ebb9708024cc` in a clean `wt-b631-proof`
checkout. Since `d18a5a45`, the board module adds `retimed_structure.tcl`
to the producer's source capture. The checked syntax-tree projection
recognizes that exact three-file list and binds the proof files by digest.
The 160 retained input paths and 18 executed setup paths have the same
exercised inputs. The generated CPU and game benches, exact clock and
guest rates, game commands and disk images all match. The verified G2,
G4 and G5 results apply through these inputs, including the completed
Quake RTL window.

The [device geometry comparison](sim-c34-1106c799-match.json) checks
`1106c799322514432b4631fc17439be4055043dc` in a clean `wt-1106-proof`
checkout. The board module and both TimeQuest scripts match the `b6310cd4`
digests. The comparator pins the revised placement-review Python for
this exact revision and the same board projection. The 160 retained
input paths, 18 setup source paths, regenerated CPU and benches, exact
clock and guest rates, game commands and disk images match through the
exercised inputs. The 105-artifact receipt reverified the retained G2,
G4 and G5 runs, including the completed Quake RTL window.

## C0 = 32

At 97.916667 MHz the [completed replay receipt](sim-c32.complete.json)
verifies 112 artifacts from source `7e3227f68081a30c8d920a060eeaa930ab880357`.
The [input comparison receipt](sim-c32-54fe2d45.complete.json) binds the
results to the fitted `54fe2d45ac1d97779aa81584d87c90853e84b066`.
The comparison matches 163 retained repository inputs and 34 executed
setup paths, ordered Verilator dependencies, the regenerated presenter,
and the exact guest rates. Both game CPUs used the eight-cycle presenter
with SHA-256 `7437e4b680112d6d8e5fb7ecf5a0a65f92ba937148b9bfbdfe1550703bdf8167`.

G2 passed L3 with 18,820 corpus tests, 4,000 random programs and 131 x87
cases for each CPU. L4 passed 148 directed and 56,000 random programs for
each CPU. The x87 benches passed 18,337 unit checks, 23,795 transcendental
checks and 3,356,352 TestFloat vectors. The regenerated TestFloat binary
has a new digest. Its 84 executed vector streams match the archived
fixtures byte for byte.

G4 passed 3DBench and Doom with DX/reference CPI ratios of 1.016157 and
1.053878. The Doom result normalizes to 2,173.097 DX-33 realtics against
the 4,682 limit.

Quake reached the first inspected demo frame at 16.000 guest seconds and
exited 0 after the full 20-second RTL window at 24.600 seconds, cycle
2,408,755,362. The RTL retired 566,870,443 instructions, including
378,772,406 in protected mode. The log and profile show zero oracle
fallback, scanout, presenter or plane underflows and capture drops, with
no stop, shutdown or reset. The final frame shows the running Necropolis
demo. [G5 full-window receipt](quake-c32-full.json)

## C0 = 28

At 111.904762 MHz, [G2](sim-c28-g2.json) and [G4](sim-c28-g4.json) passed
on the exact fitted `cb91390dbf936393e6e7e4640fe3717d58c193bc` source.
The [completed replay receipt](sim-c28.complete.json) verifies 112
artifacts for G2, G4 and G5 at that revision.
The compiled dependency lists for both game CPUs include the eight-cycle
presenter with SHA-256
`7437e4b680112d6d8e5fb7ecf5a0a65f92ba937148b9bfbdfe1550703bdf8167`.

3DBench used matching handovers and 391,666,688-cycle scoring windows.
The DX/reference CPI ratios were 1.015048 in 3DBench and 1.057348 in Doom.
The Doom run matched the flip-40 handovers and 55,952,384-cycle final
windows. It normalized to 2,180.252 DX-33 realtics against a limit of
4,682, and both CPU runs exited 0. Quake handed over to the RTL at cycle
503,571,519, displayed the `DEMO1.DEM` announcement at 13.000 seconds,
and rendered the first inspected Necropolis demo frame at 15.000 seconds.
The first-frame receipt SHA-256 is
`8e3e5240e8abc11f2f05d11b24bf6bc93ebe0808af9d9369a185f9f9ad2146c8`.
Quake exited 0 after the full 20-second RTL window at 24.500 seconds,
cycle 2,741,666,815. It retired 640,601,632 instructions, including
429,494,574 in protected mode. The log and profile show zero oracle
fallback, CPU stop, reset, video underflows, and capture drops. The
15-second, 20-second, and final images show the advancing Necropolis demo.
[G5 full-window receipt](quake-c28-full.json)

## C0 = 26

At 120.512821 MHz, [G2](sim-c26-g2.json) passed at
`9259873f54389b4956430acf9c01942f3f1af8ae`. Each CPU passed 18,820 L3
corpus cases, 4,000 random cases, and 131 x87 cases. Each passed 148 L4
directed cases and 56,000 random programs. The x87 benches passed 18,337
unit checks, 23,795 transcendental checks and 3,356,352 TestFloat
vectors. All 84 emitted TestFloat streams match the pinned references
byte for byte.

The comparison to `62012d5b5520ca56e3a1a4f515c002766b52c6b3` accounts
for the exact fitter-only SDC edit and matches 163 tracked replay inputs,
34 executed setup paths, the ordered compiler dependencies, and all
regenerated sources and guest rates. The retained
`simulation/c26-unlocated-proof/compare-62012d5b.g2.json` SHA-256 is
`3ba7d0aec81f895d519102aaa37649042746922e0793204dd5a9235f1bb22486`.
G4 passed at the same revision. The 3DBench scored windows matched at
421,794,880 cycles with a DX/reference CPI ratio of 1.014539. Doom used
matching handovers and 60,256,384-cycle final windows. It normalized to
2,183.921 DX-33 realtics against the 4,682 limit. The retained 82-file
`simulation/c26-unlocated-proof/g4-milestone/receipt.json` SHA-256 is
`a55e078024dfa969ceb2d1a4e41d65eac2b904686dfceceb65589852a4c05582`.
The exact-clock G5 replay rendered the first inspected Necropolis demo
frame at 15.000 seconds with 414,119,676 instructions retired and no CPU
stop. The retained `sim-c26-quake-demo.json` SHA-256 is
`88ccd571b1a5314b046db67d3633760e3a2574229ec10f4b19db3304fd52e933`.
The full window is running. This comparison uses the single-stage x87
fast lane and the earlier base.

## Integrated C0 = 26

[Pipelined G2](sim-c26-f3-g2.json) passed at the exact 120.512821 MHz rates
for `4fe7bada67a8e6689bc4c3dc480a844db72e0076`. The validation driver
shares the fitted `sources/misteross` tree and the DX compiler's
dependency lists contain the derived seven-stage wrapper. Both L3 and
L4 comparisons passed. The x87 unit bench passed 18,342 checks, including
19 clocks through FMUL and FWAIT, both reset inputs, uncommitted squash,
and committed completion across a later flush. The shared fast wrapper
took 13 clocks and failed the new latency assertion. The standalone
transcendental bench passed 23,795 checks. All 84 emitted TestFloat
streams matched their pinned references across 3,356,352 vectors.

The integrated game harness uses the ATA-only boot setting and absent
floppy types on both arms. The two compiled machine dependency lists
contain the eight-cycle presenter and the floppy verifier. The DX CPU's
dependency list binds the derived x87 wrapper, while the reference CPU
build uses the shared wrapper. The launch receipt
`simulation/c26-f3-fast-4fe7/game-launch.json` SHA-256 is
`e502a4335729f156564060a060f8d88e4b0b28d75d45c2c87c344c03fc97406f`.
The integrated [G4 receipt](sim-c26-f3-g4.json) verifies 75 artifacts.
Doom reached the same flip-40 handover at cycle 2,678,438,006 on both
arms. It measured 2,183.921 normalized DX-33 realtics against the 4,682
limit. 3DBench's DX/reference CPI ratio was 1.0145385. All 48 paired raw
3DBench frame captures match the earlier `9259873f` replay. G5 is
still running. Its protected retirement count already differs from
`9259873f` at guest time 10 seconds, so it has its own result.

The router-effort candidate `3fa87187a579d401dfbc0f739e29b665b45c480a`
reproduced the game compile inputs for both arms. The comparison
rehashes 106 tracked dependency files and regenerates the testbench,
audio header, and derived CPU, x87 and presenter sources. It also checks
both compiled game binaries against the launch receipt and matches
guest rates and ATA-only media. The changed board preparation function
runs in the package producer outside the game harness. The compiled-input
receipt SHA-256 is
`e5bc603469bcadf48b59ca1d9c0978cd324acdc7449d3474050658a4bbf75974`.
