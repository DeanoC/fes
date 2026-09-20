// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_gp.vh"

// Pixel-domain game and video shell, separated for deterministic simulation.
/* verilator lint_off DECLFILENAME */
module fes_pong_core (
    input  wire        pixel_clk,
    input  wire        game_reset,
    input  wire [7:0]  buttons,
    input  wire        game_frozen,
    input  wire [1:0]  paddle_speed,
    output wire        player_return,
    output wire        point,
    output wire [23:0] hdmi_rgb,
    output wire        hdmi_de,
    output wire        hdmi_hs,
    output wire        hdmi_vs,
    output wire        frame_tick,
    output wire [9:0]  playfield_x,
    output wire [9:0]  playfield_y,
    output wire        playfield_active,
    output wire        game_playing
);
    localparam [31:0] BUTTON_UP = `FES_GP_BUTTON_UP;
    localparam [31:0] BUTTON_DOWN = `FES_GP_BUTTON_DOWN;
    localparam [31:0] BUTTON_START = `FES_GP_BUTTON_START;

    wire up = |(buttons & BUTTON_UP[7:0]);
    wire down = |(buttons & BUTTON_DOWN[7:0]);
    wire start = |(buttons & BUTTON_START[7:0]);
    wire [7:0] game_red;
    wire [7:0] game_green;
    wire [7:0] game_blue;
    wire [7:0] video_red;
    wire [7:0] video_green;
    wire [7:0] video_blue;

    /* verilator lint_off PINCONNECTEMPTY */
    pong_game #(.CLOCK_HZ(74250000)) game (
        .clk(pixel_clk),
        .reset(game_reset),
        .freeze(game_frozen),
        .paddle_speed(paddle_speed),
        .frame_tick(frame_tick),
        .up(up),
        .down(down),
        .start(start),
        .pixel_x(playfield_x),
        .pixel_y(playfield_y),
        .red(game_red),
        .green(game_green),
        .blue(game_blue),
        .tone(),
        .playing(game_playing),
        .ball_x(),
        .ball_y(),
        .player_y(),
        .ai_y(),
        .player_score(),
        .ai_score(),
        .player_return(player_return),
        .point(point)
    );
    /* verilator lint_on PINCONNECTEMPTY */

    fes_video_720p video (
        .pixel_clk(pixel_clk),
        .game_red(game_red),
        .game_green(game_green),
        .game_blue(game_blue),
        .playfield_x(playfield_x),
        .playfield_y(playfield_y),
        .playfield_active(playfield_active),
        .red(video_red),
        .green(video_green),
        .blue(video_blue),
        .de(hdmi_de),
        .hsync(hdmi_hs),
        .vsync(hdmi_vs),
        .frame_tick(frame_tick)
    );

    assign hdmi_rgb = {video_red, video_green, video_blue};
endmodule
/* verilator lint_on DECLFILENAME */

// Standalone DE10-Nano shell. BUILD_ID is overridden with the build-record ID.
module top #(
    parameter [127:0] BUILD_ID = 128'h00000000000000000000000000000000
) (
    input  wire        FPGA_CLK1_50,
    output wire        HDMI_TX_CLK,
    output wire        HDMI_TX_DE,
    output wire [23:0] HDMI_TX_D,
    output wire        HDMI_TX_HS,
    output wire        HDMI_TX_VS,
    inout  wire        HDMI_I2C_SCL,
    inout  wire        HDMI_I2C_SDA
);
    wire hdmi_scl_in;
    wire hdmi_sda_in;
    wire hdmi_scl_low;
    wire hdmi_sda_low;

    // Linux controls the ADV7513 through HPS I2C at this exact hard-block
    // site. Explicit buffers preserve open-drain low-or-release behavior
    // through OSS synthesis, including feedback from an external device.
    MISTRAL_IO hdmi_scl_pad (
        .I(1'b0), .OE(hdmi_scl_low), .O(hdmi_scl_in), .PAD(HDMI_I2C_SCL)
    );
    MISTRAL_IO hdmi_sda_pad (
        .I(1'b0), .OE(hdmi_sda_low), .O(hdmi_sda_in), .PAD(HDMI_I2C_SDA)
    );
    (* BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0" *)
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .scl(hdmi_scl_in), .sda(hdmi_sda_in),
        .out_clk(hdmi_scl_low), .out_data(hdmi_sda_low)
    );

    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;
    wire mailbox_reset;
    wire game_frozen;
    wire [1:0] paddle_speed;
    wire player_return, point;
    wire [7:0] mailbox_buttons;
    wire pixel_clk;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    fes_gp mailbox (
        .clk(pixel_clk),
        .gpo(hps_to_fpga),
        .build_id(BUILD_ID),
        .gpi(fpga_to_hps),
        .game_reset(mailbox_reset),
        .buttons(mailbox_buttons),
        .game_frozen(game_frozen),
        .paddle_speed(paddle_speed),
        .player_return(player_return),
        .point(point)
    );

    pixel_pll video_clock (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk_0(pixel_clk)
    );

    /* verilator lint_off PINCONNECTEMPTY */
    fes_pong_core core (
        .pixel_clk(pixel_clk),
        .game_reset(mailbox_reset),
        .buttons(mailbox_buttons),
        .game_frozen(game_frozen),
        .paddle_speed(paddle_speed),
        .player_return(player_return),
        .point(point),
        .hdmi_rgb(HDMI_TX_D),
        .hdmi_de(HDMI_TX_DE),
        .hdmi_hs(HDMI_TX_HS),
        .hdmi_vs(HDMI_TX_VS),
        .frame_tick(),
        .playfield_x(),
        .playfield_y(),
        .playfield_active(),
        .game_playing()
    );
    /* verilator lint_on PINCONNECTEMPTY */

    assign HDMI_TX_CLK = pixel_clk;
endmodule
