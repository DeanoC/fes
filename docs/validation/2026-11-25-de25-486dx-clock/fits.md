# DE25 486DX fit results

The [method](method.md) defines admission for each rung. The producer,
fitted timing, and the loaded-route measurement are independent inputs to
that decision.

## Initial C0 = 34 sweep

The candidate is `3df878912962ede8557166d56c93ff8896869943` at 92.156863 MHz.
Seed 1 ran with Quartus Pro 26.3.0 build 114 on Powerboat. Its fitted PLL
reported M = 188, N = 3, C0 = 34 and C1 = 255.

| Seed | ALMs | Worst setup | Worst hold | Worst recovery | Worst removal | LOADED | Producer |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 45,138.5 | +0.597 ns | 0.000 ns | +7.148 ns | +0.034 ns | 3.252 ns | Refused |
| 2 | 45,176.2 | +0.574 ns | 0.000 ns | +7.265 ns | +0.029 ns | 2.996 ns | Refused |
| 3 | 45,109.5 | +0.660 ns | 0.000 ns | +6.378 ns | +0.109 ns | 2.863 ns | Refused |
| 4 | 45,126.1 | +1.044 ns | 0.000 ns | +6.845 ns | +0.056 ns | 2.754 ns | Refused |

The worst setup path is from `mult_36~mac~AY_REG` to `add_763~211` on the
machine clock in the slow fix4 0°C model. The synthesis report traces
`mult_36` to `cpu_x87_core_fast_p01`. The fitted DSP inputs trace to
`cpu_x87_core_fast_req[104]` and `[43]`. This is the x87 multiply tile to
adder path, with positive routed slack on this seed.

The producer stopped on `TMC-20602` while checking reviewed findings.
36 of 78 endpoint-tension advisory rows matched no basis, including
`system|_36681_[49]~BLOCK_INPUT_MUX_PASSTHROUGH_X58_Y23_N0_I20_dff`.
The generated CPU netlist maps its stem to `cpu_m1_out[119]`. This seed
fails admission regardless of the raw slack. The retained fitted summary
SHA-256 is `ee0de14e58a8a64815b266bf64c2b6042ad58b29adc95d928deefeaab02e19a1`.
The fitted machine netlist SHA-256 is
`5e49a28b2b28b7fc7b04432e104ea4a774eedac49b6c1ec9d6787ada2ae5a1ef`.
All four original seeds retained these same machine netlist bytes.
The original rung is refused because all four failed producer admission.
Its retained `c34/summary.json` SHA-256 is
`e9d5104725d0a66fdddbb6c4586031b5a66f618dbdd64735bcabd468dc9f5eb2`.

Seed 2 also reports nonnegative timing summary rows. Its producer stopped
on the first unrecognized `TMC-20602` row for `cpu_m1_alias~26`. The full
report has 73 rows. The original basis covers 44, the seed-1 retiming
basis covers 21, and eight fitter variants needed review. Its first
LOADED report command failed the Quartus lease audit before starting
TimeQuest. The replacement leased report passed at all four corners.
The worst setup row runs from a retimed copy of the x87 operand distance
into a fitted shifter register, with +0.574 ns at slow fix4a 0°C. The
seed-2 timing summary SHA-256 is
`a1c1628ae8d94bbc6f97f94f7b6a5d713c188a796861e9ce15a5d723f3d22416`.

Seed 3 reports 165 timing summary rows with nonnegative slack. Its worst
setup is +0.660 ns from `cpu_x87_core_fast_s4[177]` to a duplicate of
`cpu_x87_wrd` at slow fix4 0°C. The producer refused `TMC-20602` at
`_36681_[49]`. The original basis covers 34 of 60 rows. The seed-1
extension covers 20 and the seed-2 variants cover two. The remaining four
map to an M1 translation qualifier and bits 5–7 of the latched AT port.
The seed-3 timing summary SHA-256 is
`794f61bb6bb04fc09f119f8c3f827ab7d0db25e3bd628db54c5e491d4f996b2e`.

Seed 4 also reports 165 timing summary rows with nonnegative slack. Its
worst setup path is from a DSP tile retiming register (`mult_42~mac`)
to the x87 writeback mantissa (`cpu_x87_wm[5]`) at slow fix4a 0°C.
The producer refused `TMC-20602` at `cpu_dmem_read~0`. The original
basis covers 27 of 47 rows. The earlier retiming review covers eight,
and twelve additional rows trace to memory/writeback control, the AT port
latch and AT cycle strobe. The seed-4 timing summary SHA-256 is
`34d24da4e2ee4ed5b30b46d254e286912e8bd387cca41c8a589c0f19fdeb7119`.

### Retimed endpoint review

The retained project passed the all-corner timing recheck and the LOADED
route budget. Its maximum LOADED data delay plus absolute skew was
3.252 ns against 3.851 ns available at this clock.

The 36 unmatched tension rows each named one fitted register clocked by
the machine PLL at all four corners. The trace found 288 setup and 288
hold paths in the machine domain, plus the ownership and asynchronous
edge checks. All 1,008 checks passed. Worst setup was +1.949 ns and worst
hold +0.068 ns across the traced paths. The `reduce_nor_1931` register
sits between a `cpu_m1_s` alias and `cpu_l1i_page_new[2]`. The other
generated names match observed CPU controls, apart from the FDC decode
register traced to `pc386_at_core.sv`.

I edited the report wrapper after it started. The last supplemental path
collection failed after it wrote the all-corner, LOADED and tension
results. The tension checker exited zero and the retained trace passed
again with both machine-clock checks. These are the retained SHA-256
digests from seed 1.

