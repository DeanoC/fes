# i386 Phase 1 front-end fixes: split L, stored availability, D look-ahead, replicated hits

Dated record, 2026-10-25. Host simulation and routed diagnostics on Powerboat.
No package, RBF or kit. It makes the four changes the
[Phase 1 cuts](2026-10-25-i386-phase1-cuts.md) measurements pointed at, and
re-measures every stage under rule 9. That record's figures are the earlier
round in the tables below; it is unchanged. Run data:
[`2026-10-25-i386-phase1-front-end-fixes.jsonl`](2026-10-25-i386-phase1-front-end-fixes.jsonl),
one line per route with argv, tool and lock identities, GPU, fmax, per-stage
figures, envelope endpoints, logic depth, cell totals and critical path. The
stage cell mapping counts FF and M10K cells; the separate FF and M10K totals
retain their distinct meanings.

## What changed, and what it costs

| Change | Cycles | New stall cases |
| --- | --- | --- |
| **L in two stages.** L1 runs the 16 decoders on F2's register and registers each byte's length classes: prefix and run flags, opcode and fixed-immediate bytes, ModRM bytes per address size, the size-dependent immediate and offset flags. L2 adds them up for every accumulator variant into each byte's length, end and next-half bit, and writes the buffer. L1's window takes the previous block's bytes 12-15 from L2's register, since that block may not have reached the buffer. | One more stage between fetch and the buffer: every redirect costs one cycle more. Measured: the first instruction after a redirect issues in 5 cycles instead of 4 in 44 of 56 prefix-free runs, and 6 instead of 5 in the other 12. A mispredict resolved at AG costs 6 cycles instead of 5, at EX 9 instead of 8 | None: one prefix-free instruction per cycle and max(1, k) cycles for k prefixes, as before |
| **Q reads stored availability.** The buffer registers, per byte and variant, whether Q may use the instruction there, from next-cycle state. Q's check becomes part of its select. | None | None |
| **D's forwarding selects a cycle ahead.** A table records, per GPR, where its youngest value will be next cycle among the producers then in M1-WB, from the tags of the uops now in AG-EX; beside it the uop in D's destination and kind and whether it left D. D applies each GPR's registered select beside the addressing decode and picks base and index by register number: no tag compare. | None: the measured wait behind the uop ahead is unchanged for every kind and operand | None: the stall rule is the same, checked every cycle |
| **F2's hit signals replicated.** Eight sets of the four way hits, one per two bytes, so no hit drives more than 16 way-select bits. Each set compares against its own copy of the ITLB page register, since Yosys merges identical logic even on kept wires ([DeanoC/yosys#51](https://github.com/DeanoC/yosys/issues/51)). | None | None |

Two requirements the full CPU inherits:
- **Availability with a missing fetch.** `mem_ready` holds F2 until the
  response for its registered address is present. The directed `fetch-stalled`
  bench delays block 2 while Q crosses from half 0 into half 1, then verifies
  that Q waits before issuing a 10-byte instruction that reaches back into the
  released half. This covers the case where L2 is empty at the half release.
- **D's table follows the pipeline.** It is written from the producers that
  will be in M1-WB next cycle. If the stages behind D ever hold, it must be
  written from the held producers instead. Nothing behind D holds yet.

## Checks

Verilator, `scripts/test_fes_i386_frontend.py`. Every check below also passed,
with identical counts, on the netlist the locked Yosys writes from the RTL
(`--yosys`, after `proc -norom; flatten; opt -purge`). The routes use
`synth_intel_alm` separately.

| Check | Space | Cases | Result |
| --- | --- | ---: | --- |
| Length decode, L1 and L2 together, against Bochs | Exhaustive: 12 prefix choices × 500 opcodes × 256 ModRM × 256 SIB × 2 default sizes. Each byte's length for all four accumulator variants: variant 0 against the oracle's decode, variants 1-3 against its decode behind the extra 66h and 67h; every variant's end and next-half bit | 786,432,000 | 478,175,232 compared, plus 1,434,525,696 variant lengths; 0 mismatches |
| Prefix runs, MOV TR6/TR7 | Exhaustive, in all 16 decoders and every variant; MOV TR in both default sizes | 18,320 | 0 failures |
| Front end F1-Q against Bochs | Sampled: 14 streams | 4,400,000 instructions | 4,297,806 compared, 29,862 over 15 bytes flagged, 0 failures |
| Q availability with delayed fetch | 32-bit stream; block 2 withheld across the half release | 100 response-wait cycles, 82 while Q waits at the boundary | No early issue; one 10-byte cross-half MOV issued and matched Bochs |
| D and AG, cycle by cycle, against the architectural address and the stall rule | Every memory ModRM × SIB × address size, 16 times, as a uop stream through a pipeline model; producers biased onto the next uops' registers | 1,572,864 uops, 2,567,239 D cycles | 0 failures |
| Waits behind the uop ahead | Each kind of result into the next uop's base, unscaled index and scaled index | 9 | early 0, 0, 1; load 2, 2, 3; late 3, 3, 4 cycles: the design's latencies |

