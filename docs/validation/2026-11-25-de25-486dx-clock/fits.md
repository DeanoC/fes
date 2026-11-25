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
