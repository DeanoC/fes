// SPDX-License-Identifier: GPL-2.0-or-later
module fes_catch_core (
    input wire pixel_clk, exec_reset,
    input wire [7:0] buttons,
    output wire [23:0] hdmi_rgb,
    output wire hdmi_de, hdmi_hs, hdmi_vs, sound_toggle
);
    wire [9:0] x, y;
    wire frame_tick;
    wire [23:0] rgb;
    fes_catch_game game (.clk(pixel_clk), .reset(exec_reset), .frame_tick(frame_tick),
        .buttons(buttons), .x(x), .y(y), .rgb(rgb), .sound_toggle(sound_toggle),
        .paddle(), .drop_x(), .drop_y(), .score(), .lives());
    fes_video_720p video (.pixel_clk(pixel_clk),
        .game_red(rgb[23:16]), .game_green(rgb[15:8]), .game_blue(rgb[7:0]),
        .playfield_x(x), .playfield_y(y), .playfield_active(),
        .red(hdmi_rgb[23:16]), .green(hdmi_rgb[15:8]), .blue(hdmi_rgb[7:0]),
        .de(hdmi_de), .hsync(hdmi_hs), .vsync(hdmi_vs), .frame_tick(frame_tick));
endmodule