Throughput, D always ready, every issue cycle checked:

| Stream | Instructions | Cycles from first to last issue | Timing failures |
| --- | ---: | ---: | ---: |
| Prefix-free, 16- and 32-bit | 400,000 each | 400,000 each | 0 |
| Prefix-free, 8+ bytes (32-bit) / 5+ bytes (16-bit) | 100,000 each | 100,000 each | 0 |
| 0-4 prefixes, 16-bit | 400,000 | 881,579 | 0 |
| 0-4 prefixes, 32-bit | 400,000 | 879,475 | 0 |

That makes 1,799,886 checked gaps.

The D and AG stream:
- 1,572,882 addresses, 490,601 of them with a 16-bit wrap. The 18 above the
  uop count are the timed pairs.
- 345,208 operands came from a result register and 1,052,832 from the
  register file.
- 399,446 late bases (94,934 from M1, 149,875 from EX, 154,637 from WB) and
  184,993 late unscaled indexes (43,754, 69,208 and 72,031).
- 458,070 operands' youngest producer was the uop just ahead.
- 994,357 stalled cycles: 966,313 for a value further off, 8,592 for two late
  operands and 19,452 for a late scaled index. 454,578 uops stalled and then
  left D.

Mutations show the checks bite:
- **L1 tail.** L1 reading the previous block's bytes 12-15 from the buffer
  instead of L2's register: thousands of failures in every front-end stream.
- **Availability.** A next-half term that never counts the next half as
  valid: the front end stops issuing. The directed delayed-fetch case also
  exercises Q's release while the next response is absent.
- **Uop ahead.** D's table taking the uop ahead without asking whether it
  left D: 84,347 failures at 2 rounds. Dropping the uop ahead altogether:
  48,113 failures.

## Timing spikes

`scripts/benchmark_fes_i386.py`, seeds 1-4, HIP on Powerboat through
`gpu-run` with `NEXTPNR_REQUIRE_GPU=1`. Every route logged a live HIP
backend. Synthesis is `proc -norom; synth_intel_alm -family cyclonev
-nolutram` on Yosys 3a5accbd, as in the earlier round. The spikes are the
earlier round's, changed only by the work above:
- **Front end:** F1; F2 behind the 16 KiB 4-way L1I model, now with the hit
  signals replicated; L1; L2; the buffer; Q.
- **D+AG:** the producers now move through AG-EX registers into D's table.
  D's opcode decode, which would give the uop's own destination and result
  kind, is not built, so it stays stimulus: the table's update path is
  measured without it.
- **M2:** unchanged; the control. It routed exactly as before on both locks.

Each stage's figure is the worst setup slack among the registers it writes,
as the clock that stage alone would allow. A register is now labelled from
every name its output carries in the synthesized netlist, else from its
input's. Flattening had left the F2 register named only as L1 decoder inputs,
and the L1/L2 register named after L2's ports, so they fell outside every
stage. The envelope's own registers are matched first, and Yosys's
generated net names are ignored, so no stimulus register is pulled into a
stage by a port or input it shares. Every route's worst envelope endpoints
below are stimulus, signature or register-file model cells. The earlier
round's labels were checked the same way: no envelope register there was
slower than 118 MHz, so its stage figures stand.

These are historical route results, not measurements of the current RTL. The
final `now` columns use source `137aa86b41`, before the fetch-response
`mem_ready` qualifier and the later packed per-byte `lclass_t`/`lbyte_t`
change. Neither later RTL change was routed again at the current HEAD.

Routed fmax in MHz, seeds 1, 2, 3, 4:

