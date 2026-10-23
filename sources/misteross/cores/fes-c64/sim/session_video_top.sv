// SPDX-License-Identifier: GPL-2.0-or-later
// Exercise the actual C64 VIC scanout through the production shared plane.
module c64_session_video_top (
    input wire clk_sys, pixel_clk, reset_hold,
    input wire request,
    input wire [6:0] opcode,
    input wire [7:0] index,
    input wire [15:0] argument,
    output wire response_valid, response_error,
    output wire [15:0] response_data,
    output wire [27:0] machine, video,
    output wire ui_active, locked, faulted, quiesced,
    output wire [31:0] displayed_sequence, underflows
);
    reg [5:0] cycle_clock = 0;
    always @(posedge clk_sys) cycle_clock <= cycle_clock == 52 ? 0 : cycle_clock + 1'b1;
    wire [23:0] rgb;
    wire de, hs, vs;
    c64_vic vic (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk), .reset(reset_hold),
        .phi(cycle_clock == 52), .cycle_clock(cycle_clock), .cpu_bus_data(8'd0), .ba_n(),
        .cs(1'b0), .we(1'b0), .reading(1'b0), .read_sample(1'b0), .addr(6'd0), .din(8'd0), .dout(),
        .color_cs(1'b0), .color_addr(10'd0), .color_din(8'd0), .color_we(1'b0), .color_dout(),
        .bank(2'd0), .ram_addr(), .ram_data(8'd0),
        .red(rgb[23:16]), .green(rgb[15:8]), .blue(rgb[7:0]),
        .de(de), .hsync(hs), .vsync(vs), .raster(), .irq()
    );
    assign machine = {1'b1, vs, hs, de, rgb};
    fes_display_plane #(
        .PICTURE_LEFT(16'd160), .PICTURE_TOP(16'd0),
        .PICTURE_WIDTH(16'd960), .PICTURE_HEIGHT(16'd720)
    ) plane (
        .clk_sys(clk_sys), .pixel_clk(pixel_clk), .reset_hold(reset_hold), .exec_reset(1'b0),
        .request(request), .opcode(opcode), .index(index), .argument(argument),
        .response_valid(response_valid), .response_error(response_error), .response_data(response_data),
        .quiesced_sys(quiesced), .machine(machine), .video(video), .ui_active(ui_active), .locked(locked),
        .displayed_sequence(displayed_sequence), .underflows(underflows), .faulted(faulted)
    );
endmodule
