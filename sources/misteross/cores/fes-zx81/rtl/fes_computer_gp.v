// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_simple_computer.vh"

// One-request-at-a-time HPS GPO/GPI mailbox for fes.simple-computer 1.0.
module fes_computer_gp (
    input  wire         clk,
    input  wire [31:0]  gpo,
    input  wire [127:0] build_id,
    output wire [31:0]  gpi,
    output reg          exec_reset,
    output wire [39:0]  keyboard,
    output reg          media_ready,
    output reg  [14:0]  media_size,
    output reg  [7:0]   media_byte0,
    output reg  [7:0]   media_byte1,
    output reg  [7:0]   media_byte2,
    input  wire [13:0]  media_addr,
    output wire [7:0]   media_q,
    // High while zx81_machine is copying mailbox bytes into RAM. Mid-session
    // begin/eject must reject rather than abort an in-flight LOAD.
    input  wire         media_busy
);
    localparam [31:0] CAPABILITIES =
        `FES_SIMPLE_COMPUTER_INTERFACE_KEYBOARD_CAPABILITY_MASK |
        `FES_SIMPLE_COMPUTER_INTERFACE_VIDEO_FIXED_720P60_CAPABILITY_MASK |
        `FES_SIMPLE_COMPUTER_INTERFACE_MEDIA_BLOB_CAPABILITY_MASK;
    localparam [31:0] ID_MAGIC0_INDEX = `FES_SIMPLE_COMPUTER_IDENTITY_MAGIC0_INDEX;
    localparam [31:0] ID_MAGIC1_INDEX = `FES_SIMPLE_COMPUTER_IDENTITY_MAGIC1_INDEX;
    localparam [31:0] ID_TRANSPORT_MAJOR_INDEX = `FES_SIMPLE_COMPUTER_IDENTITY_TRANSPORT_MAJOR_INDEX;
    localparam [31:0] ID_TRANSPORT_MINOR_INDEX = `FES_SIMPLE_COMPUTER_IDENTITY_TRANSPORT_MINOR_INDEX;
    localparam [31:0] ID_ABI_TAG_INDEX = `FES_SIMPLE_COMPUTER_IDENTITY_ABI_TAG_INDEX;
    localparam [31:0] ID_ABI_MAJOR_INDEX = `FES_SIMPLE_COMPUTER_IDENTITY_ABI_MAJOR_INDEX;
    localparam [31:0] ID_ABI_MINOR_INDEX = `FES_SIMPLE_COMPUTER_IDENTITY_ABI_MINOR_INDEX;
    localparam [31:0] ID_CAPABILITIES_INDEX = `FES_SIMPLE_COMPUTER_IDENTITY_CAPABILITIES_INDEX;
    localparam [31:0] ID_BUILD_START_INDEX = `FES_SIMPLE_COMPUTER_IDENTITY_BUILD_IDSTART_INDEX;
    localparam [31:0] ID_MAGIC0 = `FES_SIMPLE_COMPUTER_IDENTITY_MAGIC0;
    localparam [31:0] ID_MAGIC1 = `FES_SIMPLE_COMPUTER_IDENTITY_MAGIC1;
    localparam [31:0] TRANSPORT_MAJOR = `FES_SIMPLE_COMPUTER_TRANSPORT_MAJOR;
    localparam [31:0] TRANSPORT_MINOR = `FES_SIMPLE_COMPUTER_TRANSPORT_MINOR;
    localparam [31:0] ABI_TAG = `FES_SIMPLE_COMPUTER_ABI_TAG;
    localparam [31:0] ABI_MAJOR = `FES_SIMPLE_COMPUTER_ABI_MAJOR;
    localparam [31:0] ABI_MINOR = `FES_SIMPLE_COMPUTER_ABI_MINOR;
    localparam [31:0] KEYBOARD_NEUTRAL = `FES_SIMPLE_COMPUTER_KEYBOARD_NEUTRAL_ROW;

    integer row;

    reg [4:0] key_rows [0:7];
    reg media_open;
    reg [14:0] media_ptr;
    reg [14:0] media_expected;

    (* async_reg = "true" *) reg request_meta;
    (* async_reg = "true" *) reg request_sync;
    reg acknowledged_toggle;
    reg response_error;
    reg [15:0] response_data;

    wire request_toggle = (gpo & `FES_SIMPLE_COMPUTER_REQUEST_MASK) != 32'h00000000;
    wire [31:0] command_opcode = (gpo & `FES_SIMPLE_COMPUTER_OPCODE_MASK) >> 24;
    wire [31:0] command_index = (gpo & `FES_SIMPLE_COMPUTER_INDEX_MASK) >> 16;
    wire [31:0] command_argument = gpo & `FES_SIMPLE_COMPUTER_ARGUMENT_MASK;

    assign gpi = `FES_SIMPLE_COMPUTER_SIGNATURE |
                 (acknowledged_toggle ? `FES_SIMPLE_COMPUTER_ACK_MASK : 32'h00000000) |
                 (response_error ? `FES_SIMPLE_COMPUTER_ERROR_MASK : 32'h00000000) |
                 {16'h0000, response_data};

    assign keyboard = {key_rows[7], key_rows[6], key_rows[5], key_rows[4],
                       key_rows[3], key_rows[2], key_rows[1], key_rows[0]};

    wire media_cmd = (request_sync != acknowledged_toggle) &&
                     (command_opcode == `FES_SIMPLE_COMPUTER_OPCODE_MEDIA_DATA) &&
                     media_open;
    wire media_pair = media_cmd &&
                      (command_index == `FES_SIMPLE_COMPUTER_MEDIA_DATA_PAIR_INDEX) &&
                      ({1'b0, media_ptr} + 16'd2 <= {1'b0, media_expected});
    wire media_tail = media_cmd &&
                      (command_index == `FES_SIMPLE_COMPUTER_MEDIA_DATA_TAIL_INDEX) &&
                      (command_argument[15:8] == 8'h00) &&
                      ({1'b0, media_ptr} + 16'd1 <= {1'b0, media_expected});
    wire media_we_a = media_pair | media_tail;
    wire media_we_b = media_pair;

    zx81_dpram #(.ADDRWIDTH(14), .NUMWORDS(16384)) media_ram (
        .clock(clk),
        .address_a(media_we_a ? media_ptr[13:0] : media_addr),
        .data_a(command_argument[7:0]),
        .wren_a(media_we_a),
        .q_a(media_q),
        .address_b(media_ptr[13:0] + 14'd1),
        .data_b(command_argument[15:8]),
        .wren_b(media_we_b),
        .q_b()
    );

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

    task clear_keyboard;
        begin
            for (row = 0; row < 8; row = row + 1)
                key_rows[row] <= KEYBOARD_NEUTRAL[4:0];
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
        for (row = 0; row < 8; row = row + 1)
            key_rows[row] = KEYBOARD_NEUTRAL[4:0];
    end

    always @(posedge clk) begin
        request_meta <= request_toggle;
        request_sync <= request_meta;

        if (request_sync != acknowledged_toggle) begin
            acknowledged_toggle <= request_sync;
            response_error <= 1'b0;
            response_data <= 16'h0000;

            case (command_opcode)
                `FES_SIMPLE_COMPUTER_OPCODE_IDENTITY: begin
                    if (command_index >= `FES_SIMPLE_COMPUTER_IDENTITY_WORD_COUNT)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                    else if (command_argument != 32'h00000000)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                    else
                        response_data <= identity_word(command_index, build_id);
                end
                `FES_SIMPLE_COMPUTER_OPCODE_EXECUTION: begin
                    if (command_index != `FES_SIMPLE_COMPUTER_CONTROL_INDEX)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                    else if (command_argument == `FES_SIMPLE_COMPUTER_EXECUTION_HOLD_RESET) begin
                        exec_reset <= 1'b1;
                        media_open <= 1'b0;
                        media_ptr <= 15'd0;
                        clear_keyboard;
                    end else if (command_argument == `FES_SIMPLE_COMPUTER_EXECUTION_RELEASE)
                        exec_reset <= 1'b0;
                    else
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                end
                `FES_SIMPLE_COMPUTER_OPCODE_KEYBOARD: begin
                    if (command_index >= `FES_SIMPLE_COMPUTER_KEYBOARD_ROW_COUNT)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                    else if ((command_argument & ~`FES_SIMPLE_COMPUTER_KEYBOARD_ROW_MASK) != 32'h00000000)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                    else
                        key_rows[command_index[2:0]] <= command_argument[4:0];
                end
                `FES_SIMPLE_COMPUTER_OPCODE_MEDIA_BEGIN: begin
                    if (command_index != `FES_SIMPLE_COMPUTER_CONTROL_INDEX)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                    else if (media_busy)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_STATE));
                    else if (command_argument == 32'h00000000) begin
                        // Eject/clear: next empty LOAD "" reports 0/0.
                        media_open <= 1'b0;
                        media_ready <= 1'b0;
                        media_ptr <= 15'd0;
                        media_expected <= 15'd0;
                        media_size <= 15'd0;
                    end else if (command_argument < `FES_SIMPLE_COMPUTER_MEDIA_MIN_BYTES ||
                             command_argument > `FES_SIMPLE_COMPUTER_MEDIA_MAX_BYTES)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                    else begin
                        media_open <= 1'b1;
                        media_ready <= 1'b0;
                        media_ptr <= 15'd0;
                        media_expected <= command_argument[14:0];
                        media_size <= 15'd0;
                    end
                end
                `FES_SIMPLE_COMPUTER_OPCODE_MEDIA_DATA: begin
                    if (!media_open)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_STATE));
                    else if (command_index == `FES_SIMPLE_COMPUTER_MEDIA_DATA_PAIR_INDEX) begin
                        if ({1'b0, media_ptr} + 16'd2 > {1'b0, media_expected})
                            reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
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
                    end else if (command_index == `FES_SIMPLE_COMPUTER_MEDIA_DATA_TAIL_INDEX) begin
                        if (command_argument[15:8] != 8'h00)
                            reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                        else if ({1'b0, media_ptr} + 16'd1 > {1'b0, media_expected})
                            reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
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
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                end
                `FES_SIMPLE_COMPUTER_OPCODE_MEDIA_COMMIT: begin
                    if (command_index != `FES_SIMPLE_COMPUTER_CONTROL_INDEX)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                    else if (command_argument != 32'h00000000)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                    else if (!media_open || media_ptr != media_expected)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_STATE));
                    else begin
                        media_open <= 1'b0;
                        media_ready <= 1'b1;
                        media_size <= media_expected;
                    end
                end
                default: reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_OPCODE));
            endcase
        end
    end
endmodule
