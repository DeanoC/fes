// SPDX-License-Identifier: GPL-2.0-or-later
// Standalone DE10-Nano shell. BUILD_ID is overridden with the build-record ID.
module top #(
    parameter [127:0] BUILD_ID = 128'h00000000000000000000000000000000,
    parameter bit ENABLE_GAMEPAD = 0,
    parameter bit ENABLE_MEDIA = 0
) (
    input  wire        FPGA_CLK1_50,
    output wire        HDMI_TX_CLK,
    output wire        HDMI_TX_DE,
    output wire [23:0] HDMI_TX_D,
    output wire        HDMI_TX_HS,
    output wire        HDMI_TX_VS,
    inout  wire        HDMI_I2C_SCL,
    inout  wire        HDMI_I2C_SDA
`ifdef FES_DEMO_AUDIO
    ,output wire HDMI_MCLK,
    output wire HDMI_SCLK,
    output wire HDMI_LRCLK,
    output wire HDMI_I2S
`endif
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
    wire [7:0] palette_red, palette_green, palette_blue;
    wire [7:0] mailbox_buttons;
    wire pixel_clk;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

`ifdef FES_DEMO_AUDIO
    localparam bit ENABLE_AUDIO = 1;
`else
    localparam bit ENABLE_AUDIO = 0;
`endif
    fes_application_gp #(.ENABLE_GAMEPAD(ENABLE_GAMEPAD), .ENABLE_MEDIA(ENABLE_MEDIA), .ENABLE_AUDIO(ENABLE_AUDIO)) endpoint (
        .clk(pixel_clk),
        .gpo(hps_to_fpga),
        .build_id(BUILD_ID),
        .gpi(fpga_to_hps),
        .exec_reset(mailbox_reset),
        .buttons(mailbox_buttons), .controller_buttons(), .controller_keypad(),
        .media_ready(), .media_size(),
        .media_byte0(palette_red), .media_byte1(palette_green), .media_byte2(palette_blue),
        .media_write_addr(), .media_write_data(), .media_write_enable(),
        .firmware_write_addr(), .firmware_write_data(), .firmware_write_enable(),
        .firmware_ready()
    );

    pixel_pll video_clock (
        .refclk(FPGA_CLK1_50),
        .rst(1'b0),
        .outclk_0(pixel_clk)
    );

`ifdef FES_CATCH
    wire catch_sound;
    fes_catch_core core (
        .pixel_clk(pixel_clk), .exec_reset(mailbox_reset), .buttons(mailbox_buttons),
        .hdmi_rgb(HDMI_TX_D), .hdmi_de(HDMI_TX_DE),
        .hdmi_hs(HDMI_TX_HS), .hdmi_vs(HDMI_TX_VS), .sound_toggle(catch_sound)
    );
`else
    fes_demo_core #(.ENABLE_MEDIA(ENABLE_MEDIA)) core (
        .pixel_clk(pixel_clk), .exec_reset(mailbox_reset), .buttons(mailbox_buttons),
        .palette({palette_red, palette_green, palette_blue}),
        .hdmi_rgb(HDMI_TX_D), .hdmi_de(HDMI_TX_DE),
        .hdmi_hs(HDMI_TX_HS), .hdmi_vs(HDMI_TX_VS)
    );
`endif

    assign HDMI_TX_CLK = pixel_clk;
`ifdef FES_DEMO_AUDIO
    wire audio_clk, audio_locked;
    fes_audio_pll audio_clock (.refclk(FPGA_CLK1_50), .clk(audio_clk), .locked(audio_locked));
`ifdef FES_CATCH
    fes_catch_audio audio (
        .clk(audio_clk), .locked(audio_locked), .exec_reset(mailbox_reset),
        .sound_toggle(catch_sound), .sclk(HDMI_SCLK), .lrclk(HDMI_LRCLK), .sdata(HDMI_I2S)
    );
`else
    fes_demo_audio audio (
        .clk(audio_clk), .locked(audio_locked), .exec_reset(mailbox_reset),
        .buttons(mailbox_buttons), .sclk(HDMI_SCLK), .lrclk(HDMI_LRCLK), .sdata(HDMI_I2S)
    );
`endif
    assign HDMI_MCLK = audio_clk;
`endif
endmodule
