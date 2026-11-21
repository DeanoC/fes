// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"
`include "c64_bus.vh"
// Commodore 64 machine, control and media for a board without linked ROM
// lanes or an HPS DDR media window (fes.computer 1.0 with
// fes.fabric.firmware-upload). The machine sources are the DE10 core's; this
// replaces its shell. clk_sys is the machine's 52.224 MHz. The 36 KiB of
// firmware arrives through the mailbox front end before execution can be
// released. The D64 (media unit 0) stays in the machine's own block RAM disk
// store, as on the DE10, and both cartridge sockets are vacant: a vacant
// response is all zero. The machine scans its picture out in the pixel clock
// as the DE10's does, and that word goes to the session display.
// Board wrappers own clocks, HPS transport, HDMI and audio serialization.
// No FPGA-family primitive belongs here.
module fes_c64_core #(
    parameter [127:0] BUILD_ID = 128'd0
) (
    input wire clk_sys, pixel_clk, reset_hold,
    input wire [31:0] gpo,
    output wire [31:0] gpi,
    output wire exec_reset,
    output wire [27:0] machine_video,
    output wire signed [15:0] audio_sample,
    output wire display_request,
    output wire [6:0] display_opcode,
    output wire [7:0] display_index,
    output wire [15:0] display_argument,
    input wire display_response_valid, display_response_error,
    input wire [15:0] display_response_data,
    input wire display_quiesced
);
    localparam [31:0] FIRMWARE_BYTES = 32'd36864;
    wire machine_reset = exec_reset || reset_hold;
    wire [31:0] mailbox_gpo, mailbox_gpi;
    wire [15:0] firmware_write_addr, firmware_write_data;
    wire [1:0] firmware_write_enable;
    fes_firmware_upload #(.AW(16), .BYTES(FIRMWARE_BYTES)) firmware (
        .clk(clk_sys), .gpo(gpo), .gpi(gpi),
        .mailbox_gpo(mailbox_gpo), .mailbox_gpi(mailbox_gpi), .exec_reset(exec_reset),
        .ready(), .write_addr(firmware_write_addr), .write_data(firmware_write_data),
        .write_enable(firmware_write_enable), .write_ready(1'b1)
    );

    wire [143:0] keyboard_rows;
    wire [15:0] controller_buttons;
    wire [17:0] media_write_addr;
    wire [15:0] media_write_data;
    wire [1:0] media_write_enable, unit0_state;
    fes_computer_mailbox_core #(
        .ENABLE_KEYBOARD(1), .ENABLE_PORTS(1), .ENABLE_AUDIO(1), .ENABLE_C64_DISK(1),
        .ENABLE_SESSION_DISPLAY(1), .ENABLE_HPS_DDR_WINDOW(0),
        .MEDIA_AW(18),
        .UNIT0_MIN(`FES_COMPUTER_C64_DISK_BYTES),
        .UNIT0_MAX(`FES_COMPUTER_C64_DISK_BYTES)
    ) gp_mailbox (
        .clk(clk_sys), .gpo(mailbox_gpo), .build_id(BUILD_ID), .gpi(mailbox_gpi),
        .exec_reset(exec_reset), .keyboard_rows(keyboard_rows),
        .controller_buttons(controller_buttons),
        .mouse_valid(), .mouse_dx(), .mouse_dy(), .mouse_buttons(), .mouse_ready(1'b0),
        .media_write_busy(1'b0), .media_changed(1'b0), .media_reset(1'b0), .media_frozen(),
        .media_read_req(), .media_read_addr(), .media_read_ready(1'b0), .media_read_data(8'd0),
        .save_ready(1'b0), .save_busy(1'b0), .save_size(15'd0),
        .media_write_addr(media_write_addr), .media_write_data(media_write_data),
        .media_write_enable(media_write_enable), .media_write_unit(),
        .media_write_ready(1'b1),
        .unit0_state(unit0_state), .unit0_size(),
        .unit1_state(), .unit1_size(),
        .display_request(display_request), .display_opcode(display_opcode),
        .display_index(display_index), .display_argument(display_argument),
        .display_response_valid(display_response_valid),
        .display_response_error(display_response_error),
        .display_response_data(display_response_data),
        .display_quiesced(display_quiesced),
        .storage_request(), .storage_opcode(), .storage_index(), .storage_argument(),
        .storage_response_valid(1'b0), .storage_response_error(1'b0), .storage_response_data(16'd0),
        .execution_accepted()
    );

    wire [7:0] red, green, blue;
    wire machine_de, machine_hs, machine_vs;
    /* verilator lint_off PINCONNECTEMPTY */
    c64_machine machine (
        .clk_sys(clk_sys), .reset(machine_reset),
        .keyboard_rows(keyboard_rows), .controller_buttons(controller_buttons),
        .media_write_addr(media_write_addr), .media_write_data(media_write_data),
        .media_write_enable(media_write_enable),
        .disk_ready(unit0_state == 2'(`FES_COMPUTER_MEDIA_STATE_READY)),
        .slot1_request(), .slot2_request(),
        .slot1_response(`C64_BUS_RSP'd0), .slot2_response(`C64_BUS_RSP'd0),
        .video_clk(pixel_clk),
        .red(red), .green(green), .blue(blue), .de(machine_de), .hsync(machine_hs), .vsync(machine_vs),
        .audio_sample(audio_sample),
        .debug_stage(), .debug_error(), .debug_iec(), .debug_pc(),
        .firmware_write_addr(firmware_write_addr), .firmware_write_data(firmware_write_data),
        .firmware_write_enable(firmware_write_enable)
    );
    /* verilator lint_on PINCONNECTEMPTY */
    assign machine_video = {1'b1, machine_vs, machine_hs, machine_de, red, green, blue};
endmodule
