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
