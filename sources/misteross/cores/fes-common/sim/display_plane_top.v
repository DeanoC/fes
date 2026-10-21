// SPDX-License-Identifier: GPL-2.0-or-later
// A synthetic finished 720p video word feeds the shared display plane. The
// bench can move the raster counter to break and re-establish lock.
module display_plane_top (
    input wire clk_sys, pixel_clk, reset_hold, exec_reset,
    input wire request,
    input wire [6:0] opcode,
    input wire [7:0] index,
    input wire [15:0] argument,
    output wire response_valid, response_error,
    output wire [15:0] response_data,
    output wire quiesced,
    output wire [27:0] machine, video,
    output wire [10:0] machine_h,
    output wire [9:0] machine_v,
    output wire ui_active, locked, faulted,
    output wire [31:0] displayed_sequence, underflows
);
    reg [10:0] h /* verilator public_flat_rw */ = 11'd0;
    reg [9:0] v = 10'd0;
    always @(posedge pixel_clk) begin
        if (h == 11'd1649) begin
            h <= 11'd0;
            v <= v == 10'd749 ? 10'd0 : v + 10'd1;
        end else h <= h + 11'd1;
    end
    wire de = h < 11'd1280 && v < 10'd720;
    wire hs = h >= 11'd1390 && h < 11'd1430;
    wire vs = v >= 10'd725 && v < 10'd730;
    assign machine = {1'b1, vs, hs, de, de ? {h[7:0], v[7:0], h[7:0] ^ v[7:0]} : 24'd0};
    assign machine_h = h;
    assign machine_v = v;
    fes_display_plane #(
        .PICTURE_LEFT(16'd100), .PICTURE_TOP(16'd60),
        .PICTURE_WIDTH(16'd1080), .PICTURE_HEIGHT(16'd600)
    ) plane (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk), .reset_hold(reset_hold), .exec_reset(exec_reset),
        .request(request), .opcode(opcode), .index(index), .argument(argument),
        .response_valid(response_valid), .response_error(response_error), .response_data(response_data),
        .quiesced_sys(quiesced), .machine(machine), .video(video), .ui_active(ui_active),
        .locked(locked), .displayed_sequence(displayed_sequence), .underflows(underflows),
        .faulted(faulted)
    );
endmodule
