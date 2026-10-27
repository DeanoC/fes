// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"
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
    wire menu_valid, menu_error, blit_valid, blit_error, blit_run, blit_slot, blit_idle, blit_faulted;
    wire [6:0] opcode;
    wire [7:0] index;
    wire [15:0] argument, response_data, menu_data, blit_data, blit_count, blit_completed;
    wire [31:0] submit_sequence, blit_offset;
    wire [2:0] blit_fault_code;
    // MenuBlit goes to the blitter's control, every other menu opcode to the menu's.
    wire blit_request = request && {25'd0, opcode} == `FES_APPLICATION_OPCODE_MENU_BLIT;
    assign response_valid = menu_valid || blit_valid;
    assign response_error = blit_valid ? blit_error : menu_error;
    assign response_data = blit_valid ? blit_data : menu_data;
    fes_application_gp #(.ENABLE_HPS_DDR(1), .ENABLE_MENU(1), .ENABLE_MENU_BLITTER(1)) gp (
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
        .clk(clk), .request(request && !blit_request), .opcode(opcode), .index(index), .argument(argument),
        .exec_reset(exec_reset), .drained(quiesced), .faulted(faulted),
        .submit_ready(submit_ready), .displayed_sequence(displayed_sequence), .underflows(underflows),
        .response_valid(menu_valid), .response_error(menu_error), .response_data(menu_data),
        .enable(enable), .quiesce(quiesce), .submit_valid(submit_valid), .submit_slot(submit_slot), .submit_sequence(submit_sequence)
    );
    fes_menu_blit_control blit_control (
        .clk(clk), .request(blit_request), .enable(enable && !quiesce && !faulted), .idle(blit_idle),
        .faulted(blit_faulted), .fault_code(blit_fault_code), .completed(blit_completed),
        .index(index), .argument(argument),
        .response_valid(blit_valid), .response_error(blit_error), .response_data(blit_data),
        .run_valid(blit_run), .list_offset(blit_offset), .list_count(blit_count), .target_slot(blit_slot)
    );
    fes_menu_ddr #(.BLITTER(1)) scanout (
        .clk(clk), .reset_hold(reset_hold || exec_reset), .enable(enable && !reset_hold), .quiesce(quiesce),
        .submit_valid(submit_valid), .submit_slot(submit_slot), .submit_sequence(submit_sequence),
        .blit_valid(blit_run), .blit_slot(blit_slot), .blit_offset(blit_offset), .blit_count(blit_count),
        .blit_idle(blit_idle), .blit_faulted(blit_faulted), .blit_fault_code(blit_fault_code),
        .blit_completed(blit_completed),
        .submit_ready(submit_ready), .quiesced(quiesced), .faulted(faulted),
        .displayed_sequence(displayed_sequence), .underflows(underflows), .rgb(rgb), .de(de), .hs(hs), .vs(vs)
    );
endmodule
