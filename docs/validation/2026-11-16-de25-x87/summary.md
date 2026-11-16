# DE25 pipelined x87 arithmetic backend

The standalone `fes_x87_pipe` passes its fixed **200 MHz** virtual-pin Quartus
fit and **290,640** arithmetic requests against real host x87 instructions,
with zero mismatches. This is a misteross arithmetic building block for a
future DE25 386, not a complete 387, CPU integration or playable core.

Base: FES `333900cbbdd86cce6d361977b2dd277d539ce3d3`.
The exact working-copy RTL and simulation inputs are bound in [host.json](host.json),
and the fitted RTL/build inputs in [quartus-inputs.json](quartus-inputs.json).
Every source hash matches the delivered source. Committing those bytes does
not change which bytes were compiled. The component's
[contract and commands](../../../sources/misteross/boards/de25nano/x87/README.md)
describe interfaces, pipeline stages, unsupported operations and FP32 research.

## Arithmetic and throughput

`python3 sources/misteross/boards/de25nano/x87/run.py --samples 2048 --jobs 8`
uses Verilator 5.020 and the host's actual x87 instructions, saving/restoring
its complete legacy floating-point environment. This is a modern x87 reference,
not a physical 387 capture or a TestFloat qualification.

- **368,090 checks; 290,640 arithmetic requests; zero failures.** Add, subtract,
  multiply, divide, square root and ordered/unordered compare; all PC/RC modes,
  operand classes, denormals and pseudo-denormals, unsupported formats, NaN
  payloads, signed zero, overflow and the tiny-after-precision-rounding boundary.
- **4,096 fast requests and responses in 4,096 clocks**, 15-clock response
  latency. This includes the actual completion RAM interface.
- Dependent add/multiply: 16 clocks/operation including the bench handoff.
  PC64 divide and square root: 76 clocks/operation. At the fitted 200 MHz,
  these correspond to 12.5 million dependent adds/multiplies per second and
  about 2.63 million dependent divides/square roots per second.
- A full 16-entry queue, mixed fast/slow ordering, random backpressure,
  held responses, tag wrap, flush and host-environment preservation pass.

These are arithmetic-backend rates. They do not measure a 386 instruction
stream, memory traffic, stack forwarding, game frame rate or game compatibility.

## Quartus fit

Quartus Pro **26.3.0 Build 114**, device **A5EB013BB23BE4SCS**, seed **1**,
fixed clock **5.000 ns**. The compiler ran on the 3090 builder under
`/tmp/lander-quartus-3090.lock`, with over 30 GiB free, 30-second heartbeats
and a 30-minute time cap. No GPU job, board programming or kit access ran.

| Resource | Used | Device total |
| --- | ---: | ---: |
| ALMs | 2,660 | 46,800 |
| Dedicated logic registers | 4,439 | — |
| DSP blocks | 9 | 188 |
| M20Ks | 4 | 358 |
| Logical block-memory bits | 1,568 | 7,331,840 |

[quartus.json](quartus.json) is the passing compiler/gate record;
[timing.tsv](timing.tsv) records worst paths and coverage. The gate explicitly
enumerates all four available operating conditions with `-all`.

| Corner | Setup slack | Internal register setup | Hold slack | Minimum pulse slack |
| --- | ---: | ---: | ---: | ---: |
| Slow fix4 100C | +0.795 ns | +1.244 ns | +0.004 ns | +1.978 ns |
| Slow fix4 0C | +0.228 ns | +0.704 ns | +0.052 ns | +1.968 ns |
| Slow fix4a 0C | +0.276 ns | +0.725 ns | +0.006 ns | +1.968 ns |
| Fast fix4 0C | +2.242 ns | +2.657 ns | +0.001 ns | +2.101 ns |

Recovery/removal have no paths because all resets are synchronous and are
reported explicitly as N/A. TimeQuest reports zero illegal or unconstrained
clocks. Virtual input/output delays are explicitly constrained. No board pin,
external I/O or clock-crossing contract is established.

The diagnostic has one expected medium Design Assistant finding: registers
are not reached by Agilex Reset Release IP, because this backend is driven by
its caller's synchronous reset and the virtual-pin project has no board wrapper.
A physical consumer must provide configuration reset release. This finding is
not suppressed, waived or counted as physical acceptance.

The unsuccessful fits changed RTL rather than clocks or placement seeds:

| Design | ALMs | Worst setup at 5 ns | Change informed by timing |
| --- | ---: | ---: | --- |
| Direct ingress | 2,936 | −2.089 ns | Register the request boundary |
| Retimable request register | 3,012 | −0.234 ns | Preserve that external timing boundary |
| Preserved raw request | 3,038 | −1.561 ns | Cut normalization and divide initialization |
| Shared registered normalization | 2,660 | **+0.228 ns** | Passing delivered implementation |

Raw fitted databases/reports remain under the task worktree's ignored
`sources/misteross/build/diagnostic/de25-x87/run-{initial,dispatch,boundary,normalized}/`.
The final source snapshot, helper and constraints are unchanged from the
passing run. Shared normalization removes duplicate logic while providing
real stage cuts; the slow lane shares a 68-bit subtractor.

## Scope and integration

No module ABI, package definition, shipped core, toolchain pin or default 387
identity changes. The new transaction interface is local to this standalone
backend. Existing DE10 x87 development remains separate.

Hardware evidence class: **host-only arithmetic simulation plus an internal
FPGA fit**. HIL is N/A for this change because no kit-run artifact consumes it.
There is no hardware or game acceptance and no deployment artifact.

Next integration: connect the existing x87 architectural state/commit machinery
to this decoded arithmetic interface, provide stack dependency handling and
unmasked exception results, implement the remaining operations, then fit the
complete DE25 CPU and measure the games. The FP32 expansion alternative is
assessed in the component README; no fitted FP32 alternative is claimed.
