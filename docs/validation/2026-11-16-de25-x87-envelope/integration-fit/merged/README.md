# Confirmation fits on the merged tree

One seed of each backend, fitted after main (with #1889) was merged into this
branch. They are reported **beside** the eight pre-merge fits in the parent
directory, not in place of them: one seed is one placement.

| Link | Value |
| --- | --- |
| Source commit | `00752ae14568fcb0bda42c4a3bff57460a206446`, a `git archive` of it |
| Plus | the scratch edits to `pc386_system.sv`: [`scratch-edits-backend1.patch`](scratch-edits-backend1.patch), [`scratch-edits-backend0.patch`](scratch-edits-backend0.patch) |
| Harness | `fit_i386_device.py` and `fit_timing.tcl` from PR #1899 at `3e097cb1ea118e7f8adc0f08fc0b3011396186e0` |
| Netlist as elaborated, X87_BACKEND=1 | `5350358a7f7dc0cacad211038468ed26a26aec699705a822626a7eb4e89ac508`, 133,082 lines |
| Netlist as fitted, X87_BACKEND=1 | `cee13115da5f49e67fb72e3b87868c4b15ff43b66bfed38e29678d2c8b4b1cea` |
| Netlist as elaborated, X87_BACKEND=0 | `12f98f8d95d7141f158dcfe9f61cd310f71be651f434c4ab63ad71f9b4a6e901`, 132,760 lines |
| Netlist as fitted, X87_BACKEND=0 | `c910891c96fdcf1719df5f0514e30335fdc23113b669909ad56ab5c9dcee491c` |
| Elaboration | powerboat, the locked Yosys |
| Fit host | `chip-30x0-ai-dev`, Quartus Pro 26.3.0 Build 114, 61.44 MHz, seed 1 |

`inputs.json` still reads `fes_revision: unknown`, because the tree on the fit
host is an archive with no `.git`. The revision is carried by each
`netlist.json` instead (`source_revision`), through the harness's
`--netlist-source`, which the pre-merge fits predate.

## Results

Fmax is the lowest over the four corners, for each clock's own domain.

| Fit | Source | ALMs needed | of device | LABs touched | of device | DSP | M20K | Fit / timing | `clk` Fmax, MHz | `pixel_clk` Fmax, MHz |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | --- | ---: | ---: |
| **merged**, X87_BACKEND=1, seed 1 | 00752ae14 | 40,982 | 87.57% | 4,611 | 98.53% | 42 | 138 | pass / pass | 118.68 | 175.10 |
| **merged**, X87_BACKEND=0, seed 1 | 00752ae14 | 42,375 | 90.54% | 4,632 | 98.97% | 33 | 140 | pass / pass | 110.13 | 181.00 |
| pre-merge, X87_BACKEND=1, seed 1 | 79f34caa8 + scratch (see ../README.md) | 40,986 | 87.58% | 4,558 | 97.39% | 42 | 138 | pass / pass | 115.21 | 172.80 |
| pre-merge, X87_BACKEND=1, seed 2 | 79f34caa8 + scratch (see ../README.md) | 41,055 | 87.72% | 4,602 | 98.33% | 42 | 138 | pass / pass | 115.02 | 175.13 |
| pre-merge, X87_BACKEND=1, seed 3 | 79f34caa8 + scratch (see ../README.md) | 41,122 | 87.87% | 4,597 | 98.23% | 42 | 138 | pass / pass | 117.34 | 182.12 |
| pre-merge, X87_BACKEND=1, seed 4 | 79f34caa8 + scratch (see ../README.md) | 41,054 | 87.72% | 4,555 | 97.33% | 42 | 138 | pass / pass | 119.99 | 179.44 |
| pre-merge, X87_BACKEND=0, seed 1 | 79f34caa8 + scratch (see ../README.md) | 41,569 | 88.82% | 4,606 | 98.42% | 33 | 140 | pass / pass | 115.53 | 176.96 |
| pre-merge, X87_BACKEND=0, seed 2 | 79f34caa8 + scratch (see ../README.md) | 41,476 | 88.62% | 4,593 | 98.14% | 33 | 140 | pass / pass | 117.65 | 178.60 |
| pre-merge, X87_BACKEND=0, seed 3 | 79f34caa8 + scratch (see ../README.md) | 41,517 | 88.71% | 4,588 | 98.03% | 33 | 140 | pass / pass | 121.76 | 173.46 |
| pre-merge, X87_BACKEND=0, seed 4 | 79f34caa8 + scratch (see ../README.md) | 41,509 | 88.69% | 4,578 | 97.82% | 33 | 140 | pass / pass | 115.05 | 181.98 |

## What the two merged fits show

- **Both fit and pass timing at all four corners**, with LABs under 99.5%:
  band 2 of the method's precommitted bands, for each.
- **X87_BACKEND=1 did not move**: 40,982 ALMs, against 40,986 to 41,122 over
  the four pre-merge seeds.
- **X87_BACKEND=0 moved**: 42,375 ALMs, against 41,476 to 41,569 pre-merge,
  806 above the highest pre-merge seed. The cause was not measured. The
  merge brought slice 4's review fixes into the DE10 sequenced arithmetic,
  whose arms are guarded out when X87_BACKEND=1, which would fit this pattern.
- **The pre-merge controlled A/B therefore does not describe the merged
  design.** It gave a difference of medians of 462 ALMs in the backend's
  favour. On the merged tree the one pair measured differs by 1,393
  ALMs in the same direction. That is one seed a side, so it is a difference
  between two placements, not a measured distribution; the direction agrees
  with the pre-merge result and the size is not established.
- The method's A/B rule was written for four seeds a side and is not applied
  to this pair.
