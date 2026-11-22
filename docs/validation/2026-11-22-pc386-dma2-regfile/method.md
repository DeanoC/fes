# DE25 pc386 DMA controller 2 as a register file: method (committed before the recorded fits)

Issue #2118. This is the shell area study's **A-dma2** (#2047). There, removing
controller 2 bounded the saving at 412 ALMs (spread 172). The register file keeps
readback, and the study's audit put it at about 250–280.

## Deano's decision, and the mechanism

On 22 Nov 2026 Deano chose **register file, DE25 only**. The DE10 keeps today's
full controller.

**Why not a parameter:** a `pc386_system` parameter could not keep the DE10
netlist byte-identical. Any edit to `pc386_system` moves Yosys's source lines and
auto-names. Measured:
- base: `c56f515f…`;
- with a default-0 parameter: `9a4f3840…`, with about 10,400 differing lines.

**The mechanism instead:**
- The DE25 derivation (`boards/de25nano/pc386/derive.py`, `retarget_at_core`)
  replaces `pc386_at_core`'s `dma2` `fes_dma8237` instance with
  `pc386_dma2_registers`. The module lives in a DE25 pc386 file.
- The DE10 RTL is unchanged.
- The coordinator chose this route on 22 Nov.

## The limit (also in the module's doc comment)

Controller 2 reads back as `fes_dma8237` does, but it never runs a cycle:
- A software request on channels 4–7, or command bit 6 inverting the DREQ sense,
  starts no service.
- That channel's current address and count do not count down.
- No TC status bit is set.
- No terminal count clears the request bit.

Nothing in the machine raises a controller-2 DREQ.

## Evidence

**Differential bench** (`diff/`):
- `fes_dma8237`, wired as `pc386_at_core` wires controller 2, runs against the
  register file, 16 seeds × 2M clocks.
- **Uniform traffic:** read data must match on every clock until the full
  controller starts service.
- **Quiet traffic:** no request writes and no inverted sense. Every clock must
  match.
- **Mutation check:** `diff/mutate.sh` plants six bugs, and the bench must catch
  each one.

**DE10 identity:** `de10_netlist.py` writes the DE10 seal netlist from the same
checkout path, before and after, and `cmp` must find them identical. The recorded
check uses the fixed Yosys lock with one thread.

**DE25 system:** `boards/de25nano/pc386/sim.py` must reach the prompt with check
lines identical to the base's.

## The DE25 measurement

**Board:** the 387-off DE25 pc386 board build, from #2036's head with the fixed
Yosys lock.

**Arms:** the baseline is shared with #2108 and #2117. The arm adds this
branch's `pc386/` changes.

**Seeds:** 1–4 per arm. Seed 1 is the board build; seeds 2–4 are copies of its
project with only `SEED` changed.

**Lease:** each seed holds a Pro slot of the Quartus lease.

**The quantity:**
- Whole-design `ALMs needed`, the median of four seeds.
- **Measured** only when the saving exceeds the larger spread. Otherwise the
  result is reported as "within the spread".
- Area bands are one-sided.
- RAM blocks and DSPs are reported as well.

**Timing:**
- For **every seed** of both arms, report the worst setup, hold, recovery,
  removal and minimum pulse width.
- Every slack must be ≥ 0 at 61.437908 MHz. A miss stops the work and is
  reported.
