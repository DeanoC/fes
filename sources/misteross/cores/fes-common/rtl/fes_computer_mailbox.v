// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"

// One-request-at-a-time HPS GPO/GPI mailbox for fes.computer 1.0
// (mister-packages docs/computer-io.md). The endpoint implements identity,
// execution, USB HID key rows, two controller ports and media unit 0. The
// core owns media storage: accepted data words appear on the write port for
// exactly the clock they are acknowledged (low byte at addr, high byte at
// addr+1), and unit0_state tells the core whether the unit's image may be
// presented to the machine. Media requests are valid while execution runs.
// ENABLE_APPLE2_FLOPPY, ENABLE_C64_DISK and ENABLE_SPECTRUM_TAPE each occupy
// unit 0; a core sets one of them. UNIT0_MIN/MAX must match that interface.
module fes_computer_mailbox #(
    parameter bit ENABLE_KEYBOARD = 0,
    parameter bit ENABLE_PORTS = 0,
    parameter bit ENABLE_MOUSE = 0,
    parameter bit ENABLE_AUDIO = 0,
    parameter bit ENABLE_APPLE2_FLOPPY = 0,
    parameter bit ENABLE_C64_DISK = 0,
    parameter bit ENABLE_SPECTRUM_TAPE = 0,
    parameter bit ENABLE_ATARI_ST_FLOPPY = 0,
    parameter bit ENABLE_ATARI_ST_FLOPPY_WRITE = 0,
    parameter bit ENABLE_ATARI_ST_FLOPPY_GEOMETRY = 0,
    parameter bit ENABLE_MEDIA_BACKPRESSURE = 0,
    parameter integer MEDIA_AW = 18,
    parameter [31:0] UNIT0_MIN = `FES_COMPUTER_APPLE2_FLOPPY_BYTES,
    parameter [31:0] UNIT0_MAX = `FES_COMPUTER_APPLE2_FLOPPY_BYTES
) (
    input  wire         clk,
    input  wire [31:0]  gpo,
    input  wire [127:0] build_id,
    output wire [31:0]  gpi,
    output reg          exec_reset,
    output reg  [143:0] keyboard_rows,
    output reg  [15:0]  controller_buttons,
    output wire         mouse_valid,
    output wire signed [15:0] mouse_dx,
    output wire signed [15:0] mouse_dy,
    output wire [1:0]    mouse_buttons,
    input  wire         mouse_ready,
    output wire [MEDIA_AW-1:0] media_write_addr,
    output wire [15:0]  media_write_data,
    output wire [1:0]   media_write_enable,
    input  wire        media_write_ready,
    input  wire        media_write_busy,
    input  wire        media_changed,
    output reg         media_frozen,
    output wire        media_read_req,
    output wire [19:0] media_read_addr,
    input  wire        media_read_ready,
    input  wire [7:0]  media_read_data,
    output reg  [1:0]   unit0_state,
    output reg  [31:0]  unit0_size
);
    // Unit 0 is one drive. A shell enables one of the three media parameters.
    localparam bit MEDIA = ENABLE_APPLE2_FLOPPY | ENABLE_C64_DISK | ENABLE_SPECTRUM_TAPE | ENABLE_ATARI_ST_FLOPPY;
    localparam bit WRITABLE = ENABLE_ATARI_ST_FLOPPY_WRITE && ENABLE_ATARI_ST_FLOPPY;
    localparam bit GEOMETRY = ENABLE_ATARI_ST_FLOPPY && ENABLE_ATARI_ST_FLOPPY_GEOMETRY;
    localparam [15:0] CAPABILITIES =
        16'(`FES_COMPUTER_INTERFACE_VIDEO_FIXED_720P60_CAPABILITY_MASK) |
        (ENABLE_KEYBOARD ? 16'(`FES_COMPUTER_INTERFACE_KEYBOARD_HID_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_PORTS ? 16'(`FES_COMPUTER_INTERFACE_GAMEPAD_PORTS_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_MOUSE ? 16'(`FES_COMPUTER_INTERFACE_MOUSE_RELATIVE_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_AUDIO ? 16'(`FES_COMPUTER_INTERFACE_AUDIO_PCM_S16_STEREO_48K_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_APPLE2_FLOPPY ? 16'(`FES_COMPUTER_INTERFACE_MEDIA_APPLE2_FLOPPY_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_C64_DISK ? 16'(`FES_COMPUTER_INTERFACE_MEDIA_C64_DISK_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_SPECTRUM_TAPE ? 16'(`FES_COMPUTER_INTERFACE_MEDIA_SPECTRUM_TAPE_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_ATARI_ST_FLOPPY ? 16'(`FES_COMPUTER_INTERFACE_MEDIA_ATARI_ST_FLOPPY_CAPABILITY_MASK) : 16'd0) |
        (GEOMETRY ? 16'(`FES_COMPUTER_INTERFACE_MEDIA_ATARI_ST_FLOPPY_GEOMETRY_CAPABILITY_MASK) : 16'd0) |
        (WRITABLE ? 16'(`FES_COMPUTER_INTERFACE_MEDIA_ATARI_ST_FLOPPY_WRITE_CAPABILITY_MASK) : 16'd0);
    localparam [15:0] E_OPCODE = 16'(`FES_COMPUTER_ERROR_INVALID_OPCODE);
    localparam [15:0] E_INDEX = 16'(`FES_COMPUTER_ERROR_INVALID_INDEX);
    localparam [15:0] E_ARGUMENT = 16'(`FES_COMPUTER_ERROR_INVALID_ARGUMENT);
    localparam [15:0] E_STATE = 16'(`FES_COMPUTER_ERROR_INVALID_STATE);
    localparam [1:0] EMPTY = 2'(`FES_COMPUTER_MEDIA_STATE_EMPTY);
    localparam [1:0] LOADING = 2'(`FES_COMPUTER_MEDIA_STATE_LOADING);
    localparam [1:0] READY = 2'(`FES_COMPUTER_MEDIA_STATE_READY);
    localparam [31:0] CHUNK_MAX = `FES_COMPUTER_MEDIA_CHUNK_MAX_BYTES;
    localparam [31:0] CRC_INIT = `FES_COMPUTER_MEDIA_CRC32_INITIAL;
    localparam [31:0] CRC_POLY = `FES_COMPUTER_MEDIA_CRC32_POLYNOMIAL;
    localparam [31:0] CRC_XOR = `FES_COMPUTER_MEDIA_CRC32_FINAL_XOR;

    (* async_reg = "true" *) reg request_meta;
    (* async_reg = "true" *) reg request_sync;
    reg acknowledged_toggle;
    reg response_error;
    reg [15:0] response_data;

    wire request_toggle = (gpo & `FES_COMPUTER_REQUEST_MASK) != 32'h00000000;
    wire [6:0] opcode = gpo[30:24];
    wire [7:0] index = gpo[23:16];
    wire [15:0] argument = gpo[15:0];
    wire request = request_sync != acknowledged_toggle;
    wire mouse_ok = request && ENABLE_MOUSE && !exec_reset &&
                    opcode == 7'(`FES_COMPUTER_OPCODE_MOUSE_RELATIVE) && index[7:2] == 6'd0;
    // GPO remains stable until ACK. The consumer and mailbox accept on the
    // same edge; keeping GPO held afterwards cannot repeat relative motion.
    assign mouse_valid = mouse_ok;
    assign mouse_dx = {{8{argument[7]}}, argument[7:0]};
    assign mouse_dy = {{8{argument[15]}}, argument[15:8]};
    assign mouse_buttons = index[1:0];

    assign gpi = `FES_COMPUTER_SIGNATURE |
                 (acknowledged_toggle ? `FES_COMPUTER_ACK_MASK : 32'h00000000) |
                 (response_error ? `FES_COMPUTER_ERROR_MASK : 32'h00000000) |
                 {16'h0000, response_data};

    // Snapshot ownership persists through Saved and its destructive Begin/Eject.
    // A failed save resumes explicitly; warm execution Hold never clears it.
    reg snapshot_dirty, saved_authorized;
    reg [15:0] snapshot_epoch;
    reg [1:0] snapshot_next, read_phase;
    reg [15:0] snapshot0, snapshot1;
    reg [31:0] snapshot_received;
    reg [9:0] snapshot_length, snapshot_cursor;
    reg [7:0] snapshot_ordinal, read_low;
    wire freeze_ok = request && WRITABLE && opcode == 7'(`FES_COMPUTER_OPCODE_MEDIA_SNAPSHOT_CONTROL) &&
                     index == 8'd0 && argument == 16'd0 && unit0_state == READY;
    wire snapshot_data_ok = request && WRITABLE && opcode == 7'(`FES_COMPUTER_OPCODE_MEDIA_SNAPSHOT_DATA) &&
                            argument == 16'd0 && index == snapshot_ordinal && media_frozen &&
                            unit0_state == READY && snapshot_length != 10'd0;
    wire snapshot_complete = snapshot_data_ok && read_phase == 2'd1 && media_read_ready;
    assign media_read_req = snapshot_data_ok && read_phase != 2'd2;
    wire [19:0] snapshot_address = snapshot_received[19:0] + {10'd0,snapshot_cursor} + (read_phase == 2'd1 ? 20'd1 : 20'd0);
    assign media_read_addr = snapshot_address;
    wire [31:0] snapshot_offset = {snapshot1,snapshot0};
    wire [15:0] snapshot_flags = {12'd0,media_write_busy,media_frozen,snapshot_dirty,unit0_state == READY};

    // Unit 0 transfer state.
    reg begin_staged;
    reg [1:0] begin_next;
    reg [15:0] begin0, begin1, begin2;
    reg active;
    reg [31:0] total;
    reg [31:0] expected_crc;
    reg [31:0] received;
    reg [31:0] crc;
    reg [1:0] chunk_next;
    reg [15:0] chunk0, chunk1;
    reg [9:0] chunk_length;
    reg [9:0] chunk_received;
    reg [7:0] ordinal;

    wire [4:0] unit8 = index[7:3];
    wire [2:0] field = index[2:0];
    wire [5:0] unit4 = index[7:2];
    wire [1:0] word = index[1:0];
    wire [31:0] begin_total = {begin1, begin0};
    wire [31:0] chunk_offset = {chunk1, chunk0};
    wire [9:0] remaining = chunk_length - chunk_received;
    wire data_ok = request && opcode == 7'(`FES_COMPUTER_OPCODE_MEDIA_DATA) && MEDIA &&
                   active && chunk_length != 10'd0 && index == ordinal &&
                   !(remaining == 10'd1 && argument[15:8] != 8'h00);

    assign media_write_addr = received[MEDIA_AW-1:0];
    assign media_write_data = argument;
    assign media_write_enable = {data_ok && remaining != 10'd1, data_ok};

    function [15:0] identity_word;
        input [7:0] word_index;
        input [127:0] identity;
        begin
            case (word_index)
                8'd0: identity_word = 16'(`FES_COMPUTER_IDENTITY_MAGIC0);
                8'd1: identity_word = 16'(`FES_COMPUTER_IDENTITY_MAGIC1);
                8'd2: identity_word = 16'(`FES_COMPUTER_TRANSPORT_MAJOR);
                8'd3: identity_word = 16'(`FES_COMPUTER_TRANSPORT_MINOR);
                8'd4: identity_word = 16'(`FES_COMPUTER_ABI_TAG);
                8'd5: identity_word = 16'(`FES_COMPUTER_ABI_MAJOR);
                8'd6: identity_word = 16'(`FES_COMPUTER_ABI_MINOR);
                8'd7: identity_word = CAPABILITIES;
                8'd8: identity_word = {identity[119:112], identity[127:120]};
                8'd9: identity_word = {identity[103:96], identity[111:104]};
                8'd10: identity_word = {identity[87:80], identity[95:88]};
                8'd11: identity_word = {identity[71:64], identity[79:72]};
                8'd12: identity_word = {identity[55:48], identity[63:56]};
                8'd13: identity_word = {identity[39:32], identity[47:40]};
                8'd14: identity_word = {identity[23:16], identity[31:24]};
                default: identity_word = {identity[7:0], identity[15:8]};
            endcase
        end
    endfunction

    function automatic [31:0] crc32_byte;
        input [31:0] value;
        input [7:0] data;
        integer i;
        reg [31:0] c;
        begin
            c = value ^ {24'h000000, data};
            for (i = 0; i < 8; i = i + 1)
                c = c[0] ? ((c >> 1) ^ CRC_POLY) : (c >> 1);
            crc32_byte = c;
        end
    endfunction

    task reject;
        input [15:0] code;
        begin
            response_error <= 1'b1;
            response_data <= code;
        end
    endtask

    task cancel_transfer;
        begin
            begin_staged <= 1'b0;
            begin_next <= 2'd0;
            active <= 1'b0;
            chunk_next <= 2'd0;
            chunk_length <= 10'd0;
            chunk_received <= 10'd0;
            ordinal <= 8'd0;
        end
    endtask

    initial begin
        request_meta = 1'b0;
        request_sync = 1'b0;
        acknowledged_toggle = 1'b0;
        response_error = 1'b0;
        response_data = 16'h0000;
        exec_reset = 1'b1;
        media_frozen = 1'b0;
        snapshot_dirty = 1'b0;
        saved_authorized = 1'b0;
        snapshot_epoch = 16'd0;
        snapshot_next = 2'd0;
        snapshot0 = 16'd0;
        snapshot1 = 16'd0;
        snapshot_received = 32'd0;
        snapshot_length = 10'd0;
        snapshot_cursor = 10'd0;
        snapshot_ordinal = 8'd0;
        read_phase = 2'd0;
        read_low = 8'd0;
        keyboard_rows = 144'd0;
        controller_buttons = 16'd0;
        unit0_state = EMPTY;
        unit0_size = 32'd0;
        begin_staged = 1'b0;
        begin_next = 2'd0;
        begin0 = 16'd0;
        begin1 = 16'd0;
        begin2 = 16'd0;
        active = 1'b0;
        total = 32'd0;
        expected_crc = 32'd0;
        received = 32'd0;
        crc = CRC_INIT;
        chunk_next = 2'd0;
        chunk0 = 16'd0;
        chunk1 = 16'd0;
        chunk_length = 10'd0;
        chunk_received = 10'd0;
        ordinal = 8'd0;
    end

    always @(posedge clk) begin
        request_meta <= request_toggle;
        request_sync <= request_meta;
        if (WRITABLE && media_changed) begin
            snapshot_dirty <= 1'b1;
            saved_authorized <= 1'b0;
            snapshot_epoch <= snapshot_epoch + 16'd1;
        end
        if (freeze_ok) media_frozen <= 1'b1;
        if (snapshot_data_ok && read_phase == 2'd0 && media_read_ready) begin
            read_low <= media_read_data;
            read_phase <= 2'd2; // electrical gap before the second byte request
        end else if (snapshot_data_ok && read_phase == 2'd2) read_phase <= 2'd1;
        if (request && (!ENABLE_MEDIA_BACKPRESSURE || !data_ok || media_write_ready) &&
            (!mouse_ok || mouse_ready) && (!freeze_ok || (media_frozen && !media_write_busy)) &&
            (!snapshot_data_ok || snapshot_complete)) begin
            acknowledged_toggle <= request_sync;
            response_error <= 1'b0;
            response_data <= 16'h0000;
            case (opcode)
                7'(`FES_COMPUTER_OPCODE_IDENTITY): begin
                    if (index >= 8'd16) reject(E_INDEX);
                    else if (argument != 16'd0) reject(E_ARGUMENT);
                    else response_data <= identity_word(index, build_id);
                end
                7'(`FES_COMPUTER_OPCODE_EXECUTION): begin
                    if (index != 8'd0) reject(E_INDEX);
                    else if (argument > 16'd1) reject(E_ARGUMENT);
                    else if (argument == 16'd0) begin
                        exec_reset <= 1'b1;
                        keyboard_rows <= 144'd0;
                        controller_buttons <= 16'd0;
                    end else begin
                        exec_reset <= 1'b0;
                    end
                end
                7'(`FES_COMPUTER_OPCODE_KEYBOARD_HID): begin
                    if (!ENABLE_KEYBOARD) reject(E_OPCODE);
                    else if (index > 8'd8) reject(E_INDEX);
                    else if ((index == 8'd0 && argument[3:0] != 4'd0) ||
                             (index == 8'd8 && argument[15:8] != 8'd0)) reject(E_ARGUMENT);
                    else keyboard_rows[index * 16 +: 16] <= argument;
                end
                7'(`FES_COMPUTER_OPCODE_CONTROLLER_BUTTONS): begin
                    if (!ENABLE_PORTS) reject(E_OPCODE);
                    else if (index > 8'd1) reject(E_INDEX);
                    else if (argument[15:8] != 8'd0) reject(E_ARGUMENT);
                    else if (index == 8'd0) controller_buttons[7:0] <= argument[7:0];
                    else controller_buttons[15:8] <= argument[7:0];
                end
                7'(`FES_COMPUTER_OPCODE_MOUSE_RELATIVE): begin
                    if (!ENABLE_MOUSE) reject(E_OPCODE);
                    else if (index[7:2] != 6'd0) reject(E_INDEX);
                    else if (exec_reset) reject(E_STATE);
                end
                7'(`FES_COMPUTER_OPCODE_MEDIA_INFO): begin
                    if (!MEDIA) reject(E_OPCODE);
                    else if (unit8 >= 5'd8 || field > 3'd5) reject(E_INDEX);
                    else if (argument != 16'd0) reject(E_ARGUMENT);
                    else if (unit8 == 5'd0) begin
                        case (field)
                            3'd0: response_data <= UNIT0_MIN[15:0];
                            3'd1: response_data <= UNIT0_MIN[31:16];
                            3'd2: response_data <= UNIT0_MAX[15:0];
                            3'd3: response_data <= UNIT0_MAX[31:16];
                            3'd4: response_data <= CHUNK_MAX[15:0];
                            default: response_data <= {14'd0, unit0_state};
                        endcase
                    end
                end
                7'(`FES_COMPUTER_OPCODE_MEDIA_BEGIN): begin
                    if (!MEDIA) reject(E_OPCODE);
                    else if (unit4 != 6'd0) reject(E_INDEX);
                    // Collection and accepted sector commits retain the old
                    // image even for volatile loads. Never overlap a new host
                    // upload with a writer draining through job withdrawal.
                    else if (WRITABLE && media_write_busy) reject(E_STATE);
                    else if (media_frozen && !saved_authorized) reject(E_STATE);
                    else if (active) reject(E_STATE);
                    else if (!begin_staged ? word != 2'd0 : word != begin_next) reject(E_INDEX);
                    else if (word == 2'd3) begin
                        if (begin_total < UNIT0_MIN || begin_total > UNIT0_MAX ||
                            (GEOMETRY && begin_total != 32'd368640 &&
                            begin_total != 32'd409600 &&
                            begin_total != 32'd737280 &&
                            begin_total != 32'd819200 &&
                            begin_total != 32'd373248 &&
                            begin_total != 32'd414720 &&
                            begin_total != 32'd746496 &&
                            begin_total != 32'd829440 &&
                            begin_total != 32'd377856 &&
                            begin_total != 32'd419840 &&
                            begin_total != 32'd755712 &&
                            begin_total != 32'd839680)) reject(E_ARGUMENT);
                        else begin
                            active <= 1'b1;
                            total <= begin_total;
                            expected_crc <= {argument, begin2};
                            received <= 32'd0;
                            crc <= CRC_INIT;
                            begin_staged <= 1'b0;
                            begin_next <= 2'd0;
                            chunk_next <= 2'd0;
                            chunk_length <= 10'd0;
                            chunk_received <= 10'd0;
                            ordinal <= 8'd0;
                        end
                    end else begin
                        begin_next <= begin_next + 2'd1;
                        case (word)
                            2'd0: begin
                                begin0 <= argument;
                                begin_staged <= 1'b1;
                                unit0_state <= LOADING;
                                media_frozen <= 1'b0;
                                saved_authorized <= 1'b0;
                                snapshot_dirty <= 1'b0;
                                snapshot_length <= 10'd0;
                            end
                            2'd1: begin1 <= argument;
                            default: begin2 <= argument;
                        endcase
                    end
                end
                7'(`FES_COMPUTER_OPCODE_MEDIA_CHUNK): begin
                    if (!MEDIA) reject(E_OPCODE);
                    else if (!active) reject(E_STATE);
                    else if (unit4 != 6'd0 || word != chunk_next || word == 2'd3) reject(E_INDEX);
                    else if (chunk_length != 10'd0) reject(E_STATE);
                    else if (word == 2'd2) begin
                        if (chunk_offset != received || argument == 16'd0 ||
                            {16'd0, argument} > CHUNK_MAX || {16'd0, argument} > total - received)
                            reject(E_ARGUMENT);
                        else begin
                            chunk_length <= argument[9:0];
                            chunk_received <= 10'd0;
                            ordinal <= 8'd0;
                            chunk_next <= 2'd0;
                        end
                    end else begin
                        chunk_next <= chunk_next + 2'd1;
                        if (word == 2'd0) chunk0 <= argument;
                        else chunk1 <= argument;
                    end
                end
                7'(`FES_COMPUTER_OPCODE_MEDIA_DATA): begin
                    if (!MEDIA) reject(E_OPCODE);
                    else if (!active || chunk_length == 10'd0) reject(E_STATE);
                    else if (index != ordinal) reject(E_INDEX);
                    else if (remaining == 10'd1 && argument[15:8] != 8'd0) reject(E_ARGUMENT);
                    else if (remaining == 10'd1) begin
                        crc <= crc32_byte(crc, argument[7:0]);
                        received <= received + 32'd1;
                        chunk_length <= 10'd0;
                        chunk_received <= 10'd0;
                        ordinal <= 8'd0;
                    end else begin
                        crc <= crc32_byte(crc32_byte(crc, argument[7:0]), argument[15:8]);
                        received <= received + 32'd2;
                        if (remaining == 10'd2) begin
                            chunk_length <= 10'd0;
                            chunk_received <= 10'd0;
                            ordinal <= 8'd0;
                        end else begin
                            chunk_received <= chunk_received + 10'd2;
                            ordinal <= ordinal + 8'd1;
                        end
                    end
                end
                7'(`FES_COMPUTER_OPCODE_MEDIA_COMMIT): begin
                    if (!MEDIA) reject(E_OPCODE);
                    else if (index != 8'd0) reject(E_INDEX);
                    else if (argument != 16'd0) reject(E_ARGUMENT);
                    else if (!active || begin_staged) reject(E_STATE);
                    else if (chunk_length != 10'd0 || received != total) reject(E_STATE);
                    else if ((crc ^ CRC_XOR) != expected_crc) reject(E_ARGUMENT);
                    else begin
                        unit0_state <= READY;
                        unit0_size <= total;
                        cancel_transfer;
                    end
                end
                7'(`FES_COMPUTER_OPCODE_MEDIA_EJECT): begin
                    if (!MEDIA) reject(E_OPCODE);
                    else if (index != 8'd0) reject(E_INDEX);
                    else if (argument != 16'd0) reject(E_ARGUMENT);
                    else if (WRITABLE && media_write_busy) reject(E_STATE);
                    else if (media_frozen && !saved_authorized) reject(E_STATE);
                    else begin
                        cancel_transfer;
                        unit0_state <= EMPTY;
                        media_frozen <= 1'b0;
                        saved_authorized <= 1'b0;
                        snapshot_dirty <= 1'b0;
                        snapshot_length <= 10'd0;
                        unit0_size <= 32'd0;
                    end
                end
                7'(`FES_COMPUTER_OPCODE_MEDIA_SNAPSHOT_INFO): begin
                    if (!WRITABLE) reject(E_OPCODE);
                    else if (unit8 != 5'd0) reject(E_INDEX);
                    else if (argument != 16'd0) reject(E_ARGUMENT);
                    else case (field)
                        3'd0: response_data <= snapshot_flags;
                        3'd1, 3'd2: response_data <= 16'd1;
                        3'd3: response_data <= GEOMETRY && unit0_size != 32'd737280 ? 16'd1 : 16'd0;
                        3'd4: response_data <= unit0_size[15:0];
                        3'd5: response_data <= unit0_size[31:16];
                        3'd6: response_data <= 16'd512;
                        default: response_data <= snapshot_epoch;
                    endcase
                end
                7'(`FES_COMPUTER_OPCODE_MEDIA_SNAPSHOT_CONTROL): begin
                    if (!WRITABLE) reject(E_OPCODE);
                    else if (index != 8'd0) reject(E_INDEX);
                    else if (argument > 16'd2) reject(E_ARGUMENT);
                    else if (argument == 16'd0) begin
                        if (unit0_state != READY) reject(E_STATE);
                        else begin
                            media_frozen <= 1'b1;
                            saved_authorized <= 1'b0;
                            snapshot_received <= 32'd0;
                            snapshot_next <= 2'd0;
                            snapshot_length <= 10'd0;
                            snapshot_cursor <= 10'd0;
                            snapshot_ordinal <= 8'd0;
                            read_phase <= 2'd0;
                        end
                    end else if (argument == 16'd1) begin
                        media_frozen <= 1'b0;
                        saved_authorized <= 1'b0;
                        snapshot_length <= 10'd0;
                        snapshot_next <= 2'd0;
                        read_phase <= 2'd0;
                    end else if (!media_frozen || media_write_busy || media_changed) reject(E_STATE);
                    else begin
                        snapshot_dirty <= 1'b0;
                        saved_authorized <= 1'b1;
                    end
                end
                7'(`FES_COMPUTER_OPCODE_MEDIA_SNAPSHOT_CHUNK): begin
                    if (!WRITABLE) reject(E_OPCODE);
                    else if (!media_frozen || unit0_state != READY || snapshot_length != 10'd0) reject(E_STATE);
                    else if (unit4 != 6'd0 || word != snapshot_next || word == 2'd3) reject(E_INDEX);
                    else if (word == 2'd2) begin
                        if (snapshot_offset != snapshot_received || argument == 16'd0 || argument[0] ||
                            argument > 16'd512 || {16'd0,argument} > unit0_size - snapshot_received) reject(E_ARGUMENT);
                        else begin
                            snapshot_length <= argument[9:0];
                            snapshot_cursor <= 10'd0;
                            snapshot_ordinal <= 8'd0;
                            snapshot_next <= 2'd0;
                            read_phase <= 2'd0;
                        end
                    end else begin
                        snapshot_next <= snapshot_next + 2'd1;
                        if (word == 2'd0) snapshot0 <= argument;
                        else snapshot1 <= argument;
                    end
                end
                7'(`FES_COMPUTER_OPCODE_MEDIA_SNAPSHOT_DATA): begin
                    if (!WRITABLE) reject(E_OPCODE);
                    else if (!media_frozen || unit0_state != READY || snapshot_length == 10'd0) reject(E_STATE);
                    else if (index != snapshot_ordinal) reject(E_INDEX);
                    else if (argument != 16'd0) reject(E_ARGUMENT);
                    else begin
                        response_data <= {media_read_data,read_low};
                        read_phase <= 2'd0;
                        if (snapshot_cursor + 10'd2 == snapshot_length) begin
                            snapshot_received <= snapshot_received + {22'd0,snapshot_length};
                            snapshot_length <= 10'd0;
                            snapshot_cursor <= 10'd0;
                            snapshot_ordinal <= 8'd0;
                        end else begin
                            snapshot_cursor <= snapshot_cursor + 10'd2;
                            snapshot_ordinal <= snapshot_ordinal + 8'd1;
                        end
                    end
                end
                default: reject(E_OPCODE);
            endcase
        end
    end
endmodule