| Spike | `bd7a1c0d`, earlier round | `bd7a1c0d`, now | `9128800f`, earlier round | `9128800f`, now |
| --- | --- | --- | --- | --- |
| Front end | 61.43, 67.54, 62.34, 65.78 | 85.97, 80.56, 80.95, 76.06 | 69.62, 72.01, 75.15, 71.01 | 89.54, 92.18, 91.17, 85.57 |
| D+AG | 83.13, 79.59, 84.88, 79.15 | 89.59, 87.67, 92.36, 85.34 | 91.21, 91.63, 92.36, 94.49 | 91.81, 96.23, 97.94, 89.57 |
| M2 (control) | 102.07, 106.19, 102.45, 102.03 | 102.07, 106.19, 102.45, 102.03 | 136.52, 142.31, 139.12, 139.65 | 136.52, 142.31, 139.12, 139.65 |

Per stage, lowest and highest over the four seeds:

| Stage | `bd7a1c0d`, earlier round | `bd7a1c0d`, now | `9128800f`, earlier round | `9128800f`, now | LUT levels before placement, now |
| --- | --- | --- | --- | --- | --- |
| F1 | 91.7-101.4 | 87.6-94.6 | 93.2-105.7 | 95.1-102.2 | 5 |
| F2 | 71.8-78.8 | 78.4-89.3 | 81.1-94.7 | 100.5-102.2 | 5, behind the M10K |
| L1 | L: 61.4-67.5 | 89.7-92.0 | L: 69.6-75.2 | 101.1-108.1 | 6 |
| L2 | L: 61.4-67.5 | 90.2-93.5 | L: 69.6-75.2 | 96.6-104.2 | 8 (5 without the availability bit) |
| **Q** | 64.6-70.6 | **76.1-86.0** | 74.2-79.7 | **85.6-92.2** | 8 |
| D | 79.2-84.9 | 85.3-92.4 | 91.2-94.5 | 89.6-97.9 | 7 (10 before) |
| AG | 96.1-102.2 | 91.1-95.8 | 102.2-110.6 | 102.3-106.5 | 5 with 18 carry cells, on the 16-bit-wrap path; unchanged |
| M2 | 102.0-106.2 | 102.0-106.2 | 136.5-142.3 | 136.5-142.3 | 6, unchanged |

The corrected model (`bd7a1c0d`) is the design figure; `9128800f` again
reads 10-15% higher.

Each change on its own, front end on `bd7a1c0d` (each column adds one):

| Stage | Split L | + stored availability | + replicated hits (all four) |
| --- | --- | --- | --- |
| F1 | 106.8-139.4 | 100.5-132.5 | 87.6-94.6 |
| F2 | 81.3-89.1 | 80.9-89.6 | 78.4-89.3 |
| L1 | 91.6-99.4 | 87.6-90.7 | 89.7-92.0 |
| L2 | 95.8-107.2 | 84.6-91.6 | 90.2-93.5 |
| Q | 75.5-82.2 | 73.3-77.5 | 76.1-86.0 |
| Whole spike | 75.51-82.18 | 73.31-77.55 | 76.06-85.97 |

Seed to seed, a stage's figure moves by up to 10%, so one seed does not
attribute a change; four do, roughly.

### Where each stage stands

- **L** was the slowest stage at 61-68 MHz; split, L1 is 6 LUT levels and
  L2 5 before the availability bit, routing at 92-99 and 96-107 MHz on their
  own. It is no longer the limit.
- **Q** is now the slowest stage, at 76-86 MHz (86-92 on `9128800f`). In all
  eight final front-end routes, on both locks, its path runs from the
  accumulator or start through the select of the byte's availability and
  run bit to the issue decision, and from there to the enable of
  `acc_linear`: 6-7 cells, 10.8-13.1 ns, 8.4-9.8 ns of it routing. Every
  attribution route ends the same way at `acc_linear`, start or its alias. Stored
  availability shortened the select by a level, but the issue decision's
  fanout to the accumulator's enables and clears stays, and Q did not get
  faster for it: 73-78 MHz with it against 76-82 without. It cost L2 three
  LUT levels (5 to 8: the sums, the next-half bit, then the availability bit)
  and about 10%.
