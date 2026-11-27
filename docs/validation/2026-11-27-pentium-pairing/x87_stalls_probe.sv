// SPDX-License-Identifier: MIT
// Read-only simulation bind for the C28 DX replay model.
module x87_stalls_probe(
    input logic clk,
    input logic issue, otherwise_ready, discard, hold, busy, esc_or_wait, fwait,
    input logic board_latch, prior_operation, pending_control, store_value, save_read,
    input logic active_pair, input logic [10:0] older_opcode, head_opcode
);
    // Sample the same pre-edge inputs as the DUT. The host reads these two
    // words after posedge eval, including current RAM/I/O responses.
    logic [11:0] sample_bits /* verilator public_flat_rd */;
    logic [22:0] pair_bits /* verilator public_flat_rd */;
    always_ff @(posedge clk) begin
        sample_bits <= {save_read, store_value, pending_control, prior_operation, board_latch,
                        fwait, esc_or_wait, busy, hold, discard, otherwise_ready, issue};
        pair_bits <= {active_pair, older_opcode, head_opcode};
    end
endmodule

bind fes_i386_cpu x87_stalls_probe p5_x87 (
    .clk(clk),
    .issue(issue),
    // fes_i386_cpu's D issue enable with just x87_hold removed. The host
    // checks issue == otherwise_ready && !hold on every sampled clock.
    .otherwise_ready(cur_valid && !ac_wait && !(addr_use && lea_stall)
        && !(reads_seg && seg_hazard) && !(mem && mseg_hazard)
        && !store_hazard && !flag_hazard && !sp_hazard && !stack_wait
        && !t_wait && !uc_wait && !str_wait && !str_hazard && !io_hazard
        && !(ser_wait && !dc.x_prev) && !(dc.uop.ntchk && nt_busy)
        && !freeze && !halted && !gov_block),
    .discard(redirect || wb_flush), .hold(x87_hold), .busy(trace_x87_busy),
    .esc_or_wait(x87_iss), .fwait(x87.iss_wait),
    // Factor the five terms of fes_i386_x87.hold. The terms can overlap.
    .board_latch(!x87.iss_nowait && x87.latched
        && (x87.iss_wait || (x87.iss_first && !x87.started))),
    .prior_operation(x87.iss_nowait ? (x87.fired || x87.busy)
        : x87.iss_wait ? x87.busy
        : x87.iss_first && (x87.fired || (!x87.started && x87.busy))),
    .pending_control(x87.nw != 0 && (x87.iss_nowait ? !x87.nw_waiting
        : x87.iss_wait || (x87.iss_first && !x87.started))),
    .store_value(!x87.iss_nowait && !x87.iss_wait && x87.val_form
        && x87.seq != x87.S_RDY),
    .save_read(!x87.iss_nowait && !x87.iss_wait && x87.iss_save_rd
        && (x87.iss_part[4:2] + x87.top != x87.ra_live)),
    // fired means the active instruction's first uop left D. A first ESC
    // currently held in D therefore belongs to a different instruction.
    .active_pair(x87.started && x87.fired && x87.iss_first && !x87.iss_wait),
    .older_opcode(x87.cur_op_raw), .head_opcode(x87.iss_op)
);
