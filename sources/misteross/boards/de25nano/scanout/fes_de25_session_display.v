// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"
`include "fes_display_plane.vh"
// Standard-core session display. Only the kernel-owned scanout CSR can publish
// memory. GP controls visibility and drain, and cannot submit a physical frame.
module fes_de25_session_display #(
    parameter [127:0] BUILD_ID = 128'd0,
    parameter [15:0] PICTURE_LEFT = 16'd312,
    parameter [15:0] PICTURE_TOP = 16'd90,
    parameter [15:0] PICTURE_WIDTH = 16'd656,
    parameter [15:0] PICTURE_HEIGHT = 16'd540
) (
    input wire clk_sys, pixel_clk, reset_n, exec_reset,
    input wire clock_fault, display_ready, stream_ready,
    input wire [27:0] machine,
    input wire request,
    input wire [6:0] opcode,
    input wire [7:0] index,
    input wire [15:0] argument,
    output wire response_valid, response_error,
    output wire [15:0] response_data,
    output wire quiesced_sys,
    output wire [27:0] video,
    output reg ui_active = 1'b0,
    output wire locked, faulted,
    input wire [6:0] address,
    input wire read, write,
    input wire [3:0] byteenable,
    input wire [31:0] writedata,
    output wire [31:0] readdata,
    output wire readdatavalid, writeresponsevalid,
    output wire [1:0] response,
    output wire waitrequest,
    output wire [31:0] avm_address,
    output wire [6:0] avm_burstcount,
    output wire avm_read,
    output wire [15:0] avm_byteenable,
    input wire avm_waitrequest,
    input wire [127:0] avm_readdata,
    input wire avm_readdatavalid,
    input wire [1:0] avm_response
);
    wire [10:0] raster_h;
    wire [9:0] raster_v;
    wire [23:0] plane_rgb;
    wire [7:0] plane_alpha;
    wire running, drained, pending;
    wire [31:0] frame_sequence, underflows, errors;
    wire enable, quiesce;
    wire pixel_request, pixel_response_valid, pixel_response_error;
    wire [6:0] pixel_opcode;
    wire [7:0] pixel_index;
    wire [15:0] pixel_argument, pixel_response_data;
    (* async_reg = "true" *) reg [1:0] execution_sync = 2'b11;
    (* async_reg = "true" *) reg [1:0] quiesced_sync = 2'b11;
    reg raster_fault = 1'b0, was_enabled = 1'b0;
    reg [31:0] opening_sequence = 32'd0;
    wire frame_end = raster_h == 11'd1649 && raster_v == 10'd749;
    wire quiesced = drained && !ui_active && (!enable || quiesce);
    // Drain is decoded from reader counters and bridge state. Register the
    // complete result in its pixel domain before the two-stage return crossing.
    (* preserve, dont_merge *) reg quiesced_source = 1'b1;
    assign faulted = raster_fault || errors != 32'd0;
    assign quiesced_sys = quiesced_sync[1];
    always @(posedge clk_sys) quiesced_sync <= {quiesced_sync[0], quiesced_source};
    always @(posedge pixel_clk) begin
        quiesced_source <= quiesced;
        execution_sync <= {execution_sync[0], exec_reset};
        was_enabled <= enable;
        if (enable && !was_enabled) opening_sequence <= frame_sequence;
        if (ui_active && !locked) raster_fault <= 1'b1;
        if (frame_end) begin
            if (!enable || quiesce || faulted || !running || !locked) ui_active <= 1'b0;
            else if (!ui_active && frame_sequence != opening_sequence) ui_active <= 1'b1;
        end
    end
    fes_display_cdc crossing (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk), .request(request),
        .opcode(opcode), .index(index), .argument(argument),
        .response_valid(response_valid), .response_error(response_error), .response_data(response_data),
        .pixel_request(pixel_request), .pixel_opcode(pixel_opcode), .pixel_index(pixel_index),
        .pixel_argument(pixel_argument), .pixel_response_valid(pixel_response_valid),
        .pixel_response_error(pixel_response_error), .pixel_response_data(pixel_response_data)
    );
    fes_menu_control #(.ALLOW_SUBMIT(1'b0), .FAULT_QUIESCE_ACK(1'b1),
        .PIXEL_FORMAT(16'(`FES_DISPLAY_PLANE_PIXEL_FORMAT_PREMULTIPLIED_RGBA)),
        .PICTURE_INFO(1'b1), .PICTURE_LEFT(PICTURE_LEFT), .PICTURE_TOP(PICTURE_TOP),
        .PICTURE_WIDTH(PICTURE_WIDTH), .PICTURE_HEIGHT(PICTURE_HEIGHT)) control (
        .clk(pixel_clk), .request(pixel_request), .opcode(pixel_opcode), .index(pixel_index),
        .argument(pixel_argument), .exec_reset(execution_sync[1]), .drained(quiesced),
        .faulted(faulted), .submit_ready(1'b0), .external_pending(pending),
        .displayed_sequence(frame_sequence), .underflows(underflows),
        .response_valid(pixel_response_valid), .response_error(pixel_response_error),
        .response_data(pixel_response_data), .enable(enable), .quiesce(quiesce),
        .submit_valid(), .submit_slot(), .submit_sequence()
    );
    fes_de25_scanout #(.BUILD_ID(BUILD_ID), .EXTERNAL_RASTER(1'b1)) memory (
        .clk(pixel_clk), .reset_n(reset_n), .clock_fault(clock_fault), .display_ready(display_ready), .stream_ready(stream_ready),
        .raster_h(raster_h), .raster_v(raster_v), .address(address), .read(read), .write(write),
        .byteenable(byteenable), .writedata(writedata), .readdata(readdata),
        .readdatavalid(readdatavalid), .writeresponsevalid(writeresponsevalid),
        .response(response), .waitrequest(waitrequest), .avm_address(avm_address),
        .avm_burstcount(avm_burstcount), .avm_read(avm_read), .avm_byteenable(avm_byteenable),
        .avm_waitrequest(avm_waitrequest), .avm_readdata(avm_readdata),
        .avm_readdatavalid(avm_readdatavalid), .avm_response(avm_response),
        .video_rgb(plane_rgb), .video_alpha(plane_alpha), .video_de(), .video_hs(), .video_vs(),
        .scanout_running(running), .scanout_drained(drained), .scanout_pending(pending), .scanout_sequence(frame_sequence),
        .scanout_underflows(underflows), .scanout_errors(errors)
    );
    fes_display_compositor compositor (
        .pixel_clk(pixel_clk), .machine(machine), .show(ui_active && !faulted && locked),
        .launcher_rgb(plane_rgb), .launcher_alpha(plane_alpha), .video(video),
        .locked(locked), .raster_h(raster_h), .raster_v(raster_v)
    );
endmodule
