# i386 memory operands: data port, loads, stores, real-mode limits

Dated record, 2026-10-26. Host simulation and routed diagnostics on Powerboat.
No package, RBF or kit.

The one-uop instructions the CPU already executes gain their ModRM memory
forms. This covers ALU, MOV, TEST, INC, DEC, NOT, NEG, the shifts and
rotates, SHLD, SHRD, SETcc, MOVZX, MOVSX, and MOV to and from a segment
register. They run on a data port that stands for the L1D's output, at a
fixed latency.

The port's random latency, and the holds behind D it needs, are the next
slice. #1095 recorded the requirement that D's forwarding tables then follow
held producers.

Run data: [`2026-10-26-i386-memory-operands.jsonl`](2026-10-26-i386-memory-operands.jsonl).
- The before figures are the [EX cut 2](2026-10-26-i386-ex-cut2.md) after rows
  (`b89513835f`). They were not re-routed: this branch's base differs from
  their sources only by a comment in `fes_i386_qalign.sv` and the benchmark's
  reporting code.
- A Quartus row's `fingerprint` is the first 12 hex digits of the SHA-256 of
  its `sources` map, as JSON with sorted keys.

## Result

| | Before (`b89513835f`) | After (`f269f8a3e3`) |
| --- | --- | --- |
| nextpnr `bd7a1c0d` (HIP, gpu-run) | 75.09, 75.67, 71.75, 83.33 (D, Q, D, Q) | 74.38, 73.59, 76.28, 74.84 (D, D, EX, D) |
| Quartus 17.0.2 (oracle) | 103.35, 105.27, 102.77, 106.11 (Q ×4) | 102.06, 104.82, 105.57, 104.20 (Q ×4) |
| Lockstep corpus states | 417,804 (register forms) | 1,214,000 (every form) |

- **Every in-scope SingleStepTests/80386 state now runs, memory forms
  included.** All 1,214,000 pass, on the RTL and on Yosys's netlist.
- **On Quartus the whole CPU is unchanged** within the seed spread, still
  Q-limited.
- **On nextpnr it costs about 2 MHz on average** (74.8 against 76.5), D-limited.
  - The memory forms' decode takes DC from 82.1-96.2 to 74.4-80.5.
  - The load merge takes M2 from 92.2-105.4 to 81.8-86.5.
  - Neither limits yet: D does, at 73.6-81.3, then EX.
- **Two cycle costs.**
  - A load that directly follows a store waits one cycle.
  - A memory operand waits while its segment register is being written.

## The data path

- **DC** decodes the memory forms.
  - A memory operand is a load, a store, or both in one uop
    (read-modify-write). It stands for the register it replaces, so no GPR is
    read or written for it.
  - Its segment is the override, else SS for a BP-, EBP- or ESP-based address,
    else DS.
  - LOCK becomes legal on the read-modify-write forms of ADD, OR, ADC, SBB,
    AND, SUB, XOR, NOT, NEG, INC and DEC.
  - An immediate after a ModRM's operand bytes is the instruction's last
    bytes, so DC takes it from the length minus the prefixes, beside the
    decode, not from the SIB and displacement decode. That held DC's depth to
    10 levels, from 11.
- **D** picks the address operands through `fes_i386_dfwd`, as for LEA, and
  the segment's base. It also picks the segment limit less the access's bytes
  but one, which reset precomputes for each segment and 1-, 2- and 4-byte
  sizes.
- **AG** computes the linear address and the offset with the existing adders.
- **M1** checks the limit with one compare: #SS(0) for SS, #GP(0) otherwise.
  It sends a load's address to the port, and takes the load's distance from
  the two stores that will not have written when the port reads:
  - the store in EX, which is in WB when the load is in M2;
  - the store in WB, which writes on the read edge and so is missed: an M10K
    reads old data there.
- **M2** aligns the port's 8 bytes to the access, then takes each byte an older
  store writes from that store: the WB register first, then a one-entry
  retired-store register. Every merge source is a register, and the selects
  decode M1's registered distances.
- **EX** takes the loaded value through its existing bypass selects. A
  memory operand is left out of `x` or `y` in M2, and EX ORs or XORs it in by
  lane, as a WB result. The source select folds into the same LUT, so EX's
  front stays one level.
- **WB** writes a store's bytes on the edge it retires.

## Holds in D

- **Store then load: one cycle.**
  - Without it, the store one ahead of a load would still be in EX, its data
    not computed, when the load is in M2.
  - In the latency-free random reruns, 97.8% of 10,212 memory reads after a
    store retire exactly two cycles behind it. The rest wait longer for an
    address operand.
  - All 98,329 other instructions after a store retire one cycle behind it.
  - The lockstep fails a run if a load after a store is under two cycles, or
    another instruction without a memory operand over one.
- **Segment write then memory operand.**
  - The operand waits until the write retires, as a selector read already
    did.
  - Which segments an older uop writes is a register, built from what will be
    in AG to WB next cycle, so each wait is one select.
- **Issue loses its flush term.** A flush clears AG and the forwarding tables,
  a taken branch's redirect resets DC and D's EIP on the same edge, and a
  fault or HLT stops the CPU. That took WB's HLT detection off D's path and
  D's depth back to 8.

## CS limit at fetch

The memory forms are long enough for instructions to run past offset FFFFh.
Bochs and the 386EX corpus agree on what happens there:
- EIP is not cut to 16 bits in 16-bit code. An instruction that ends at
  FFFFh leaves EIP 10000h, and the next fetch raises #GP(0) at CS's limit.
- An instruction that runs past FFFFh raises #GP(0) at its fetch, ahead of any
  fault its decode found.

