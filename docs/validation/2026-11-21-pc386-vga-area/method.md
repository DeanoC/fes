# pc386 VGA area: method (committed before the board fits)

The task: make the pc386 VGA (`pc386_vga`, `pc386_vga_scanout`) smaller without
changing anything a guest can observe, and list the cuts that would change
behaviour without making them.

## What counts as behaviour-preserving

A cut is kept only if the VGA is **cycle-exact at its ports**. Every output of
`pc386_vga`, in every clock, must equal the base commit's:

- `io_done`, `io_rdata`;
- `mem_ready`, `mem_rvalid`, `mem_rdata`;
- every `p1_*` and `cap_*` signal;
- `frame_*`, `start_latched`, `frame_count`, `scanout_underflows`,
  `capture_drops`.

This includes `io_rdata` and `mem_rdata` between their strobes. A cut that
moves any handshake by one clock is a behaviour change. It goes on the
report-only list.

**The proof is a differential bench, [diff/](diff/).**
- The base commit's VGA, renamed `pc386_vga_ref`, runs beside this branch's on
  the same inputs. The bench fails on the first clock where any output
  differs.
- Traffic is random:
  - port bytes to every VGA register, with index writes biased to real
    indices;
  - MMIO reads and writes under every memory map, chain-4, odd/even and
    planar mode, read and write mode, and byte-enable pattern;
  - port 1 answers after a random latency, with random waitrequest;
  - capture sees random waitrequest;
  - reset is pulsed at random mid-run, so RAM contents that a reset does not
    clear are exercised.
- It runs two ways:
  - **RTL:** RTL against RTL, with `PC386_RAM_COLLISION` defined.
  - **Netlist:** the base RTL against the netlist the seal's Yosys recipe
    (`yosys_netlist.SEAL`, locked i386 Yosys) writes for this branch.
- **Mutation check.** Each new invariant gets a planted bug, and the bench
  must fail on it. A bench that passes a planted bug proves nothing.

**The existing suites run as well:**
- `sim_fes_pc386.py --suite vga`, which includes the Bochs VGA replay;
- `--suite system`;
- a fresh lockstep;
- the DE10 reseal and a claimed kit run (FreeDOS and one VGA-graphics game).

## The DE25 measurement

**Board:** the 387-off DE25 pc386 board build from #2036
(`boards/de25nano/pc386/build.py build`). #2036 is not on main, so:
- **Baseline:** #2036's head, `3401bb193`. It already contains main at
  `f3140cb33`, the base of this PR.
- **Arm:** `3401bb193` plus this PR's `pc386_vga.sv` and
  `pc386_vga_scanout.sv`. Nothing else changes.

**Seeds and tools:**
- **Seeds 1–4** for each arm. Seed 1 is the board build. Seeds 2–4 are copies
  of its generated project with only `SEED` changed, as #2035's controlled
  pair did.
- **Quartus:** Prime Pro 26.3 on Powerboat, A5EB013BB23BE4SCS.
- **Lease:** every Quartus run, including each seed, holds a slot of
  Powerboat's Pro lease pool (`/tmp/fes-quartus-pro-1.lock`, then
  `/tmp/lander-quartus-powerboat-pro.lock`; PR #2050's pool). A slot is taken
  per seed.
- **Netlist identity:** each arm's seeds 2–4 compile the exact machine
  netlist bytes (`fes_mailbox/pc386_system.v`) that its seed 1 staged, and the
  run records their SHA-256. The machine elaborates at a fixed path, so a
  revision's netlist is reproducible (yosys#80).

**The quantity:**
- **ALMs:** whole-design `ALMs needed`, the median over four seeds per arm.
- **Saving:** the baseline median minus the arm median.
- **Measured** only if the saving is larger than the larger of the two arms'
  spreads (max − min). Otherwise it is reported as "within the spread".
- **Bands:** area bands are one-sided. The arm fails only if it grows.
- **Also reported:** RAM blocks (M20K and MLAB) and DSPs.

**Timing:**
- Worst setup, hold, recovery, removal and minimum pulse width over every
  clock and corner, per seed.
- Every seed of the arm must close, with every slack ≥ 0, with the board's
  SDC as it is (system clock 61.437908 MHz).
- No SDC, timing or fitter setting is changed. If a seed fails, I stop and
  report; I do not relax anything.

**Cuts below fit noise:** a cut smaller than about 100 ALMs sits inside fit
noise, so the kept cuts are fitted together as one arm. Per-cut figures come
from single-seed VGA-only fits and the locked-Yosys bench, and are labelled as
estimates.

### Amendment, 21 November 2026, before any board fit finished

The first baseline build took Lander's legacy Pro slot
(`/tmp/lander-quartus-powerboat-pro.lock`). A Lander signoff job then waited
on that slot with a one-hour limit. I stopped both of my board runs before
either finished a fit; no figure comes from them.

The amended lease rule:
- Every board run takes only `/tmp/fes-quartus-pro-1.lock` and waits for it.
- The baseline and the arm run one after the other.

The arms, seeds, netlist rule and bands are unchanged.
