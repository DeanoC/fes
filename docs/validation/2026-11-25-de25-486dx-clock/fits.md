# DE25 486DX fit results

The [method](method.md) defines admission for each rung. The producer,
fitted timing, and the loaded-route measurement are independent inputs to
that decision.

## Initial C0 = 34 sweep

The candidate is `3df878912962ede8557166d56c93ff8896869943` at 92.156863 MHz.
Seed 1 ran with Quartus Pro 26.3.0 build 114 on Powerboat. Its fitted PLL
reported M = 188, N = 3, C0 = 34 and C1 = 255.

| Seed | ALMs | Worst setup | Worst hold | Worst recovery | Worst removal | Producer |
| ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 1 | 45,138.5 | +0.597 ns | 0.000 ns | +7.148 ns | +0.034 ns | Refused |

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
The remaining original seeds still run against the same candidate.

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

The pc386 basis now covers these traced retiming forms for `TMC-20602`.
The seed-1 report matches 42 rows under the original basis and 36 under
the new one, with each row matched once. The rung collector requires a
passing fitted clock and corner audit for every row matched by the new
basis. The retained seed-1 build remains refused. The updated candidate
needs four fresh fits through the producer.

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
