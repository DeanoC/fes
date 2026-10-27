// SPDX-License-Identifier: MIT
// Small scanout top for the capture FIFO overflow and recovery bench.
module vga_capture_top (
    input wire clk, reset, cap_waitrequest,
    output wire [1:0] frame_buffer,
    output wire [15:0] frame_sequence, frame_count,
    output wire [31:0] capture_drops
);
    wire [7:0] status, dac_read_data;
    wire [31:0] underflows;
    wire fetch_request;
    wire [14:0] fetch_beat;
    wire [28:0] cap_address;
    wire [7:0] cap_burstcount;
    wire cap_write, frame_start, frame_done, start_latched;
    wire [63:0] cap_writedata;
    wire [9:0] frame_width, frame_height;

    pc386_vga_scanout scanout (
        .clk(clk), .reset(reset), .misc(8'd0), .video_enable(1'b1), .seq_reset(2'b11),
        .seq_clocking(8'h01), .char_map(6'd0), .shift_mode(2'b00), .graphics(1'b1),
        .crtc0(8'd160), .crtc1(8'd159), .crtc6(8'd2), .crtc7(8'd0), .crtc8(8'd0), .crtc9(8'd0),
        .crtc10(8'h20), .crtc11(8'd0), .crtc12(8'd0), .crtc13(8'd0), .crtc14(8'd0), .crtc15(8'd0),
        .crtc16(8'd0), .crtc17(8'd0), .crtc18(8'd2), .crtc19(8'd0), .crtc20(8'd0), .crtc23(8'd0),
        .crtc24(8'd0), .attr_pas(1'b1), .attr_mode(8'd0), .overscan(6'd0), .plane_enable(4'hf),
        .pel_pan(4'd0), .color_select(4'd0), .pel_mask(8'hff),
        .palette0(8'd0), .palette1(8'd0), .palette2(8'd0), .palette3(8'd0),
        .palette4(8'd0), .palette5(8'd0), .palette6(8'd0), .palette7(8'd0),
        .palette8(8'd0), .palette9(8'd0), .palette10(8'd0), .palette11(8'd0),
        .palette12(8'd0), .palette13(8'd0), .palette14(8'd0), .palette15(8'd0),
        .dac_write(1'b0), .dac_write_index(8'd0), .dac_write_data(18'd0),
        .dac_read_index(8'd0), .dac_read_cycle(2'd0), .dac_read_data(dac_read_data), .status(status),
        .fetch_request(fetch_request), .fetch_beat(fetch_beat), .fetch_grant(1'b0), .fetch_valid(1'b0),
        .fetch_data(64'd0), .cap_address(cap_address), .cap_burstcount(cap_burstcount),
        .cap_write(cap_write), .cap_writedata(cap_writedata), .cap_waitrequest(cap_waitrequest),
        .frame_buffer(frame_buffer), .frame_width(frame_width), .frame_height(frame_height),
        .frame_sequence(frame_sequence), .present_buffer(2'd0), .frame_start(frame_start),
        .frame_done(frame_done), .start_latched(start_latched), .frame_count(frame_count),
        .underflows(underflows), .capture_drops(capture_drops)
    );

    wire unused = ^{status, dac_read_data, underflows, fetch_request, fetch_beat, cap_address, cap_burstcount,
                    cap_write, cap_writedata, frame_width, frame_height, frame_start, frame_done, start_latched};
endmodule
