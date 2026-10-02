// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_simple_computer.vh"
// The DDR launcher plane has independent control/reset/drain. ZX81 owns the
// raster, so opening or closing never changes HDMI sync or machine execution.
module zx81_session_display (
    input wire clk_sys, pixel_clk, reset_hold, exec_reset,
    input wire request,
    input wire [6:0] opcode,
    input wire [7:0] index,
    input wire [15:0] argument,
    output wire response_valid, response_error,
    output wire [15:0] response_data,
    output wire quiesced_sys,
    input wire [10:0] raster_h,
    input wire [9:0] raster_v,
    input wire [23:0] machine_rgb,
    output wire [23:0] rgb,
    output reg ui_active = 1'b0,
    output wire [31:0] displayed_sequence, underflows,
    output wire faulted
);
    wire pixel_request, pixel_response_valid, pixel_response_error;
    wire [6:0] pixel_opcode;
    wire [7:0] pixel_index;
    wire [15:0] pixel_argument, pixel_response_data;
    zx81_display_cdc crossing (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk), .request(request),
        .opcode(opcode), .index(index), .argument(argument),
        .response_valid(response_valid), .response_error(response_error), .response_data(response_data),
        .pixel_request(pixel_request), .pixel_opcode(pixel_opcode),
        .pixel_index(pixel_index), .pixel_argument(pixel_argument),
        .pixel_response_valid(pixel_response_valid), .pixel_response_error(pixel_response_error),
        .pixel_response_data(pixel_response_data)
    );
    (* async_reg = "true" *) reg [1:0] execution_sync = 2'b11;
    (* async_reg = "true" *) reg [1:0] quiesced_sync = 2'b11;
    wire enable, quiesce, submit_valid, submit_slot, submit_ready, drained;
    wire [31:0] submit_sequence;
    wire port_reset, port_drained;
    // On a hard hold the reader cannot see responses and must never restart.
    // Only the guard can prove its accepted/queued traffic has actually drained.
    wire quiesced = port_drained && (drained || (hard_fault && port_reset)) &&
                    !ui_active && (!enable || quiesce);
    always @(posedge pixel_clk) execution_sync <= {execution_sync[0], exec_reset};
    always @(posedge clk_sys) quiesced_sync <= {quiesced_sync[0], quiesced};
    assign quiesced_sys = quiesced_sync[1];
    fes_menu_control #(.FAULT_QUIESCE_ACK(1'b1)) control (
        .clk(pixel_clk), .request(pixel_request), .opcode(pixel_opcode),
        .index(pixel_index), .argument(pixel_argument), .exec_reset(execution_sync[1]),
        .drained(quiesced), .faulted(faulted), .submit_ready(submit_ready),
        .displayed_sequence(displayed_sequence), .underflows(underflows),
        .response_valid(pixel_response_valid), .response_error(pixel_response_error),
        .response_data(pixel_response_data), .enable(enable), .quiesce(quiesce),
        .submit_valid(submit_valid), .submit_slot(submit_slot), .submit_sequence(submit_sequence)
    );
    wire [23:0] launcher_rgb;
    wire [27:0] address;
    wire [7:0] burstcount;
    wire read, waitrequest, readdatavalid;
    wire [127:0] readdata;
    reg started = 1'b0, hard_fault = 1'b0, scanout_fault = 1'b0;
    reg was_enabled = 1'b0;
    reg [31:0] opening_sequence = 32'd0, opening_underflows = 32'd0;
    wire frame_end = raster_h == 11'd1649 && raster_v == 10'd749;
    assign faulted = hard_fault || scanout_fault;
    always @(posedge pixel_clk) begin
        if (enable && !port_reset && !reset_hold) started <= 1'b1;
        // An unexpected PLL hold hides old guard responses. Keep that port
        // contained, but retain the machine and allow the runtime to close UI.
        if (started && reset_hold && !drained) hard_fault <= 1'b1;
        if (ui_active && underflows != opening_underflows) scanout_fault <= 1'b1;
        was_enabled <= enable;
        if (enable && !was_enabled) begin
            opening_sequence <= displayed_sequence;
        end
        if (frame_end) begin
            if (!enable || quiesce || faulted || reset_hold) ui_active <= 1'b0;
            else if (!ui_active && displayed_sequence != opening_sequence) begin
                // Hidden startup retries may have counted misses. Monitor only
                // underruns after the completed launcher frame becomes visible.
                opening_underflows <= underflows;
                ui_active <= 1'b1;
            end
        end
    end
    assign rgb = ui_active && !faulted && !reset_hold ? launcher_rgb : machine_rgb;
    fes_menu_video #(.WINDOW_BASE(`FES_SIMPLE_COMPUTER_HPS_DDR_WINDOW_BASE),
                    .EXTERNAL_RASTER(1'b1), .COMPLETE_FRAME_ACK(1'b1)) video (
        .clk(pixel_clk), .rst(port_reset), .enable(enable && !faulted), .quiesce(quiesce),
        .raster_h(raster_h), .raster_v(raster_v),
        .submit_valid(submit_valid), .submit_slot(submit_slot), .submit_sequence(submit_sequence),
        .submit_ready(submit_ready), .displayed_sequence(displayed_sequence),
        .underflows(underflows), .quiesced(drained), .rgb(launcher_rgb), .de(), .hs(), .vs(),
        .address(address), .burstcount(burstcount), .read(read), .waitrequest(waitrequest),
        .readdata(readdata), .readdatavalid(readdatavalid)
    );
    // No execution hold: physical machine reset and display drain are separate.
    fes_hps_ddr #(.P0_WRITE_ENABLE(1'b0), .P1_ENABLE(1'b0), .P2_ENABLE(1'b0)) memory (
        .hold(reset_hold || hard_fault),
        .p0_clk(pixel_clk), .p0_reset(port_reset), .p0_drained(port_drained), .p0_address(address), .p0_burstcount(burstcount),
        .p0_waitrequest(waitrequest), .p0_readdata(readdata), .p0_readdatavalid(readdatavalid),
        .p0_read(read), .p0_writedata(128'd0), .p0_byteenable(16'd0), .p0_write(1'b0),
        .p1_clk(pixel_clk), .p1_reset(), .p1_drained(), .p1_address(29'd0), .p1_burstcount(8'd0),
        .p1_waitrequest(), .p1_readdata(), .p1_readdatavalid(), .p1_read(1'b0),
        .p1_writedata(64'd0), .p1_byteenable(8'd0), .p1_write(1'b0),
        .p2_clk(pixel_clk), .p2_reset(), .p2_drained(), .p2_address(29'd0), .p2_burstcount(8'd0),
        .p2_waitrequest(), .p2_readdata(), .p2_readdatavalid(), .p2_read(1'b0),
        .p2_writedata(64'd0), .p2_byteenable(8'd0), .p2_write(1'b0)
    );
endmodule
