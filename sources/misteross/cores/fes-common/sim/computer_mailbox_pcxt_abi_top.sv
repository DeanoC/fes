// SPDX-License-Identifier: GPL-3.0-or-later
// Combine a PC/XT capability with a mainline 1.1 capability to guard word 16.
`include "fes_computer.vh"
module computer_mailbox_pcxt_abi_top (
    input wire clk,
    input wire [31:0] gpo,
    output wire [31:0] gpi
);
    /* verilator lint_off PINCONNECTEMPTY */
    fes_computer_mailbox_core #(
        .ENABLE_PCXT_FLOPPY(1),
        .ENABLE_ATA_DISK(1),
        .MEDIA_AW(19),
        .UNIT0_MIN(`FES_COMPUTER_PCXT_FLOPPY_BYTES),
        .UNIT0_MAX(`FES_COMPUTER_PCXT_FLOPPY_BYTES)
    ) core (
        .clk(clk), .gpo(gpo), .build_id(128'd0), .gpi(gpi), .exec_reset(),
        .keyboard_rows(), .controller_buttons(), .mouse_valid(), .mouse_dx(), .mouse_dy(),
        .mouse_buttons(), .mouse_ready(1'b0),
        .media_write_addr(), .media_write_data(), .media_write_enable(), .media_write_unit(),
        .media_write_ready(1'b1), .media_write_busy(1'b0), .media_changed(1'b0), .media_reset(1'b0),
        .media_frozen(), .media_read_req(), .media_read_addr(),
        .media_read_ready(1'b0), .media_read_data(8'd0),
        .save_ready(1'b0), .save_busy(1'b0), .save_size(15'd0),
        .unit0_state(), .unit0_size(), .unit1_state(), .unit1_size(),
        .display_request(), .display_opcode(), .display_index(), .display_argument(),
        .display_response_valid(1'b0), .display_response_error(1'b0),
        .display_response_data(16'd0), .display_quiesced(1'b1),
        .storage_request(), .storage_opcode(), .storage_index(), .storage_argument(),
        .storage_response_valid(1'b0), .storage_response_error(1'b0),
        .storage_response_data(16'd0), .execution_accepted()
    );
    /* verilator lint_on PINCONNECTEMPTY */
endmodule
