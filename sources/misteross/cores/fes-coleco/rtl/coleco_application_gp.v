// SPDX-License-Identifier: GPL-2.0-or-later
// Application endpoint plus the console's bounded cartridge staging store.
module coleco_application_gp (
    input wire clk,
    input wire [31:0] gpo,
    input wire [127:0] build_id,
    output wire [31:0] gpi,
    output wire exec_reset,
    output wire [15:0] controller_buttons,
    output wire [23:0] controller_keypad,
    output wire media_ready,
    output wire [15:0] media_size,
    input wire [14:0] media_addr,
    output wire [7:0] media_q
);
    wire [14:0] write_addr;
    wire [15:0] write_data;
    wire [1:0] write_enable;
    fes_application_gp #(.ENABLE_CONTROLLER_PORTS(1), .ENABLE_KEYPAD_PORTS(1),
                          .ENABLE_MEDIA(1), .ENABLE_MEDIA_STREAM(1)) endpoint (
        .clk(clk), .gpo(gpo), .build_id(build_id), .gpi(gpi),
        .exec_reset(exec_reset), .buttons(),
        .controller_buttons(controller_buttons), .controller_keypad(controller_keypad),
        .media_ready(media_ready), .media_size(media_size),
        .media_byte0(), .media_byte1(), .media_byte2(),
        .media_write_addr(write_addr), .media_write_data(write_data),
        .media_write_enable(write_enable)
    );
    coleco_dpram #(.ADDRWIDTH(15), .NUMWORDS(32768)) media_ram (
        .clock(clk), .address_a(write_enable[0] ? write_addr : media_addr),
        .data_a(write_data[7:0]), .wren_a(write_enable[0]), .q_a(media_q),
        .address_b(write_addr + 15'd1), .data_b(write_data[15:8]),
        .wren_b(write_enable[1]), .q_b()
    );
endmodule
