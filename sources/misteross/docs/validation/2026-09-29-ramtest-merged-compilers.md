# RAM-test: merged compiler diagnostic integration

Dated host-only record, 2026-09-29, for [issue #264](https://github.com/DeanoC/fes/issues/264).

The opt-in timing diagnostic now selects merged Yosys `22bf145d` and nextpnr
`24f9a1db`, including initialized-FSM support, the accepted generic compiler
stack, report-guided local LUT remapping, and the later open-flop asynchronous-clear
correctness fix. Mistral remains `7ed06e21`. The production package compiler lock,
RAM-test RTL, ABI and shared contracts are unchanged.

## Reproduction and selection

From `sources/misteross` at measured FES commit
`9be676760d83c58e9ae655b864748f5704c5d507`:

```sh
make toolchain-fes-ramtest-timing
make diagnose-fes-ramtest-timing GPU_DEVICE=1
```

The recipe synthesizes once, then routes seed 2 / HeAP weight 10 / exponent 2
with enable-replication budget 4. It next uses that completed report to request
generic remap candidate 0 with up to eight LAB groups and fully routes again.
Both passes use the same synthesis, BUILD_ID, constraints and authenticated HIP
installation. The remap is selected only if final memory Fmax improves, the other
clock maxima do not regress, and final signoff reports no hold violations.
An explicit no-qualified-candidate preflight retains the baseline; routing failure,
CPU fallback, absent signoff or an invalid clock table fails the diagnostic.

`build/fes-ramtest-timing-130/ranking.json` retains both outcomes and the selected
winner. `passing` denotes the three frequency constraints, not package or hardware
acceptance. The hold check screens final nextpnr warnings; it is not a claim of
complete package signoff. The RBFs remain host-only diagnostic outputs.

## Measurement

| Same merged tools, synthesis and seed | Memory MHz | Pixel MHz | Capture MHz |
| --- | ---: | ---: | ---: |
| Baseline (selected by conservative rule) | 106.157112 | 94.500099 | 347.793488 |
| Generic candidate 0, groups budget 8 | **109.950516** | 94.357430 | 347.793488 |

Both routes completed on the live HIP backend with RBF generation, final analogue
signoff and no final reported hold violations. Memory improves **3.57%**. Pixel
loses 0.142670 MHz (0.15%) but still comfortably exceeds its 74.25 MHz requirement;
capture is unchanged. `remap_status` is `rejected` because the configured selection
rule requires nonregressing clock maxima, which is stricter than merely meeting
the pixel constraint. The faster remap is retained as a measured tradeoff, not
automatically selected. Both routes still fail the 130 MHz memory constraint.
No earlier 112.170502 or 116.918037 MHz result has been reproduced by this fresh run.

The generic pass composes the slot-free ALUT2/ALUT4 pair into a new ALUT5 at
`MISTRAL_MCOMB.28.5.6`, serving nine enables in three LAB groups. Its predicted
primary gain was 1.747 ns. All 19,863 existing routed cell names retain their BELs;
exactly one LUT is added, with no removed cells or register moves. This result
uses no fixture-specific compiler hooks.

The baseline's worst memory path is HPS `cmd_ready_1` → ALUT2 (31,16) → ALUT4
(24,16) → `m1_burstcount_MISTRAL_FF_Q_7.ENA` (28,5): 7.422 ns routing and
0.800 ns logic, 9.420 ns effective setup total. After remapping, the worst path
moves to DDR0 error/status control: `ddr0_errors_MISTRAL_FF_Q_15.Q` (9,32) →
`ddr0_status_MISTRAL_FF_Q_92.ENA` (34,22), 7.106 ns routing and 1.407 ns logic,
9.095 ns effective setup total. Mapped suffixes are cell identifiers, not asserted
logical bit indices. These are different critical paths, not a matched-path delay
comparison.

The fresh baseline is below the previous 109.051254 MHz baseline. Of 14,618
common routed cell names, 14,540 have different BELs. This is a substantial
placement change, not a replay of the historical layout. The new BUILD_ID and
merged compiler changes, including the asynchronous-clear correctness fix, were
not isolated from one another. The evidence therefore does not establish which
change caused the lower baseline. A controlled compiler comparison on a fixed
synthesis/BUILD_ID is the next way to separate those effects; the new DDR0
error/status cone is the next concrete path target after this remap.


The measured BUILD_ID is `7168b508ab424b70f1c0f87b2035d821`. All 13 RTL source
hashes match the earlier current-source experiment. The recipe, compiler revisions
and BUILD_ID changed, so this result is not an isolated compiler-versus-compiler
performance comparison. The paired passes above isolate the remap within this
invocation.

Earlier current-source manual remapping reached 112.170502 MHz from a
109.051254 MHz baseline. The historical 116.918037 MHz used older RTL/BUILD_ID
and two fixture-specific hooks. Neither historical number is relabeled as a
measurement of these merged pins. The prior matched Quartus result, 134.17 MHz,
also used the previous BUILD_ID and has not been remeasured here.

## Verification and evidence

The authenticated compiler bootstrap, 38 focused RAM-test/search/HIP tests,
`make check`, and independent source review passed. New paired-run tests were
observed failing before implementation; they cover remap selection, another-clock
regression, final reported holds, explicit no-candidate, unrelated routing failure,
missing signoff and CPU fallback in either completed pass.

Independent final evidence review passed: all 22 raw artifact hashes, 25 source
hashes, exact JSON regeneration, timing/hold results and placement comparisons
were verified against the saved outputs and measured commit.

The adjacent JSON records exact tool identities, input hashes, ranking, clock
results, final memory critical paths and raw artifact hashes. Raw local evidence
is archived in `out/ramtest-merged-remap/measured-diagnostic/`; the previous
installation and diagnostic outputs are retained alongside it as
`pre-merged-install/` and `pre-merged-diagnostic/`. Raw outputs are ignored and
must be regenerated or supplied separately to replay their hashes. No hardware
was programmed or tested, and 130 MHz OSS closure remains unresolved.
