# A second integer slot or more x87 overlap on DE25

The proposed DE25 Pentium-compatible core could spend logic on a second integer issue slot or on letting more independent x87 operations proceed. The full 111.905 MHz 486DX machine already reached 45,205 of 46,800 ALMs in its fullest reported fit. This experiment measures what each change could buy on Doom and Quake before budgeting a prototype. [486DX fit](https://github.com/DeanoC/fes/pull/2264)

## Which prototype to fit

**Fit the x87 prototype first.** In Quake after the first sampled rendered frame, x87 alone blocked issue on 528,526,285 clocks. The restricted integer slot found 15,200,990 candidate pairs in that same interval. At the C28 clock, recovering 2.88% of those x87 stall clocks would equal the integer slot's best case of one saved clock per pair. The integer slot's best case in Doom H2 was 2.53% of clocks. [Verified counters, inputs and digests](c28-replay.json)

| C28 window | Clocks | Retirements | Eligible pairs | Pairs per 100 retirements | Max clocks saved by pairing | Sole x87 stall clocks (% of window) |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Doom G4 H2, x87 off | 55,952,384 | 31,690,474 | 1,415,435 | 4.47 | 2.53% | 0 (0.00%) |
| Quake G5, full 20 s | 2,238,095,296 | 640,601,632 | 39,229,579 | 6.12 | 1.75% | 587,630,856 (26.26%) |
| Quake G5, from first sampled render | 1,063,095,232 | 249,801,864 | 15,200,990 | 6.09 | 1.43% | 528,526,285 (49.72%) |

Each pair consumes two adjacent retirements. The shares of retired instructions in candidate pairs are 8.93%, 12.25% and 12.17%, respectively. At one saved clock per pair with other costs fixed, the corresponding break-even clocks for a dual-issue fit are 109.07, 109.94 and 110.30 MHz against the 111.905 MHz C28 baseline.

| Sole x87 stall clocks by observed hold condition | Full Quake | From first sampled render |
| --- | ---: | ---: |
| Waiting for a prior operation only | 512,929,110 | 462,843,479 |
| Waiting for a store value only | 33,087,994 | 29,633,155 |
| Both prior operation and store value | 41,613,743 | 36,049,651 |
| Pending control only | 9 | 0 |

The first prototype should target the wait for a prior x87 operation. Quake already issued a non-ESC, non-WAIT micro-op during x87 work on 20,398,630 clocks of the rendered interval. Of its 528,526,285 sole-stall clocks, the probe identified 275,846 with a true stack-register read-after-write dependency and 363,819 without one in the covered register-arithmetic forms. The other 527,886,620 lack a dependency classification. Attribute the dominant opcode pairs before choosing which results to forward or operations to overlap. Even recovering all 363,819 clocks in the no-RAW class would save only 0.034% of the interval.

The fit decision prioritizes an experiment. Its performance test is the change in elapsed time for the same work with the board CPU settings, evaluated at the prototype's routed whole-machine clock. Resource use and timing on the DE25 remain open.

## Renderer loops

| Work examined | Result | Scope |
| --- | --- | --- |
| Doom column renderer | One eligible pair in a 15-instruction loop. Its two PCs account for at most 524,960 pairs or 1.05% of a 50-million-clock DX window, assuming one saved clock per pair. | Original shareware instructions matched against the earlier DX site profile. The 15 sites account for 27.79% of retirements. |
| Doom span renderer | Zero eligible pairs in the 16-instruction loop. | One pass through the same executable's loop. |
| Quake textured span | Twelve eligible pairs in 114 instructions on the continuing eight-pixel path, twelve in 108 on the path leading to the last pixel. | Two paths in id Software's i386 renderer, also matched in the DOS executable. The continuing path executes 30 integer or control instructions after issuing a divide. |

The static paths explain which operations the classifier accepts. The full replay above measures how often the running workload actually presents a pair. [Doom loop evidence](doom_column/results.md) · [Quake span evidence](quake_span/README.md)

## Full replay

**The published game replay and the fitted board have different CPU identities.** The original runner uses the DX caches and load-forwarding changes at the C28 clock, but it elaborates the standalone CPU with its default `I486=0`. It enables x87 for Quake only. The board package sets `I486=1` and `X87_PRESENT=1`. This experiment preserves the published settings for its profile comparison. Doom serves as an x87-off control. A production-DX speed claim needs measurements with the board settings. [Historical game setup](https://github.com/DeanoC/fes/blob/35b061f58bfb2c97eb143a00fdb35818e0a67594/docs/validation/2026-11-25-de25-486dx-clock/sim_games.py#L127) · [CPU compilation](https://github.com/DeanoC/fes/blob/35b061f58bfb2c97eb143a00fdb35818e0a67594/docs/validation/2026-11-25-de25-486dx-clock/sim_games.py#L293) · [Board parameters](https://github.com/DeanoC/fes/blob/35b061f58bfb2c97eb143a00fdb35818e0a67594/sources/misteross/boards/de25nano/pc386/build.py#L202)

Run the script from this directory against a separate checkout of the post-F3 C28 replay revision `35b061f58bfb2c97eb143a00fdb35818e0a67594`. It uses the original game launch arguments, firmware, memory model, clock and hash-checked game disks. Output goes to a fresh directory. Each workload writes its original CPU profile, online counters, and a verified result after checking the profile digest and window. `--workload doom` and `--workload quake` are supported separately.

```sh
python3 replay.py --source /path/to/fes-c28 --drives /path/to/census-disks \
    --cache-root /path/to/locked-tools --output /path/to/new-output
```

- **Doom** uses the second half of the one-second DX window after flip 40. The historical site-count exercise above uses a different DX revision and clock.
- **Quake** uses all 20 seconds of the RTL replay, from cycle 503,571,519 to 2,741,666,815. A separate count starts at cycle 1,678,571,583, when the first sampled frame shows the rendered demo. The full and latter windows overlap and must be interpreted separately.
- **Integer pairing** consumes adjacent retired instructions greedily. V accepts simple unprefixed register moves, arithmetic and comparisons. U additionally accepts simple MOV loads and stores. The model rejects register and status-flag conflicts, including partial-register aliases. Unsupported instructions, faults and control-flow boundaries break pairs. This is the proposed small V slot. A full P5 would admit different pairs.
- **x87 stalls** count machine clocks when x87 was the sole local issue blocker, while recording idle/parked clocks, competing blockers and integer issue during x87 work separately. For two supported non-popping register arithmetic operations, the probe distinguishes read-after-write stack-register dependencies from other serialization. All other forms retain an explicit unclassified count. Speculative work, memory latency and ordered retirement can constrain how many of these clocks a redesigned unit could recover.

Pair eligibility and sole-blocker samples measure different events in the same execution. Their percentages cannot be added to predict a speedup.

## Checks

The fetched opcode, ModR/M, SIB, prefix count, mode and length travel through the simulation CPU to writeback. This allows the trace to follow the bytes actually executed under paging and self-modifying code. The host tests cover partial-register aliases, cache/address operands, control-flow and profile-window boundaries, parked clocks and x87 stall accounting. The compact fetch tag produced identical classifier results to full bytes on 5,898,240 inputs.

slang 12.2 elaborated the original C28 CPU and the instrumented CPU, each with both ISA settings, with zero errors or warnings. The bound probe was present in the elaborated design. On Powerboat, the instrumented RTL completed both replays with zero oracle fallback steps after handover. The complete Doom and Quake CPU profiles matched their published SHA-256 digests, as did the Doom H2 window and the three published Quake frame checkpoints. All 25 raw Quake frames matched the retained C28 control. The unpaged retire crosschecks found zero disagreements in either game. Full Quake had 3,627 untagged ordinary retirements, including 1,116 in the rendered interval. These break the pair stream. [Doom reference](https://github.com/DeanoC/fes/blob/122b82c4e78ed08b8a011993c9d0af99ecf85d04/docs/validation/2026-11-25-de25-486dx-clock/sim-c28-f3-g4.json) · [Quake reference](https://github.com/DeanoC/fes/blob/122b82c4e78ed08b8a011993c9d0af99ecf85d04/docs/validation/2026-11-25-de25-486dx-clock/quake-c28-f3-full.json)
