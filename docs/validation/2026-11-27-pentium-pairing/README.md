# A second integer slot or more x87 overlap on DE25

The proposed DE25 Pentium-compatible core could spend logic on a second integer issue slot or on letting more independent x87 operations proceed. The full 111.905 MHz 486DX machine already reached 45,205 of 46,800 ALMs in its fullest reported fit. This experiment measures what each change could buy on Doom and Quake before budgeting a prototype. [486DX fit](https://github.com/DeanoC/fes/pull/2264)

## Results available

| Work examined | Result | Scope |
| --- | --- | --- |
| Doom column renderer | One eligible pair in a 15-instruction loop. Its two PCs account for at most 524,960 pairs or 1.05% of a 50-million-clock DX window, assuming one saved clock per pair. | Original shareware instructions matched against the earlier DX site profile. The 15 sites account for 27.79% of retirements. |
| Doom span renderer | Zero eligible pairs in the 16-instruction loop. | One pass through the same executable's loop. |
| Quake textured span | Twelve eligible pairs in 114 instructions on the continuing eight-pixel path, twelve in 108 on the path leading to the last pixel. | Two paths in id Software's i386 renderer, also matched in the DOS executable. The continuing path executes 30 integer or control instructions after issuing a divide. |

The published post-F3 C28 Quake replay retired 640,601,632 instructions in 2,238,095,296 clocks from the DOS prompt through part of the demo. Even if *every* instruction paired, a change that only removes one issue clock per pair with all other costs fixed would save at most 14.31% of this window. Under that assumption the best same-clock speedup is 1.167×, and the best-case break-even clock is 95.89 MHz. The actual pair count can tighten that bound. As the window includes startup, the replay driver also isolates the tail after the first recorded rendered frame. [Quake receipt](https://github.com/DeanoC/fes/blob/122b82c4e78ed08b8a011993c9d0af99ecf85d04/docs/validation/2026-11-25-de25-486dx-clock/quake-c28-f3-full.json)

The two Doom loops give little support for a restricted second slot in that workload. The Quake path has both integer pairing opportunities and work that could overlap a floating-point divide. How much of that overlap the existing DX already achieves is a measured input to the choice. The full-window pair counts, sole x87 issue stalls, and a prototype timing/resource fit are pending. [Doom evidence and reproduction](doom_column/results.md) · [Quake evidence and reproduction](quake_span/README.md)

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

The pairing calculation assumes a paired issue saves one clock and preserves the other stall costs. The counts for pairing and x87 stalls cannot be added to predict a speedup. A prototype must demonstrate throughput at its routed clock in the full machine. The full replay needs the project builder's Verilator tools and its two census disk images. They were unavailable on the development host when this record was written.

## Checks

The fetched opcode, ModR/M, SIB, prefix count, mode and length travel through the simulation CPU to writeback. This allows the trace to follow the bytes actually executed under paging and self-modifying code. The host tests cover partial-register aliases, cache/address operands, control-flow and profile-window boundaries, parked clocks and x87 stall accounting. The compact fetch tag produced identical classifier results to full bytes on 5,898,240 inputs.

slang 12.2 elaborated the original C28 CPU and the instrumented CPU, each with both ISA settings, with zero errors or warnings. The bound probe was present in the elaborated design. Functional RTL replay and Quartus fitting remain open.
