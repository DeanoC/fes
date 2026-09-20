// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"

// One-request-at-a-time HPS GPO/GPI mailbox for fes.application 1.0.
module fes_application_gp #(
    parameter bit ENABLE_GAMEPAD = 0,
    parameter bit ENABLE_CONTROLLER_PORTS = 0,
    parameter bit ENABLE_KEYPAD_PORTS = 0,
    parameter bit ENABLE_MEDIA = 0,
    parameter bit ENABLE_MEDIA_STREAM = 0,
    parameter bit ENABLE_AUDIO = 0,
    parameter bit ENABLE_FIRMWARE = 0
) (
    input  wire         clk,
    input  wire [31:0]  gpo,
    input  wire [127:0] build_id,
    output wire [31:0]  gpi,
    output reg          exec_reset,
    output reg [7:0]    buttons,
    output reg [15:0]   controller_buttons,
    output reg [23:0]   controller_keypad,
    output wire [(ENABLE_MEDIA_STREAM ? 15 : 14)-1:0] media_write_addr,
    output wire [15:0]  media_write_data,
    output wire [1:0]   media_write_enable,
    output reg          media_ready,
    output reg  [(ENABLE_MEDIA_STREAM ? 15 : 14):0] media_size,
    output reg  [7:0]   media_byte0,
    output reg  [7:0]   media_byte1,
    output reg  [7:0]   media_byte2,
    output wire [12:0]  firmware_write_addr,
    output wire [15:0]  firmware_write_data,
    output wire [1:0]   firmware_write_enable,
    output reg          firmware_ready
);
    localparam integer MEDIA_AW = ENABLE_MEDIA_STREAM ? 15 : 14;
    localparam [31:0] CAPABILITIES =
        `FES_APPLICATION_INTERFACE_VIDEO_FIXED_720P60_CAPABILITY_MASK |
        (ENABLE_GAMEPAD ? `FES_APPLICATION_INTERFACE_GAMEPAD_CAPABILITY_MASK : 32'd0) |
        (ENABLE_CONTROLLER_PORTS ? `FES_APPLICATION_INTERFACE_GAMEPAD_PORTS_CAPABILITY_MASK : 32'd0) |
        (ENABLE_KEYPAD_PORTS ? `FES_APPLICATION_INTERFACE_KEYPAD_PORTS_CAPABILITY_MASK : 32'd0) |
        (ENABLE_MEDIA ? `FES_APPLICATION_INTERFACE_MEDIA_BLOB_CAPABILITY_MASK : 32'd0) |
        (ENABLE_MEDIA_STREAM ? `FES_APPLICATION_INTERFACE_MEDIA_BLOB_STREAM_CAPABILITY_MASK : 32'd0) |
        (ENABLE_AUDIO ? `FES_APPLICATION_INTERFACE_AUDIO_PCM_S16_STEREO_48K_CAPABILITY_MASK : 32'd0) |
        (ENABLE_FIRMWARE ? `FES_APPLICATION_INTERFACE_FIRMWARE_BLOB_CAPABILITY_MASK : 32'd0);
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
    localparam [31:0] STREAM_CRC_INIT = `FES_APPLICATION_MEDIA_STREAM_CRC32_INITIAL;
    localparam [31:0] STREAM_CRC_POLY = `FES_APPLICATION_MEDIA_STREAM_CRC32_POLYNOMIAL;
    localparam [31:0] STREAM_CRC_XOR = `FES_APPLICATION_MEDIA_STREAM_CRC32_FINAL_XOR;
    localparam [31:0] STREAM_MIN = `FES_APPLICATION_MEDIA_STREAM_MIN_BYTES;
    localparam [31:0] STREAM_MAX = `FES_APPLICATION_MEDIA_STREAM_GUARANTEED_MAX_BYTES;
    localparam [31:0] STREAM_CHUNK_MAX = `FES_APPLICATION_MEDIA_STREAM_CHUNK_MAX_BYTES;

    reg media_open;
    reg [14:0] media_ptr;
    reg [14:0] media_expected;
    reg firmware_open;
    reg [13:0] firmware_ptr;

    // Stream 1.0 staging. Unused when ENABLE_MEDIA_STREAM is zero.
    // verilator lint_off UNUSED
    // verilator lint_off UNDRIVEN
    reg        stream_active;
    reg [2:0]  stream_begin_next;
    reg [1:0]  stream_chunk_next;
    reg [15:0] stream_begin0;
    reg [15:0] stream_begin1;
    reg [15:0] stream_begin2;
    reg [15:0] stream_chunk0;
    reg [15:0] stream_chunk1;
    reg [31:0] stream_total;
    reg [31:0] stream_expected_crc;
    reg [31:0] stream_received;
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

    wire stream_req = ENABLE_MEDIA_STREAM && (request_sync != acknowledged_toggle);
    wire stream_data_op = stream_req &&
        (command_opcode == `FES_APPLICATION_OPCODE_MEDIA_STREAM_DATA);
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

    // Endpoint emits accepted writes; applications own storage and consumption.
    // Pair bytes target addr and addr+1, low byte first. No RAM is imposed.
    assign media_write_addr = stream_we_a ? stream_received[MEDIA_AW-1:0] : media_ptr[MEDIA_AW-1:0];
    assign media_write_data = command_argument[15:0];
    assign media_write_enable = {media_we_b | stream_we_b, media_we_a | stream_we_a};

    wire firmware_cmd = ENABLE_FIRMWARE && exec_reset && (request_sync != acknowledged_toggle) &&
                     (command_opcode == `FES_APPLICATION_OPCODE_FIRMWARE_DATA) &&
                     firmware_open;
    wire firmware_pair = firmware_cmd &&
                      (command_index == `FES_APPLICATION_FIRMWARE_DATA_PAIR_INDEX) &&
                      ({1'b0, firmware_ptr} + 15'd2 <= 15'd8192);
    wire firmware_tail = firmware_cmd &&
                      (command_index == `FES_APPLICATION_FIRMWARE_DATA_TAIL_INDEX) &&
                      (command_argument[15:8] == 8'h00) &&
                      ({1'b0, firmware_ptr} + 15'd1 == 15'd8192);
    assign firmware_write_addr = firmware_ptr[12:0];
    assign firmware_write_data = command_argument[15:0];
    assign firmware_write_enable = {firmware_pair, firmware_pair | firmware_tail};

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

    function automatic [31:0] crc32_byte;
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
        if (ENABLE_MEDIA_STREAM && !ENABLE_MEDIA) $error("stream requires blob media");
        if (ENABLE_GAMEPAD && ENABLE_CONTROLLER_PORTS) $error("gamepad interfaces are mutually exclusive");
        if (ENABLE_KEYPAD_PORTS && !ENABLE_CONTROLLER_PORTS) $error("keypad requires controller ports");
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
        media_size = '0;
        media_byte0 = 8'h00;
        media_byte1 = 8'h00;
        media_byte2 = 8'h00;
        firmware_ready = 1'b0;
        firmware_open = 1'b0;
        firmware_ptr = 14'd0;
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
        buttons = 8'd0;
        controller_buttons = 16'd0;
        controller_keypad = 24'd0;
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
                        controller_buttons <= 16'd0;
                        controller_keypad <= 24'd0;
                    end else if (command_argument == `FES_APPLICATION_EXECUTION_RELEASE) begin
                        if ((ENABLE_MEDIA || ENABLE_MEDIA_STREAM) && (!media_ready || media_open || stream_active || stream_begin_next != 0))
                            reject_command(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                        else if (ENABLE_FIRMWARE && firmware_open)
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
                `FES_APPLICATION_OPCODE_CONTROLLER_BUTTONS: begin
                    if (!ENABLE_CONTROLLER_PORTS)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
                    else if (command_index >= `FES_APPLICATION_CONTROLLER_PORT_COUNT)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if ((command_argument & ~`FES_APPLICATION_CONTROLLER_BUTTON_MASK) != 0)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                    else if (command_index == 0)
                        controller_buttons[7:0] <= command_argument[7:0];
                    else
                        controller_buttons[15:8] <= command_argument[7:0];
                end
                `FES_APPLICATION_OPCODE_CONTROLLER_KEYPAD: begin
                    if (!ENABLE_KEYPAD_PORTS)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
                    else if (command_index >= `FES_APPLICATION_CONTROLLER_PORT_COUNT)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if ((command_argument & ~`FES_APPLICATION_CONTROLLER_KEYPAD_MASK) != 0)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                    else if (command_index == 0)
                        controller_keypad[11:0] <= command_argument[11:0];
                    else
                        controller_keypad[23:12] <= command_argument[11:0];
                end
                `FES_APPLICATION_OPCODE_MEDIA_BEGIN: begin
                    if (!ENABLE_MEDIA)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
                    else if (!exec_reset || media_open || stream_active || stream_begin_next != 0)
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
                        media_size <= '0;
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
                        media_size <= (MEDIA_AW+1)'(media_expected);
                    end
                end
                `FES_APPLICATION_OPCODE_MEDIA_STREAM_INFO: begin
                    if (!ENABLE_MEDIA_STREAM)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
                    else if (command_index > `FES_APPLICATION_MEDIA_STREAM_INFO_CHUNK_MAX_INDEX)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if (command_argument != 32'h00000000)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                    else begin
                        case (command_index)
                            `FES_APPLICATION_MEDIA_STREAM_INFO_MIN_LO_INDEX:
                                response_data <= STREAM_MIN[15:0];
                            `FES_APPLICATION_MEDIA_STREAM_INFO_MIN_HI_INDEX:
                                response_data <= STREAM_MIN[31:16];
                            `FES_APPLICATION_MEDIA_STREAM_INFO_MAX_LO_INDEX:
                                response_data <= STREAM_MAX[15:0];
                            `FES_APPLICATION_MEDIA_STREAM_INFO_MAX_HI_INDEX:
                                response_data <= STREAM_MAX[31:16];
                            default:
                                response_data <= STREAM_CHUNK_MAX[15:0];
                        endcase
                    end
                end
                `FES_APPLICATION_OPCODE_MEDIA_STREAM_BEGIN: begin
                    if (!ENABLE_MEDIA_STREAM)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
                    else if (!exec_reset || media_open || stream_active)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else if ((command_index > 32'd3) ||
                             (command_index != {29'd0, stream_begin_next}))
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if (command_index == 32'd3) begin
                        if (({stream_begin1, stream_begin0} < STREAM_MIN) ||
                            ({stream_begin1, stream_begin0} > STREAM_MAX))
                            reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                        else begin
                            stream_total <= {stream_begin1, stream_begin0};
                            stream_expected_crc <= {command_argument[15:0], stream_begin2};
                            stream_active <= 1'b1;
                            media_size <= '0;
                            media_byte0 <= 8'd0;
                            media_byte1 <= 8'd0;
                            media_byte2 <= 8'd0;
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
                        if (command_index == 32'd0) begin
                            stream_begin0 <= command_argument[15:0];
                            media_size <= '0;
                            media_byte0 <= 8'd0;
                            media_byte1 <= 8'd0;
                            media_byte2 <= 8'd0;
                        end
                        else if (command_index == 32'd1)
                            stream_begin1 <= command_argument[15:0];
                        else
                            stream_begin2 <= command_argument[15:0];
                    end
                end
                `FES_APPLICATION_OPCODE_MEDIA_STREAM_CHUNK: begin
                    if (!ENABLE_MEDIA_STREAM)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
                    else if (!exec_reset || !stream_active || (stream_chunk_length != 32'd0))
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else if ((command_index > 32'd2) ||
                             (command_index != {30'd0, stream_chunk_next}))
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if (command_index == 32'd2) begin
                        if (({stream_chunk1, stream_chunk0} != stream_received) ||
                            (command_argument < STREAM_MIN) ||
                            (command_argument > STREAM_CHUNK_MAX) ||
                            (stream_received > stream_total) ||
                            (command_argument > (stream_total - stream_received)))
                            reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
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
                `FES_APPLICATION_OPCODE_MEDIA_STREAM_DATA: begin
                    if (!ENABLE_MEDIA_STREAM)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
                    else if (!exec_reset || !stream_active || (stream_chunk_length == 32'd0))
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else if ((command_index > 32'd255) ||
                             (command_index != {23'd0, stream_ordinal}))
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if ((stream_remaining == 32'd1) &&
                             (command_argument[15:8] != 8'h00))
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
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
                `FES_APPLICATION_OPCODE_MEDIA_STREAM_COMMIT: begin
                    if (!ENABLE_MEDIA_STREAM)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
                    else if (command_index != `FES_APPLICATION_CONTROL_INDEX)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if (command_argument != 32'h00000000)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                    else if (!exec_reset || !stream_active ||
                             (stream_chunk_next != 2'd0) ||
                             (stream_chunk_length != 32'd0) ||
                             (stream_received != stream_total))
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else if ((stream_crc ^ STREAM_CRC_XOR) != stream_expected_crc)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                    else begin
                        stream_active <= 1'b0;
                        media_ready <= 1'b1;
                        media_size <= stream_total[(ENABLE_MEDIA_STREAM ? 15 : 14):0];
                    end
                end
                `FES_APPLICATION_OPCODE_MEDIA_STREAM_ABORT: begin
                    if (!ENABLE_MEDIA_STREAM)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
                    else if (command_index != `FES_APPLICATION_CONTROL_INDEX)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if (command_argument != 32'h00000000)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                    else if (!exec_reset || media_open)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else
                        clear_stream;
                end
                `FES_APPLICATION_OPCODE_FIRMWARE_BEGIN: begin
                    if (!ENABLE_FIRMWARE)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
                    else if (!exec_reset || firmware_open || media_open || stream_active)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else if (command_index != `FES_APPLICATION_CONTROL_INDEX)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if (command_argument != `FES_APPLICATION_FIRMWARE_BYTES)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                    else begin
                        firmware_open <= 1'b1;
                        firmware_ready <= 1'b0;
                        firmware_ptr <= 14'd0;
                    end
                end
                `FES_APPLICATION_OPCODE_FIRMWARE_DATA: begin
                    if (!ENABLE_FIRMWARE)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
                    else if (!exec_reset || !firmware_open)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else if (command_index == `FES_APPLICATION_FIRMWARE_DATA_PAIR_INDEX) begin
                        if ({1'b0, firmware_ptr} + 15'd2 > 15'd8192)
                            reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                        else
                            firmware_ptr <= firmware_ptr + 14'd2;
                    end else if (command_index == `FES_APPLICATION_FIRMWARE_DATA_TAIL_INDEX) begin
                        if (command_argument[15:8] != 8'h00 ||
                            {1'b0, firmware_ptr} + 15'd1 != 15'd8192)
                            reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                        else
                            firmware_ptr <= firmware_ptr + 14'd1;
                    end else
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                end
                `FES_APPLICATION_OPCODE_FIRMWARE_COMMIT: begin
                    if (!ENABLE_FIRMWARE)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
                    else if (!exec_reset)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else if (command_index != `FES_APPLICATION_CONTROL_INDEX)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_INDEX));
                    else if (command_argument != 32'h00000000)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_ARGUMENT));
                    else if (!firmware_open || firmware_ptr != 14'd8192)
                        reject_command(16'(`FES_APPLICATION_ERROR_INVALID_STATE));
                    else begin
                        firmware_open <= 1'b0;
                        firmware_ready <= 1'b1;
                    end
                end
                default: reject_command(16'(`FES_APPLICATION_ERROR_INVALID_OPCODE));
            endcase
        end
    end
endmodule
