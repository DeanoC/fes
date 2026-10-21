# PC/XT async_reg transfer: board netlist comparison, 21 October 2026

**The producer's `async_reg` transfer changes no mapped logic on the complete PC/XT board.** For all four display/output variants, the plain `synth_intel_alm` netlist and the producer's split-recipe netlist are equal as complete Yosys JSON documents once the cell-level `async_reg` attribute is removed. The transfer marks exactly ten `MISTRAL_FF` cells per variant: four reset-chain FFs and six video clock-domain-crossing FFs. Routing the CGA Direct pair with the producer's seed-1 recipe gives a byte-identical RBF and timing report, with every placement and route equal. This is host-only evidence; it makes no timing, CDC, metastability, package or hardware claim.

## Scope and fixed inputs

FES revision `0e506fbe37766bf629b0bce13cb2cb21fa5d2da7`, with no `sources/misteross` working-tree changes. The [review](https://github.com/DeanoC/fes/pull/836#discussion_r4333775189) noted that the earlier fixture covered only reset chains, while the board also marks its video crossings. Yosys `3a5accbd` is the executable in the producer receipt of the 19 October campaign's `fresh-37177c9c` run, SHA-256 `71eb21e86017064c141bca25e0ecd7fd39dcb3671f3d560fbc467eb880b2a650`, read from an immutable cache slot built for the PC/XT lock. No RTL, constraint, lock or producer output changes. Exposing the producer's Yosys script as `yosys_program()` leaves its text byte-identical for every variant.

## Method

`check.py` generates the board inputs and runs `build_fes_pcxt.yosys_program()` twice per variant in one scratch directory. The first run uses the plain base `synth_intel_alm -nolutram -nodsp -top top`. The second uses the producer's `-run begin:map_ffs; attrmvcp -copy -attr async_reg; … -run map_ffs:` recipe. Both include the CPU operand and ROM clock techmaps, final hierarchy, cleanup and `check -assert`. The checker requires:

- no cell-level `async_reg` in the plain netlist;
- `async_reg="true"` on exactly the FFs whose output drives an `async_reg` net (the video crossings) and the four FFs cleared by PLL `locked` (the reset chains), and on no other cell;
- equal complete JSON documents after removing only that cell attribute: every module, cell, type, parameter, connection, net, port and remaining attribute.

The reset chains preset on lock loss, so mapping stores them inverted. Consumers take the inversion into their LUT masks or a `MISTRAL_NOT`. The `sys_reset`/`pixel_reset` net names and wire attributes do not survive, so without the transfer no mapped cell records their intent. The six crossing nets keep their wire attribute in both flows.

## Result

| Variant | Cells | `MISTRAL_FF` | Marked FFs (reset + crossing) | Equal after removal |
| --- | ---: | ---: | ---: | --- |
| CGA Direct | 10,437 | 3,698 | 4 + 6 | Yes |
| CGA Scanlines | 10,472 | 3,707 | 4 + 6 | Yes |
| MDA Direct | 10,316 | 3,666 | 4 + 6 | Yes |
| MDA Scanlines | 10,355 | 3,675 | 4 + 6 | Yes |

The crossing FFs are `system.video.adapter.{ack,sent,hold}_{meta,sync}_MISTRAL_FF_Q`. The reset-chain FFs carry Yosys autonames beginning `locked_MISTRAL_FF_ACLR`, which differ slightly between variants. `result.json` lists them with per-variant netlist digests that omit only `src` locations and the Yosys creator string. Four parallel jobs took 754 seconds; each synthesis peaked near 360 MiB.

## Route outcome

Both CGA Direct netlists were then routed with the producer's exact argv: seed 1, timing weight 2000, criticality exponent 5, 100 MHz, the board QSF/SDC and `NEXTPNR_REQUIRE_GPU=1`. The compiler is the PC/XT lock's nextpnr `552ba246`, SHA-256 `725d9dd05b2c1f287555a7ad8c29a05c9c119493491b7f1c78c8c843f905dff7`, from the same cache slot. One `gpu-run --owner lander` lease held Powerboat GPU 1 (HIP, AMD Radeon AI PRO R9700) for both routes. Another job held GPU 0, so host CPU was shared.

| Netlist | Exit | Route seconds | System / pixel / ROM / capture MHz | RBF SHA-256 |
| --- | ---: | ---: | --- | --- |
| Plain | 1 | 259.354 | 64.466 / 92.123 / 112.680 / 341.297 | `64f32d616d806d46…` |
| Transfer | 1 | 247.345 | 64.466 / 92.123 / 112.680 / 341.297 | `64f32d616d806d46…` |

Both runs fail the 100 MHz system clock and exit 1 identically. The RBF and `timing.json` are byte-identical. After naming every bit by its net and removing the ten attributes, the routed netlists are equal: all 12,334 placements and 15,483 net routes match. The nextpnr source reads no `async_reg` attribute; only Mistral test fixtures mention it. Its JSON writer numbers bits by net-name ID, and importing the attribute interns one more name earlier, so some `routed.json` bit numbers shift by one. The logged `Checksum` values also differ because they hash cell attributes. With those and wall-time values masked, the logs are identical. `route-ab.json` records the argv, lease, device, durations and artifact digests.

## Limits

The route comparison covers one variant at the production seed. No earlier campaign completed a route of the current local-ROM producer netlist, so the 64.466 MHz system result has no like-for-like baseline in those records; it is not timing evidence. The earlier PC/XT route campaigns routed netlists from older producer revisions or private probes. This comparison does not make any of them current-producer evidence; each still needs its own exact-netlist binding. The attribute is not a timing exception or CDC qualification, and the README's reset-chain requirements remain open.

## Reproduction

```sh
python3 docs/validation/2026-10-21-pcxt-reset-metadata/check.py \
  --yosys /path/to/yosys-3a5accbd
```

The command recomputes all four comparisons and compares them with `result.json`. Exit zero means the invariant held and the retained per-variant result reproduced. `--write` replaces the retained result after an intended input change. The default test suite's fixture in `sources/misteross/tests/test_pcxt_reset_metadata.py` covers the same reset and crossing patterns in about one second.
