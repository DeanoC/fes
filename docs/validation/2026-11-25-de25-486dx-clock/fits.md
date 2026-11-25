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

## First C0 = 34 repair sweep

Revision `e255d4e3a09fd29ad2cfeed465bf49a15e1c17ce` ran at the original
Powerboat checkout path. Its first seed elaborated the same machine
netlist bytes and fitted at 44,943.3 ALMs. The worst setup, hold,
recovery, and removal were +0.543, +0.002, +7.199, and +0.041 ns.
The LOADED route measured 3.011 ns against 3.851 ns available.

The producer refused `TMC-20602` on two further spellings of the
previously reviewed `machine_at_select_fdc` and `machine_at_cycle_strobe`
registers. The two names belong to the FDC port decode and AT I/O-cycle
qualifier. The trace passed all 308 checks for the 11 non-original rows,
with worst setup of +2.177 ns and hold of +0.211 ns. The retained producer
verdict is refused. The next revision checks the actual fitted clocks and
paths for the revised basis during the registered DX build, before package
admission.

| Seed 1 file | SHA-256 |
| --- | --- |
| `loaded.tsv` | `f8a03ead56ff9f647e9d3c55ebf13aba7036303d011aab1905fe44db77b6217d` |
| `tension-paths.tsv` | `ccb511b309650a3a7dc0ae4873281a9c82c2833cfe57a710f4f12bc945a55a1f` |
| `tension-summary.json` | `47f5e452e5d9e97a3ac6ea17c2560d93b8458345d5044523c0c6822c4157c5e3` |

Seed 2 fitted at 44,994.1 ALMs with worst setup +0.523 ns, hold 0.000 ns,
recovery +6.419 ns and removal +0.019 ns. The LOADED route was 3.460 ns.
The producer refused `TMC-20602` on `cpu_m1_out[20]`. Nine unrecognized
rows included additional M1 paging and access qualifiers, M1 valid and
two AT decode registers. The fitted launch and capture paths place
`add_146` and `reduce_or_2303` in the M1 end-address and access-type
expressions, respectively. These two names are Quartus-generated.
All 868 checks passed for the 31 retimed rows, with worst traced setup
of +1.720 ns and hold of +0.067 ns.

| Seed 2 file | SHA-256 |
| --- | --- |
| `loaded.tsv` | `ffa8fdbc317d4b75d8d04475b7cfb0bec3ae83b2f5f18f94c60a66c61b16e128` |
| `tension-paths.tsv` | `09a4f63ca5cc53b4f6d51ddfb38f976f78fe1a4eecd03efcb8760c7abe359d4f` |
| `tension-summary.json` | `1cf10b114aacca43ad9b591eaf53914f528f4df1fca2e82e0389c02ce255784c` |

Seed 3 fitted at 44,820.1 ALMs with worst setup +0.491 ns, hold 0.000 ns,
recovery +6.583 ns and removal +0.094 ns. The LOADED route was 2.584 ns.
The original producer refused `TMC-20602` at the new `cpu_m1_tx` clone.
The fitted trace resolved all 26 retimed rows and passed 728 checks.
Worst traced setup was +2.228 ns and worst hold was +0.063 ns.
The additional compiler names are scoped to the traced store-wait adder
paths and AT decoder slice. The fitted paths identify those cones. The
generated netlist has no individual RTL alias for the AT fitter names.

| Seed 3 file | SHA-256 |
| --- | --- |
| `loaded.tsv` | `ac124f0d70d96ed0db3306285df658d4509fa33749fa07b82f8b32686049f2b7` |
| `tension-paths.tsv` | `2d5033ede0cfa952db9e93f48c190c5336bbfdee31d049ee5446de4f04a9b604` |
| `tension-summary.json` | `ad59c8fefd338322f92ff31ead0505a2053ada638ff4fff23251a25d6d87118f` |

Seed 4 fitted at 45,160.1 ALMs with worst setup +0.866 ns, hold 0.000 ns,
recovery +7.324 ns and removal +0.030 ns. The LOADED route was 2.770 ns.
The producer refused `TMC-20602` on an additional clone of `cpu_m1_tx`.
The other two new forms came from the FDC selector and latched AT port
bit 8. All 364 routed checks passed for the 13 retimed rows. Worst traced
setup was +1.870 ns and worst hold was +0.157 ns.

| Seed 4 file | SHA-256 |
| --- | --- |
| `loaded.tsv` | `af6e0873b06943c2dea899f83140d8f33889a7e15671aa21489628f6df252068` |
| `tension-paths.tsv` | `9f0c2b7da885816d6366bc6feb033f32b2ad856567e7cf7dc5141962826e2c1e` |
| `tension-summary.json` | `23fcffdae8f6881f08680a303a212ca4d98d4baf9eb7c562511fb5b19c9af7f6` |

All four builds of this revision are refused by the producer. The
retained `c34-reviewed/summary.json` SHA-256 is
`258817539e09ec306f150a8ffba6ec09cabce63dd364b1d6a7da4ba2220a663e`.

The alternate builder's authenticated Yosys has the same locked commit
but a different binary digest. A preflight in the same absolute checkout
path generated different Verilog beyond the Yosys banner and source
attributes. The current fit sweep uses Powerboat's original Yosys binary
and checkout path.

## Running a seed on either builder

From `sources/misteross`, the validation wrapper runs one registered seed
and collects the reports inside the same capped lease. Set `QUARTUS_ROOTDIR`
to the pinned Pro installation and choose the toolchain slot for the host.
Supplemental TimeQuest reports use a private copy to preserve the compiled
database and producer receipt.

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
