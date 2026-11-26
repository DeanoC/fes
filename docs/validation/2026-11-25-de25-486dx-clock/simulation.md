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
