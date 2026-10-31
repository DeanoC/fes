# Reset-domain review of the original HPS diagnostic

This static audit concerns producer `4b7b88775ca41e5cd680f1510419d1a368d057dd`, SOF SHA-256
`3bd8ff20634b3401a15a0d5458e49590c539bb1e9e6d7c5fe4999a6703f49250`. Its
`output_files/golden_top.tq.drc.signoff.rpt` SHA-256 is
`74749725abd6ca81dee638432616415dc07bbd1f44d43880f8e5d692e274ad0b`. That report groups **15 reset chains**
under one RDC-50003 result in `pll_inst|iopll_0_outclk0`; this is not evidence
of 15 observed functional failures. [Reviewed RTL hashes](rdc-source-sha256.json)
identify the retained files under the original private project's root.

[Altera's RDC-50003 guidance](https://resources.altera.com/quartushelp/24.3/da_rules/rdc_50003.htm)
allows a documented token/handshake argument for protected crossings. Vendor
origin, chain count and ordinary valid/ready alone do not provide that argument.

| Original chains | Local source evidence | Remaining boundary |
| --- | --- | --- |
| 3 JTAG sink-crossers | Merged endpoint resets and returned-toggle acknowledgement block transfers until the remote endpoint advances after reset. | This protects the inspected clock-crosser exchange, not its entire master/interconnect boundary. |
| 3 JTAG clock-sense chains | Reset reaches only a clock-sensor DEBUG telemetry bit. | No command/response reset fanout was found; this is not physical or MTBF qualification. |
| 4 HPS ready-latency adapters | Separate two-register reset synchronizers and FIFO valid/ready plumbing. | No demonstrated peer-reset completion or traffic-quiescence contract. |
| 2 top/peripheral subsystem controllers | Local reset sequencing; no global reset-completion acknowledgement. | Cross-boundary release/traffic coordination remains unproven. |
| 3 private JTAG master controllers | Command state resets locally; bus acceptance uses ordinary waitrequest. | No acknowledgement proves that the shared interconnect has left reset. |

Thus **six chains have bounded local protection/isolation evidence** and
**nine chains still need outer-boundary coordination or quiescence proof**.
These counts classify inspected chains; they are not whole-boundary signoff,
nine proven data-corruption faults, or grounds to waive the aggregate rule.

## Exact source anchors

All paths below are relative to the original private `project/` identified in
the hash record. For JTAG paths, `<M>` is each of `fpga_m`, `hps_f2sdram` and
`hps_m`; the common RTL copies have identical bytes, while wrapper instance
names differ. The hash record lists every concrete path.

- `jtag_subsys/ip/jtag_subsys/<M>/altera_jtag_dc_streaming_191/synth/altera_avalon_st_clock_crosser.v:74-156`:
  ready XOR, merged resets, zeroed toggles and forward/returned handshake.
  In the same directory, `altera_jtag_dc_streaming.v:230-246` selects backward
  synchronization depth eight.
- `jtag_subsys/ip/jtag_subsys/<M>/altera_jtag_dc_streaming_191/synth/altera_jtag_streaming.v:210-240,478`:
  clock-sensor synchronization/reset and DEBUG sampling.
- `jtag_subsys/ip/jtag_subsys/<M>/altera_jtag_avalon_master_191/synth/<M>_altera_jtag_avalon_master_191_unfwqdy.v:304-332`:
  private depth-two controller, reset-request support disabled.
  `jtag_subsys/ip/jtag_subsys/<M>/altera_avalon_packets_to_master_1922/synth/altera_avalon_packets_to_master.v:221-242,364,423`
  resets command state and implements ordinary bus acceptance.
- `jtag_subsys/jtag_subsys/synth/jtag_subsys.v:40-69` distributes common reset
  to the three masters and leaves their `master_reset` outputs unconnected.
- `hps_subsys/ip/hps_subsys/agilex_hps/intel_sundancemesa_hps_100/synth/agilex_hps_intel_sundancemesa_hps_100_z5bqzvi.v:1544-1553`:
  HPS-to-FPGA and lightweight-HPS-to-FPGA reset synchronizers.
- `hps_subsys/ip/hps_subsys/agilex_hps/intel_sundancemesa_mpfe_100/synth/agilex_hps_intel_sundancemesa_mpfe_100_u4663zy.sv:419-429,579-589`:
  F2SDRAM and F2SoC reset synchronizers.
- `hps_subsys/ip/hps_subsys/agilex_hps/intel_sundancemesa_hps_100/synth/ready_latency/ready_latency_reset_synchronizer.sv:35-47`
  supplies two-register release. In that directory,
  `ready_latency_adapter.sv:80,98,110,124` clears its FIFO and derives ready/valid
  from FIFO state, without peer-reset completion. The MPFE reset-synchronizer
  copy is also bound in the hash record.
- `qsys_top/synth/qsys_top.v:127,576,613,695,697` distributes the common reset
  into the four HPS bridge domains. Lines `1105-1135` configure the top reset
  controller; reset request is an output and request inputs are tied zero.
- `peripheral_subsys/peripheral_subsys/synth/peripheral_subsys.v:199-229`
  configures the separate peripheral controller with reset-request support off.

## Scope of the reset repair

The proposed PLL-lock-aware, two-register top reset introduces a new chain and
changes the reset source topology. It requires a **fresh fitted reset/clock
inventory and timing/DRC reports**; this original 15-chain audit cannot be
relabeled as acceptance of that result. Its narrow asynchronous-clear ingress
exception must leave interstage data and released-reset recovery/removal paths
timed. No blanket RDC waiver or hardware/MTBF acceptance is supplied here.

No device was programmed or physically exercised for this audit.
