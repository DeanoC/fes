# RAM tester: fixed-placement Quartus-style state copies

For [FES #264](https://github.com/DeanoC/fes/issues/264), test the saved
Quartus fit's nearby port-1 `draining` and `finishing` state copies without
repeating the earlier whole-design placement reshuffle. The retained OSS route
is **116.918037 MHz** memory. This same-LAB copy trial completes a fresh route
at **116.713356 MHz** and is rejected as a standalone optimization. FES base:
`71bfd325`.

## Bounded compiler trial

Quartus places original and duplicate state FFs in the same LABs near the
feedback logic. The earlier OSS netlist trial inserted two equivalent FFs
*before* placement; it changed almost the entire placement and stopped at a
retained-enable guard. Here an opt-in nextpnr diagnostic runs *after* the
accepted enable replication, timeout rewrite and retained-enable passes. It
adds a copy of each port-1 FF in its original LAB and redirects only the four
previously verified `enter_read`/DDR1 `write` LUT inputs. Each copy has the
same D, clock, control connections, parameters and pin states as its source.
All existing BELs remain fixed.

Private nextpnr commit
[`c6f25983`](https://github.com/DeanoC/nextpnr/commit/c6f25983307d2782c29876773717ff1d9be9cba2)
on `probe/ramtest-feedback-local-copy` is based on `ffe2351e`. Set
`NEXTPNR_MISTRAL_FEEDBACK_STATE_COPY=<prefix>` to enable the fixture-specific
hook and before/after snapshots. Without a nonempty prefix it allocates no
identifiers or cells. Site selection stays inside the source FF's LAB and
requires packed-site legality; it does not relocate an original cell. This is
a private diagnostic, not a producer recipe change or a general FF-duplication
pass.

## Controlled result

The frozen compiler's disabled full-design control matches 12 saved baseline
outputs: seven are byte-identical and five JSON snapshots differ only in the
binary's `creator` version string. The enabled placement starts from that
same design and completes both copies. An independent packed-graph checker
finds exactly two new FFs, four named LUT-input rewires, unchanged original
cells and nets otherwise, and **103,282 unchanged original pin states**.
The copies occupy FF sites (39,32,52) and (39,27,58), beside their sources
at (39,32,58) and (39,27,56). The subsequent full route preserves the BELs
of all **20,014** cells common to the retained baseline; it adds only those
two FFs and two route-through cells on their D inputs.

Both routes use the same initialized-FSM synthesis and BUILD_ID, board
constraints, seed 2, HeAP weight 10/exponent 2, enable-replication budget
four and GPU 1. The copied route finishes normally in 657.439 seconds,
emits an RBF and completes final analogue timing.

| Final analogue Fmax | Retained baseline | Same-LAB state copies |
| --- | ---: | ---: |
| Memory clock | **116.918037 MHz** | 116.713356 MHz |
| Pixel clock (74.25 MHz required) | 76.958595 MHz | 75.409096 MHz |
| Capture clock | 332.494110 MHz | 332.494110 MHz |

The critical memory path remains HPS `cmd_ready_1` through port-1 slot-free
ALUT2 and the original skid-enable ALUT3 to `skid_burstcount[5].ENA`.
Its three routed arcs change from 3.602/1.407/1.552 ns to
3.607/1.423/1.546 ns: total routing **6.561→6.576 ns**, a 15 ps
regression. Logic stays 0.800 ns and effective setup grows 8.553→8.568 ns.
The copies do not touch that logical path; routing the changed design perturbs
its physical delays slightly. The two new D-input route-through cells show
that local FF duplication also needs input routing. The final log reports
one memory setup warning and no hold violations.

This proves the copied FFs fit legally and preserve the accepted placement,
but they do not close the remaining HPS-ready/control boundary. No gain is
stacked. The separately measured 117.868927 MHz existing-enable reuse is
still a tradeoff, not part of this route. The retained 116.918037 MHz result
remains the accepted comparison, and 130 MHz is unresolved. A subsequent
candidate must address the ready root together with its feedback, control
and data boundary; more isolated copies at this placement do not target the
limiting path.

## Verification and scope

Three focused real-backend tests pass after a RED link failure on the missing
copy functions. The complete backend CTest target passes. Independent control
and copy-structure checks pass; the evidence summary regenerates byte-for-byte
and verifies 88 raw artifacts and 11 external inputs. Exact compiler, binary,
RBF, graph and timing identities are in the adjacent
[JSON record](2026-09-28-ramtest-feedback-local-copy.json). Raw artifacts are
under `out/ramtest-feedback-local-copy/`.

Host-only compiler investigation. No production RTL, recipe, producer lock,
ABI, shared contract, PR or hardware operation changes.
