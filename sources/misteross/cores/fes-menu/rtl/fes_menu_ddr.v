// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"
// Menu scanout on port 0 and, with BLITTER, the menu blitter on ports 1 and
// 2. The diagnostic build has no GP presentation, blitter or physical
// reset/release control. Quiesce halts the blitter and must complete before
// a live hold. An unexpected hold fails closed until reprogramming because
// the shared guards hide old read responses.
module fes_menu_ddr #(
    parameter bit BLITTER = 0
) (
    input wire clk, reset_hold, enable, quiesce,
    input wire submit_valid, submit_slot,
    input wire [31:0] submit_sequence,
    input wire blit_valid, blit_slot,
    input wire [31:0] blit_offset,
    input wire [15:0] blit_count,
    output wire submit_ready, quiesced,
    output wire [31:0] displayed_sequence, underflows,
    output wire blit_idle, blit_faulted,
    output wire [2:0] blit_fault_code,
    output wire [15:0] blit_completed,
    output wire [23:0] rgb,
    output wire de, hs, vs,
    output reg faulted = 1'b0
);
    wire port_reset, blit_reset, drained, p1_drained, p2_drained;
    wire [27:0] address;
    wire [7:0] burstcount;
    wire read, waitrequest, readdatavalid;
    wire [127:0] readdata;
    wire [28:0] p1_address, p2_address;
    wire [7:0] p1_burstcount, p2_burstcount, p1_byteenable;
    wire p1_read, p1_write, p1_waitrequest, p1_readdatavalid;
    wire p2_read, p2_waitrequest, p2_readdatavalid;
    wire [63:0] p1_writedata, p1_readdata, p2_readdata;
    wire all_drained = drained && (!BLITTER || (blit_idle && p1_drained && p2_drained));
    // The f2sdram responses are too far from the GP control and the hold
    // check for one clock, so the drained proof is registered. A sample from
    // before quiesce rose reads not drained: scanout reports drained only
    // while quiesced or disabled, and the menu is disabled only by a quiesce.
    // Quiesce gates the output so Enable clears quiesced at once.
    reg started = 1'b0, drained_q = 1'b0;
    always @(posedge clk) begin
        if (enable && !port_reset && !reset_hold) started <= 1'b1;
        if (started && reset_hold && !drained_q) faulted <= 1'b1;
        drained_q <= all_drained;
    end
    assign quiesced = drained_q && quiesce && !faulted;
    fes_menu_video #(.WINDOW_BASE(`FES_APPLICATION_HPS_DDR_WINDOW_BASE)) video (
        .raster_h(11'd0), .raster_v(10'd0),
        .clk(clk), .rst(port_reset), .enable(enable && !faulted), .quiesce(quiesce),
        .submit_valid(submit_valid), .submit_slot(submit_slot), .submit_sequence(submit_sequence),
        .submit_ready(submit_ready), .displayed_sequence(displayed_sequence),
        .underflows(underflows), .quiesced(drained), .rgb(rgb), .alpha(), .de(de), .hs(hs), .vs(vs),
        .address(address), .burstcount(burstcount), .read(read), .waitrequest(waitrequest),
        .readdata(readdata), .readdatavalid(readdatavalid)
    );
    generate if (BLITTER) begin : blit
        // The blitter finishes a run when port 1 takes its last write beat,
        // but the port's guard may still hold beats for the hard block. A
        // run reports idle and counts only once port 1 has drained too, so
        // the runtime never submits a slot whose last writes are in flight.
        wire run_idle;
        wire [15:0] run_completed;
        reg idle_q = 1'b1;
        reg [15:0] completed_q = 16'd0;
        always @(posedge clk) begin
            idle_q <= run_idle && p1_drained;
            if (blit_reset) completed_q <= 16'd0;
            else if (run_idle && p1_drained) completed_q <= run_completed;
        end
        assign blit_idle = idle_q;
        assign blit_completed = completed_q;
        // Destinations on port 1, commands and sources on port 2.
        fes_menu_blitter #(
            .WINDOW_BASE(`FES_APPLICATION_HPS_DDR_WINDOW_BASE), .SLOT_BYTES(`FES_APPLICATION_MENU_SLOT_BYTES),
            .LIST_OFFSET(`FES_APPLICATION_MENU_BLIT_LIST_OFFSET), .LIST_BYTES(`FES_APPLICATION_MENU_BLIT_LIST_BYTES),
            .HEAP_OFFSET(`FES_APPLICATION_MENU_BLIT_HEAP_OFFSET), .HEAP_BYTES(`FES_APPLICATION_MENU_BLIT_HEAP_BYTES)
        ) blitter (
            .clk(clk), .rst(blit_reset), .halt(quiesce || faulted),
            .run_valid(blit_valid), .list_offset(blit_offset), .list_count(blit_count), .target_slot(blit_slot),
            .run_ready(), .idle(run_idle), .completed(run_completed), .faulted(blit_faulted), .fault_code(blit_fault_code),
            .s_address(p2_address), .s_burstcount(p2_burstcount), .s_read(p2_read),
            .s_waitrequest(p2_waitrequest), .s_readdata(p2_readdata), .s_readdatavalid(p2_readdatavalid),
            .d_address(p1_address), .d_burstcount(p1_burstcount), .d_read(p1_read), .d_write(p1_write),
            .d_writedata(p1_writedata), .d_byteenable(p1_byteenable),
            .d_waitrequest(p1_waitrequest), .d_readdata(p1_readdata), .d_readdatavalid(p1_readdatavalid)
        );
    end else begin : no_blit
        assign {p1_address, p1_burstcount, p1_read, p1_write, p1_writedata, p1_byteenable} = 111'd0;
        assign {p2_address, p2_burstcount, p2_read} = 38'd0;
        assign {blit_idle, blit_faulted, blit_fault_code, blit_completed} = {1'b1, 20'd0};
    end endgenerate
    fes_hps_ddr #(.P0_WRITE_ENABLE(1'b0), .P1_ENABLE(BLITTER), .P2_ENABLE(BLITTER), .P2_WRITE_ENABLE(1'b0)) memory (
        .hold(reset_hold || faulted),
        .p0_drained(), .p0_clk(clk), .p0_reset(port_reset), .p0_address(address), .p0_burstcount(burstcount),
        .p0_waitrequest(waitrequest), .p0_readdata(readdata), .p0_readdatavalid(readdatavalid),
        .p0_read(read), .p0_writedata(128'd0), .p0_byteenable(16'd0), .p0_write(1'b0),
        .p1_drained(p1_drained), .p1_clk(clk), .p1_reset(blit_reset), .p1_address(p1_address), .p1_burstcount(p1_burstcount),
        .p1_waitrequest(p1_waitrequest), .p1_readdata(p1_readdata), .p1_readdatavalid(p1_readdatavalid), .p1_read(p1_read),
        .p1_writedata(p1_writedata), .p1_byteenable(p1_byteenable), .p1_write(p1_write),
        .p2_drained(p2_drained), .p2_clk(clk), .p2_reset(), .p2_address(p2_address), .p2_burstcount(p2_burstcount),
        .p2_waitrequest(p2_waitrequest), .p2_readdata(p2_readdata), .p2_readdatavalid(p2_readdatavalid), .p2_read(p2_read),
        .p2_writedata(64'd0), .p2_byteenable(8'd0), .p2_write(1'b0)
    );
endmodule
