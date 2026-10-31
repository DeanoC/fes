# DE25-Nano Quartus 26.3 diagnostic compile, 2026-10-28

Quartus Prime Pro compiled the standalone FPGA clock/LED diagnostic successfully
on Powerboat. This is compiler evidence for `A5EB013BB23BE4SCS`. No board was
programmed; physical acceptance remains `not-run`.

## Source, tool and artifact

- Base diagnostic: `6b2b29b7e74bcee40cabb5e2ed1ba1af688f2b80`.
- Tested producer: `304142aa0d93206f20b78d3a55661bb5a124b114`.
- Tool: `Version 26.3.0 Build 114 10/14/2026 SC Pro Edition`, installed at
  `/data/altera_pro/26.3`; the fitter acquired `quartus_agilex5e`.
- Project: `de25nano_blinky`, seed 1, two CPU workers.
- SOF: `output_files/de25nano_blinky.sof`, 554322 bytes.
- SOF SHA-256: `c837729e8f9443316d761769bbc58d44c7068c832a59e1d871e3cf8cb50a2980`.

The retained private output directory is
`/home/deano/.codex/worktrees/c134/fes/out/dev/de25-nano-bringup/out/de25-quartus-26.3-304142aa0d/`.
It contains the SOF, copied inputs, generated IP and full compiler reports.
[inputs.json](inputs.json), [build.json](build.json),
[quartus-version.txt](quartus-version.txt), [ip-generation.log](ip-generation.log)
and [sta.summary](sta.summary) are exact copies from that run. All five copied
functional inputs match the producer commit and the recorded source hashes.
[Report excerpts](report-excerpts.txt) retain the reviewed clock, closure,
coverage, pin and warning rows; [report hashes](report-sha256.txt) identify the
full reports and generated vendor SDC retained in the private output.

The command, run from the FES task worktree, was:

```sh
make -C sources/misteross/boards/de25nano build \
  QUARTUS_ROOTDIR=/data/altera_pro/26.3 \
  OUTPUT=/home/deano/.codex/worktrees/c134/fes/out/dev/de25-nano-bringup/out/de25-quartus-26.3-304142aa0d
```

The runner generated IP and compiled the copied project in that new directory:

```sh
/data/altera_pro/26.3/quartus/bin/quartus_ipgenerate --generate_project_ip_files --synthesis=verilog de25nano_blinky
/data/altera_pro/26.3/quartus/bin/quartus_sh --flow compile de25nano_blinky
```

Reset Release stays at version `19.4.9`. This matches both the reviewed Terasic
example's Quartus 25.1.1 generation report and the installed 26.3 component
`ip/altera/pgm/altera_s10_user_rst_clkgate/altera_s10_user_rst_clkgate_hw.tcl`.
The latter explicitly supports Agilex 5. Actual IP generation completed with
zero errors and zero warnings.

## Timing and warning boundaries

Full compilation completed with zero errors and two warnings. The fitted
`CLOCK0_50` pin is DJ35; LED pins 0 through 7 are DF35, DJ32, DN22, DP23, DN25,
DP25, DJ27 and DP30. All nine use 1.1 V, matching the selected board inputs.

STA reports two constrained clocks: `CLOCK0_50` at 20 ns and the vendor reset
control's `internal_clk` at 10 ns. Required 50 MHz fabric timing passes:

| Check | Worst slack (ns) | TNS (ns) |
| --- | ---: | ---: |
| Setup | 18.141 | 0.000 |
| Hold | 0.101 | 0.000 |
| Recovery | 19.039 | 0.000 |
| Removal | 0.171 | 0.000 |
| Minimum pulse width | 9.594 | 0.000 |

The vendor IP SDC groups `internal_clk` asynchronously. Its recovery/removal
transfer to `CLOCK0_50` is reported as ignored, not timed; STA reports no
internal-clock setup/recovery paths. The coverage report lists zero illegal
clocks, zero unconstrained clocks, zero unconstrained inputs and eight
unconstrained outputs for both setup and hold, exactly `LEDR[0]` through
`LEDR[7]`.

- Warning 25315: each LED output lacks explicit termination and slew settings.
  The fitted defaults are slew 1, 9 mA and series 40 ohms without calibration.
- Warning 21620: signoff Design Assistant rule `TMC-20012` reports eight
  missing LED output delays, with zero waivers. This is its sole failing rule.
  The diagnostic has no external synchronous LED timing contract.

The report's constrained **Timing Closure** is **Pass**; overall **Design
Closure** is **Fail** because of those unconstrained outputs and their high
severity Design Assistant result. STA finds one two-register synchronizer
chain but excludes it entirely from MTBF analysis; design MTBF is **Not
Calculated**. No output timing, electrical or MTBF qualification is claimed.
The receipt consequently retains `timing_acceptance: not-qualified`.

## Software checks and remaining hardware gate

Five focused diagnostic build tests, `make check` and documentation checks
passed. The compile above is the real configured-tool check; fake-compiler
tests establish runner behavior separately.

The affected `make test-changed` run passed 870 parent tests with one skip.
The FPGA suite ran 1,365 tests with 35 skips. Its only failures were 12
command-text assertions across ten existing Make-entrypoint test methods:
the invocation's `PYTHON=/absolute/venv/bin/python` override was inherited by
nested Make calls, whereas those assertions expect the default `python3`.
All ten methods passed a separate retry with `PYTHON` and Make overrides
removed (4.369 seconds; [retry output](entrypoint-retry.log)). All six DE25
checks passed in the original FPGA run. No source or assertion was changed
to handle the invocation issue. The aggregate run remains recorded as failed
in `out/de25-quartus-test-changed.log`; the focused retry is separate evidence.

The remaining physical check is a manual JTAG SRAM load of this exact SOF on
an identified SCS board. Verify the eight active-low LEDs count at half-second
intervals and record the board/device identity and SOF digest. An R1-marked
board requires separate device qualification. No flash write is part of this
diagnostic. Linux boot, HPS, memory, display, runtime and FES package support
remain separate integration work.
