// SPDX-License-Identifier: GPL-3.0-or-later
module st_media_sim_top (
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
    fes_computer_mailbox #(
        .ENABLE_ATARI_ST_FLOPPY(1), .ENABLE_MEDIA_BACKPRESSURE(1), .MEDIA_AW(20),
        .UNIT0_MIN(737280), .UNIT0_MAX(737280)
    ) gp_endpoint (
        .clk(clk), .gpo(gpo), .gpi(gpi), .build_id(128'd0), .exec_reset(exec_reset),
        .keyboard_rows(unused_keyboard), .controller_buttons(unused_controller),
        .media_write_addr(source_addr), .media_write_data(source_data),
        .media_write_enable(source_enable), .media_write_ready(source_ready),
        .unit0_state(unit0_state), .unit0_size(unit0_size)
    );
    st_media_writer writer (.*);
endmodule
