// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"
// Diagnostic DDR adoption: no GP presentation or physical reset/release control.
// Quiesce must complete before a live hold. An unexpected hold fails closed
// until reprogramming because the shared guard hides old read responses.
module fes_menu_ddr (
    input wire clk, reset_hold, enable, quiesce,
    input wire submit_valid, submit_slot,
    input wire [31:0] submit_sequence,
    output wire submit_ready, quiesced,
    output wire [31:0] displayed_sequence, underflows,
    output wire [23:0] rgb,
    output wire de, hs, vs,
    output reg faulted = 1'b0
);
    wire port_reset, drained;
    wire [27:0] address;
    wire [7:0] burstcount;
    wire read, waitrequest, readdatavalid;
    wire [127:0] readdata;
    reg started = 1'b0;
    always @(posedge clk) begin
        if (enable && !port_reset && !reset_hold) started <= 1'b1;
        if (started && reset_hold && !drained) faulted <= 1'b1;
    end
    assign quiesced = drained && !faulted;
    fes_menu_video #(.WINDOW_BASE(`FES_APPLICATION_HPS_DDR_WINDOW_BASE)) video (
        .raster_h(11'd0), .raster_v(10'd0),
        .clk(clk), .rst(port_reset), .enable(enable && !faulted), .quiesce(quiesce),
        .submit_valid(submit_valid), .submit_slot(submit_slot), .submit_sequence(submit_sequence),
        .submit_ready(submit_ready), .displayed_sequence(displayed_sequence),
        .underflows(underflows), .quiesced(drained), .rgb(rgb), .de(de), .hs(hs), .vs(vs),
        .address(address), .burstcount(burstcount), .read(read), .waitrequest(waitrequest),
        .readdata(readdata), .readdatavalid(readdatavalid)
    );
    fes_hps_ddr #(.P0_WRITE_ENABLE(1'b0), .P1_ENABLE(1'b0), .P2_ENABLE(1'b0)) memory (
        .hold(reset_hold || faulted),
        .p0_drained(), .p0_clk(clk), .p0_reset(port_reset), .p0_address(address), .p0_burstcount(burstcount),
        .p0_waitrequest(waitrequest), .p0_readdata(readdata), .p0_readdatavalid(readdatavalid),
        .p0_read(read), .p0_writedata(128'd0), .p0_byteenable(16'd0), .p0_write(1'b0),
        .p1_drained(), .p1_clk(clk), .p1_reset(), .p1_address(29'd0), .p1_burstcount(8'd0),
        .p1_waitrequest(), .p1_readdata(), .p1_readdatavalid(), .p1_read(1'b0),
        .p1_writedata(64'd0), .p1_byteenable(8'd0), .p1_write(1'b0),
        .p2_drained(), .p2_clk(clk), .p2_reset(), .p2_address(29'd0), .p2_burstcount(8'd0),
        .p2_waitrequest(), .p2_readdata(), .p2_readdatavalid(), .p2_read(1'b0),
        .p2_writedata(64'd0), .p2_byteenable(8'd0), .p2_write(1'b0)
    );
endmodule
