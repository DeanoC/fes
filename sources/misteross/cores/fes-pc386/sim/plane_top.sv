// SPDX-License-Identifier: MIT
// pc386 port 2 bench top: the presenter, the plane bridge, the port 2 arbiter
// and the shared display plane as pc386_machine wires them, with the capture
// writer's port and the frame reports driven by the bench.
module plane_top (
    input wire clk, reset, pixel_clk, pixel_locked,
    input wire [1:0] frame_buffer,
    input wire [9:0] frame_width, frame_height,
    input wire [15:0] frame_sequence,
    input wire [28:0] capture_address,
    input wire [7:0] capture_burstcount,
    input wire capture_write,
    input wire [63:0] capture_writedata,
    output wire capture_waitrequest,
    input wire request,
    input wire [6:0] opcode,
    input wire [7:0] index,
    input wire [15:0] argument,
    output wire response_valid, response_error,
    output wire [15:0] response_data,
    output wire [27:0] video,
    output wire ui_active, locked, faulted,
    output wire [31:0] displayed_sequence, plane_underflows, present_underflows,
    output wire [28:0] p2_address,
    output wire [7:0] p2_burstcount,
    output wire p2_read, p2_write,
    output wire [63:0] p2_writedata,
    output wire [7:0] p2_byteenable,
    input wire p2_waitrequest,
    input wire [63:0] p2_readdata,
    input wire p2_readdatavalid
);
    reg [1:0] pixel_reset_sync = 2'b11;
    always @(posedge pixel_clk) pixel_reset_sync <= {pixel_reset_sync[0], reset || !pixel_locked};
    wire pixel_reset = pixel_reset_sync[1];
    wire [1:0] present_buffer;
    wire [28:0] present_address;
    wire [7:0] present_burstcount;
    wire present_read, present_waitrequest, present_readdatavalid;
    wire [27:0] presenter_video;
    wire [31:0] frames_shown;
    pc386_presenter presenter (
        .clk(clk), .reset(reset),
        .frame_buffer(frame_buffer), .frame_width(frame_width), .frame_height(frame_height),
        .frame_sequence(frame_sequence), .present_buffer(present_buffer),
        .ddr_address(present_address), .ddr_burstcount(present_burstcount), .ddr_read(present_read),
        .ddr_waitrequest(present_waitrequest), .ddr_readdata(p2_readdata), .ddr_readdatavalid(present_readdatavalid),
        .pixel_clk(pixel_clk), .pixel_reset(pixel_reset),
        .video(presenter_video), .underflows(present_underflows), .frames_shown(frames_shown)
    );
    wire plane_hold, plane_ddr_reset, plane_ddr_drained, plane_read, plane_waitrequest, plane_readdatavalid;
    wire [27:0] plane_address;
    wire [7:0] plane_burstcount;
    wire [127:0] plane_readdata;
    wire quiesced;
    fes_display_plane #(
        .PICTURE_LEFT(16'd160), .PICTURE_TOP(16'd0), .PICTURE_WIDTH(16'd960), .PICTURE_HEIGHT(16'd720)
    ) display (
        .clk_sys(clk), .pixel_clk(pixel_clk), .reset_hold(!pixel_locked), .exec_reset(reset),
        .request(request), .opcode(opcode), .index(index), .argument(argument),
        .response_valid(response_valid), .response_error(response_error), .response_data(response_data),
        .quiesced_sys(quiesced), .machine(presenter_video), .video(video), .ui_active(ui_active), .locked(locked),
        .displayed_sequence(displayed_sequence), .underflows(plane_underflows), .faulted(faulted),
        .ddr_hold(plane_hold), .ddr_reset(plane_ddr_reset), .ddr_drained(plane_ddr_drained),
        .ddr_address(plane_address), .ddr_burstcount(plane_burstcount), .ddr_read(plane_read),
        .ddr_waitrequest(plane_waitrequest), .ddr_readdata(plane_readdata), .ddr_readdatavalid(plane_readdatavalid)
    );
    wire [28:0] bridge_address;
    wire [7:0] bridge_burstcount;
    wire bridge_read, bridge_waitrequest, bridge_readdatavalid;
    pc386_plane_bridge bridge (
        .clk(clk), .reset(reset), .pixel_clk(pixel_clk), .pixel_reset(pixel_reset),
        .ddr_hold(plane_hold), .ddr_reset(plane_ddr_reset), .ddr_drained(plane_ddr_drained),
        .plane_address(plane_address), .plane_burstcount(plane_burstcount), .plane_read(plane_read),
        .plane_waitrequest(plane_waitrequest), .plane_readdata(plane_readdata),
        .plane_readdatavalid(plane_readdatavalid),
        .port_address(bridge_address), .port_burstcount(bridge_burstcount), .port_read(bridge_read),
        .port_waitrequest(bridge_waitrequest), .port_readdata(p2_readdata), .port_readdatavalid(bridge_readdatavalid)
    );
    pc386_port2 port2 (
        .clk(clk), .reset(reset),
        .plane_address(bridge_address), .plane_burstcount(bridge_burstcount), .plane_read(bridge_read),
        .plane_waitrequest(bridge_waitrequest), .plane_readdatavalid(bridge_readdatavalid),
        .present_address(present_address), .present_burstcount(present_burstcount), .present_read(present_read),
        .present_waitrequest(present_waitrequest), .present_readdatavalid(present_readdatavalid),
        .capture_address(capture_address), .capture_burstcount(capture_burstcount), .capture_write(capture_write),
        .capture_writedata(capture_writedata), .capture_waitrequest(capture_waitrequest),
        .address(p2_address), .burstcount(p2_burstcount), .read(p2_read), .write(p2_write),
        .writedata(p2_writedata), .byteenable(p2_byteenable), .waitrequest(p2_waitrequest),
        .readdatavalid(p2_readdatavalid)
    );
    wire unused = ^{present_buffer, frames_shown, quiesced};
endmodule
