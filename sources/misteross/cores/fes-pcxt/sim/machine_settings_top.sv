// SPDX-License-Identifier: GPL-2.0-or-later
module machine_settings_top (
    input  wire        clk,
    input  wire [31:0] gpo,
    input  wire [127:0] build_id,
    output wire [31:0] gpi,
    output wire        exec_reset,
    output wire [1:0]  video_card,
    output wire [1:0]  mono_tint
);
    wire storage_request, storage_response_valid, storage_response_error, execution_accepted;
    wire [6:0] storage_opcode;
    wire [7:0] storage_index;
    wire [15:0] storage_argument, storage_response_data;
    /* verilator lint_off PINCONNECTEMPTY */
    fes_computer_mailbox_pcxt_machine #(
        .ENABLE_KEYBOARD(1), .ENABLE_AUDIO(1), .ENABLE_WRITE(1), .ENABLE_MACHINE(1)
    ) mailbox (
        .clk(clk), .gpo(gpo), .build_id(build_id), .gpi(gpi),
        .exec_reset(exec_reset), .keyboard_rows(), .media_reset(1'b0),
        .media_write_addr(), .media_write_data(), .media_write_enable(),
        .media_write_ready(1'b1), .media_write_busy(1'b0), .media_changed(1'b0),
        .media_frozen(), .media_read_req(), .media_read_addr(),
        .media_read_ready(1'b0), .media_read_data(8'd0),
        .unit0_state(), .unit0_size(),
        .storage_request(storage_request), .storage_opcode(storage_opcode),
        .storage_index(storage_index), .storage_argument(storage_argument),
        .storage_response_valid(storage_response_valid),
        .storage_response_error(storage_response_error),
        .storage_response_data(storage_response_data),
        .execution_accepted(execution_accepted)
    );
    /* verilator lint_on PINCONNECTEMPTY */
    pcxt_machine_settings settings (
        .clk(clk), .exec_reset(exec_reset), .execution_accepted(execution_accepted),
        .request(storage_request), .opcode(storage_opcode), .index(storage_index),
        .argument(storage_argument), .response_valid(storage_response_valid),
        .response_error(storage_response_error), .response_data(storage_response_data),
        .video_card(video_card), .mono_tint(mono_tint)
    );
endmodule
