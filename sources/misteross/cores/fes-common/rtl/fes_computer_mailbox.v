// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"

// Port list used by shells that leave the session-display plane off.
// Response inputs are tied idle here because Verilator rejects ranged defaults.
// The endpoint itself is fes_computer_mailbox_core, which ZX81 instantiates
// so it can connect the session-display plane.
module fes_computer_mailbox #(
    parameter bit ENABLE_KEYBOARD = 0,
    parameter bit ENABLE_PORTS = 0,
    parameter bit ENABLE_MOUSE = 0,
    parameter bit ENABLE_AUDIO = 0,
    parameter bit ENABLE_APPLE2_FLOPPY = 0,
    parameter bit ENABLE_C64_DISK = 0,
    parameter bit ENABLE_SPECTRUM_TAPE = 0,
    parameter bit ENABLE_ZX81_TAPE = 0,
    parameter bit ENABLE_SESSION_DISPLAY = 0,
    parameter bit ENABLE_ATARI_ST_FLOPPY = 0,
    parameter bit ENABLE_ATARI_ST_FLOPPY_WRITE = 0,
    parameter bit ENABLE_ATARI_ST_FLOPPY_GEOMETRY = 0,
    parameter bit ENABLE_MEDIA_BACKPRESSURE = 0,
    parameter integer MEDIA_AW = 18,
    parameter [31:0] UNIT0_MIN = `FES_COMPUTER_APPLE2_FLOPPY_BYTES,
    parameter [31:0] UNIT0_MAX = `FES_COMPUTER_APPLE2_FLOPPY_BYTES
) (
    input  wire         clk,
    input  wire [31:0]  gpo,
    input  wire [127:0] build_id,
    output wire [31:0]  gpi,
    output wire         exec_reset,
    output wire [143:0] keyboard_rows,
    output wire [15:0]  controller_buttons,
    output wire         mouse_valid,
    output wire signed [15:0] mouse_dx,
    output wire signed [15:0] mouse_dy,
    output wire [1:0]    mouse_buttons,
    input  wire         mouse_ready,
    output wire [MEDIA_AW-1:0] media_write_addr,
    output wire [15:0]  media_write_data,
    output wire [1:0]   media_write_enable,
    input  wire        media_write_ready,
    input  wire        media_write_busy,
    input  wire        media_changed,
    output wire        media_frozen,
    output wire        media_read_req,
    output wire [19:0] media_read_addr,
    input  wire        media_read_ready,
    input  wire [7:0]  media_read_data,
    output wire [1:0]   unit0_state,
    output wire [31:0]  unit0_size
);
    /* verilator lint_off UNUSEDSIGNAL */
    wire unused_display_request;
    wire [6:0] unused_display_opcode;
    wire [7:0] unused_display_index;
    wire [15:0] unused_display_argument;
    /* verilator lint_on UNUSEDSIGNAL */
    fes_computer_mailbox_core #(
        .ENABLE_KEYBOARD(ENABLE_KEYBOARD),
        .ENABLE_PORTS(ENABLE_PORTS),
        .ENABLE_MOUSE(ENABLE_MOUSE),
        .ENABLE_AUDIO(ENABLE_AUDIO),
        .ENABLE_APPLE2_FLOPPY(ENABLE_APPLE2_FLOPPY),
        .ENABLE_C64_DISK(ENABLE_C64_DISK),
        .ENABLE_SPECTRUM_TAPE(ENABLE_SPECTRUM_TAPE),
        .ENABLE_ZX81_TAPE(ENABLE_ZX81_TAPE),
        .ENABLE_SESSION_DISPLAY(ENABLE_SESSION_DISPLAY),
        .ENABLE_ATARI_ST_FLOPPY(ENABLE_ATARI_ST_FLOPPY),
        .ENABLE_ATARI_ST_FLOPPY_WRITE(ENABLE_ATARI_ST_FLOPPY_WRITE),
        .ENABLE_ATARI_ST_FLOPPY_GEOMETRY(ENABLE_ATARI_ST_FLOPPY_GEOMETRY),
        .ENABLE_MEDIA_BACKPRESSURE(ENABLE_MEDIA_BACKPRESSURE),
        .MEDIA_AW(MEDIA_AW),
        .UNIT0_MIN(UNIT0_MIN),
        .UNIT0_MAX(UNIT0_MAX)
    ) core (
        .clk(clk), .gpo(gpo), .build_id(build_id), .gpi(gpi),
        .exec_reset(exec_reset), .keyboard_rows(keyboard_rows),
        .controller_buttons(controller_buttons),
        .mouse_valid(mouse_valid), .mouse_dx(mouse_dx), .mouse_dy(mouse_dy),
        .mouse_buttons(mouse_buttons), .mouse_ready(mouse_ready),
        .media_write_addr(media_write_addr), .media_write_data(media_write_data),
        .media_write_enable(media_write_enable), .media_write_ready(media_write_ready),
        .media_write_busy(media_write_busy), .media_changed(media_changed),
        .media_frozen(media_frozen),
        .media_read_req(media_read_req), .media_read_addr(media_read_addr),
        .media_read_ready(media_read_ready), .media_read_data(media_read_data),
        .unit0_state(unit0_state), .unit0_size(unit0_size),
        .display_request(unused_display_request), .display_opcode(unused_display_opcode),
        .display_index(unused_display_index), .display_argument(unused_display_argument),
        .display_response_valid(1'b0), .display_response_error(1'b0),
        .display_response_data(16'd0), .display_quiesced(1'b1)
    );
endmodule
