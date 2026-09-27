# RAM tester: fitted Quartus control-cone comparison, 2026-09-28

For [FES #264](https://github.com/DeanoC/fes/issues/264), inspect the saved
same-RTL Quartus fit that reaches 139.30 MHz memory, following the rejected
[fixed-HPS experiments](2026-09-28-ramtest-hps-fixed-anchor.md). FES base is
`a3b51bae`. This is a read-only comparison of a private copy of the existing
fitted database; there is no Quartus recompilation or new RTL/compiler change.
The saved fit was constrained to 130 MHz; 139.30 MHz is its reported achievable
Fmax, not a separate compile target.

## Matched paths

Quartus reports below use its Slow 1100mV 100C model and the 7.692 ns memory
setup relationship. “Effective setup” is relationship minus reported slack,
including clock effects and uncertainty. OSS totals are from final analogue
signoff. These are corresponding logical paths in different physical fits,
not a calibration of either tool's delay model on an equivalent route.

| Logical path | Quartus LUT levels / effective setup | OSS LUT levels / effective setup | OSS run |
| --- | --- | --- | --- |
| HPS ready1 → port1 skid_address[27].ENA | 1 / 4.307 ns | 2 / 8.588 ns | Best, 116.44 MHz |
| DDR2 idle[23] → address[25].ENA | 3 / 4.517 ns | 8 / 11.013 ns | Fixed anchor, 90.80 MHz |
| DDR1 idle[14] → beats_left[2] | 3 / 4.278 ns, **D** | 7 / 10.525 ns, **ENA** | Fixed + pins, 95.01 MHz |

The first two reports explicitly terminate at the same register control pin.
The third is only a corresponding register-level comparison: Quartus's worst
reported path reaches D, while the OSS path reaches ENA. Do not treat their
terminal timing arcs as identical. RTL bit identities come from signal aliases,
not mapped cell-name suffixes.

The ready path is `cmd_ready_1` → `skid_write~0` → `skid_address[27].ena`.
Quartus places the single LUT at (35,62), with fanout 109, and the selected
register at (37,62). Its data delay is 3.427 ns and clock skew is -0.780 ns;
setup slack is 3.385 ns. The best OSS ready path instead passes through ALUT2
at (30,26) and ALUT3 at (24,20) before reaching its register at (27,14).
A high fanout alone therefore does not explain the gap.

The Quartus timeout/address path is `idle[23]` at (40,55) → `Equal13~2` at
(40,55) → `Selector225~0` at (39,56) → `address[0]~1` at (39,56) → the address
register's ENA at (40,52). The three LUT fanouts are 6, 12 and 29. Data delay
is 4.287 ns, clock skew -0.130 ns and slack 3.175 ns. The eight-LUT OSS path
spans from (46,14) through shared control logic to its destination at (40,44).
Both logic depth and distribution differ substantially.

## Fitted Boolean structure

The ready enable's five direct inputs are `m_read`, `m_write`, `filler`, the
fitted `from_core~0` signal and `cmd_ready_1`. Its fitted function is:

```text
(m_read | m_write) & ~cmd_ready_1 & (filler | from_core)
```

This absorbs a broader set of logic than composing the existing OSS ALUT2 and
ALUT3 while retaining their `enter_read`/`enter_write` boundaries. It is a
specific alternative mapping to investigate, rather than a repeat of the
previous two-cell composition.

The timeout path uses a six-input partition of `idle == 1000000`, then a LUT
implementing `~stop & ~(eq0 & eq1 & eq2 & eq3)`, then the address-enable decision
combining state and command conditions. The four equality partitions operate
in parallel. The OSS critical chain instead crosses eight mapped LUTs,
including several small intermediate control functions. The next hypothesis
is that different sharing and factoring decisions serialize the OSS control
path. That hypothesis still requires tracing the synthesis transformations
and proving a replacement; path depth alone does not identify the responsible
pass.

Quartus also retains eight explicitly named duplicate one-hot FFs among the
24 fitted DDR1/DDR2 state FFs inspected here. Their names and connectivity are
recorded, but this is not proof that copying those registers would improve
OSS timing. For `beats_left[2]`, the fitted ENA input comes from `hold_sync`,
while the timeout-dependent decision goes through the data mux.

Four local fitted functions—the ready enable, equality partition, timeout/stop
combination and address enable—match independently written Boolean expressions
for all 64 LUT rows. Quartus's returned mask ordering is handled explicitly.
This checks the decoded local functions, not whole-design cross-tool sequential
equivalence or the semantics of every upstream fitted signal.

## What this establishes

The passing fit provides concrete mapping and locality targets. It does not
justify copying only the LUT count: earlier equivalent one-LUT ready-path
composition experiments regressed whole-design timing. Compare the fitted
Boolean functions, register-control choices and sharing/duplication together,
then test one equivalent compiler transformation against the best 116.44 MHz
candidate. No placement or synthesis change is adopted by this comparison.

The raw directory `out/ramtest-quartus-control-cones/` retains the queries,
full routing reports, fitted atom connectivity/masks, OSS critical-cone
extraction and provenance. `compare_paths.py` independently counts the actual
LUT rows and retains the endpoint pin; `oss_cones.py` records the corresponding
mapped cells, input-driver identities and source hashes. Raw LUT masks require
the associated pin mapping/polarity; this extraction alone is not an equivalence
proof.


## Evidence and validation

The queried copy's compiled database and original QSF/fit/map/STA reports match
the original verified fit byte-for-byte. The original record hashes all 13 RTL
files and the BUILD_ID. The original STA Fmax table was re-read to confirm
139.30 MHz memory and 99.54 MHz pixel. Queries use Quartus 17.0.2 Build 602 Lite.
Only copied timing caches change during analysis; no fitter is run.

The adjacent JSON records provenance, path and fitted-function summaries, OSS
mapped logic and raw artifact hashes. Regenerating the path summaries and all
four truth-table checks is deterministic. No RTL, compiler, producer lock,
shared contract or hardware state changes in this stage. The best OSS result
remains 116.44 MHz; this comparison has not produced another timing gain.
