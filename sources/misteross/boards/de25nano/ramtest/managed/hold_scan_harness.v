// SPDX-License-Identifier: GPL-2.0-or-later
// Integration harness: real mailbox, hold guard and scan engine on one clock.
module fes_de25_ramtest_hold_scan_harness (
    input wire clk, reset_n, transport_ok,
    input wire [31:0] gpo,
    output wire [31:0] gpi,
    output wire drained,
    output wire [31:0] address,
    output wire [4:0] burstcount,
    output wire read, write,
    output wire [15:0] writedata,
    output wire [1:0] byteenable,
    input wire waitrequest, readdatavalid, writeresponsevalid,
    input wire [15:0] readdata,
    input wire [1:0] response
);
    wire [31:0] mailbox_gpo;
    wire hold_requested, exec_reset;
    fes_de25_ramtest_hold_guard guard (.clk(clk), .gpo(gpo), .mailbox_gpi(gpi),
        .drained(drained), .mailbox_gpo(mailbox_gpo), .hold_requested(hold_requested));
    fes_computer_mailbox_core mailbox_inst (
        .clk(clk), .gpo(mailbox_gpo), .build_id(128'd1), .gpi(gpi),
        .exec_reset(exec_reset), .keyboard_rows(), .controller_buttons(),
        .mouse_valid(), .mouse_dx(), .mouse_dy(), .mouse_buttons(), .mouse_ready(1'b0),
        .media_write_addr(), .media_write_data(), .media_write_enable(), .media_write_unit(),
        .media_write_ready(1'b1), .media_write_busy(1'b0), .media_changed(1'b0),
        .media_reset(1'b0), .media_frozen(), .media_read_req(), .media_read_addr(),
        .media_read_ready(1'b0), .media_read_data(8'd0), .save_ready(1'b0),
        .save_busy(1'b0), .save_size(15'd0), .unit0_state(), .unit0_size(),
        .unit1_state(), .unit1_size(), .display_request(), .display_opcode(),
        .display_index(), .display_argument(), .display_response_valid(1'b0),
        .display_response_error(1'b0), .display_response_data(16'd0),
        .display_quiesced(1'b1), .storage_request(), .storage_opcode(),
        .storage_index(), .storage_argument(), .storage_response_valid(1'b0),
        .storage_response_error(1'b0), .storage_response_data(16'd0), .execution_accepted()
    );
    fes_de25_managed_ram_scan #(.TEST_BYTES(32), .MAX_BURST(16), .TIMEOUT(64),
        .LATENCY_SAMPLES(1)) scan (.clk(clk), .reset_n(reset_n),
        .run_permitted(!exec_reset && transport_ok), .hold_requested(hold_requested), .calibrated(1'b1),
        .drained(drained), .result(), .result_flags(), .address(address), .burstcount(burstcount),
        .read(read), .write(write), .writedata(writedata), .byteenable(byteenable),
        .waitrequest(waitrequest), .readdatavalid(readdatavalid),
        .writeresponsevalid(writeresponsevalid), .readdata(readdata), .response(response));
endmodule
