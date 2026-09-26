// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_simple_computer.vh"

// One-request-at-a-time HPS GPO/GPI mailbox for fes.simple-computer 1.0.
//
// The host still sees one request toggle and one ACK. ACK and the response
// are published together, after the command has been applied: one clock after
// acceptance, or two clocks for a media pair (low byte, then high byte).
// Port B is the CPU read and uses only media_addr. The pointer compare and
// the command decoder never enter that read.
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
    reg [31:0] gpo_payload;
    reg acknowledged_toggle;
    reg response_error;
    reg [15:0] response_data;

    // Bounds are registered on their own so the 15-bit compare ends at a
    // flop. The next host command cannot arrive before they catch a commit.
    reg pair_room;
    reg tail_room;
    reg ptr_match;

    // phase 0 idle, 1 first commit beat, 2 pair high-byte beat.
    reg [1:0] phase;
    reg commit_error;
    reg [15:0] commit_data;
    reg commit_toggle;
    reg cap_we_a;
    reg cap_pair;
    reg [13:0] cap_addr_a;
    reg [13:0] cap_addr_hi;
    reg [7:0] cap_data_a;
    reg [7:0] cap_data_hi;
    reg cap_wr_reset, cap_reset;
    reg cap_wr_open, cap_open;
    reg cap_wr_ready, cap_ready;
    reg cap_wr_ptr;
    reg [14:0] cap_ptr;
    reg cap_wr_expected;
    reg [14:0] cap_expected;
    reg cap_wr_size;
    reg [14:0] cap_size;
    reg cap_wr_b0, cap_wr_b1, cap_wr_b2;
    reg [7:0] cap_b0, cap_b1, cap_b2;
    reg cap_clr_keys, cap_wr_key;
    reg [2:0] cap_key_i;
    reg [4:0] cap_key_v;

    wire request_toggle = (gpo & `FES_SIMPLE_COMPUTER_REQUEST_MASK) != 32'h00000000;
    wire [31:0] command_opcode = (gpo_payload & `FES_SIMPLE_COMPUTER_OPCODE_MASK) >> 24;
    wire [31:0] command_index = (gpo_payload & `FES_SIMPLE_COMPUTER_INDEX_MASK) >> 16;
    wire [31:0] command_argument = gpo_payload & `FES_SIMPLE_COMPUTER_ARGUMENT_MASK;

    assign gpi = `FES_SIMPLE_COMPUTER_SIGNATURE |
                 (acknowledged_toggle ? `FES_SIMPLE_COMPUTER_ACK_MASK : 32'h00000000) |
                 (response_error ? `FES_SIMPLE_COMPUTER_ERROR_MASK : 32'h00000000) |
                 {16'h0000, response_data};

    assign keyboard = {key_rows[7], key_rows[6], key_rows[5], key_rows[4],
                       key_rows[3], key_rows[2], key_rows[1], key_rows[0]};

    zx81_dpram #(.ADDRWIDTH(14), .NUMWORDS(16384)) media_ram (
        .clock(clk),
        .address_a(cap_addr_a),
        .data_a(cap_data_a),
        .wren_a(cap_we_a),
        .q_a(),
        .address_b(media_addr),
        .data_b(8'h00),
        .wren_b(1'b0),
        .q_b(media_q)
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
            commit_error <= 1'b1;
            commit_data <= error_code;
        end
    endtask

    task clear_keyboard;
        begin
            for (row = 0; row < 8; row = row + 1)
                key_rows[row] <= KEYBOARD_NEUTRAL[4:0];
        end
    endtask

    task clear_capture;
        begin
            commit_error <= 1'b0;
            commit_data <= 16'h0000;
            cap_we_a <= 1'b0;
            cap_pair <= 1'b0;
            cap_wr_reset <= 1'b0;
            cap_wr_open <= 1'b0;
            cap_wr_ready <= 1'b0;
            cap_wr_ptr <= 1'b0;
            cap_wr_expected <= 1'b0;
            cap_wr_size <= 1'b0;
            cap_wr_b0 <= 1'b0;
            cap_wr_b1 <= 1'b0;
            cap_wr_b2 <= 1'b0;
            cap_clr_keys <= 1'b0;
            cap_wr_key <= 1'b0;
        end
    endtask

    task arm_low_byte;
        input [7:0] data;
        input [13:0] addr;
        begin
            cap_we_a <= 1'b1;
            cap_addr_a <= addr;
            cap_data_a <= data;
            if (addr == 14'd0) begin
                cap_wr_b0 <= 1'b1;
                cap_b0 <= data;
            end
            if (addr == 14'd1) begin
                cap_wr_b1 <= 1'b1;
                cap_b1 <= data;
            end
            if (addr == 14'd2) begin
                cap_wr_b2 <= 1'b1;
                cap_b2 <= data;
            end
        end
    endtask

    task arm_pair_high;
        input [7:0] data;
        input [13:0] low_addr;
        begin
            cap_pair <= 1'b1;
            cap_addr_hi <= low_addr + 14'd1;
            cap_data_hi <= data;
            if (low_addr == 14'd0) begin
                cap_wr_b1 <= 1'b1;
                cap_b1 <= data;
            end
            if (low_addr == 14'd1) begin
                cap_wr_b2 <= 1'b1;
                cap_b2 <= data;
            end
        end
    endtask

    task publish_response;
        begin
            acknowledged_toggle <= commit_toggle;
            response_error <= commit_error;
            response_data <= commit_data;
            if (cap_wr_reset)
                exec_reset <= cap_reset;
            if (cap_wr_open)
                media_open <= cap_open;
            if (cap_wr_ready)
                media_ready <= cap_ready;
            if (cap_wr_ptr)
                media_ptr <= cap_ptr;
            if (cap_wr_expected)
                media_expected <= cap_expected;
            if (cap_wr_size)
                media_size <= cap_size;
            if (cap_wr_b0)
                media_byte0 <= cap_b0;
            if (cap_wr_b1)
                media_byte1 <= cap_b1;
            if (cap_wr_b2)
                media_byte2 <= cap_b2;
            if (cap_clr_keys)
                clear_keyboard;
            if (cap_wr_key)
                key_rows[cap_key_i] <= cap_key_v;
            cap_we_a <= 1'b0;
            cap_pair <= 1'b0;
            phase <= 2'd0;
        end
    endtask

    initial begin
        request_meta = 1'b0;
        request_sync = 1'b0;
        gpo_payload = 32'h00000000;
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
        pair_room = 1'b0;
        tail_room = 1'b0;
        ptr_match = 1'b1;
        phase = 2'd0;
        commit_error = 1'b0;
        commit_data = 16'h0000;
        commit_toggle = 1'b0;
        cap_we_a = 1'b0;
        cap_pair = 1'b0;
        cap_addr_a = 14'd0;
        cap_addr_hi = 14'd0;
        cap_data_a = 8'h00;
        cap_data_hi = 8'h00;
        cap_wr_reset = 1'b0;
        cap_reset = 1'b0;
        cap_wr_open = 1'b0;
        cap_open = 1'b0;
        cap_wr_ready = 1'b0;
        cap_ready = 1'b0;
        cap_wr_ptr = 1'b0;
        cap_ptr = 15'd0;
        cap_wr_expected = 1'b0;
        cap_expected = 15'd0;
        cap_wr_size = 1'b0;
        cap_size = 15'd0;
        cap_wr_b0 = 1'b0;
        cap_wr_b1 = 1'b0;
        cap_wr_b2 = 1'b0;
        cap_b0 = 8'h00;
        cap_b1 = 8'h00;
        cap_b2 = 8'h00;
        cap_clr_keys = 1'b0;
        cap_wr_key = 1'b0;
        cap_key_i = 3'd0;
        cap_key_v = 5'd0;
        for (row = 0; row < 8; row = row + 1)
            key_rows[row] = KEYBOARD_NEUTRAL[4:0];
    end

    always @(posedge clk) begin
        request_meta <= request_toggle;
        request_sync <= request_meta;
        gpo_payload <= gpo;
        pair_room <= ({1'b0, media_ptr} + 16'd2) <= {1'b0, media_expected};
        tail_room <= ({1'b0, media_ptr} + 16'd1) <= {1'b0, media_expected};
        ptr_match <= (media_ptr == media_expected);

        if (phase == 2'd2) begin
            publish_response;
        end else if (phase == 2'd1) begin
            if (cap_pair) begin
                cap_we_a <= 1'b1;
                cap_addr_a <= cap_addr_hi;
                cap_data_a <= cap_data_hi;
                cap_pair <= 1'b0;
                phase <= 2'd2;
            end else
                publish_response;
        end else if (request_sync != acknowledged_toggle) begin
            phase <= 2'd1;
            commit_toggle <= request_sync;
            clear_capture;

            case (command_opcode)
                `FES_SIMPLE_COMPUTER_OPCODE_IDENTITY: begin
                    if (command_index >= `FES_SIMPLE_COMPUTER_IDENTITY_WORD_COUNT)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                    else if (command_argument != 32'h00000000)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                    else
                        commit_data <= identity_word(command_index, build_id);
                end
                `FES_SIMPLE_COMPUTER_OPCODE_EXECUTION: begin
                    if (command_index != `FES_SIMPLE_COMPUTER_CONTROL_INDEX)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                    else if (command_argument == `FES_SIMPLE_COMPUTER_EXECUTION_HOLD_RESET) begin
                        cap_wr_reset <= 1'b1;
                        cap_reset <= 1'b1;
                        cap_wr_open <= 1'b1;
                        cap_open <= 1'b0;
                        cap_wr_ptr <= 1'b1;
                        cap_ptr <= 15'd0;
                        cap_clr_keys <= 1'b1;
                    end else if (command_argument == `FES_SIMPLE_COMPUTER_EXECUTION_RELEASE) begin
                        cap_wr_reset <= 1'b1;
                        cap_reset <= 1'b0;
                    end else
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                end
                `FES_SIMPLE_COMPUTER_OPCODE_KEYBOARD: begin
                    if (command_index >= `FES_SIMPLE_COMPUTER_KEYBOARD_ROW_COUNT)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                    else if ((command_argument & ~`FES_SIMPLE_COMPUTER_KEYBOARD_ROW_MASK) != 32'h00000000)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                    else begin
                        cap_wr_key <= 1'b1;
                        cap_key_i <= command_index[2:0];
                        cap_key_v <= command_argument[4:0];
                    end
                end
                `FES_SIMPLE_COMPUTER_OPCODE_MEDIA_BEGIN: begin
                    if (media_busy)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_STATE));
                    else if (command_index == `FES_SIMPLE_COMPUTER_MEDIA_EJECT_INDEX) begin
                        // Eject/clear: next empty LOAD "" reports 0/0.
                        // Control-index begin with argument 0 stays invalid.
                        if (command_argument != 32'h00000000)
                            reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                        else begin
                            cap_wr_open <= 1'b1;
                            cap_open <= 1'b0;
                            cap_wr_ready <= 1'b1;
                            cap_ready <= 1'b0;
                            cap_wr_ptr <= 1'b1;
                            cap_ptr <= 15'd0;
                            cap_wr_expected <= 1'b1;
                            cap_expected <= 15'd0;
                            cap_wr_size <= 1'b1;
                            cap_size <= 15'd0;
                        end
                    end else if (command_index != `FES_SIMPLE_COMPUTER_CONTROL_INDEX)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                    else if (command_argument < `FES_SIMPLE_COMPUTER_MEDIA_MIN_BYTES ||
                             command_argument > `FES_SIMPLE_COMPUTER_MEDIA_MAX_BYTES)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                    else begin
                        cap_wr_open <= 1'b1;
                        cap_open <= 1'b1;
                        cap_wr_ready <= 1'b1;
                        cap_ready <= 1'b0;
                        cap_wr_ptr <= 1'b1;
                        cap_ptr <= 15'd0;
                        cap_wr_expected <= 1'b1;
                        cap_expected <= command_argument[14:0];
                        cap_wr_size <= 1'b1;
                        cap_size <= 15'd0;
                    end
                end
                `FES_SIMPLE_COMPUTER_OPCODE_MEDIA_DATA: begin
                    if (!media_open)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_STATE));
                    else if (command_index == `FES_SIMPLE_COMPUTER_MEDIA_DATA_PAIR_INDEX) begin
                        if (!pair_room)
                            reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                        else begin
                            arm_low_byte(command_argument[7:0], media_ptr[13:0]);
                            arm_pair_high(command_argument[15:8], media_ptr[13:0]);
                            cap_wr_ptr <= 1'b1;
                            cap_ptr <= media_ptr + 15'd2;
                        end
                    end else if (command_index == `FES_SIMPLE_COMPUTER_MEDIA_DATA_TAIL_INDEX) begin
                        if (command_argument[15:8] != 8'h00)
                            reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                        else if (!tail_room)
                            reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                        else begin
                            arm_low_byte(command_argument[7:0], media_ptr[13:0]);
                            cap_wr_ptr <= 1'b1;
                            cap_ptr <= media_ptr + 15'd1;
                        end
                    end else
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                end
                `FES_SIMPLE_COMPUTER_OPCODE_MEDIA_COMMIT: begin
                    if (command_index != `FES_SIMPLE_COMPUTER_CONTROL_INDEX)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                    else if (command_argument != 32'h00000000)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                    else if (!media_open || !ptr_match)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_STATE));
                    else begin
                        cap_wr_open <= 1'b1;
                        cap_open <= 1'b0;
                        cap_wr_ready <= 1'b1;
                        cap_ready <= 1'b1;
                        cap_wr_size <= 1'b1;
                        cap_size <= media_expected;
                    end
                end
                default: reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_OPCODE));
            endcase
        end
    end
endmodule
