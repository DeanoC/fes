# i386 data-port latency: D to WB freeze while a load waits

Dated record, 2026-10-26. Host simulation and routed diagnostics on Powerboat.
No package, RBF or kit.

After [memory operands](2026-10-26-i386-memory-operands.md) the data port had
a fixed latency, and no stage behind D ever held. This slice adds
`dmem_valid`: a load waits in M2 for its bytes. The lockstep's data port takes
the same random 0-12 cycle latency the fetch port has.

Run data: [`2026-10-26-i386-data-latency.jsonl`](2026-10-26-i386-data-latency.jsonl).
- The before figures are the memory-operand rows (`f269f8a3e3`). Since those
  routes, the auto-PR system's reset-edge store fix (`d2a52e5b0e`) has changed
  `wb_store` by one term. They were not re-routed.
- A Quartus row's `fingerprint` is the first 12 hex digits of the SHA-256 of
  its `sources` map, as JSON with sorted keys.

## The freeze

A load in M2 whose bytes have not arrived freezes D to WB:
- every stage behind DC keeps its uop;
- D issues nothing and nothing retires;
- this lasts until the bytes arrive.

This needs no tag-based captures, no new forwarding paths, and no change to
how D's tables are built:
- **Distances hold.** Nothing moves relative to anything else, so every
  distance-based operand capture stays right.
- **AG's late operands stay right.** They are taken from the M1, EX and WB
  registers.
- **The store merge stays right.** Its sources are the store in WB and the
  store that retired on the read edge.
- **D's forwarding tables keep their state.** `fes_i386_dops` and
  `fes_i386_dfwd` take a `hold` input.

A partial stall, holding AG to M2 while EX and WB drain, would break all of
the above.

Details:
- **The port.** It registers a load's address on the edge M1 advances. The
  bytes are memory as it was on that edge, so the store retiring on that edge
  is missed, as with an M10K's old-data read, and comes from the retired-store
  register.
- **The freeze is shallow.** Whether M2 holds a load is a register set as the
  load enters (`m2_load`), so the freeze is one gate on `dmem_valid`. A load
  that fails the limit check waits too, then faults.
- **WB's flush and redirect wait for the freeze,** not the other way round.
  A taken branch or a fault in WB waits out a wrong-path load's bytes, which
  costs cycles only in that case. Keeping WB's decision out of the freeze
  took D from 10 to 8 LUT levels, M2 from 10 to 7 and WB from 9 to 6.
- **Gated state:** the segment-busy vector, AG's address operands, the store
  distances, the retired-store register, EX's running flags, retirement and
  the store's write.

## Result

| | Before (`f269f8a3e3`) | After (`68a407616c`) |
| --- | --- | --- |
| nextpnr `bd7a1c0d` (HIP, gpu-run) | 74.38, 73.59, 76.28, 74.84 (D, D, EX, D) | 70.73, 66.84, 75.48, 71.12 (Q, Q, DC, Q) |
| Quartus 17.0.2 (oracle) | 102.06, 104.82, 105.57, 104.20 (Q ×4) | 103.33, 104.83, 99.89, 105.15 (Q, EX, Q, D) |

- **nextpnr loses about 4 MHz on average,** 71.0 against 74.8.
  - Q limits three seeds at 66.8-71.1 MHz. Its worst path is its own loop
    (`align.acc` → `issue` → `acc_count`, 7 cells, 74-76% routing), and Q's
    RTL is unchanged.
  - Q reaches 79.5 on seed 3, so the loss is placement and routing.
  - The likely cause is the freeze's enable network, which reaches about
    5,000 flip-flops beside the front end.
- **Quartus is within its seed spread** apart from seed 3's 99.89, Q-limited.
- **Yosys's depth matches the memory slice's** except M1, at 7 levels against
  5. D 8, DC 10, M2 7, WB 6.

## Checks

| Check | RTL | Yosys `238eac9a49` netlist |
| --- | --- | --- |
| Lockstep corpus, every in-scope state | 1,214,000 pass | the same |
| Lockstep random, 16 seeds × 2,000 programs, data latency on | 18,206,454 retirements, 0 fail; 12,456,473 cycles waiting for a load's bytes | the same |
| Latency-free reruns (seeds 1, 2) | trace unchanged; holds cycle-exact | the same |
| Unbuilt encodings stop | 82,048 of 82,048 | the same |
| Reset while a store waits in WB (the auto-PR system's check) | pass | pass |

Mutations, each on 3,500 corpus states and 2 × 200 random programs:

| Mutation | Result |
| --- | --- |
| M2 and EX ignore the freeze | 1,347 corpus states and 371 of 400 programs fail; the latency-free trace changes |
| D's data-operand table ignores the freeze | 212 of 400 programs fail; the latency-free trace changes (the corpus, one instruction per run, passes) |

## Per stage, MHz

nextpnr, seeds 1-4:

| Stage | Before | After |
| --- | --- | --- |
| Q | 76.7-78.3 | 66.8-79.5 |
| EX | 74.3-78.8 | 72.8-77.4 |
| DC | 74.4-80.5 | 75.5-78.0 |
| D | 73.6-81.3 | 76.6-80.1 |
| L2 | 85.0-89.6 | 78.5-87.7 |
| AG | 81.8-86.2 | 80.0-86.4 |
| F2 | 79.7-85.7 | 80.1-83.1 |
| M2 | 81.8-86.5 | 82.5-85.4 |
| M1 | 95.2-111.2 | 92.2-101.8 |
| WB | 89.8-96.2 | 91.0-103.5 |

Quartus, seeds 1-4:

| Stage | Before | After |
| --- | --- | --- |
| Q | 102.1-105.6 | 99.9-105.8 |
| EX | 102.7-109.1 | 104.8-111.0 |
| D | 114.8-122.9 | 105.2-120.9 |
| DC | 106.7-115.3 | 107.0-117.6 |
| L2 | 107.7-112.0 | 107.8-115.5 |
| M2 | 110.5-121.3 | 111.9-121.1 |

Size: 12,319 ALUTs and 7,797 flip-flops on Yosys, against 12,333 and 7,796.
Quartus fits 7,623-7,638 ALMs against 7,500-7,542.

## What the design doc should say

This is reported for the doc's owner to write; the doc is unchanged here.

- **An L1D miss, or any late load, freezes D to WB.** That is what "a load miss
  stalls the pipeline until the critical dword arrives" means as built.
- **Latency table.** A taken branch or fault in WB behind a waiting load
  waits for the load's bytes.
- **Clock.** On nextpnr the freeze's fanout costs about 4 MHz, through Q's
  placement. A later cut may register the freeze one stage ahead, or replicate
  it per stage group. That needs a way around DeanoC/yosys#51 for
  combinational replicas.
