# PC/XT CPU critical-path investigation, 20 October 2026

**Timing remains open; no candidate is promoted.** This follows the user's request to inspect repeated CPU path problems and test a targeted change at 100 MHz. [PR #836](https://github.com/DeanoC/fes/pull/836) remains parked on [#840](https://github.com/DeanoC/fes/issues/840).

## Scope and findings

The starting checkout is `4989651d30b1e7c01dcbb3003191960f3935c870`. The private source profile remains based on functional checkpoint `37177c9c`: PIC feedback shortcut, sixteen fixed microcode RAM sites and actual ROM phase 3500 ps. None of those private profile changes is adopted here. All runs retain nextpnr `c2c01bda6d93cfe6e4d32d54e4d408995396e068`, Mistral `8fcc4cb4`, locked Yosys `3a5accbd`, the same device, weight 2000, critexp 5, seeds 1/2/3 and unchanged clocks/SDC. The baseline is the exact current-main comparison in [the preceding campaign](../2026-10-20-pcxt-timing-playbook/summary.md).

The top 64 system paths from each baseline were inspected: 172 of 192 traverse carry arithmetic. The samples contain 50/6/64 failing paths for seeds 1/2/3 respectively; these are sampled paths, not a count of all failing design paths. Repeated arcs have only one to six sinks in several cases. For seed 3, 54 sampled paths share the register-to-operand-to-carry entrance. The baseline seed-1 worst path has 3.10 ns logic and 8.12 ns routing. This supports investigating mux structure and physical locality, rather than assuming that arithmetic width or very high fanout alone is the limitation.

## Proved candidate

Only the two ADD operand selections change: replace the inferred masked `select16` expressions with instances of the existing `pcxt_logic_operand1_mux`, preserving the generated ADD source masks. The existing post-synthesis native map gives both selectors exactly two LUT levels on all sixteen bits. Arithmetic, architectural registers, reset behaviour, microcode, clock phases and cycle latency are unchanged at RTL level. The synthesized design has 50 more LUT cells and three more FF cells; no additional RTL register or pipeline stage was introduced. The baseline operand boundaries were absorbed by synthesis, so the retained routed paths, rather than a nonexistent standalone net, provide their depth evidence.

An all-input SAT proof compares the actual replacement instances with the original masked selectors. A second proof compares the native LUT-mapped instances with those same equations, including both masks and arbitrary non-one-hot selectors/data. Both pass. The existing two native-map regression tests also pass, including partial constants and structural depth. These are proofs of the changed combinational cones, not a new monolithic CPU proof. The complete private patch, candidate source, synthesis command, source closure, proofs, logs and analysis scripts are embedded in `runs.jsonl`.

The candidate netlist SHA-256 is `183b7eb9b65b5917726f77246a18c8ed5e80998c8634dcd201b457d127b9fb88`. The unfenced routes change only that netlist relative to each matching current-main baseline. No DSP is added: the CPU's hardware ALU is add/logic based, while guest multiply/divide execute through microcode.

## Routed comparison

| Seed | Baseline system MHz | Two-level ADD mux MHz | ADD mux + arithmetic region MHz |
| --- | ---: | ---: | ---: |
| 1 | 89.135 | 89.686 | 87.758 |
| 2 | 96.209 | 91.100 | 91.651 |
| 3 | 87.773 | 92.157 | 94.260 |

All six routes fail the unchanged 100 MHz system setup requirement; the other required clocks pass. Full per-clock native setup/hold summaries, actual GPU devices, commands, hashes and repair progression are in the records. Neither recipe passes any seed, so neither satisfies the same-recipe two-of-three gate. No new Quartus sample, full producer qualification or hardware run is claimed.

The unfenced seed-1 worst path remains ADD-selector to register (11.150 ns); seed 2 moves to microcode-PC/address-copy distribution (10.977 ns), with motherboard paths close behind; seed 3 is ADD-selector to register (10.851 ns). The shallower mux therefore does not establish an overall timing improvement.

## Placement tests and their limits

The arithmetic region is `X35 Y2 X43 Y9`, covering 296 cells: the two ADD muxes, carry logic and ADD selector registers. It sits near the fixed ROM columns and historical CPU placement. Host-only legal-placement preflights pass for all three seeds. All routed region checks independently confirm nonempty target matches and no fenced cell outside the rectangle.

This region still permits long paths to the register bank and result mux. Its final seed-1 path goes from R2 at (46,4) through the fenced mux/carry logic, then from carry (39,4) to result mux (45,7) and back to a register at (39,4). Those last two routes cost 3.603 ns. The worst path still has about 8.00 ns routing versus 3.39 ns logic. A legal rectangle is not proof of useful intra-LAB packing or short routes; region constraints also restrict some compiler remapping opportunities.

One evidence-led refinement extends the same rectangle to result muxes and CPU register endpoints. Its first preflight is rejected because `eu_alu_logic*` matches no synthesized core cell. That rejected attempt is retained. Removing only that unmatched selector yields a 683-cell target; its corrected preflight times out after 300 seconds in analytical placement without a legal placement checkpoint. No route is launched for the endpoint refinement. This is a bounded convergence failure, not proof that the region is impossible or a diagnosed tool bug. No rectangle sweep or timeout extension follows.

## Validation, budget and handoff

Parent `make check` passes for 19 generated consumers and 37 fixture copies. Evidence validation checks all eleven terminal records, six native setup failures, the remaining clock and hold passes, all 296 fenced cells per routed seed, both SAT proofs and byte-identical previous evidence. `git diff --check` passes. The existing PC/XT simulation suite is not rerun: no committed RTL changes, and the private combinational replacement is covered by the two all-input proofs.

The original three-hour follow-up cap is reused, not increased. All GPU routes go through shared FIFO phase leases, owner `pcxt-agent`, maximum two GPUs, with strict live-GPU routing and no vendor/device pin. Each route reserves 1800 seconds before admission; terminal accounting subtracts only logged lease waits rounded down and retains CPU work. Host-only placement preflights use no GPU. The prior source and evidence commits remain unchanged.

All 11 records are terminal: six routes and five host placement attempts. This investigation charges 1,569.768 seconds (0.436 hours). Including the preceding current-main comparison, the shared follow-up uses 2,302.326 of 10,800 seconds. Historical plus follow-up usage is 21,420.741 seconds (5.950 hours). No GPU job remains running for this investigation.

The candidate stays private because it does not meet the timing gate. Committed RTL, core lock, recipe, package identity and shared contracts are unchanged. The next integration step is a demonstrably better CPU mapping/packing candidate, then the unchanged two-of-three gate, a fresh complete producer build and exact-artifact timing qualification. The evidence specifically identifies operand-to-carry, result-mux/writeback and microcode-address distribution as remaining paths; moving unrelated peripherals to 50 MHz would not remove these CPU paths. A future placement change should account for the full path, not just the adder or a broad bounding rectangle.
