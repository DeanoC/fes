// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"
`include "apple2_bus.vh"
`ifndef FES_APPLE2_BUILD_ID
`define FES_APPLE2_BUILD_ID 128'h00000000000000000000000000000000
`endif

// DE10-Nano shell for the FES Apple II (fes.computer 1.0). The mailbox,
// machine, Disk II and slot bus run in the 52.224 MHz system domain; video
// is scanned from RAM in the 74.25 MHz HDMI domain; audio leaves through the
// shared 12.288 MHz I2S serializer. Slots 2, 4, 5 and 7 are physical sockets
// (vacant here; a linked card replaces the vacant response), slot 6 is the
// built-in Disk II controller, and slots 1 and 3 are vacant.
module top #(
    parameter [127:0] BUILD_ID = `FES_APPLE2_BUILD_ID
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

    apple2_system_pll system_clock (
        .refclk(FPGA_CLK1_50), .rst(1'b0),
        .outclk_0(clk_sys), .audio_clk(audio_clk), .locked(audio_locked)
    );
    pixel_pll video_clock (.refclk(FPGA_CLK1_50), .rst(1'b0), .outclk_0(pixel_clk));

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps), .gp_out(hps_to_fpga)
    );

    wire hdmi_scl_low;
    wire hdmi_sda_low;
    wire hdmi_scl_in;
    wire hdmi_sda_in;
    MISTRAL_IO hdmi_scl_pad (.I(1'b0), .OE(hdmi_scl_low), .O(hdmi_scl_in), .PAD(HDMI_I2C_SCL));
    MISTRAL_IO hdmi_sda_pad (.I(1'b0), .OE(hdmi_sda_low), .O(hdmi_sda_in), .PAD(HDMI_I2C_SDA));
    (* BEL = "cyclonev_hps_interface_peripheral_i2c.52.60.0" *)
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .scl(hdmi_scl_in), .sda(hdmi_sda_in),
        .out_clk(hdmi_scl_low), .out_data(hdmi_sda_low)
    );

    // ------------------------------------------------------------------
    // fes.computer mailbox, keyboard and media unit 0.
    // ------------------------------------------------------------------
    wire exec_reset;
    wire [143:0] keyboard_rows;
    wire [15:0] controller_buttons;
    wire [17:0] media_write_addr;
    wire [15:0] media_write_data;
    wire [1:0] media_write_enable;
    wire [1:0] unit0_state;
    fes_computer_mailbox #(
        .ENABLE_KEYBOARD(1), .ENABLE_PORTS(1), .ENABLE_AUDIO(1), .ENABLE_APPLE2_FLOPPY(1),
        .MEDIA_AW(18)
    ) gp_mailbox (
        .clk(clk_sys), .gpo(hps_to_fpga), .build_id(BUILD_ID), .gpi(fpga_to_hps),
        .exec_reset(exec_reset), .keyboard_rows(keyboard_rows),
        .controller_buttons(controller_buttons),
        .mouse_valid(), .mouse_dx(), .mouse_dy(), .mouse_buttons(), .mouse_ready(1'b0),
        .media_write_busy(1'b0), .media_changed(1'b0), .media_frozen(),
        .media_read_req(), .media_read_addr(), .media_read_ready(1'b0), .media_read_data(8'd0),
        .media_write_addr(media_write_addr), .media_write_data(media_write_data),
        .media_write_enable(media_write_enable), .media_write_ready(1'b1), .unit0_state(unit0_state),
        .unit0_size()
    );

    wire key_event;
    wire [6:0] key_code;
    wire reset_key;
    wire [1:0] apple_keys;
    apple2_keyboard keyboard (
        .clk(clk_sys), .reset(exec_reset), .rows(keyboard_rows),
        .key_event(key_event), .key_code(key_code), .reset_key(reset_key),
        .apple_keys(apple_keys)
    );

    // Controller port 0 is the joystick: A/B are buttons 0/1 (with the Alt
    // keys), the D-pad sets paddles 0/1 to their end stops. Port 1 drives
    // paddles 2/3 and button 2.
    function [7:0] axis;
        input low, high;
        axis = low ? 8'd0 : high ? 8'd255 : 8'd128;
    endfunction
    wire [2:0] buttons = {controller_buttons[12],
                          controller_buttons[5] | apple_keys[1],
                          controller_buttons[4] | apple_keys[0]};
    wire [31:0] paddles = {axis(controller_buttons[8], controller_buttons[9]),
                           axis(controller_buttons[10], controller_buttons[11]),
                           axis(controller_buttons[0], controller_buttons[1]),
                           axis(controller_buttons[2], controller_buttons[3])};

    // ------------------------------------------------------------------
    // Machine and slot bus.
    // ------------------------------------------------------------------
    wire [`A2_BUS_REQ-1:0] slot_request;
    wire [7:0] slot_devsel;
    wire [7:0] slot_iosel;
    wire [`A2_BUS_RSP-1:0] response [1:7];
    wire text_mode, mixed_mode, page2, hires_mode;
    wire [15:0] video_addr;
    wire [7:0] video_data;
    wire speaker;
    wire signed [15:0] slot_audio;
    wire cpu_cycle;

    function [`A2_BUS_REQ-1:0] slot_word;
        input [`A2_BUS_REQ-1:0] common;
        input devsel, iosel;
        begin
            slot_word = common;
            slot_word[`A2_BUS_DEVSEL] = devsel;
            slot_word[`A2_BUS_IOSEL] = iosel;
        end
    endfunction

    /* verilator lint_off PINCONNECTEMPTY */
    apple2_machine machine (
        .clk_sys(clk_sys), .reset(exec_reset), .reset_key(reset_key),
        .key_event(key_event), .key_code(key_code),
        .buttons(buttons), .paddles(paddles), .cassette_in(1'b0),
        .slot_request(slot_request), .slot_devsel(slot_devsel), .slot_iosel(slot_iosel),
        .slot_response({response[7], response[6], response[5], response[4],
                        response[3], response[2], response[1], `A2_BUS_RSP'd0}),
        .video_text(text_mode), .video_mixed(mixed_mode),
        .video_page2(page2), .video_hires(hires_mode), .annunciators(),
        .video_clk(pixel_clk), .video_addr(video_addr), .video_data(video_data),
        .speaker(speaker), .cassette_out(), .slot_audio(slot_audio),
        .debug_pc_addr(), .cpu_cycle(cpu_cycle), .debug_bus_data(), .debug_bus_write()
    );
    /* verilator lint_on PINCONNECTEMPTY */

    assign response[1] = `A2_BUS_RSP'd0;
    assign response[3] = `A2_BUS_RSP'd0;

    // Physical sockets. The vacant plug response is constant zero; card
    // linking replaces these boundary registers' inputs inside each socket.
    (* keep *) wire [`A2_BUS_REQ-1:0] slot2_plug_request;
    (* keep *) wire [`A2_BUS_REQ-1:0] slot4_plug_request;
    (* keep *) wire [`A2_BUS_REQ-1:0] slot5_plug_request;
    (* keep *) wire [`A2_BUS_REQ-1:0] slot7_plug_request;
    apple2_slot_socket2 slot2 (
        .clock(clk_sys), .request(slot_word(slot_request, slot_devsel[2], slot_iosel[2])),
        .response(response[2]), .plug_request(slot2_plug_request), .plug_response(`A2_BUS_RSP'd0)
    );
    apple2_slot_socket4 slot4 (
        .clock(clk_sys), .request(slot_word(slot_request, slot_devsel[4], slot_iosel[4])),
        .response(response[4]), .plug_request(slot4_plug_request), .plug_response(`A2_BUS_RSP'd0)
    );
    apple2_slot_socket5 slot5 (
        .clock(clk_sys), .request(slot_word(slot_request, slot_devsel[5], slot_iosel[5])),
        .response(response[5]), .plug_request(slot5_plug_request), .plug_response(`A2_BUS_RSP'd0)
    );
    apple2_slot_socket7 slot7 (
        .clock(clk_sys), .request(slot_word(slot_request, slot_devsel[7], slot_iosel[7])),
        .response(response[7]), .plug_request(slot7_plug_request), .plug_response(`A2_BUS_RSP'd0)
    );

    // Slot 6: built-in Disk II controller and drive 1 on media unit 0.
    wire [3:0] phases;
    wire motor_on, drive2, bit_ce, read_bit, write_protect;
    wire [17:0] disk_read_addr;
    wire [7:0] disk_read_data;
    apple2_disk2_card disk_card (
        .clk(clk_sys), .request(slot_request), .devsel(slot_devsel[6]),
        .response(response[6]), .phases(phases), .motor_on(motor_on), .drive2(drive2),
        .bit_ce(bit_ce), .read_bit(read_bit), .write_protect(write_protect)
    );
    apple2_disk_store disk_store (
        .clk(clk_sys), .write_addr(media_write_addr), .write_data(media_write_data),
        .write_enable(media_write_enable), .read_addr(disk_read_addr), .read_data(disk_read_data)
    );
    /* verilator lint_off PINCONNECTEMPTY */
    apple2_disk2_drive drive (
        .clk(clk_sys), .reset(exec_reset), .cpu_ce(cpu_cycle), .phases(phases),
        .motor_on(motor_on), .drive2(drive2),
        .disk_present(unit0_state == 2'(`FES_COMPUTER_MEDIA_STATE_READY)),
        .media_addr(disk_read_addr), .media_q(disk_read_data), .bit_ce(bit_ce),
        .read_bit(read_bit), .write_protect(write_protect), .track(), .quarter_track()
    );

    // ------------------------------------------------------------------
    // Video and audio.
    // ------------------------------------------------------------------
    apple2_video video (
        .pixel_clk(pixel_clk), .text_mode(text_mode), .mixed_mode(mixed_mode),
        .page2(page2), .hires_mode(hires_mode), .ram_addr(video_addr), .ram_data(video_data),
        .red(HDMI_TX_D[23:16]), .green(HDMI_TX_D[15:8]), .blue(HDMI_TX_D[7:0]),
        .de(HDMI_TX_DE), .hsync(HDMI_TX_HS), .vsync(HDMI_TX_VS), .frame_tick()
    );
    /* verilator lint_on PINCONNECTEMPTY */
    assign HDMI_TX_CLK = pixel_clk;

    wire signed [15:0] audio_sample;
    apple2_audio audio_mix (
        .clk(clk_sys), .reset(exec_reset), .cpu_cycle(cpu_cycle), .speaker(speaker),
        .slot_audio(slot_audio), .sample(audio_sample)
    );
    fes_audio_output audio (
        .source_clk(clk_sys), .audio_clk(audio_clk), .locked(audio_locked), .hold(exec_reset),
        .left_sample(audio_sample), .right_sample(audio_sample),
        .sclk(HDMI_SCLK), .lrclk(HDMI_LRCLK), .sdata(HDMI_I2S)
    );
    assign HDMI_MCLK = audio_clk;
endmodule
