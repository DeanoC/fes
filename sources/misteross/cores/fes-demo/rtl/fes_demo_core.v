// SPDX-License-Identifier: GPL-2.0-or-later
// Procedural image: no CPU, ROM, RAM or application-specific host branch.
module fes_demo_core #(parameter bit ENABLE_MEDIA = 0) (
    input wire pixel_clk,
    input wire exec_reset,
    input wire [7:0] buttons,
    input wire [23:0] palette,
    output wire [23:0] hdmi_rgb,
    output wire hdmi_de, hdmi_hs, hdmi_vs
);
    wire [9:0] x, y;
    wire frame_tick;
    reg [7:0] phase = 8'd0;
    wire [7:0] red = exec_reset ? 8'd0 : x[7:0] + phase;
    wire [7:0] green = exec_reset ? 8'd0 : y[7:0] + phase;
    wire [7:0] blue = exec_reset ? 8'd0 : (x[7:0] ^ y[7:0]) + phase;
    wire [23:0] tint = ENABLE_MEDIA ? palette : 24'hffffff;
    always @(posedge pixel_clk) begin
        if (exec_reset)
            phase <= 8'd0;
        else if (frame_tick)
            phase <= buttons[2] ? phase - 8'd1 : phase + (buttons[3] ? 8'd4 : 8'd1);
    end
    fes_video_720p video (
        .pixel_clk(pixel_clk),
        .game_red(red & tint[23:16]), .game_green(green & tint[15:8]),
        .game_blue(blue & tint[7:0]),
        .playfield_x(x), .playfield_y(y), .playfield_active(),
        .red(hdmi_rgb[23:16]), .green(hdmi_rgb[15:8]), .blue(hdmi_rgb[7:0]),
        .de(hdmi_de), .hsync(hdmi_hs), .vsync(hdmi_vs), .frame_tick(frame_tick)
    );
endmodule
