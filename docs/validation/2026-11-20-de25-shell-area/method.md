# DE25 pc386 shell and AT-device area study: method (committed before the fits)

Measure only. No RTL changes, and no RTL-change PR. Every ablation runs on a
scratch commit on chip-30x0-ai-dev, and its patch is kept in this record.

Quartus Prime Pro 26.3 on chip-30x0-ai-dev, Agilex 5 A5EB013BB23BE4SCS.

## Two arms

**Shell arm (387 off).**
- **Baseline:** DE25 slice 3's board build `j`. That is draft PR #2036 at
  `bc0a124ba`, built by `boards/de25nano/pc386/build.py build`. Its derived
  machine netlist is SHA-256 `5b80c0cd…`.
  - Seed 1 is the board build itself.
  - Seeds 2–4 are #2035's `pair-j-board-s2..4`: the same project with only
    `SEED` changed.
- **Each shell ablation:** one scratch commit on `bc0a124ba`, built the same
  way (seed 1), then seeds 2–4 by changing only `SEED`.
- **Netlist check:** the machine netlist must hash to `5b80c0cd…`. If it does
  not, the ablation is reported as not comparable.
- This arm is 387-off, because #2036 is. Shell cuts do not depend on the 387.

**AT arm (387 on).**
- **Fit:** the #1899 virtual-pin envelope, `scripts/fit_i386_device.py
  --design machine --device de25`, flat.
- **Baseline:** the #2037 head (`4f1546783` plus `identity-on.patch`).
  - The per-entity ranking (hierarchy kept, seed 1, 43,018 ALMs) is used only
    to choose candidates.
  - I elaborate the baseline netlist myself, unpatched. If its SHA-256 equals
    #2037's `elab-head` netlist, #2037's `out-head-s1..4` are the baseline.
    Otherwise I fit my own four-seed baseline.
- **Each AT ablation:** a patch on that tree, elaborated by the same script,
  with seeds 1–4.

## The quantity

- **ALMs:** the whole-design `ALMs needed [=A-B+C]` row from the fit summary.
  Entity rows rank candidates; they are never the measured saving.
- **Resources:** RAM blocks (M20K) and DSP blocks from the fit summary.
- **Timing:** worst setup, hold, recovery, removal and minimum pulse width,
  reported as they come. No SDC or timing setting is changed in any arm.

## What is reported per candidate

- **The ablation bound:** the baseline median minus the ablation median, over
  four seeds each, with each arm's spread (max − min).
- **Measurability:** a bound counts as measured only if it is larger than the
  larger of the two spreads. Otherwise it is reported as "within the spread".
  This rule decides measurability only. It is not a pass band.
- **Bands:** area bands are one-sided (Deano, 20 November 2026). An ablation
  is never failed for being smaller.
- **Positive control:** the fit report shows the removed entity gone, or the
  changed parameter in effect. An ablation without a positive control is
  reported as not measured.
- **Which board each cut affects:**
  - DE25 pc386 only;
  - every DE25 core on the managed shell;
  - DE10 as well.
- **Cost:** functional and compatibility cost. For device cuts, also the cycle
  cost at game-census rates.

## Deano's rule for cycle-for-area trades

A cut that adds cycles is allowed only on the DE25 387-on build. DE10 and
387-off builds keep today's timing through a per-board parameter, as
`X87_PRESENT` does: one RTL path, no shim. Any cut that can't be expressed that
way is flagged.

## Ablations are upper bounds

An ablation removes, or reconfigures, the logic outright. A real cut keeps
some of it, so a bound is the most that cut can save, not a forecast.

## Shell candidates (fixed now)

1. **S-ocm:** #1956's commits `5a1b17149`, `aca62ea12` and `5a414780a`,
   cherry-picked onto `bc0a124ba`, with its `linux_dma.py` conflict resolved
   to keep #2036's window routes.
   - It removes the vendor on-chip memory and sets `H2F_Width` to 0.
   - Positive control: `ocm` and `hps2fpga_axi4_rl_adp_inst` are absent.
2. **S-rsp:** Qsys `qsys_mm.responseFifoType` set to
   `EMBEDDED_MEMORY_BASED` in `peripheral_subsys` and `qsys_top`. The vendor
   default is `REGISTER_BASED`.
   - Positive control: the agent response FIFOs show M20K or MLAB use, and
     their logic ALMs drop.
3. **S-lat:** `qsys_top` `qsys_mm.maxAdditionalLatency` set from 4 to 0.
   - Positive control: the `mux_pipeline`, `agent_pipeline` and
     `limiter_pipeline` stages in `mm_interconnect_0` are gone or reduced.

**Not ablated, entity bound only:**
- **The second F2SDRAM master:** `mm_interconnect_0` is 2,054 ALMs, against
  770 on the stock Spectrum reference. The alternative, merging the window
  into the scanout master before Qsys, is an RTL design change in #2036's
  files.
- **Vendor HPS adapters** (`sm_hps`, `sm_mpfe`).

## AT candidates

Fixed in a dated amendment below, after the RTL audit and before any AT fit.

### Amendment, 20 November 2026: how the shell arm meets the netlist rule

The first shell ablation's board build, S-ocm seed 1, prepared a machine
netlist that hashes to `514306fb…`, not `5b80c0cd…`. A diff shows only two
kinds of difference: the worktree path in `src` attributes, and Yosys
autoname indices, which follow that path. No cell changed.

The rule stays as written: every arm fits `5b80c0cd…`. To meet it, each
shell ablation is generated by its own board build. Then each of seeds 1–4
is compiled from a copy of that generated project, with build j's
`fes_mailbox/pc386_system.v` copied in. The script records each copy's
SHA-256.

- The board build's own seed-1 compile is not used.
- The first run's seeds 2–4 were stopped before they finished, and are not
  used.

This changes no band.

### Amendment, 20 November 2026 (before any AT fit)

**Own AT baseline.** My unpatched elaboration of the #2037 tree hashes to
`cc9b0a89…`. #2037's `elab-head` hashes to `1fb3e286…`, because the netlist
depends on the source path. The AT arm therefore fits its own baseline,
`base`, at seeds 1–4, and #2037's fits are not used as the baseline.

**AT ablations.** Each is a patch in `patches/`, applied to a copy of the
baseline tree.

1. **A-dma2** (`dma2.patch`): controller 2 removed. Reads return `FFh`, and
   there is no hold. Upper bound for a "registers only" controller 2.
   - Positive control: no `dma2` cells in the netlist.
2. **A-fifo** (`fifo.patch`): the PS/2 FIFO (keyboard and mouse) gets one
   write port and `ramstyle = "MLAB"`.
   - Behaviour is kept: the clear path's push is folded into the same port.
   - Positive control: MLAB use in the fit, and the FIFO registers gone from
     the netlist.
3. **A-kbcram** (`kbcram.patch`): 8042 RAM bytes 1–31 dropped. Reads return 0
   and writes are ignored. Byte 0, the command byte, is kept.
4. **A-pitbcd** (`pitbcd.patch`): KF8253 BCD counting dropped. Only the binary
   decrement feeds the counter.
   - Positive control: no `dec_bcd_q` registers in the netlist.

Each ablation's netlist must differ from `base`'s only through its patch.
That is checked by the patch's positive control. Byte-identity outside the
patch is not reachable, because Yosys autonames shift.

## Screening

There is no seed-1 screening. Every candidate gets four seeds.
