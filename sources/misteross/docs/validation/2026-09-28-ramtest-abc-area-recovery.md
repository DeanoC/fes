# RAM tester: ABC9 area-recovery isolation, 2026-09-28

For [FES #264](https://github.com/DeanoC/fes/issues/264), follow the
[Quartus fitted-cone comparison](2026-09-28-ramtest-quartus-control-cones.md)
by testing whether ABC9's area-recovery policy contributes to the deeper OSS
control paths. FES base is `051d957b`. This is a host-only compiler experiment
using existing ABC controls; no production compiler or RTL change is adopted.
The restricted mapping reduces selected cone depths but regresses final memory
Fmax from 116.44 to 105.49 MHz. It is rejected; the best candidate stays at
116.44 MHz and the 130 MHz target remains unresolved.

## Controlled synthesis comparison

All variants start from the same 13 RTL files, generated headers, defines and
BUILD_ID as the 116.44 MHz candidate. They use initialized-FSM Yosys
`c156dd886d87113c30bc2bf422ccb9ae0208b2a1` and the same frozen ABC executable.
A fresh default synthesis reproduces the entire original top module exactly.
The LUT costs, asymmetric pin delays, maximum LUT size six and `W600` stay fixed.

| Mapping policy | Cells | Ready1 → skid address[27] ENA | Idle2[23] → address[25] ENA | Idle1[14] → beats_left[2] ENA |
| --- | ---: | ---: | ---: | ---: |
| Default | 14,941 | 2 | 8 | 7 |
| Omit final `&mfs` only | 14,992 | 2 | 8 | 7 |
| Disable flow/exact-area recovery rounds and omit `&mfs` | 15,590 | 1 | 6 | 7 |

The path columns count the longest structurally connected LUT chain between
the selected logical signal and FF ENA, before placement. They include
arithmetic ALUTs and exclude simple inversion/buffering. These are neither
sensitized timing paths nor routed delay measurements.

The default ABC script is:

```text
&scorr; &sweep; &dc2; &dch -f -r; &ps; &if -W 600 -v; &mfs
```

The restricted variant uses:

```text
&scorr; &sweep; &dc2; &dch -f -r; &ps; &if -W 600 -F 0 -A 0 -v
```

The default mapper keeps its global estimated delay at 7,384 ps while its
reported area progresses through 8,232, 7,970, 6,045, 5,753, 5,670 and 5,656.
The restricted variant retains the first three P rounds and area 6,045. This
does not disable every area-oriented operation. Removing `&mfs` alone leaves
all three selected depths unchanged; disabling the preceding recovery rounds
changes two of them and adds 649 mapped cells (about 4.3%). Other cones change
too, so a subsequent full-route comparison measures the whole-flow response.

The new ready ALUT5 has mask `0x32323200`, with inputs A=m1_write,
B=cmd_ready_1, C=m1_read, D=enter_read and E=enter_write. Quartus's fitted LUT
instead absorbs the `filler`/`from_core` cut. This experiment therefore tests
ABC's recovery policy rather than reproducing the Quartus Boolean structure.

The unperturbed intermediate trace of the timeout/address cone starts with a
24-bit equality, a two-bit inequality and a ten-input AND reduction. Generic
mapping produces nine primitive OR/AND levels; ABC9 produces eight LUT levels.
The architecture's `cmp2lut` pass does not handle equality, so this comparison
does not retain a protected six-bit-partition structure like the fitted
Quartus cone. This identifies a remaining factoring/mapping target, without
proving that forcing that partition would improve routed timing. FF mapping
already determines the D/ENA split before ABC9 runs.

## Equivalence and routed qualification

An independent strict combinational miter compares every FF/hard-block input
and top output for arbitrary shared register/hard-block/top inputs. All
58,673 sink bits match across 8,696 source bits and 8,317 boundary cells,
including 8,221 FFs. Boundary types, parameters, metadata, port directions and
widths match; initialization also matches on the paired physical Q signals.
These final netlists have no residual explicit init attributes; both use the
same `MISTRAL_FF` model with initial Q=0, after the earlier legal initialization
mapping.
Clocks, enables and reset/load pins are included. There is no disabled-cycle
don't-care relaxation and no ignored unknown cell.

Pre-autoname RTLIL exports and debug rename logs establish exact register
identity despite 28 changed final FF names. Continuing both export runs
reproduces the original/candidate final modules exactly. The miter passes
Yosys SAT with 3,498,731 variables and 9,401,653 clauses. Eight focused checker
tests include rejected LUT, clock, enable, initialization, metadata and
duplicate-Q mutations, plus stale-success removal on a failed check.

The physical run uses the frozen best nextpnr
`71c513e1539c5adc1e16c92882dc731fe24ebf9b`, Mistral
`7ed06e21c18b047ec5c6d6a7e85e5ea2c8827039`, GPU 1, seed 2,
HeAP exponent 2, weight 10, default beta 0.5 and enable-replication budget 4.
The new synthesis JSON is the only input-policy change. Placement, replication
selection and routing can all respond to the changed mapping.

| Final analogue Fmax | Best baseline | Restricted mapping |
| --- | ---: | ---: |
| Memory | 116.441544 MHz | 105.485237 MHz |
| Pixel | 77.905884 MHz | 75.227562 MHz |
| Capture | 332.494110 MHz | 231.986435 MHz |

The run completes normally with a legal 7,007,204-byte RBF and no final reported
hold violations. The sole final warning is the memory-clock timing miss.
Memory regresses about 9.4%; pixel still passes its 74.25 MHz constraint.
No enable replica qualifies in this mapping, versus one in the best baseline.
The independent snapshot check confirms that the replication pass preserves
all 15,539 packed cells and 107,630 pin states when it selects zero copies.

The new worst memory path is RTL `ddr1_status[296]` to
`ddr1_test.cmd_end.DATAIN`, through the `cmd_following` arithmetic/control cone.
It has 20 arithmetic ALUT arcs and four ordinary LUT arcs, with 3.489 ns logic
and 5.540 ns routing. Including clock-to-Q, skew and setup gives 9.480 ns.
The RTL identities come from the input synthesis aliases; final routed JSON
does not retain every original alias. This is a different limiting path from
the baseline's ready-to-ENA path, so it is not a matched-path delay comparison.

The experiment establishes that recovery policy contributes to the extra
control-cone depth, but globally disabling these rounds is not a timing fix.
It changes mapping and physical distribution throughout the design. The next
bounded synthesis target is the timeout equality/feedback-enable factoring:
preserve or construct the wider parallel cuts seen in Quartus while retaining
the default mapping elsewhere, then prove equivalence and measure the full
route. A D-versus-ENA change would be a separate experiment. No timing-model
pessimism or universal ABC policy conclusion follows from this single seed.

## Reproduction

Raw scripts, logs and source hashes are under `out/ramtest-control-factoring`.
`abc-policy/nomfs-fresh/map.ys` is the successful no-final-mfs control;
`abc-policy/norecovery/map.ys` is the restricted mapping. It executes the
normal synthesis frontend through FF mapping, then repeats the architecture's
map-LUT commands with the replacement ABC script. The exclusive
`begin:map_luts` endpoint is intentional.

Exploratory checkpoint attempts are retained as diagnostic evidence, not used
as route inputs. A JSON checkpoint calls `design->sort()` and can change
subsequent mapping order. Fresh full synthesis is authoritative; RTLIL exports
without sorting allow intermediate inspection without that perturbation.
ABC's built-in combinational verification passes for the successful mappings.

The adjacent JSON records measurements, equivalence, invocation details and
artifact hashes. There is no producer-lock, shared-contract or hardware change.
All 143 artifact and 55 external-input hashes verify. The summary, identity
mapping, intermediate trace and three cone summaries regenerate byte-for-byte.
Independent mapping, proof and final-result reviews found no blockers.
