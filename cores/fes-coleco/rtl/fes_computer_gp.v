// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_simple_computer.vh"

// One-request-at-a-time HPS GPO/GPI mailbox for fes.simple-computer 1.0.
// ENABLE_MEDIA_STREAM adds fes.media.blob-stream 1.0 (opcodes 7..12, 32 KiB
// RAM, capability bit 3). Default zero keeps the original 16 KiB blob ports
// and HOLD_RESET/RELEASE behaviour for Coleco, SG-1000 and ZX81 callers.
// With stream enabled, HoldReset still cancels an incomplete legacy blob and
// does not discard in-progress stream staging.
module fes_computer_gp #(
    parameter ENABLE_MEDIA_STREAM = 0
) (
    input  wire         clk,
    input  wire [31:0]  gpo,
    input  wire [127:0] build_id,
    output wire [31:0]  gpi,
    output reg          exec_reset,
    output wire [39:0]  keyboard,
    output reg          media_ready,
    output reg  [(ENABLE_MEDIA_STREAM ? 15 : 14):0] media_size,
    output reg  [7:0]   media_byte0,
    output reg  [7:0]   media_byte1,
    output reg  [7:0]   media_byte2,
    input  wire [(ENABLE_MEDIA_STREAM ? 15 : 14)-1:0] media_addr,
    output wire [7:0]   media_q
);
    localparam integer MEDIA_AW = ENABLE_MEDIA_STREAM ? 15 : 14;
    localparam integer MEDIA_WORDS = ENABLE_MEDIA_STREAM ? 32768 : 16384;
    localparam [31:0] CAPABILITIES =
        `FES_SIMPLE_COMPUTER_INTERFACE_KEYBOARD_CAPABILITY_MASK |
        `FES_SIMPLE_COMPUTER_INTERFACE_VIDEO_FIXED_720P60_CAPABILITY_MASK |
        `FES_SIMPLE_COMPUTER_INTERFACE_MEDIA_BLOB_CAPABILITY_MASK |
        (ENABLE_MEDIA_STREAM ?
         `FES_SIMPLE_COMPUTER_INTERFACE_MEDIA_BLOB_STREAM_CAPABILITY_MASK :
         32'h00000000);
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
    localparam [31:0] STREAM_CRC_INIT = `FES_SIMPLE_COMPUTER_MEDIA_STREAM_CRC32_INITIAL;
    localparam [31:0] STREAM_CRC_POLY = `FES_SIMPLE_COMPUTER_MEDIA_STREAM_CRC32_POLYNOMIAL;
    localparam [31:0] STREAM_CRC_XOR = `FES_SIMPLE_COMPUTER_MEDIA_STREAM_CRC32_FINAL_XOR;
    localparam [31:0] STREAM_MIN = `FES_SIMPLE_COMPUTER_MEDIA_STREAM_MIN_BYTES;
    localparam [31:0] STREAM_SMS_MAX = `FES_SIMPLE_COMPUTER_MEDIA_STREAM_GUARANTEED_MAX_BYTES;
    localparam [31:0] STREAM_CHUNK_MAX = `FES_SIMPLE_COMPUTER_MEDIA_STREAM_CHUNK_MAX_BYTES;

    integer row;

    reg [4:0] key_rows [0:7];
    reg media_open /* verilator public */;
    reg [14:0] media_ptr;
    reg [14:0] media_expected;

    // Stream 1.0 staging. Unused when ENABLE_MEDIA_STREAM is zero.
    // verilator lint_off UNUSED
    // verilator lint_off UNDRIVEN
    reg        stream_active /* verilator public */;
    reg [2:0]  stream_begin_next /* verilator public */;
    reg [1:0]  stream_chunk_next;
    reg [15:0] stream_begin0;
    reg [15:0] stream_begin1;
    reg [15:0] stream_begin2;
    reg [15:0] stream_chunk0;
    reg [15:0] stream_chunk1;
    reg [31:0] stream_total;
    reg [31:0] stream_expected_crc;
    reg [31:0] stream_received /* verilator public */;
    reg [31:0] stream_crc;
    reg [31:0] stream_chunk_length;
    reg [31:0] stream_chunk_received;
    reg [8:0]  stream_ordinal;
    // verilator lint_on UNDRIVEN
    // verilator lint_on UNUSED

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

    wire stream_req = ENABLE_MEDIA_STREAM && (request_sync != acknowledged_toggle);
    wire stream_data_op = stream_req &&
        (command_opcode == `FES_SIMPLE_COMPUTER_OPCODE_MEDIA_STREAM_DATA);
    wire [31:0] stream_remaining = stream_chunk_length - stream_chunk_received;
    wire stream_data_phase = exec_reset && stream_active &&
        (stream_chunk_length != 32'd0) &&
        (command_index == {23'd0, stream_ordinal}) &&
        (command_index <= 32'd255);
    wire stream_odd_pad_bad = (stream_remaining == 32'd1) &&
        (command_argument[15:8] != 8'h00);
    wire stream_data_accept = stream_data_op && stream_data_phase && !stream_odd_pad_bad;
    wire stream_we_a = stream_data_accept;
    wire stream_we_b = stream_data_accept && (stream_remaining > 32'd1);

    wire ram_we_a = media_we_a | stream_we_a;
    wire ram_we_b = media_we_b | stream_we_b;
    wire [MEDIA_AW-1:0] ram_addr_a = ram_we_a ?
        (stream_we_a ? stream_received[MEDIA_AW-1:0] : media_ptr[MEDIA_AW-1:0]) :
        media_addr;
    wire [MEDIA_AW-1:0] ram_addr_b = stream_we_a ?
        stream_received[MEDIA_AW-1:0] + {{(MEDIA_AW-1){1'b0}}, 1'b1} :
        media_ptr[MEDIA_AW-1:0] + {{(MEDIA_AW-1){1'b0}}, 1'b1};

    coleco_dpram #(.ADDRWIDTH(MEDIA_AW), .NUMWORDS(MEDIA_WORDS)) media_ram (
        .clock(clk),
        .address_a(ram_addr_a),
        .data_a(command_argument[7:0]),
        .wren_a(ram_we_a),
        .q_a(media_q),
        .address_b(ram_addr_b),
        .data_b(command_argument[15:8]),
        .wren_b(ram_we_b),
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

    function [31:0] crc32_byte;
        input [31:0] crc;
        input [7:0] data;
        integer i;
        reg [31:0] c;
        begin
            c = crc ^ {24'h000000, data};
            for (i = 0; i < 8; i = i + 1)
                c = c[0] ? ((c >> 1) ^ STREAM_CRC_POLY) : (c >> 1);
            crc32_byte = c;
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

    task clear_stream;
        begin
            stream_active <= 1'b0;
            stream_begin_next <= 3'd0;
            stream_chunk_next <= 2'd0;
            stream_begin0 <= 16'h0000;
            stream_begin1 <= 16'h0000;
            stream_begin2 <= 16'h0000;
            stream_chunk0 <= 16'h0000;
            stream_chunk1 <= 16'h0000;
            stream_total <= 32'd0;
            stream_expected_crc <= 32'd0;
            stream_received <= 32'd0;
            stream_crc <= STREAM_CRC_INIT;
            stream_chunk_length <= 32'd0;
            stream_chunk_received <= 32'd0;
            stream_ordinal <= 9'd0;
            media_ready <= 1'b0;
        end
    endtask

    task note_media_bytes;
        input [31:0] offset;
        input [7:0] low_byte;
        input [7:0] high_byte;
        input pair;
        begin
            if (offset == 32'd0)
                media_byte0 <= low_byte;
            if (offset == 32'd0)
                media_byte1 <= pair ? high_byte : media_byte1;
            if (offset == 32'd1)
                media_byte1 <= low_byte;
            if (offset == 32'd1)
                media_byte2 <= pair ? high_byte : media_byte2;
            if (offset == 32'd2)
                media_byte2 <= low_byte;
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
        media_size = 0;
        media_byte0 = 8'h00;
        media_byte1 = 8'h00;
        media_byte2 = 8'h00;
        stream_active = 1'b0;
        stream_begin_next = 3'd0;
        stream_chunk_next = 2'd0;
        stream_begin0 = 16'h0000;
        stream_begin1 = 16'h0000;
        stream_begin2 = 16'h0000;
        stream_chunk0 = 16'h0000;
        stream_chunk1 = 16'h0000;
        stream_total = 32'd0;
        stream_expected_crc = 32'd0;
        stream_received = 32'd0;
        stream_crc = STREAM_CRC_INIT;
        stream_chunk_length = 32'd0;
        stream_chunk_received = 32'd0;
        stream_ordinal = 9'd0;
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
                        clear_keyboard;
                        // Abort an incomplete legacy blob. Stream staging stays across Hold.
                        if (!ENABLE_MEDIA_STREAM || media_open) begin
                            media_open <= 1'b0;
                            media_ptr <= 15'd0;
                        end
                    end else if (command_argument == `FES_SIMPLE_COMPUTER_EXECUTION_RELEASE) begin
                        if (ENABLE_MEDIA_STREAM &&
                            (!media_ready || stream_active || (stream_begin_next != 3'd0)))
                            reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_STATE));
                        else
                            exec_reset <= 1'b0;
                    end else
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
                    else if (ENABLE_MEDIA_STREAM &&
                             (stream_active || (stream_begin_next != 3'd0)))
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_STATE));
                    else if (command_argument < `FES_SIMPLE_COMPUTER_MEDIA_MIN_BYTES ||
                             command_argument > `FES_SIMPLE_COMPUTER_MEDIA_MAX_BYTES)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                    else begin
                        media_open <= 1'b1;
                        media_ready <= 1'b0;
                        media_ptr <= 15'd0;
                        media_expected <= command_argument[14:0];
                        media_size <= 0;
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
                `FES_SIMPLE_COMPUTER_OPCODE_MEDIA_STREAM_INFO: begin
                    if (!ENABLE_MEDIA_STREAM)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_OPCODE));
                    else if (command_index > `FES_SIMPLE_COMPUTER_MEDIA_STREAM_INFO_CHUNK_MAX_INDEX)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                    else if (command_argument != 32'h00000000)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                    else begin
                        case (command_index)
                            `FES_SIMPLE_COMPUTER_MEDIA_STREAM_INFO_MIN_LO_INDEX:
                                response_data <= STREAM_MIN[15:0];
                            `FES_SIMPLE_COMPUTER_MEDIA_STREAM_INFO_MIN_HI_INDEX:
                                response_data <= STREAM_MIN[31:16];
                            `FES_SIMPLE_COMPUTER_MEDIA_STREAM_INFO_MAX_LO_INDEX:
                                response_data <= STREAM_SMS_MAX[15:0];
                            `FES_SIMPLE_COMPUTER_MEDIA_STREAM_INFO_MAX_HI_INDEX:
                                response_data <= STREAM_SMS_MAX[31:16];
                            default:
                                response_data <= STREAM_CHUNK_MAX[15:0];
                        endcase
                    end
                end
                `FES_SIMPLE_COMPUTER_OPCODE_MEDIA_STREAM_BEGIN: begin
                    if (!ENABLE_MEDIA_STREAM)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_OPCODE));
                    else if (!exec_reset || media_open || stream_active)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_STATE));
                    else if ((command_index > 32'd3) ||
                             (command_index != {29'd0, stream_begin_next}))
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                    else if (command_index == 32'd3) begin
                        if (({stream_begin1, stream_begin0} < STREAM_MIN) ||
                            ({stream_begin1, stream_begin0} > STREAM_SMS_MAX))
                            reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                        else begin
                            stream_total <= {stream_begin1, stream_begin0};
                            stream_expected_crc <= {command_argument[15:0], stream_begin2};
                            stream_active <= 1'b1;
                            stream_begin_next <= 3'd0;
                            stream_begin0 <= 16'h0000;
                            stream_begin1 <= 16'h0000;
                            stream_begin2 <= 16'h0000;
                            stream_received <= 32'd0;
                            stream_crc <= STREAM_CRC_INIT;
                            stream_chunk_next <= 2'd0;
                            stream_chunk0 <= 16'h0000;
                            stream_chunk1 <= 16'h0000;
                            stream_chunk_length <= 32'd0;
                            stream_chunk_received <= 32'd0;
                            stream_ordinal <= 9'd0;
                            media_ready <= 1'b0;
                        end
                    end else begin
                        media_ready <= 1'b0;
                        stream_begin_next <= stream_begin_next + 3'd1;
                        if (command_index == 32'd0)
                            stream_begin0 <= command_argument[15:0];
                        else if (command_index == 32'd1)
                            stream_begin1 <= command_argument[15:0];
                        else
                            stream_begin2 <= command_argument[15:0];
                    end
                end
                `FES_SIMPLE_COMPUTER_OPCODE_MEDIA_STREAM_CHUNK: begin
                    if (!ENABLE_MEDIA_STREAM)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_OPCODE));
                    else if (!exec_reset || !stream_active || (stream_chunk_length != 32'd0))
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_STATE));
                    else if ((command_index > 32'd2) ||
                             (command_index != {30'd0, stream_chunk_next}))
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                    else if (command_index == 32'd2) begin
                        if (({stream_chunk1, stream_chunk0} != stream_received) ||
                            (command_argument < STREAM_MIN) ||
                            (command_argument > STREAM_CHUNK_MAX) ||
                            (stream_received > stream_total) ||
                            (command_argument > (stream_total - stream_received)))
                            reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                        else begin
                            stream_chunk_length <= command_argument;
                            stream_chunk_received <= 32'd0;
                            stream_ordinal <= 9'd0;
                            stream_chunk_next <= 2'd0;
                            stream_chunk0 <= 16'h0000;
                            stream_chunk1 <= 16'h0000;
                        end
                    end else begin
                        stream_chunk_next <= stream_chunk_next + 2'd1;
                        if (command_index == 32'd0)
                            stream_chunk0 <= command_argument[15:0];
                        else
                            stream_chunk1 <= command_argument[15:0];
                    end
                end
                `FES_SIMPLE_COMPUTER_OPCODE_MEDIA_STREAM_DATA: begin
                    if (!ENABLE_MEDIA_STREAM)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_OPCODE));
                    else if (!exec_reset || !stream_active || (stream_chunk_length == 32'd0))
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_STATE));
                    else if ((command_index > 32'd255) ||
                             (command_index != {23'd0, stream_ordinal}))
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                    else if ((stream_remaining == 32'd1) &&
                             (command_argument[15:8] != 8'h00))
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                    else begin
                        if (stream_remaining == 32'd1) begin
                            note_media_bytes(stream_received, command_argument[7:0], 8'h00, 1'b0);
                            stream_crc <= crc32_byte(stream_crc, command_argument[7:0]);
                            stream_received <= stream_received + 32'd1;
                            stream_ordinal <= 9'd0;
                            stream_chunk_length <= 32'd0;
                            stream_chunk_received <= 32'd0;
                        end else begin
                            note_media_bytes(stream_received, command_argument[7:0],
                                             command_argument[15:8], 1'b1);
                            stream_crc <= crc32_byte(
                                crc32_byte(stream_crc, command_argument[7:0]),
                                command_argument[15:8]);
                            stream_received <= stream_received + 32'd2;
                            if (stream_chunk_received + 32'd2 == stream_chunk_length) begin
                                stream_chunk_length <= 32'd0;
                                stream_chunk_received <= 32'd0;
                                stream_ordinal <= 9'd0;
                            end else begin
                                stream_chunk_received <= stream_chunk_received + 32'd2;
                                stream_ordinal <= stream_ordinal + 9'd1;
                            end
                        end
                    end
                end
                `FES_SIMPLE_COMPUTER_OPCODE_MEDIA_STREAM_COMMIT: begin
                    if (!ENABLE_MEDIA_STREAM)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_OPCODE));
                    else if (command_index != `FES_SIMPLE_COMPUTER_CONTROL_INDEX)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                    else if (command_argument != 32'h00000000)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                    else if (!exec_reset || !stream_active ||
                             (stream_chunk_next != 2'd0) ||
                             (stream_chunk_length != 32'd0) ||
                             (stream_received != stream_total))
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_STATE));
                    else if ((stream_crc ^ STREAM_CRC_XOR) != stream_expected_crc)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                    else begin
                        stream_active <= 1'b0;
                        media_ready <= 1'b1;
                        media_size <= stream_total[(ENABLE_MEDIA_STREAM ? 15 : 14):0];
                    end
                end
                `FES_SIMPLE_COMPUTER_OPCODE_MEDIA_STREAM_ABORT: begin
                    if (!ENABLE_MEDIA_STREAM)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_OPCODE));
                    else if (command_index != `FES_SIMPLE_COMPUTER_CONTROL_INDEX)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_INDEX));
                    else if (command_argument != 32'h00000000)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_ARGUMENT));
                    else if (!exec_reset || media_open)
                        reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_STATE));
                    else
                        clear_stream;
                end
                default: reject_command(16'(`FES_SIMPLE_COMPUTER_ERROR_INVALID_OPCODE));
            endcase
        end
    end
endmodule
