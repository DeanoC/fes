// SPDX-License-Identifier: GPL-2.0-or-later
// ROM-less Pong on the pinned, unmodified MiSTer framework.
module emu (
    `include "sys/emu_ports.vh"
);
    wire clk_sys;
    pll pll (.refclk(CLK_50M), .rst(1'b0), .outclk_0(clk_sys));
    assign CLK_VIDEO = clk_sys;

    `include "build_id.v"
    localparam CONF_STR = {
        "Pong;;",
        "R0,Reset;",
        "J1,Unused,Unused,Unused,Start;",
        "V,v", `BUILD_DATE
    };
    wire [127:0] status;
    wire [31:0] joystick;
    wire [1:0] buttons;
    hps_io #(.CONF_STR(CONF_STR)) hps_io (
        .clk_sys(clk_sys), .HPS_BUS(HPS_BUS),
        .joystick_0(joystick), .buttons(buttons), .status(status),
        .status_in(128'd0), .status_set(1'b0), .status_menumask(16'd0),
        .new_vmode(1'b0), .video_rotated(1'b0),
        .info_req(1'b0), .info(8'd0),
        .sd_rd(1'b0), .sd_wr(1'b0),
        .ioctl_upload_req(1'b0), .ioctl_upload_index(8'd0),
        .ioctl_din(8'd0), .ioctl_wait(1'b0),
        .EXT_BUS(), .gamma_bus()
    );

    wire [9:0] x, y;
    wire frame_tick, active;
    pong_video video (
        .clk(clk_sys), .ce_pixel(CE_PIXEL), .frame_tick(frame_tick),
        .x(x), .y(y), .active(active), .hsync(VGA_HS), .vsync(VGA_VS)
    );
    assign VGA_DE = active;
    wire tone;
    pong_game #(.CLOCK_HZ(20000000)) game (
        .clk(clk_sys), .reset(RESET | status[0] | buttons[1]),
        .freeze(1'b0), .paddle_speed(2'd1),
        .frame_tick(frame_tick), .up(joystick[3]), .down(joystick[2]),
        .start(joystick[7]), .pixel_x(x), .pixel_y(y),
        .red(VGA_R), .green(VGA_G), .blue(VGA_B), .tone(tone),
        .playing(LED_USER), .ball_x(), .ball_y(), .player_y(), .ai_y(),
        .player_score(), .ai_score(), .player_return(), .point()
    );
    assign VIDEO_ARX = 13'd4;
    assign VIDEO_ARY = 13'd3;
    assign AUDIO_S = 1'b1;
    assign AUDIO_L = tone ? 16'h2000 : 16'h0000;
    assign AUDIO_R = AUDIO_L;
    assign AUDIO_MIX = 2'd0;

    assign ADC_BUS = 'Z;
    assign USER_OUT = '1;
    assign {UART_RTS, UART_TXD, UART_DTR} = '0;
    assign {SD_SCK, SD_MOSI, SD_CS} = 'Z;
    assign {SDRAM_DQ, SDRAM_A, SDRAM_BA, SDRAM_CLK, SDRAM_CKE, SDRAM_DQML,
            SDRAM_DQMH, SDRAM_nWE, SDRAM_nCAS, SDRAM_nRAS, SDRAM_nCS} = 'Z;
    assign {DDRAM_CLK, DDRAM_BURSTCNT, DDRAM_ADDR, DDRAM_DIN, DDRAM_BE,
            DDRAM_RD, DDRAM_WE} = '0;
    assign {VGA_SL, VGA_F1, VGA_SCALER, VGA_DISABLE, HDMI_FREEZE,
            HDMI_BLACKOUT, HDMI_BOB_DEINT} = '0;
    assign {LED_DISK, LED_POWER, BUTTONS} = '0;
endmodule
