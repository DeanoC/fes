// SPDX-License-Identifier: GPL-2.0-or-later
// Application endpoint plus the console's bounded cartridge staging store.
module coleco_application_gp #(
    parameter bit ENABLE_FIRMWARE = 0,
    // The native shell's fes.video.session-display plane, through HPS DDR.
    parameter bit ENABLE_SESSION_DISPLAY = 0
) (
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
    output wire [7:0] media_q,
    output wire [12:0] firmware_write_addr,
    output wire [15:0] firmware_write_data,
    output wire [1:0] firmware_write_enable,
    output wire display_request,
    output wire [6:0] display_opcode,
    output wire [7:0] display_index,
    output wire [15:0] display_argument,
    input wire display_response_valid, display_response_error, display_quiesced,
    input wire [15:0] display_response_data
);
    wire [14:0] write_addr;
    wire [15:0] write_data;
    wire [1:0] write_enable;
`ifdef FES_COLECO_MEGACART_LINK
    localparam bit ENABLE_MEDIA_ENDPOINT = 0;
`elsif FES_COLECO_ROM_LINK
    localparam bit ENABLE_MEDIA_ENDPOINT = 0;
`else
    localparam bit ENABLE_MEDIA_ENDPOINT = 1;
`endif
    fes_application_gp #(.ENABLE_CONTROLLER_PORTS(1), .ENABLE_KEYPAD_PORTS(1),
                          .ENABLE_MEDIA(ENABLE_MEDIA_ENDPOINT), .ENABLE_MEDIA_STREAM(ENABLE_MEDIA_ENDPOINT),
                          .ENABLE_AUDIO(1), .ENABLE_FIRMWARE(ENABLE_FIRMWARE),
                          .ENABLE_HPS_DDR(ENABLE_SESSION_DISPLAY), .ENABLE_SESSION_DISPLAY(ENABLE_SESSION_DISPLAY)) endpoint (
        .clk(clk), .gpo(gpo), .build_id(build_id), .menu_request(display_request), .menu_opcode(display_opcode),
        .menu_index(display_index), .menu_argument(display_argument),
        .menu_response_valid(display_response_valid), .menu_response_error(display_response_error),
        .menu_response_data(display_response_data), .menu_quiesced(display_quiesced),
        .gpi(gpi),
        .exec_reset(exec_reset), .buttons(),
        .controller_buttons(controller_buttons), .controller_keypad(controller_keypad),
        .media_ready(media_ready), .media_size(media_size),
        .media_byte0(), .media_byte1(), .media_byte2(),
        .media_write_addr(write_addr), .media_write_data(write_data),
        .media_write_enable(write_enable),
        .firmware_write_addr(firmware_write_addr), .firmware_write_data(firmware_write_data),
        .firmware_write_enable(firmware_write_enable), .firmware_ready()
    );
`ifdef FES_COLECO_MEGACART_LINK
    assign media_q = 8'hff;
`elsif FES_COLECO_ROM_LINK
    assign media_q = 8'hff;
`else
    coleco_dpram #(.ADDRWIDTH(15), .NUMWORDS(32768)) media_ram (
        .clock(clk), .address_a(write_enable[0] ? write_addr : media_addr),
        .data_a(write_data[7:0]), .wren_a(write_enable[0]), .q_a(media_q),
        .address_b(write_addr + 15'd1), .data_b(write_data[15:8]),
        .wren_b(write_enable[1]), .q_b()
    );
`endif
endmodule
