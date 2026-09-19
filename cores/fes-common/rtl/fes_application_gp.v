// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"

// One-request-at-a-time HPS GPO/GPI mailbox for fes.application 1.0.
module fes_application_gp #(
    parameter bit ENABLE_GAMEPAD = 0,
    parameter bit ENABLE_MEDIA = 0
) (
    input  wire         clk,
    input  wire [31:0]  gpo,
    input  wire [127:0] build_id,
    output wire [31:0]  gpi,
    output reg          exec_reset,
    output reg [7:0]    buttons,
    output wire [13:0]  media_write_addr,
    output wire [15:0]  media_write_data,
    output wire [1:0]   media_write_enable,
    output reg          media_ready,
    output reg  [14:0]  media_size,
    output reg  [7:0]   media_byte0,
    output reg  [7:0]   media_byte1,
    output reg  [7:0]   media_byte2
);
    localparam [31:0] CAPABILITIES =
        `FES_APPLICATION_INTERFACE_VIDEO_FIXED_720P60_CAPABILITY_MASK |
        (ENABLE_GAMEPAD ? `FES_APPLICATION_INTERFACE_GAMEPAD_CAPABILITY_MASK : 32'd0) |
        (ENABLE_MEDIA ? `FES_APPLICATION_INTERFACE_MEDIA_BLOB_CAPABILITY_MASK : 32'd0);
    localparam [31:0] ID_MAGIC0_INDEX = `FES_APPLICATION_IDENTITY_MAGIC0_INDEX;
    localparam [31:0] ID_MAGIC1_INDEX = `FES_APPLICATION_IDENTITY_MAGIC1_INDEX;
    localparam [31:0] ID_TRANSPORT_MAJOR_INDEX = `FES_APPLICATION_IDENTITY_TRANSPORT_MAJOR_INDEX;
    localparam [31:0] ID_TRANSPORT_MINOR_INDEX = `FES_APPLICATION_IDENTITY_TRANSPORT_MINOR_INDEX;
    localparam [31:0] ID_ABI_TAG_INDEX = `FES_APPLICATION_IDENTITY_ABI_TAG_INDEX;
    localparam [31:0] ID_ABI_MAJOR_INDEX = `FES_APPLICATION_IDENTITY_ABI_MAJOR_INDEX;
    localparam [31:0] ID_ABI_MINOR_INDEX = `FES_APPLICATION_IDENTITY_ABI_MINOR_INDEX;
    localparam [31:0] ID_CAPABILITIES_INDEX = `FES_APPLICATION_IDENTITY_CAPABILITIES_INDEX;
    localparam [31:0] ID_BUILD_START_INDEX = `FES_APPLICATION_IDENTITY_BUILD_IDSTART_INDEX;
    localparam [31:0] ID_MAGIC0 = `FES_APPLICATION_IDENTITY_MAGIC0;
    localparam [31:0] ID_MAGIC1 = `FES_APPLICATION_IDENTITY_MAGIC1;
    localparam [31:0] TRANSPORT_MAJOR = `FES_APPLICATION_TRANSPORT_MAJOR;
    localparam [31:0] TRANSPORT_MINOR = `FES_APPLICATION_TRANSPORT_MINOR;
    localparam [31:0] ABI_TAG = `FES_APPLICATION_ABI_TAG;
    localparam [31:0] ABI_MAJOR = `FES_APPLICATION_ABI_MAJOR;
    localparam [31:0] ABI_MINOR = `FES_APPLICATION_ABI_MINOR;
    reg media_open;
    reg [14:0] media_ptr;
    reg [14:0] media_expected;

    (* async_reg = "true" *) reg request_meta;
    (* async_reg = "true" *) reg request_sync;
    reg acknowledged_toggle;
    reg response_error;
    reg [15:0] response_data;

    wire request_toggle = (gpo & `FES_APPLICATION_REQUEST_MASK) != 32'h00000000;
    wire [31:0] command_opcode = (gpo & `FES_APPLICATION_OPCODE_MASK) >> 24;
    wire [31:0] command_index = (gpo & `FES_APPLICATION_INDEX_MASK) >> 16;
    wire [31:0] command_argument = gpo & `FES_APPLICATION_ARGUMENT_MASK;

    assign gpi = `FES_APPLICATION_SIGNATURE |
                 (acknowledged_toggle ? `FES_APPLICATION_ACK_MASK : 32'h00000000) |
                 (response_error ? `FES_APPLICATION_ERROR_MASK : 32'h00000000) |
                 {16'h0000, response_data};

    wire media_cmd = ENABLE_MEDIA && exec_reset && (request_sync != acknowledged_toggle) &&
                     (command_opcode == `FES_APPLICATION_OPCODE_MEDIA_DATA) &&
                     media_open;
    wire media_pair = media_cmd &&
                      (command_index == `FES_APPLICATION_MEDIA_DATA_PAIR_INDEX) &&
                      ({1'b0, media_ptr} + 16'd2 <= {1'b0, media_expected});
    wire media_tail = media_cmd &&
                      (command_index == `FES_APPLICATION_MEDIA_DATA_TAIL_INDEX) &&
                      (command_argument[15:8] == 8'h00) &&
                      media_expected[0] &&
                      ({1'b0, media_ptr} + 16'd1 == {1'b0, media_expected});
    wire media_we_a = media_pair | media_tail;
    wire media_we_b = media_pair;

    // Endpoint emits accepted writes; applications own storage and consumption.
    // Pair bytes target addr and addr+1, low byte first. No RAM is imposed.
    assign media_write_addr = media_ptr[13:0];
    assign media_write_data = command_argument[15:0];
    assign media_write_enable = {media_we_b, media_we_a};

    function [15:0] identity_word;
        input [31:0] index;
        input [127:0] identity;
        begin
            case (index)
                ID_MAGIC0_INDEX: identity_word = ID_MAGIC0[15:0];
                ID_MAGIC1_INDEX: identity_word = ID_MAGIC1[15:0];
                ID_TRANSPORT_MAJOR_INDEX: identity_word = TRANSPORT_MAJOR[15:0];
                ID_TRANSPORT_MINOR_INDEX: identity_word = TRANSPORT_MINOR[15:0];
                ID_ABI_TAG_INDEX: identity_word = ABI_TAG[15:0];
                ID_ABI_MAJOR_INDEX: identity_word = ABI_MAJOR[15:0];
                ID_ABI_MINOR_INDEX: identity_word = ABI_MINOR[15:0];
                ID_CAPABILITIES_INDEX: identity_word = CAPABILITIES[15:0];
                ID_BUILD_START_INDEX + 0:
                    identity_word = {identity[119:112], identity[127:120]};
                ID_BUILD_START_INDEX + 1:
                    identity_word = {identity[103:96], identity[111:104]};
                ID_BUILD_START_INDEX + 2:
                    identity_word = {identity[87:80], identity[95:88]};
                ID_BUILD_START_INDEX + 3:
                    identity_word = {identity[71:64], identity[79:72]};
                ID_BUILD_START_INDEX + 4:
                    identity_word = {identity[55:48], identity[63:56]};
                ID_BUILD_START_INDEX + 5:
                    identity_word = {identity[39:32], identity[47:40]};
                ID_BUILD_START_INDEX + 6:
                    identity_word = {identity[23:16], identity[31:24]};
                ID_BUILD_START_INDEX + 7:
                    identity_word = {identity[7:0], identity[15:8]};
                default: identity_word = 16'h0000;
            endcase
        end
    endfunction

    task reject_command;
        input [15:0] error_code;
        begin
            response_error <= 1'b1;
            response_data <= error_code;
        end
    endtask

    initial begin
        request_meta = 1'b0;
        request_sync = 1'b0;
        acknowledged_toggle = 1'b0;
        response_error = 1'b0;
        response_data = 16'h0000;
        exec_reset = 1'b1;
        media_ready = 1'b0;
        media_open = 1'b0;
        media_ptr = 15'd0;
        media_expected = 15'd0;
        media_size = 15'd0;
        media_byte0 = 8'h00;
        media_byte1 = 8'h00;
        media_byte2 = 8'h00;
        buttons = 8'd0;
    end

    always @(posedge clk) begin
        request_meta <= request_toggle;
        request_sync <= request_meta;

        if (request_sync != acknowledged_toggle) begin
            acknowledged_toggle <= request_sync;
            response_error <= 1'b0;
            response_data <= 16'h0000;

            case (command_opcode)
                `FES_APPLICATION_OPCODE_IDENTITY: begin
                    if (command_index >= `FES_APPLICATION_IDENTITY_WORD_COUNT)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if (command_argument != 32'h00000000)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                    else
                        response_data <= identity_word(command_index, build_id);
                end
                `FES_APPLICATION_OPCODE_EXECUTION: begin
                    if (command_index != `FES_APPLICATION_CONTROL_INDEX)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if (command_argument == `FES_APPLICATION_EXECUTION_HOLD_RESET) begin
                        exec_reset <= 1'b1;
                        // HOLD preserves staged media; BEGIN replaces it.
                        buttons <= 8'd0;
                    end else if (command_argument == `FES_APPLICATION_EXECUTION_RELEASE) begin
                        if (ENABLE_MEDIA && (!media_ready || media_open))
                            reject_command(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                        else
                            exec_reset <= 1'b0;
                    end
                    else
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                end
                `FES_APPLICATION_OPCODE_BUTTONS: begin
                    if (!ENABLE_GAMEPAD)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
                    else if (command_index != `FES_APPLICATION_CONTROL_INDEX)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if ((command_argument & ~`FES_APPLICATION_BUTTON_MASK) != 32'd0)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                    else
                        buttons <= command_argument[7:0];
                end
                `FES_APPLICATION_OPCODE_MEDIA_BEGIN: begin
                    if (!ENABLE_MEDIA)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
                    else if (!exec_reset || media_open)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else if (command_index != `FES_APPLICATION_CONTROL_INDEX)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if (command_argument < `FES_APPLICATION_MEDIA_MIN_BYTES ||
                             command_argument > `FES_APPLICATION_MEDIA_MAX_BYTES)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                    else begin
                        media_open <= 1'b1;
                        media_ready <= 1'b0;
                        media_ptr <= 15'd0;
                        media_expected <= command_argument[14:0];
                        media_size <= 15'd0;
                        media_byte0 <= 8'd0;
                        media_byte1 <= 8'd0;
                        media_byte2 <= 8'd0;
                    end
                end
                `FES_APPLICATION_OPCODE_MEDIA_DATA: begin
                    if (!ENABLE_MEDIA)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
                    else if (!exec_reset || !media_open)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else if (command_index == `FES_APPLICATION_MEDIA_DATA_PAIR_INDEX) begin
                        if ({1'b0, media_ptr} + 16'd2 > {1'b0, media_expected})
                            reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                        else begin
                            if (media_ptr[13:0] == 14'd0)
                                media_byte0 <= command_argument[7:0];
                            if (media_ptr[13:0] == 14'd0)
                                media_byte1 <= command_argument[15:8];
                            else if (media_ptr[13:0] == 14'd1)
                                media_byte1 <= command_argument[7:0];
                            if (media_ptr[13:0] == 14'd1)
                                media_byte2 <= command_argument[15:8];
                            else if (media_ptr[13:0] == 14'd2)
                                media_byte2 <= command_argument[7:0];
                            media_ptr <= media_ptr + 15'd2;
                        end
                    end else if (command_index == `FES_APPLICATION_MEDIA_DATA_TAIL_INDEX) begin
                        if (command_argument[15:8] != 8'h00)
                            reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                        else if (!media_expected[0] ||
                                 {1'b0, media_ptr} + 16'd1 != {1'b0, media_expected})
                            reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                        else begin
                            if (media_ptr[13:0] == 14'd0)
                                media_byte0 <= command_argument[7:0];
                            if (media_ptr[13:0] == 14'd1)
                                media_byte1 <= command_argument[7:0];
                            if (media_ptr[13:0] == 14'd2)
                                media_byte2 <= command_argument[7:0];
                            media_ptr <= media_ptr + 15'd1;
                        end
                    end else
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                end
                `FES_APPLICATION_OPCODE_MEDIA_COMMIT: begin
                    if (!ENABLE_MEDIA)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
                    else if (!exec_reset)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else if (command_index != `FES_APPLICATION_CONTROL_INDEX)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if (command_argument != 32'h00000000)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                    else if (!media_open || media_ptr != media_expected)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else begin
                        media_open <= 1'b0;
                        media_ready <= 1'b1;
                        media_size <= media_expected;
                    end
                end
                default: reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
            endcase
        end
    end
endmodule
