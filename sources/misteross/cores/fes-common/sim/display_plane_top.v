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
    wire ddr_hold, ddr_reset, ddr_drained, ddr_read, ddr_waitrequest, ddr_readdatavalid;
    wire [27:0] ddr_address;
    wire [7:0] ddr_burstcount;
    wire [127:0] ddr_readdata;
    fes_display_plane #(
        .PICTURE_LEFT(16'd100), .PICTURE_TOP(16'd60),
        .PICTURE_WIDTH(16'd1080), .PICTURE_HEIGHT(16'd600)
    ) plane (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk), .reset_hold(reset_hold), .exec_reset(exec_reset),
        .request(request), .opcode(opcode), .index(index), .argument(argument),
        .response_valid(response_valid), .response_error(response_error), .response_data(response_data),
        .quiesced_sys(quiesced), .machine(machine), .video(video), .ui_active(ui_active),
        .locked(locked), .displayed_sequence(displayed_sequence), .underflows(underflows),
        .faulted(faulted), .ddr_hold(ddr_hold), .ddr_reset(ddr_reset), .ddr_drained(ddr_drained),
        .ddr_address(ddr_address), .ddr_burstcount(ddr_burstcount), .ddr_read(ddr_read),
        .ddr_waitrequest(ddr_waitrequest), .ddr_readdata(ddr_readdata),
        .ddr_readdatavalid(ddr_readdatavalid)
    );
    // The shell's side of the plane: port 0 only, as the ZX81 wires it.
    fes_hps_ddr #(.P0_WRITE_ENABLE(1'b0), .P1_ENABLE(1'b0), .P2_ENABLE(1'b0)) memory (
        .hold(ddr_hold),
        .p0_clk(pixel_clk), .p0_reset(ddr_reset), .p0_drained(ddr_drained),
        .p0_address(ddr_address), .p0_burstcount(ddr_burstcount),
        .p0_waitrequest(ddr_waitrequest), .p0_readdata(ddr_readdata),
        .p0_readdatavalid(ddr_readdatavalid), .p0_read(ddr_read),
        .p0_writedata(128'd0), .p0_byteenable(16'd0), .p0_write(1'b0),
        .p1_clk(pixel_clk), .p1_reset(), .p1_drained(), .p1_address(29'd0), .p1_burstcount(8'd0),
        .p1_waitrequest(), .p1_readdata(), .p1_readdatavalid(), .p1_read(1'b0),
        .p1_writedata(64'd0), .p1_byteenable(8'd0), .p1_write(1'b0),
        .p2_clk(pixel_clk), .p2_reset(), .p2_drained(), .p2_address(29'd0), .p2_burstcount(8'd0),
        .p2_waitrequest(), .p2_readdata(), .p2_readdatavalid(), .p2_read(1'b0),
        .p2_writedata(64'd0), .p2_byteenable(8'd0), .p2_write(1'b0)
    );
endmodule
