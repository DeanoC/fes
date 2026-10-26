# Coleco native video timing seed selection

Date: 2026-10-25
Core: `fes.coleco`, Direct and Scanlines native video parts
Evidence: host GPU routes from the saved final-shell campaign; no hardware acceptance is claimed for seed 13.

## Scope and method

The saved job completed 96 routes across three shell snapshots. This record uses only the final-shell matrix: seeds 1–16 for Direct and seeds 1–16 for Scanlines (32 completed routes). The other 64 routes are excluded. No route or sweep was run to prepare this record.

Each route used the same frozen shell and part inputs for its variant, the 74.25 MHz `pixel_clk` target, GPU router, HeAP timing weight 300, default critexp 2, and `--timing-allow-fail` so timing misses could finish and be recorded. The flag was diagnostic; every row retains the final timing result and no constraint or seal check changed. Full per-run argv, environment, GPU, input hashes, toolchain, all clock Fmax values, route time, and route/timing digests are in [`runs.jsonl`](runs.jsonl).

The records identify FES source `361375a7fa5c8ecdd69574a959b85e2ea613f486` and authenticated toolchain slot `fcccef5894b61d0914dc9a8307cc1b702fd7007e8ed68f134349dd29344381ce`: Yosys `5391eeb1` (SHA-256 `25547d4270020281cf857dc1c38e842db79051638a36702c48659ff01c0f6bfa`), nextpnr-mistral `266e18b5` (SHA-256 `582731ba8bf4057510574d07752e6639cd69c40f66faa69ed15ccc488760c16e`), Mistral `8fcc4cb4` (SHA-256 `05d18641b9bc7a1314b29d83a3a0e6fe4842c86f27713ad7ccfc8fbe382a44ee`), HIP architectures `gfx1100;gfx1201`.

Frozen route-input hashes:

| Variant | Original Yosys synth | Routed cart input | Scaffold | QSF | SDC |
| --- | --- | --- | --- | --- | --- |
| direct | `00690d3edc70a9dbffa48b5bd1e8aaf163ca33f51cecb044d2723d6f958152af` | `8e9cfe3bd4c1363c9bf54da8db48257d68efb6158463397018bee68de04fa250` | `7355f8c932b2784f6fbf254bfe40cdae4c47a9b9672ecb9f4d329afe433ea64d` | `35f48121aa1559c2cd2524c4c0d525d2e92eb90d445b81837b62b438dfd87155` | `c73578f78b7e73444d2f298d78d8197517a061e77f9138fccb64d1494798e019` |
| scanlines | `601710b7469d0514d10c2ca7cb20c55eed30ed65abbca853c5918db1425db6b2` | `57ba9bf4d2c0713b3c50f2ca358ab01e87217f69af38b83cd2f5ba07bdffb8e8` | `7355f8c932b2784f6fbf254bfe40cdae4c47a9b9672ecb9f4d329afe433ea64d` | `35f48121aa1559c2cd2524c4c0d525d2e92eb90d445b81837b62b438dfd87155` | `c73578f78b7e73444d2f298d78d8197517a061e77f9138fccb64d1494798e019` |

## Final-shell per-seed results

The shared selection criterion is the highest lower `pixel_clk` Fmax among seeds that meet the 74.25 MHz target in both variants. Seeds 4, 5, 7, 10 and 13 pass both; seed 13 has the highest shared result at 76.400 MHz.

| Seed | Direct pixel_clk MHz | Scanlines pixel_clk MHz | Shared result |
| ---: | ---: | ---: | --- |
| 1 | 79.688 | 71.129 | MISS |
| 2 | 78.370 | 69.764 | MISS |
| 3 | 81.613 | 68.947 | MISS |
| 4 | 79.523 | 75.075 | PASS |
| 5 | 77.531 | 75.432 | PASS |
| 6 | 77.280 | 69.195 | MISS |
| 7 | 75.081 | 74.979 | PASS |
| 8 | 80.457 | 66.516 | MISS |
| 9 | 78.394 | 70.343 | MISS |
| 10 | 79.108 | 75.013 | PASS |
| 11 | 80.841 | 71.048 | MISS |
| 12 | 79.258 | 74.184 | MISS |
| 13 | 77.616 | 76.400 | PASS |
| 14 | 83.319 | 69.536 | MISS |
| 15 | 78.266 | 70.442 | MISS |
| 16 | 78.388 | 68.908 | MISS |

For every route, `system_clock.clocks[0]` reached 52.260 MHz against 52.225 MHz and `system_clock.clocks[1]` reached 152.184 MHz against 12.288 MHz; both passed. The per-run records carry the exact values.

## Seed 1 to seed 13 comparison

| Variant | Clock | Seed 1 MHz | Seed 13 MHz | Target MHz | Seed 13 result |
| --- | --- | ---: | ---: | ---: | --- |
| direct | `pixel_clk` | 79.688 | 77.616 | 74.250 | PASS |
| direct | `system_clock.clocks[0]` | 52.260 | 52.260 | 52.225 | PASS |
| direct | `system_clock.clocks[1]` | 152.184 | 152.184 | 12.288 | PASS |
| scanlines | `pixel_clk` | 71.129 | 76.400 | 74.250 | PASS |
| scanlines | `system_clock.clocks[0]` | 52.260 | 52.260 | 52.225 | PASS |
| scanlines | `system_clock.clocks[1]` | 152.184 | 152.184 | 12.288 | PASS |

Seed 1 misses Scanlines at 71.129 MHz. Seed 13 passes Direct at 77.616 MHz and Scanlines at 76.400 MHz, the best shared result in this final-shell matrix. This matrix remains frozen-input host route evidence. The subsequent production shell and seed-13 Direct/Scanlines builds and seals are recorded in [production-builds.md](production-builds.md); Kit HIL remains pending.

The retained compact Scanlines JSONL ends at seed 15, while its saved `runs/scanlines/seed-16/run.json` and route log show a completed seed-16 miss. The seed-16 row was reconstructed from those saved artifacts and the contemporaneous GPU status record; no route was rerun.
