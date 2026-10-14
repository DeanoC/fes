// SPDX-License-Identifier: GPL-3.0-or-later
module st_media_sim_top #(parameter bit GEOMETRY = 0) (
    input wire clk, cold_reset,
    input wire [31:0] gpo,
    output wire [31:0] gpi,
    output wire exec_reset,
    output wire [1:0] unit0_state,
    output wire [31:0] unit0_size,
    output wire memory_req,
    output wire [19:1] memory_addr,
    output wire [15:0] memory_wdata,
    output wire [1:0] memory_byte_enable,
    input wire memory_ready
);
    wire [19:0] source_addr;
    wire [15:0] source_data;
    wire [1:0] source_enable;
    wire source_ready;
    wire [143:0] unused_keyboard;
    wire [15:0] unused_controller;
    wire [34:0] unused_mouse;
    wire [21:0] unused_snapshot;
    wire unused_display_request;
    wire [6:0] unused_display_opcode;
    wire [7:0] unused_display_index;
    wire [15:0] unused_display_argument;
    fes_computer_mailbox #(
        .ENABLE_ATARI_ST_FLOPPY(1), .ENABLE_ATARI_ST_FLOPPY_GEOMETRY(GEOMETRY), .ENABLE_ATARI_ST_FLOPPY_WRITE(GEOMETRY), .ENABLE_MEDIA_BACKPRESSURE(1), .MEDIA_AW(20),
        .UNIT0_MIN(GEOMETRY ? 368640 : 737280), .UNIT0_MAX(GEOMETRY ? 839680 : 737280)
    ) gp_endpoint (
        .clk(clk), .gpo(gpo), .gpi(gpi), .build_id(128'd0), .exec_reset(exec_reset),
        .keyboard_rows(unused_keyboard), .controller_buttons(unused_controller),
        .mouse_valid(unused_mouse[0]), .mouse_dx(unused_mouse[16:1]), .mouse_dy(unused_mouse[32:17]), .mouse_buttons(unused_mouse[34:33]), .mouse_ready(1'b0),
        .media_write_busy(1'b0), .media_changed(1'b0), .media_frozen(unused_snapshot[0]),
        .media_read_req(unused_snapshot[1]), .media_read_addr(unused_snapshot[21:2]), .media_read_ready(1'b0), .media_read_data(8'd0),
        .media_write_addr(source_addr), .media_write_data(source_data),
        .media_write_enable(source_enable), .media_write_ready(source_ready),
        .unit0_state(unit0_state), .unit0_size(unit0_size),
        .display_request(unused_display_request), .display_opcode(unused_display_opcode),
        .display_index(unused_display_index), .display_argument(unused_display_argument),
        .display_response_valid(1'b0), .display_response_error(1'b0),
        .display_response_data(16'd0), .display_quiesced(1'b1)
    );
    st_media_writer writer (.*);
endmodule
