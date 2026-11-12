// SPDX-License-Identifier: GPL-2.0-or-later
// Application endpoint with a bounded staging store for diagnostic cartridges.
module sg1000_application_gp (
    input wire clk,
    input wire [31:0] gpo,
    input wire [127:0] build_id,
    output wire [31:0] gpi,
    output wire exec_reset,
    output wire [15:0] controller_buttons,
    output wire media_ready,
    output wire [14:0] media_size,
    input wire [13:0] media_addr,
    output wire [7:0] media_q
);
    wire [13:0] write_addr;
    wire [15:0] write_data;
    wire [1:0] write_enable;
`ifdef FES_SG1000_ROM_LINK
    localparam bit ENABLE_MEDIA_ENDPOINT = 0;
`else
    localparam bit ENABLE_MEDIA_ENDPOINT = 1;
`endif
    fes_application_gp #(.ENABLE_CONTROLLER_PORTS(1), .ENABLE_MEDIA(ENABLE_MEDIA_ENDPOINT),
                         .ENABLE_AUDIO(1)) endpoint (
        .clk(clk), .gpo(gpo), .build_id(build_id), .gpi(gpi), .exec_reset(exec_reset),
        .buttons(), .controller_buttons(controller_buttons), .controller_keypad(),
        .media_write_addr(write_addr), .media_write_data(write_data),
        .media_write_enable(write_enable), .media_ready(media_ready), .media_size(media_size),
        .media_byte0(), .media_byte1(), .media_byte2(),
        .firmware_write_addr(), .firmware_write_data(), .firmware_write_enable(), .firmware_ready(),
        .menu_request(), .menu_opcode(), .menu_index(), .menu_argument(),
        .menu_response_valid(1'b0), .menu_response_error(1'b0), .menu_quiesced(1'b1),
        .menu_response_data(16'd0)
    );
`ifdef FES_SG1000_ROM_LINK
    assign media_q = 8'hff;
`else
    coleco_dpram #(.ADDRWIDTH(14), .NUMWORDS(16384)) media_ram (
        .clock(clk), .address_a(write_enable[0] ? write_addr : media_addr),
        .data_a(write_data[7:0]), .wren_a(write_enable[0]), .q_a(media_q),
        .address_b(write_addr + 14'd1), .data_b(write_data[15:8]),
        .wren_b(write_enable[1]), .q_b()
    );
`endif
endmodule
