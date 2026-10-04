// SPDX-License-Identifier: GPL-2.0-or-later
// Controller-level rate, timeout and stop checks without a video or CPU shell.
module byte_bench (
    input wire clk, reset, stop, stall_ack,
    input wire [1:0] rate,
    output wire pass, fail, stopped, timed_out,
    output wire [7:0] completed,
    output wire [15:0] checks,
    output wire [25:0] fault_addr,
    output wire [2:0] fault_step,
    output wire [1:0] fault_be,
    output wire [15:0] fault_payload, fault_expect, fault_got,
    output wire [31:0] refresh_masked_writes
);
    wire start, write, done;
    wire [25:0] addr;
    wire [15:0] wdata, rdata;
    wire [1:0] be;
    wire pin_clk, cke, ncs, nras, ncas, nwe, dqml, dqmh, oe;
    wire [1:0] ba;
    wire [12:0] a;
    wire [15:0] dq_out;
    wire [15:0] dq;
    reg [15:0] rise = 16'd0, fall = 16'd0;
    reg [15:0] rise_q = 16'd0, fall_q = 16'd0;
    assign dq = oe ? dq_out : 16'hzzzz;
    always @(posedge clk) begin
        rise <= dq;
        rise_q <= rise;
        fall_q <= fall;
    end
    always @(negedge clk) fall <= dq;
    sdram_byte_lane lane (
        .clk(clk), .reset(reset), .stop(stop), .start(start), .write(write),
        .addr(addr), .wdata(wdata), .byte_enable(be), .done(done && !stall_ack), .rdata(rdata),
        .pass(pass), .fail(fail), .stopped(stopped), .timed_out(timed_out),
        .completed(completed), .checks(checks), .fault_addr(fault_addr), .fault_step(fault_step),
        .fault_be(fault_be), .fault_payload(fault_payload), .fault_expect(fault_expect), .fault_got(fault_got)
    );
    sdram_addon_port #(.BYTE_MASK_ENABLED(1)) controller (
        .clk(clk), .clk_pin(clk), .rate(rate), .reset(reset), .start(start), .write(write),
        .addr(addr), .wdata(wdata), .write_byte_enable(be), .initialized(), .done(done), .rdata(rdata),
        .sdram_clk(pin_clk), .sdram_cke(cke), .sdram_ncs(ncs), .sdram_nras(nras),
        .sdram_ncas(ncas), .sdram_nwe(nwe), .sdram_ba(ba), .sdram_a(a),
        .sdram_dqml(dqml), .sdram_dqmh(dqmh), .dq_out(dq_out), .dq_oe(oe),
        .dq_rise(rise_q), .dq_fall(rate == 2'd2 ? fall : fall_q)
    );
    sdram_model chip (.mask_fault(3'd0), .clk(pin_clk), .cke(cke), .ncs(ncs),
        .nras(nras), .ncas(ncas), .nwe(nwe), .ba(ba), .a(a), .dqml(dqml), .dqmh(dqmh), .dq(dq));
    assign refresh_masked_writes = chip.refresh_masked_writes;
endmodule