- **F2** has only compares and a way select behind the M10K (rule 7). The
  replicated hits were realized: synthesis and routing keep 160 page flops
  and eight 56-LUT compare sets, and no hit feeds more than 16 way-select
  LUTs instead of 128. F2 did not move: 78-89 MHz against 81-90. Its limit is
  not the hit fanout; M2, the same shape at 64 bits, routes at 102-106.
- **F1** fell from 100-139 to 88-95 MHz with the replicated hits. Its worst
  register is the fetch address in every build, on an unchanged path:
  L2's valid bit and the buffer state to the fill, F2's advance and the
  address enable. The replicas add 140 page flops on `mem_load`, the net that
  also enables the 19 M10K read ports (fanout 40 to 180), and seven compare
  sets beside the M10Ks. The page flops themselves stay at 112 MHz or above,
  so this is placement around the L1I model, not a deeper path.
- **D** rose from 79-85 to 85-92 MHz. Its depth before placement fell from 10
  LUT levels to 7. The routed path runs from the SIB register through the
  addressing decode, the select of the table and the forwarded value by
  register number, and the row choice into AG's operand register: 7-8 cells,
  10.8-11.7 ns, 6.8-7.6 ns of it routing.
- **AG** fell from 96-102 to 91-96 MHz with no change of its own. Its worst
  register is still the M1 result that feeds its late operand. The D+AG spike
  grew from 1,711 to 2,351 LUTs, mostly the eight 32-bit forwarded values, so
  AG's placement is looser.
- **M2** routed exactly as before.

### Against rules 7-9

- **Rule 7.** F2 keeps M2's shape and replicated hits; it still routes 12-26%
  below M2 on `bd7a1c0d`.
- **Rule 8.** AG is unchanged: two LUT levels before its 32-bit chain and one
  after; its deepest path is the 16-bit wrap's, through the chain's first 18
  bits. It misses on the corrected model only since D's table enlarged the
  spike around it.
- **Rule 9.** Every stage but M2 still misses 100 MHz on the corrected model,
  so the clock lands at the measured figure: about 76-86 MHz for the front
  end, set by Q, and 85-92 MHz for D and AG. On `9128800f` the same routes
  give 86-92 and 90-98 MHz.

Options the paths point at, for a later decision. None is made here:
- **Q:** load `acc_linear` whenever the accumulator is empty instead of on
  the issue decision, and duplicate start and the accumulator's two size
  flags for the select's fanout. Drop the stored availability, which cost L2
  and did not speed Q.
- **F2:** trace its own routed path; nextpnr's report traces only the
  slowest. Replicating only the compare's last level, with one page register,
  would keep F1's fanout as it was, but Yosys has no RTL spelling for it. On
  fork main `d4ac300d9d`, the `nomerge` attribute (DeanoC/yosys#31) keeps
  register copies apart but not the logic of an `always_comb`
  ([DeanoC/yosys#51](https://github.com/DeanoC/yosys/issues/51#issuecomment-6395940049)).
- **D:** the forwarded values could be registered per GPR a cycle ahead as
  well, leaving D the select by register number and the scaling.

The checked-in route rows are emitted by the benchmark writer, including the
tool, lock, source, synthesis, logic-depth and `cells_by_stage` fields. They
retain the labels recorded at run time: `round 3`, `round 3, split L only`,
and `round 3, split L and availability`. The `--version` argument labels new
rows. The frontend spike ties `mem_ready` high, so these routes do not measure
the later fetch-response qualifier.

### Area

| Spike | COMB | FF | M10K |
| --- | ---: | ---: | ---: |
| Front end, with the L1I model and envelope | 5,869 (5,622) | 3,230 (2,581) | 19 |
| D+AG, with the register file, segment caches and envelope | 2,351 (1,711) | 1,346 (1,269) | 0 |
| M2 | 339 (339) | 587 (587) | 16 |

Earlier round in brackets.

### Sources

The final routes are of `137aa86b41`. The fetch-response `mem_ready`
qualifier and the later conversion to packed per-byte `lclass_t`/`lbyte_t`
records followed those route inputs. Neither change was re-routed at the
current HEAD. The split-L-only routes are of `aab085bc97`'s RTL with L1 and
L2's instances
renamed as at `8cb56e7a6c`, so the stage labels find them; the
stored-availability routes are of `8cb56e7a6c`. All three used the harness at
`8cb56e7a6c` or later; the `.jsonl` records each route's source digests.
