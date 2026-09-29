// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"
`include "spectrum_bus.vh"
`ifndef FES_SPECTRUM_BUILD_ID
`define FES_SPECTRUM_BUILD_ID 128'h00000000000000000000000000000000
`endif

// DE10-Nano shell for the FES ZX Spectrum (fes.computer 1.0). The mailbox,
// Z80, tape player and edge sockets run in the 52.224 MHz system domain.
// Video is scanned from RAM in the 74.25 MHz HDMI domain. Audio leaves
// through the shared 12.288 MHz I2S serializer. Sockets 1-4 are physical
// and vacant here; a linked card replaces the vacant response.
module top #(
    parameter [127:0] BUILD_ID = `FES_SPECTRUM_BUILD_ID
) (
    input  wire        FPGA_CLK1_50,
    output wire        HDMI_TX_CLK,
    output wire        HDMI_TX_DE,
    output wire [23:0] HDMI_TX_D,
    output wire        HDMI_TX_HS,
    output wire        HDMI_TX_VS,
    inout  wire        HDMI_I2C_SCL,
    inout  wire        HDMI_I2C_SDA,
    output wire        HDMI_MCLK, HDMI_SCLK, HDMI_LRCLK, HDMI_I2S
);
    wire clk_sys;
    wire pixel_clk;
    wire audio_clk, audio_locked;
    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;

    spectrum_system_pll system_clock (
        .refclk(FPGA_CLK1_50), .rst(1'b0),
        .outclk_0(clk_sys), .audio_clk(audio_clk), .locked(audio_locked)
    );
    pixel_pll video_clock (.refclk(FPGA_CLK1_50), .rst(1'b0), .outclk_0(pixel_clk));

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps), .gp_out(hps_to_fpga)
    );

    wire hdmi_scl_low, hdmi_sda_low, hdmi_scl_in, hdmi_sda_in;
    MISTRAL_IO hdmi_scl_pad (.I(1'b0), .OE(hdmi_scl_low), .O(hdmi_scl_in), .PAD(HDMI_I2C_SCL));
    MISTRAL_IO hdmi_sda_pad (.I(1'b0), .OE(hdmi_sda_low), .O(hdmi_sda_in), .PAD(HDMI_I2C_SDA));
    (* BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0" *)
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .scl(hdmi_scl_in), .sda(hdmi_sda_in),
        .out_clk(hdmi_scl_low), .out_data(hdmi_sda_low)
    );

    wire exec_reset;
    wire [143:0] keyboard_rows;
    wire [15:0] controller_buttons;
    wire [15:0] media_write_addr;
    wire [15:0] media_write_data;
    wire [1:0] media_write_enable;
    wire [1:0] unit0_state;
    wire [31:0] unit0_size;
    fes_computer_mailbox #(
        .ENABLE_KEYBOARD(1), .ENABLE_PORTS(1), .ENABLE_AUDIO(1),
        .ENABLE_SPECTRUM_TAPE(1), .MEDIA_AW(16),
        .UNIT0_MIN(`FES_COMPUTER_SPECTRUM_TAPE_MIN_BYTES),
        .UNIT0_MAX(`FES_COMPUTER_SPECTRUM_TAPE_MAX_BYTES)
    ) gp_mailbox (
        .clk(clk_sys), .gpo(hps_to_fpga), .build_id(BUILD_ID), .gpi(fpga_to_hps),
        .exec_reset(exec_reset), .keyboard_rows(keyboard_rows),
        .controller_buttons(controller_buttons),
        .media_write_addr(media_write_addr), .media_write_data(media_write_data),
        .media_write_enable(media_write_enable),
        .unit0_state(unit0_state), .unit0_size(unit0_size)
    );

    wire [39:0] matrix;
    spectrum_keyboard keyboard (
        .clk(clk_sys), .reset(exec_reset), .rows(keyboard_rows), .matrix(matrix)
    );
    // Kempston: bit0 right, bit1 left, bit2 down, bit3 up, bit4 fire.
    wire [4:0] kempston = {
        controller_buttons[4] | controller_buttons[5],
        controller_buttons[0], controller_buttons[1],
        controller_buttons[2], controller_buttons[3]
    };

    wire [`SP_BUS_REQ-1:0] bus_request;
    wire [`SP_BUS_RSP-1:0] response1, response2, response3, response4;
    wire [2:0] border;
    wire flash_on, speaker, ear, cpu_cycle;
    wire signed [15:0] slot_audio;
    wire [15:0] video_addr;
    wire [7:0] video_data;
    spectrum_machine machine (
        .clk_sys(clk_sys), .reset(exec_reset), .matrix(matrix), .kempston(kempston),
        .unit_state(unit0_state), .unit_size(unit0_size),
        .media_write_addr(media_write_addr), .media_write_data(media_write_data),
        .media_write_enable(media_write_enable),
        .bus_request(bus_request),
        .response1(response1), .response2(response2),
        .response3(response3), .response4(response4),
        .border(border), .flash_on(flash_on), .speaker(speaker), .ear(ear),
        .cpu_cycle(cpu_cycle), .slot_audio(slot_audio),
        .video_clk(pixel_clk), .video_addr(video_addr), .video_data(video_data),
        .sig8000(), .sig8001(), .sig8002(), .sig8003(), .sig8004()
    );

    (* keep *) wire [`SP_BUS_REQ-1:0] slot1_plug_request;
    (* keep *) wire [`SP_BUS_REQ-1:0] slot2_plug_request;
    (* keep *) wire [`SP_BUS_REQ-1:0] slot3_plug_request;
    (* keep *) wire [`SP_BUS_REQ-1:0] slot4_plug_request;
    spectrum_slot_socket1 slot1 (
        .clock(clk_sys), .request(bus_request), .response(response1),
        .plug_request(slot1_plug_request), .plug_response(`SP_BUS_RSP'd0)
    );
    spectrum_slot_socket2 slot2 (
        .clock(clk_sys), .request(bus_request), .response(response2),
        .plug_request(slot2_plug_request), .plug_response(`SP_BUS_RSP'd0)
    );
    spectrum_slot_socket3 slot3 (
        .clock(clk_sys), .request(bus_request), .response(response3),
        .plug_request(slot3_plug_request), .plug_response(`SP_BUS_RSP'd0)
    );
    spectrum_slot_socket4 slot4 (
        .clock(clk_sys), .request(bus_request), .response(response4),
        .plug_request(slot4_plug_request), .plug_response(`SP_BUS_RSP'd0)
    );

    spectrum_video video (
        .pixel_clk(pixel_clk), .border(border), .flash_on(flash_on),
        .ram_addr(video_addr), .ram_data(video_data),
        .red(HDMI_TX_D[23:16]), .green(HDMI_TX_D[15:8]), .blue(HDMI_TX_D[7:0]),
        .de(HDMI_TX_DE), .hsync(HDMI_TX_HS), .vsync(HDMI_TX_VS),
        .pixel_x(), .pixel_y()
    );
    assign HDMI_TX_CLK = pixel_clk;

    wire signed [15:0] audio_sample;
    spectrum_audio audio_mix (
        .clk(clk_sys), .reset(exec_reset), .cpu_cycle(cpu_cycle),
        .speaker(speaker), .slot_audio(slot_audio), .sample(audio_sample)
    );
    fes_audio_output audio (
        .source_clk(clk_sys), .audio_clk(audio_clk), .locked(audio_locked), .hold(exec_reset),
        .left_sample(audio_sample), .right_sample(audio_sample),
        .sclk(HDMI_SCLK), .lrclk(HDMI_LRCLK), .sdata(HDMI_I2S)
    );
    assign HDMI_MCLK = audio_clk;
endmodule
