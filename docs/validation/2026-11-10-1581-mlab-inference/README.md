# #1581 S1d: return-stack MLAB inference, 10 November 2026

Spike S1d of [RTL front-end lanes](../../design/rtl-frontend-lanes.md)
(tracker [#1581](https://github.com/DeanoC/fes/issues/1581), issue
[#1629](https://github.com/DeanoC/fes/issues/1629)). It builds the i386
return stack (`fes_i386_ras`, from the Phase 4 RAS slice) in three forms,
each through the pc386 yosys→Quartus flow and through Quartus reading the
SystemVerilog directly, and answers open decision D9: does a one-write-port
form infer an MLAB and save at least 150 ALMs? No production code changes;
the variants are spike sources in `rtl/`.

**Result.**

- **MLAB inference works under 17.0.2.** Both one-write-port forms infer
  an MLAB in both flows: `Info (276038): Inferred altdpram megafunction`,
  RAM summary type `MLAB`, 512 bits in 20 memory ALMs. The 276007 and 276004
  warnings that left the OPL2 memories in logic appeared in none of the
  eight builds.
- **Area (yosys lane, the one pc386 seals).** The current form becomes
  registers: Yosys's `memory_map … r:WR_PORTS>1` rule maps the two-write
  stack before Quartus sees it. The harness build takes **483 ALMs and 671
  registers**. The one-write-port, async-read form (v2) takes **77 ALMs and
  153 registers**: **406 ALMs and 518 registers fewer**, well over the
  150-ALM bar.
- **Function.** v2 is cycle-exact with the current form (200,018 clocks of
  push, pop, redirect, wrap and held-reset traffic, Verilator). It needs no
  CPU change. The registered-read form (v3) returns `top` one clock late,
  which the front end cannot use as it stands.
- **Timing.** Every build meets every analysis at the pc386 system clock
  (61.44 MHz) at all four corners. v2 raises the harness Fmax from 145.8 to
  276.9 MHz at the slow 100C corner.
- **D9: go.** S2 should land with form v2, the one-port async-read stack
  the design doc's P1 already specifies. S2's own pc386 fit measures the
  real in-CPU saving and slack; this harness only shows the direction and
  that inference happens.

## Results

Six builds the spike asked for, plus v3b, a control added to see what a
registered read costs once its latency is hidden. Device 5CSEBA6U23I7 (the
pc386 producer's `board.TARGET`), seed 1, 61.44 MHz constraint.

ALMs are "ALMs needed" for the whole harness (the harness is identical in
every build, so differences are the return stack's). Fmax is the harness's,
at the two slow corners; the worst path differs between builds.

| Flow | Form | Stack mapped to | RAM messages | ALMs | RAS-only ALMs | Registers | Memory bits | Fmax slow 100C / −40C (MHz) | Worst setup / hold slack (ns) |
|---|---|---|---|---:|---:|---:|---|---|---|
| yosys→Quartus | v1 current | none (FFs) | none | 483 | (flattened) | 671 | 0 | 145.75 / 150.08 | 9.415 / 0.161 |
| yosys→Quartus | v2 one port, async read | **MLAB** | 276038 | **77** | (flattened) | 153 | 512 MLAB | 276.85 / 279.17 | 12.664 / 0.125 |
| yosys→Quartus | v3 one port, registered read | **MLAB** | 276038 | 77 | (flattened) | 157 | 512 MLAB | 246.12 / 253.16 | 12.213 / 0.122 |
| Quartus direct | v1 current | **M10K** (AUTO) | 276029, 276020 | 62 | 27.9 | 201 | 512 M10K (1 block, 10,240 impl. bits) | 189.83 / 187.37 | 10.939 / 0.150 |
| Quartus direct | v2 one port, async read | **MLAB** | 276038 | 80 | 36.3 | 158 | 512 MLAB | 260.35 / 255.56 | 12.363 / 0.136 |
| Quartus direct | v3 one port, registered read | **MLAB** | 276038 | 80 | 36.7 | 153 | 512 MLAB | 273.52 / 272.26 | 12.603 / 0.160 |
| yosys→Quartus | v3b control: registered read at next pointer, bypass | MLAB | 276038 | 92 | (flattened) | 182 | 512 MLAB | 176.30 / 181.03 | 10.604 / 0.140 |
| Quartus direct | v3b control | MLAB | 276038 | 92 | 48.3 | 184 | 512 MLAB | 184.30 / 189.93 | 10.850 / 0.138 |

"RAS-only ALMs" is the `fes_i386_ras:ras` row of the fitter's by-entity
table, which exists only where Quartus keeps the hierarchy (the yosys flow
flattens). Every MLAB row uses 20 ALMs for memory (32 memory ALUTs,
32-address deep); the RAS-only figure includes them. "Fmax" is the
unrestricted and the restricted Fmax, which are equal in every build.

Per build: `reports/<flow>-<form>/` holds `map-ram.txt` (RAM messages, the
Analysis & Synthesis RAM summary, registers packed into the RAM),
`fit-extract.txt` (fit summary, resource usage, by-entity table, Fitter RAM
summary), `sta-extract.txt` (every corner's slack, the slow-corner Fmax
tables, unconstrained paths), the `.qsf`, and `result.json`. The yosys
builds also keep the Yosys program, log and the netlist Quartus read.
`reports/summary.json` has all eight plus the tool identities.

### Observations

- **The current form is only expensive on the yosys lane.** Quartus reading
  the SystemVerilog itself merges the two write sites (they are in one
  `always_ff` under an if/else chain) and infers a RAM: an `altsyncram` it
  places in an M10K, with the read address taken from `ptr`'s next value and
  32 pass-through registers (276020) for read-during-write. That build is
  62 ALMs. On the yosys lane, `memory_collect` keeps two write ports, and
  pc386's `memory_map t:$mem_v2 r:WR_PORTS>1 %i a:ramstyle %d` maps the
  memory to registers (`reports/yosys-v1_current/yosys.log`, "Mapping memory
  \ras.stack"). This is #1581's "30 memories with more than one write port"
  in miniature. It belongs to the lanes work, not to S2; S2 removes the
  second port at the source, which serves both lanes.
- **The harness inflates the current form.** In the yosys flow the current
  stack costs about 400 ALMs in this harness, against the 289 the area audit
  measured in the CPU (`docs/validation/2026-11-08-i386-area-audit/`). In
  the harness the 16×32 read mux and the write decode cannot share logic
  with anything else. The in-CPU saving is S2's to measure; at the audit's
  289 ALMs, less about 20–40 for the MLAB form, it is still over 150.
- **Quartus packs the registered read into the RAM.** In v3 (both flows)
  the `top` register becomes the MLAB's output register ("Registers Packed
  Into Inferred Megafunctions"). The harness's `top_q` register sits behind
  a two-way select so it cannot be packed in the same way: the CPU uses
  `top` through `btb_target = btb_entry.ret ? ras_top : btb_entry.target`
  (`fes_i386_fetchbuf.sv:155`), so the harness copies that shape. Without
  the select, Quartus moved `top_q` into the v2 MLAB and the harness would
  not have matched the CPU.

## The forms

`rtl/ras_v1_current.sv` is `fes_i386_ras.sv` from main at `97a589e89`,
unchanged. It writes the stack at two sites under one reset priority: the
redirect branch writes `stack[set_ptr]` when `set_wr`, else the push branch
writes `stack[ptr + 1]`. The read is `top = stack[ptr]`.

### v2: one write port, asynchronous read (`rtl/ras_v2_async.sv`)

The two sites never fire together, and reset writes no entry, so they fold
into one port, exactly as the design doc's P1 gives:

```
we = !reset && (redirect ? set_wr : push)
wa = redirect ? set_ptr : ptr + 1
wd = redirect ? set_lin : push_lin
```

The pointer update is unchanged and the read stays `top = stack[ptr]`. The
stack carries `(* ramstyle = "MLAB, no_rw_check" *)`; the yosys netlist
keeps it (`reports/yosys-v2_async/s1d_harness.yosys.v`), since pc386 strips
only `src` and `hdlname`.

- **Behaviour and latency:** none changed. `ptr` and `top` equal the current
  form's on every clock (see Equivalence).
- **What the CPU needs:** nothing beyond the module. The ports are the same.
- **`no_rw_check` is safe.** The MLAB's read is asynchronous on `ptr`; a
  write lands at the clock edge and `top` shows it from the next clock, as
  the flip-flop form does. The only cycle where the write address can equal
  the read address is a redirect with `set_wr` and `set_ptr == ptr` (a push
  writes `ptr + 1`, never `ptr`). In that cycle the front end ignores `top`:
  fetchbuf loads `mem_block` from `redirect_linear`, not `btb_target`
  (`fes_i386_fetchbuf.sv:151`), and the redirect branch of its register
  block loads nothing from `btb_target` (`:259-270`; `ras_top` has no other
  reader, `:155`). So whatever an MLAB returns for a read during its own
  write in that cycle never reaches a result. From the next clock on, `top`
  shows the written entry in both forms. The bench below runs on the RTL,
  so it checks the port merge, not the silicon read-during-write
  behaviour; the argument above is what covers that cycle.

### v3: one write port, registered read (`rtl/ras_v3_regread.sv`)

The same write port; `top <= stack[ptr]` is a register.

- **Behaviour and latency:** `top` at clock t is the current form's `top` at
  t−1 (checked, latency 1). After every push, pop or redirect the first
  clock's `top` is stale.
- **What the CPU would need:** F2 uses `ras_top` in the same clock that it
  decides `pred_now` and pops (`fes_i386_fetchbuf.sv:154-158`), so either a
  read at the next pointer (v3b) or a clock of delay on a predicted RET that
  follows any stack movement, which costs RET prediction and needs its own
  DOOM rerun. Nothing in v3 is worth that: v2 already infers the MLAB with a
  better Fmax.

### v3b control: registered read at the next pointer (`rtl/ras_v3b_regread_bypass.sv`)

Reads `stack[ptr_next]` into the `top` register and bypasses the write data
when there is a write (every write goes to the entry the pointer moves to).
It is cycle-exact with the current form after the first clock, so it shows
the price of hiding v3's latency: +15 ALMs over v2 and about 100 MHz less
harness Fmax, because `ptr_next` (from `pop`, which is `pred_now`, from the
BTB compare) becomes the RAM read address. In the CPU that path starts at
the BTB lookup, which is already a long one. Not recommended.

## Equivalence

`sim/s1d_tb.sv` drives one form with a fixed xorshift sequence: it first
writes all sixteen entries by redirect, then 200,000 clocks in phases of
random length (1–200 clocks) of five kinds: everything random including
overlapping requests, push-heavy (wraps up), pop-heavy (wraps down),
frequent redirects, and reset held with every write request asserted. It
prints the inputs and `top` on each clock. `scripts/compare_traces.py`
checks that the stimulus is identical and that each variant's `top` equals
the current form's at the stated latency. `scripts/run_equiv.sh` runs all
of it.

```
Verilator 5.032 2025-01-01 rev (Debian 5.032-1+b2)
v2_async:           PASS: 200018 clocks compared at latency 0 (78845 writes, 40788 reset clocks)
v3_regread:         PASS: 200016 clocks compared at latency 1 (78845 writes, 40788 reset clocks)
v3b_regread_bypass: PASS: 200017 clocks compared at latency 0 (78845 writes, 40788 reset clocks)
control:            v3_regread differs from current at latency 0, as expected
```

The 40,788 reset clocks include the held-reset phases, where a write is
requested on every clock; any write under reset would show as a later `top`
mismatch. This is a bench of the module on its own, with Debian's Verilator
5.032 (not the lock's pinned Verilator build). It is not a lockstep run;
S2 owes lockstep on RTL and netlist, test386 and a seal, as the plan says.

## Method

`scripts/run_s1d.py REPO WORK [BUILD …]` builds each form in each flow, one
at a time:

- **yosys flow:** the pc386 producer authenticates the lock's Yosys
  (`_authenticate_tools` with the shared toolchain cache), and the spike
  calls the producer's own `machine_program()` with the spike's sources.
  Only `hierarchy -top` changes: the top is `s1d_harness` and the guest-rate
  `-chparam`s are dropped. Every other pass is as sealed: `proc -norom;
  flatten; opt -purge; memory_collect; memory_map t:$mem_v2 r:WR_PORTS>1 %i
  a:ramstyle %d; opt -purge; attrmap -remove src; attrmap -remove hdlname`.
  The netlist goes through the producer's `plain_identifiers()`, and Quartus
  reads it as `VERILOG_FILE`.
- **direct flow:** Quartus reads `rtl/s1d_harness.sv` and the form as
  `SYSTEMVERILOG_FILE`.
- **Quartus project:** the producer's `project_qsf` settings that apply to a
  block without the shell: family, device, seed 1, `VERILOG_INPUT_VERSION
  SYSTEMVERILOG_2005`. `NUM_PARALLEL_PROCESSORS 2`, not `ALL`, because the
  host was shared. No other assignment, so every synthesis and fitter option
  is the 17.0.2 default (as in the pc386 seal). `quartus_map`,
  `quartus_fit`, `quartus_sta`, each capped at 15 minutes; no assembler.
- **Harness (`rtl/s1d_harness.sv`), identical for every build:** a 73-bit
  shift register from one pin drives every return-stack input from a
  register; `top` goes through a two-way select (the CPU's shape) to a
  register, then into a rotating 32-bit signature that leaves on one pin.
  Every timed path is register to register; the only unconstrained paths
  are the two data pins.
- **Constraint (`rtl/s1d.sdc`):** one 16.276 ns clock (61.44 MHz, pc386's
  `SYSTEM_CLOCK_HZ`) and `derive_clock_uncertainty`. No exceptions.

Tool identities (`reports/summary.json`):

| Tool | Identity |
|---|---|
| Quartus | Prime Lite 17.0.2 Build 602 07/19/2017 SJ; `quartus_sh` sha256 `71035cc10a244002d1b07bef128c7cd429f34ccd69b417c0a80f47c639d8ab93` |
| Yosys | pc386 lock, DeanoC/yosys `0a7927bf126eb3d868c50b3bb5e11b69ff656ea9`, binary sha256 `2f204840f5842a04b1e2a4a3c4d25a8ea4c7263c57efbedf82ced4328d76c6ed` ("Yosys 0.70+ (git sha1 0a7927bf1)") |
| Verilator (bench only) | 5.032 2025-01-01 (Debian 5.032-1+b2) |

Run on Powerboat, 10 November 2026, from main `97a589e89`, alongside other
agents' Quartus jobs. Each build took 70–110 s. No kit, no GPU.

To rerun:

```
export CACHE_ROOT=<shared misteross toolchain cache> QUARTUS_ROOTDIR=<17.0.2 quartus dir>
python3 docs/validation/2026-11-10-1581-mlab-inference/scripts/run_s1d.py "$PWD" <work dir>
docs/validation/2026-11-10-1581-mlab-inference/scripts/run_equiv.sh <work dir>
```

`run_s1d.py` rewrites `reports/`. The Quartus project directories stay in
the work directory and are not part of this record. In the committed Yosys
logs, programs and netlists, the checkout and work paths are written as
`<repo>` and `<work>`.

## Not covered

- The in-CPU saving and slack. The harness shows inference and direction;
  S2's pc386 fit is the measurement D9's bar applies to.
- One seed per build. The harness Fmax differences of a few tens of MHz
  between the MLAB rows are within what a seed can move on a design this
  small; the v1-yosys and v3b gaps are larger.
- Whether Quartus direct would put the current form in an MLAB with the
  attribute added. Not needed for S2, which removes the second port; a
  follow-up for the Quartus front-end lane if it matters there.