D now does both when CS's limit is FFFFh, as every real-mode CS load leaves
it. Under a larger limit it stays unimplemented, since the general CS-limit
check is not built. The old rule cut EIP and made any next EIP past FFFFh
unimplemented.

## Checks

| Check | RTL | Yosys `238eac9a49` netlist |
| --- | --- | --- |
| Lockstep corpus, every in-scope state | 1,214,000 pass (104,438 faulting: 22,360 #UD, 70,705 #GP, 11,373 #SS) | the same |
| Lockstep random, 16 seeds × 2,000 programs | 18,206,454 retirements, 0 fail, trace unchanged without latency | the same |
| Unbuilt encodings stop | 82,048 of 82,048 | the same |
| Flag and store holds, cycle-exact | pass | pass |
| Front end and D/AG suites | pass | pass |

**The harness compares memory now.** For each instruction, both sides must
write the same bytes with the same values. A corpus-follow divergence takes
memory as well, the oracle adopting the RTL's bytes.

**Random programs take memory forms.** ModRM register operands become 16-bit
memory operands at random, with a displacement inserted ahead of any
immediate. 67h and CS overrides are dropped from those instructions:
- random registers would put a 32-bit offset past the limit almost every time;
- a CS override would store into the code.

111 of the 32,000 programs still stored into their own code and stopped
there, since the RTL has no self-modifying-code detection yet.

**Harness faults found on the way, each fixed:**
- **Stripped 67h prefixes.** The generator stripped 67h from LEA, which kept
  its 32-bit operand bytes, so the program ran off into data on both sides.
- **Stale exception frames.** The oracle's exception frames, which the RTL
  does not write yet, were not cleared between runs, and later programs read
  them.
- **Fault outcomes.** A divergence entry that takes every field (the
  invalid-SIB scaled base, the LOCK-before-length #UD) also decides whether
  the instruction faults and with which vector. The scaled base moves accesses
  across the limit.
- **Segment registers.** Adopting the RTL's state for such an entry now
  includes the segment registers.

## Stage labels

The `cpu` spike gains an L1D model behind the data port: two M10K banks of
even and odd dwords, written by the stimulus. The CPU's stores feed the
signature. Labels follow:
- M1 writes the L1D model's address registers and the store distances;
- WB writes the retired-store register;
- D writes `d_seg_busy`;
- AG writes `m1_s`, whose fields Quartus can name after M1's output record
  `m1_out`.

The label counts change only where new registers are (Yosys):

| Stage | Before | After |
| --- | --- | --- |
| Q | 81 | 125 (Q's bytes 7-10 and prefix count, which DC now reads) |
| DC | 254 | 274 |
| D | 454 | 503 (`d_seg_busy`, the limit operands in `ag_s`) |
| AG | 246 | 353 (`m1_s` carries the address, offset and limit) |
| M1 | 246 | 355 |
| M2 | 352 | 421 (`ex_s` carries the loaded value) |
| WB | 939 | 1,643 (576 precomputed limit bits, the retired store) |
| envelope | 1,493 | 1,493 |

Size: 12,333 ALUTs against about 10,400, and 7,796 flip-flops against 6,663.
Quartus fits 7,500-7,542 ALMs against 6,419-6,438.

## Per stage, MHz

nextpnr, seeds 1-4:

| Stage | Before | After |
| --- | --- | --- |
| D | 71.8-87.9 | 73.6-81.3 |
| EX | 75.6-84.0 | 74.3-78.8 |
| DC | 82.1-96.2 | 74.4-80.5 |
| Q | 75.7-83.3 | 76.7-78.3 |
| AG | 75.9-88.9 | 81.8-86.2 |
| M2 | 92.2-105.4 | 81.8-86.5 |
| F2 | 79.7-88.0 | 79.7-85.7 |
| L2 | 87.9-89.4 | 85.0-89.6 |
| L1 | 91.0-94.8 | 90.5-93.3 |
| M1 | 97.5-105.4 | 95.2-111.2 |
| F1 | 94.1-107.9 | 92.5-104.1 |
| WB | 92.7-114.7 | 89.8-96.2 |

Quartus, seeds 1-4:

| Stage | Before | After |
| --- | --- | --- |
| Q | 102.8-106.1 | 102.1-105.6 |
| EX | 109.3-115.6 | 102.7-109.1 |
| DC | 109.9-120.7 | 106.7-115.3 |
| L2 | 109.0-113.6 | 107.7-112.0 |
| M2 | 117.3-127.8 | 110.5-121.3 |
| D | 110.1-115.3 | 114.8-122.9 |
| AG | 132.1-138.1 | 120.6-126.5 |
| M1 | 138.3-142.7 | 123.3-147.5 |

## Next limits

**nextpnr: D, 73.6-81.3 MHz.** Seeds 1, 2 and 4 run from DC's register through
the LEA/address stall (`dfwd.late` or `addr32`) to `issue` and `dc_load`, at
5-8 cells and 71-79% routing. EX follows on seed 3, from its controls into
the flags at WB.

**Quartus: Q, 102.1-105.6 MHz**, as before.

## What the design doc should say

This is reported for the doc's owner to write; the doc is unchanged here.

- **Latency table.**
  - A load directly after a store costs 1 cycle.
  - A memory operand after a real-mode segment load waits until that load
    retires.
- **Stage table, M1 and M2.**
  - M1's limit check is the one compare the doc describes. M1 also takes two
    store distances, which are parallel carry chains.
  - M2's merge has two registered sources: the store in WB and the store that
    retired on the read edge.
- **16-bit EIP.** EIP is not cut to 16 bits, so an instruction ending at FFFFh
  faults on the next fetch.
- **The data port's latency, and the holds behind D, are the next slice.**
