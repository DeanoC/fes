// SPDX-License-Identifier: GPL-2.0-or-later
module fes_menu_endpoint (
    input wire clk, reset_hold,
    input wire [31:0] gpo,
    input wire [127:0] build_id,
    output wire [31:0] gpi, displayed_sequence, underflows,
    output wire exec_reset, enable, quiesced, faulted,
    output wire [23:0] rgb,
    output wire de, hs, vs
);
    wire request, response_valid, response_error, quiesce, submit_valid, submit_slot, submit_ready;
    wire [6:0] opcode;
    wire [7:0] index;
    wire [15:0] argument, response_data;
    wire [31:0] submit_sequence;
    fes_application_gp #(.ENABLE_HPS_DDR(1), .ENABLE_MENU(1)) gp (
        .clk(clk), .gpo(gpo), .build_id(build_id), .gpi(gpi), .exec_reset(exec_reset),
        .buttons(), .controller_buttons(), .controller_keypad(), .media_write_addr(),
        .media_write_data(), .media_write_enable(), .media_ready(), .media_size(),
        .media_byte0(), .media_byte1(), .media_byte2(), .firmware_write_addr(),
        .firmware_write_data(), .firmware_write_enable(), .firmware_ready(),
        .menu_request(request), .menu_opcode(opcode), .menu_index(index), .menu_argument(argument),
        .menu_response_valid(response_valid), .menu_response_error(response_error),
        .menu_response_data(response_data), .menu_quiesced(quiesced)
    );
    fes_menu_control control (
        .clk(clk), .request(request), .opcode(opcode), .index(index), .argument(argument),
        .exec_reset(exec_reset), .drained(quiesced), .faulted(faulted),
        .submit_ready(submit_ready), .displayed_sequence(displayed_sequence), .underflows(underflows),
        .response_valid(response_valid), .response_error(response_error), .response_data(response_data),
        .enable(enable), .quiesce(quiesce), .submit_valid(submit_valid), .submit_slot(submit_slot), .submit_sequence(submit_sequence)
    );
    fes_menu_ddr scanout (
        .clk(clk), .reset_hold(reset_hold || exec_reset), .enable(enable && !reset_hold), .quiesce(quiesce),
        .submit_valid(submit_valid), .submit_slot(submit_slot), .submit_sequence(submit_sequence),
        .submit_ready(submit_ready), .quiesced(quiesced), .faulted(faulted),
        .displayed_sequence(displayed_sequence), .underflows(underflows), .rgb(rgb), .de(de), .hs(hs), .vs(vs)
    );
endmodule