| File | SHA-256 |
| --- | --- |
| `loaded.tsv` | `1a06ac7568f1009d52b1a9dd58826c8c459ecfee01297e369f8fc5bfcec4e3e5` |
| `tension-paths.tsv` | `051782fd61d9cf041f1e0e3fe2311979bf923f916dc052bf9779ac1575a759cc` |
| `tension-summary-same-clock.json` | `4accea27c7e94f21046719c716de1be07706f8feddfc790b686d28b2a3044070` |

Seed 2's LOADED maximum is 2.996 ns against 3.851 ns available. The
29 non-original advisory endpoints each resolve to a unique machine
register. All 812 trace checks passed, with worst incoming or outgoing
setup of +1.353 ns and hold of 0.000 ns. The eight new forms cover the
M1 transaction request, the M1 valid bit, an L1D stall qualifier and the
AT floppy-drive port decoder. The generated machine netlist SHA-256
matches seed 1 exactly.

| Seed 2 file | SHA-256 |
| --- | --- |
| `loaded.tsv` | `26307ec4767003e1f67ab415ad0d09845d94a0469b6e73cb0fb3183a2406cfb5` |
| `tension-paths.tsv` | `7cf75abe876a7d5b4c7ea873df19f013ee64660593d6ab38518344790a7a0af6` |
| `tension-summary-synchronous.json` | `6283c8a77ee207fba27a0a0c6df669c969f50c79fcdcadd5421f45a18521b3e6` |

Seed 3's LOADED maximum is 2.863 ns against 3.851 ns available. All
728 endpoint checks passed for the 26 non-original advisory rows. The
worst traced setup was +2.063 ns and the worst hold was +0.053 ns.

| Seed 3 file | SHA-256 |
| --- | --- |
| `loaded.tsv` | `56482bcca02d799e3c17298cccb045693d49076aa7251b370cce739e798d0406` |
| `tension-paths.tsv` | `9d02685378029ca5267ba6964514666c9b90091d7b5346534ced71193e775e43` |
| `tension-summary.json` | `28ca31f7216e77cc76a962141a6254863d9f546e9d0d473d0cec02eecbc99056` |

Seed 4's LOADED maximum is 2.754 ns against 3.851 ns available. All
560 endpoint checks passed for the 20 non-original advisory rows. The
worst traced setup was +1.626 ns and the worst hold was +0.098 ns.

| Seed 4 file | SHA-256 |
| --- | --- |
| `loaded.tsv` | `ff8402618bf0a3161579faf545d34f209ae2dc02ff454887b2bad7e9fd57cca4` |
| `tension-paths.tsv` | `0e7ee567512a0694a1ade85053412ec5416beda4dd72974ab6191acb851047c7` |
| `tension-summary.json` | `3fcffdeb7ede343f03a8451c634a387fd97c85858e4a1b6a202d0922a6c97c90` |

The pc386 basis now covers these traced retiming forms for `TMC-20602`.
The seed-1 report matches 42 rows under the original basis and 36 under
the new one, with each row matched once. The rung collector requires a
passing fitted clock and corner audit for every row matched by the new
basis. The registered producer checks that the prepared project and build
record name the same model. A 486SX fit still has to match the original
`TMC-20602` basis before package admission. The retained seed-1 build
remains refused. The updated candidate
needs four fresh fits through the producer. The seed-1 repair job queued
on the alternate host was canceled before it acquired a slot after seed
2 exposed other fitter variants. Finish the original sweep and resolve
its findings before starting the four repair fits.

[Altera TMC-20602 rule](https://resources.altera.com/quartushelp/20.1/da_rules/tmc_20602.htm)
and [TimeQuest clock query](https://docs.altera.com/r/docs/683432/26.3/quartus-prime-pro-edition-user-guide-scripting/get_clocks-quartus-sdc).

## Running a seed on either builder

From `sources/misteross`, the validation wrapper runs one registered seed
and collects the reports inside the same capped lease. Set `QUARTUS_ROOTDIR`
to the pinned Pro installation and choose the toolchain slot for the host.

```sh
python3 scripts/quartus_jobs.py run --edition pro --cap-minutes 300 \
  --label 486dx-2263-c34-s2 -- \
  bash ../../docs/validation/2026-11-25-de25-486dx-clock/fit_seed.sh \
  "$results" c34 2 "$vendor_archive" "$cache" "$gpu_router"
```

For `quartus_jobs.py remote`, use the pushed candidate, replace `run` with
`remote`, and pass `'{run}'` and `'{cache}'` for the result and cache roots.
Capture the host and run directory for each seed and keep the evidence on
its build host until the per-rung comparison is recorded.

Run `collect_fit.py DIVIDER BUILD_DIRECTORY LOG_DIRECTORY --seed SEED`
on each builder. Keep its JSON even when the seed fails. Combine all four
reports with `collect_fit.py DIVIDER --seed-reports SEED1 SEED2 SEED3 SEED4`.
The collector requires four distinct seeds at the same committed revision.

## Software checks

`make test-changed TEST_CHANGED_ARGS='--base da4acd2ac'` at `2902c12fe`
passed the parent suite and found three FPGA test failures. The pc386
producer fixture still expected the original 61.437908 MHz DX clock.
It now checks the clock and audio ratio of the selected DX model. The
three focused pc386 producer tests and all 16 DX tests passed after that
correction.

The other two failures reproduced at the merge base. The DE25 PC/XT
standard-shell test and PC/XT PPI reset test use waivers the system
Verilator 5.032 cannot read. The PPI reset test passes on 5.054. The
DE25 video compile has an additional reproducible 5.054 failure tracked
in [#2275](https://github.com/DeanoC/fes/issues/2275).
