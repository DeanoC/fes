// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"

// Atari ST mailbox. The accepting edge assigns acknowledged_toggle from the
// synchronized request and does not elaborate the display-wait hold. The
// command payload is captured beside that synchronizer, so the HPS GP port
// ends at a flip-flop. Only the request toggle is an async_reg synchronizer.
// The payload flops are ordinary registers: the bus is held stable while the
// toggle crosses, and a synchronizer constraint would pin all 31 bits
// together instead of letting them pack next to the decoder.
//
// The accept cone does not contain the wide compares or the CRC XOR. Begin
// size, chunk and snapshot alignment, bytes remaining and the commit residue
// are registers. Reflected CRC-32 of a media word is a parallel XOR that
// runs on the following clock, into crc and crc_good. That clock is free:
// the host holds GPO until ACK, and the next request toggle still crosses
// two synchronizer stages before another command can be accepted. Relative
// mouse samples and the write-data word stay on the live GPO so a held
// command still matches the shared mailbox on the accept edge.
// sim-fes-atari-st-mailbox-direct fails if opcodes, acknowledgement, or the
// media and input behaviour ST uses drift.
module fes_computer_mailbox_direct #(
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
    localparam [31:0] CRC_XOR = `FES_COMPUTER_MEDIA_CRC32_FINAL_XOR;

    (* async_reg = "true" *) reg request_meta;
    (* async_reg = "true" *) reg request_sync;
    // Same two edges as the request toggle. keep stops the pair collapsing
    // into one flop or into the HPS port. They are not async_reg.
    (* keep *) reg [30:0] payload_meta;
    (* keep *) reg [30:0] payload_sync;
    reg acknowledged_toggle;
    reg response_error;
    reg [15:0] response_data;

    wire request_toggle = (gpo & `FES_COMPUTER_REQUEST_MASK) != 32'h00000000;
    wire [6:0] opcode = payload_sync[30:24];
    wire [7:0] index = payload_sync[23:16];
    wire [15:0] argument = payload_sync[15:0];
    wire [15:0] gpo_argument = gpo[15:0];
    wire request = request_sync != acknowledged_toggle;
    wire mouse_ok = request && ENABLE_MOUSE && !exec_reset &&
                    opcode == 7'(`FES_COMPUTER_OPCODE_MOUSE_RELATIVE) && index[7:2] == 6'd0;
    // GPO remains stable until ACK. The consumer and mailbox accept on the
    // same edge; keeping GPO held afterwards cannot repeat relative motion.
    // Motion and the write word follow the live port. The registered payload
    // is the same value once the request synchronizer has fired.
    assign mouse_valid = mouse_ok;
    assign mouse_dx = {{8{gpo_argument[7]}}, gpo_argument[7:0]};
    assign mouse_dy = {{8{gpo_argument[15]}}, gpo_argument[15:8]};
    assign mouse_buttons = gpo[17:16];

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
    // Registered on the clock after the state they read. The request
    // synchronizer gives the next command two clocks, so the accept edge
    // sees these results and not the wide comparators or the CRC tree.
    reg [9:0] remaining;
    reg begin_argument_ok;
    reg chunk_match;
    reg snapshot_match;
    reg commit_ready;
    reg crc_good;
    reg crc_pending;
    reg crc_arm;
    reg crc_single;
    reg [15:0] crc_data;
    reg [31:0] bytes_left;
    reg [31:0] snapshot_left;

    wire [4:0] unit8 = index[7:3];
    wire [2:0] field = index[2:0];
    wire [5:0] unit4 = index[7:2];
    wire [1:0] word = index[1:0];
    wire [31:0] begin_total = {begin1, begin0};
    wire [31:0] chunk_offset = {chunk1, chunk0};
    wire begin_listed =
        begin_total == 32'd368640 || begin_total == 32'd409600 ||
        begin_total == 32'd737280 || begin_total == 32'd819200 ||
        begin_total == 32'd373248 || begin_total == 32'd414720 ||
        begin_total == 32'd746496 || begin_total == 32'd829440 ||
        begin_total == 32'd377856 || begin_total == 32'd419840 ||
        begin_total == 32'd755712 || begin_total == 32'd839680;
    wire begin_in_range = begin_total >= UNIT0_MIN && begin_total <= UNIT0_MAX;
    wire data_ok = request && opcode == 7'(`FES_COMPUTER_OPCODE_MEDIA_DATA) && MEDIA &&
                   active && chunk_length != 10'd0 && index == ordinal &&
                   !(remaining == 10'd1 && argument[15:8] != 8'h00);

    assign media_write_addr = received[MEDIA_AW-1:0];
    assign media_write_data = gpo_argument;
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
                // The ABI 1.0 identity: every capability here fits word 7.
                8'd6: identity_word = 16'd0;
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

    // Parallel reflected CRC-32 (polynomial 0xEDB88320). Same residue as the
    // serial byte recurrence. The fold register below, not the accept case,
    // absorbs this XOR tree.
    function automatic [31:0] crc32_byte;
        input [31:0] crc_in;
        input [7:0] data;
        begin
            crc32_byte[0] = crc_in[2] ^ crc_in[8] ^ data[2];
            crc32_byte[1] = crc_in[0] ^ crc_in[3] ^ crc_in[9] ^ data[0] ^ data[3];
            crc32_byte[2] = crc_in[0] ^ crc_in[1] ^ crc_in[4] ^ crc_in[10] ^ data[0] ^ data[1] ^ data[4];
            crc32_byte[3] = crc_in[1] ^ crc_in[2] ^ crc_in[5] ^ crc_in[11] ^ data[1] ^ data[2] ^ data[5];
            crc32_byte[4] = crc_in[0] ^ crc_in[2] ^ crc_in[3] ^ crc_in[6] ^ crc_in[12] ^ data[0] ^ data[2] ^ data[3] ^ data[6];
            crc32_byte[5] = crc_in[1] ^ crc_in[3] ^ crc_in[4] ^ crc_in[7] ^ crc_in[13] ^ data[1] ^ data[3] ^ data[4] ^ data[7];
            crc32_byte[6] = crc_in[4] ^ crc_in[5] ^ crc_in[14] ^ data[4] ^ data[5];
            crc32_byte[7] = crc_in[0] ^ crc_in[5] ^ crc_in[6] ^ crc_in[15] ^ data[0] ^ data[5] ^ data[6];
            crc32_byte[8] = crc_in[1] ^ crc_in[6] ^ crc_in[7] ^ crc_in[16] ^ data[1] ^ data[6] ^ data[7];
            crc32_byte[9] = crc_in[7] ^ crc_in[17] ^ data[7];
            crc32_byte[10] = crc_in[2] ^ crc_in[18] ^ data[2];
            crc32_byte[11] = crc_in[3] ^ crc_in[19] ^ data[3];
            crc32_byte[12] = crc_in[0] ^ crc_in[4] ^ crc_in[20] ^ data[0] ^ data[4];
            crc32_byte[13] = crc_in[0] ^ crc_in[1] ^ crc_in[5] ^ crc_in[21] ^ data[0] ^ data[1] ^ data[5];
            crc32_byte[14] = crc_in[1] ^ crc_in[2] ^ crc_in[6] ^ crc_in[22] ^ data[1] ^ data[2] ^ data[6];
            crc32_byte[15] = crc_in[2] ^ crc_in[3] ^ crc_in[7] ^ crc_in[23] ^ data[2] ^ data[3] ^ data[7];
            crc32_byte[16] = crc_in[0] ^ crc_in[2] ^ crc_in[3] ^ crc_in[4] ^ crc_in[24] ^ data[0] ^ data[2] ^ data[3] ^ data[4];
            crc32_byte[17] = crc_in[0] ^ crc_in[1] ^ crc_in[3] ^ crc_in[4] ^ crc_in[5] ^ crc_in[25] ^ data[0] ^ data[1] ^ data[3] ^ data[4] ^ data[5];
            crc32_byte[18] = crc_in[0] ^ crc_in[1] ^ crc_in[2] ^ crc_in[4] ^ crc_in[5] ^ crc_in[6] ^ crc_in[26] ^ data[0] ^ data[1] ^ data[2] ^ data[4] ^ data[5] ^ data[6];
            crc32_byte[19] = crc_in[1] ^ crc_in[2] ^ crc_in[3] ^ crc_in[5] ^ crc_in[6] ^ crc_in[7] ^ crc_in[27] ^ data[1] ^ data[2] ^ data[3] ^ data[5] ^ data[6] ^ data[7];
            crc32_byte[20] = crc_in[3] ^ crc_in[4] ^ crc_in[6] ^ crc_in[7] ^ crc_in[28] ^ data[3] ^ data[4] ^ data[6] ^ data[7];
            crc32_byte[21] = crc_in[2] ^ crc_in[4] ^ crc_in[5] ^ crc_in[7] ^ crc_in[29] ^ data[2] ^ data[4] ^ data[5] ^ data[7];
            crc32_byte[22] = crc_in[2] ^ crc_in[3] ^ crc_in[5] ^ crc_in[6] ^ crc_in[30] ^ data[2] ^ data[3] ^ data[5] ^ data[6];
            crc32_byte[23] = crc_in[3] ^ crc_in[4] ^ crc_in[6] ^ crc_in[7] ^ crc_in[31] ^ data[3] ^ data[4] ^ data[6] ^ data[7];
            crc32_byte[24] = crc_in[0] ^ crc_in[2] ^ crc_in[4] ^ crc_in[5] ^ crc_in[7] ^ data[0] ^ data[2] ^ data[4] ^ data[5] ^ data[7];
            crc32_byte[25] = crc_in[0] ^ crc_in[1] ^ crc_in[2] ^ crc_in[3] ^ crc_in[5] ^ crc_in[6] ^ data[0] ^ data[1] ^ data[2] ^ data[3] ^ data[5] ^ data[6];
            crc32_byte[26] = crc_in[0] ^ crc_in[1] ^ crc_in[2] ^ crc_in[3] ^ crc_in[4] ^ crc_in[6] ^ crc_in[7] ^ data[0] ^ data[1] ^ data[2] ^ data[3] ^ data[4] ^ data[6] ^ data[7];
            crc32_byte[27] = crc_in[1] ^ crc_in[3] ^ crc_in[4] ^ crc_in[5] ^ crc_in[7] ^ data[1] ^ data[3] ^ data[4] ^ data[5] ^ data[7];
            crc32_byte[28] = crc_in[0] ^ crc_in[4] ^ crc_in[5] ^ crc_in[6] ^ data[0] ^ data[4] ^ data[5] ^ data[6];
            crc32_byte[29] = crc_in[0] ^ crc_in[1] ^ crc_in[5] ^ crc_in[6] ^ crc_in[7] ^ data[0] ^ data[1] ^ data[5] ^ data[6] ^ data[7];
            crc32_byte[30] = crc_in[0] ^ crc_in[1] ^ crc_in[6] ^ crc_in[7] ^ data[0] ^ data[1] ^ data[6] ^ data[7];
            crc32_byte[31] = crc_in[1] ^ crc_in[7] ^ data[1] ^ data[7];
        end
    endfunction

    function automatic [31:0] crc32_pair;
        input [31:0] crc_in;
        input [15:0] data;
        begin
            crc32_pair[0] = crc_in[0] ^ crc_in[4] ^ crc_in[6] ^ crc_in[7] ^ crc_in[10] ^ crc_in[16] ^ data[0] ^ data[4] ^ data[6] ^ data[7] ^ data[10];
            crc32_pair[1] = crc_in[1] ^ crc_in[5] ^ crc_in[7] ^ crc_in[8] ^ crc_in[11] ^ crc_in[17] ^ data[1] ^ data[5] ^ data[7] ^ data[8] ^ data[11];
            crc32_pair[2] = crc_in[2] ^ crc_in[6] ^ crc_in[8] ^ crc_in[9] ^ crc_in[12] ^ crc_in[18] ^ data[2] ^ data[6] ^ data[8] ^ data[9] ^ data[12];
            crc32_pair[3] = crc_in[3] ^ crc_in[7] ^ crc_in[9] ^ crc_in[10] ^ crc_in[13] ^ crc_in[19] ^ data[3] ^ data[7] ^ data[9] ^ data[10] ^ data[13];
            crc32_pair[4] = crc_in[4] ^ crc_in[8] ^ crc_in[10] ^ crc_in[11] ^ crc_in[14] ^ crc_in[20] ^ data[4] ^ data[8] ^ data[10] ^ data[11] ^ data[14];
            crc32_pair[5] = crc_in[5] ^ crc_in[9] ^ crc_in[11] ^ crc_in[12] ^ crc_in[15] ^ crc_in[21] ^ data[5] ^ data[9] ^ data[11] ^ data[12] ^ data[15];
            crc32_pair[6] = crc_in[0] ^ crc_in[4] ^ crc_in[7] ^ crc_in[12] ^ crc_in[13] ^ crc_in[22] ^ data[0] ^ data[4] ^ data[7] ^ data[12] ^ data[13];
            crc32_pair[7] = crc_in[1] ^ crc_in[5] ^ crc_in[8] ^ crc_in[13] ^ crc_in[14] ^ crc_in[23] ^ data[1] ^ data[5] ^ data[8] ^ data[13] ^ data[14];
            crc32_pair[8] = crc_in[0] ^ crc_in[2] ^ crc_in[6] ^ crc_in[9] ^ crc_in[14] ^ crc_in[15] ^ crc_in[24] ^ data[0] ^ data[2] ^ data[6] ^ data[9] ^ data[14] ^ data[15];
            crc32_pair[9] = crc_in[1] ^ crc_in[3] ^ crc_in[4] ^ crc_in[6] ^ crc_in[15] ^ crc_in[25] ^ data[1] ^ data[3] ^ data[4] ^ data[6] ^ data[15];
            crc32_pair[10] = crc_in[2] ^ crc_in[5] ^ crc_in[6] ^ crc_in[10] ^ crc_in[26] ^ data[2] ^ data[5] ^ data[6] ^ data[10];
            crc32_pair[11] = crc_in[3] ^ crc_in[6] ^ crc_in[7] ^ crc_in[11] ^ crc_in[27] ^ data[3] ^ data[6] ^ data[7] ^ data[11];
            crc32_pair[12] = crc_in[0] ^ crc_in[4] ^ crc_in[7] ^ crc_in[8] ^ crc_in[12] ^ crc_in[28] ^ data[0] ^ data[4] ^ data[7] ^ data[8] ^ data[12];
            crc32_pair[13] = crc_in[0] ^ crc_in[1] ^ crc_in[5] ^ crc_in[8] ^ crc_in[9] ^ crc_in[13] ^ crc_in[29] ^ data[0] ^ data[1] ^ data[5] ^ data[8] ^ data[9] ^ data[13];
            crc32_pair[14] = crc_in[1] ^ crc_in[2] ^ crc_in[6] ^ crc_in[9] ^ crc_in[10] ^ crc_in[14] ^ crc_in[30] ^ data[1] ^ data[2] ^ data[6] ^ data[9] ^ data[10] ^ data[14];
            crc32_pair[15] = crc_in[2] ^ crc_in[3] ^ crc_in[7] ^ crc_in[10] ^ crc_in[11] ^ crc_in[15] ^ crc_in[31] ^ data[2] ^ data[3] ^ data[7] ^ data[10] ^ data[11] ^ data[15];
            crc32_pair[16] = crc_in[0] ^ crc_in[3] ^ crc_in[6] ^ crc_in[7] ^ crc_in[8] ^ crc_in[10] ^ crc_in[11] ^ crc_in[12] ^ data[0] ^ data[3] ^ data[6] ^ data[7] ^ data[8] ^ data[10] ^ data[11] ^ data[12];
            crc32_pair[17] = crc_in[0] ^ crc_in[1] ^ crc_in[4] ^ crc_in[7] ^ crc_in[8] ^ crc_in[9] ^ crc_in[11] ^ crc_in[12] ^ crc_in[13] ^ data[0] ^ data[1] ^ data[4] ^ data[7] ^ data[8] ^ data[9] ^ data[11] ^ data[12] ^ data[13];
            crc32_pair[18] = crc_in[1] ^ crc_in[2] ^ crc_in[5] ^ crc_in[8] ^ crc_in[9] ^ crc_in[10] ^ crc_in[12] ^ crc_in[13] ^ crc_in[14] ^ data[1] ^ data[2] ^ data[5] ^ data[8] ^ data[9] ^ data[10] ^ data[12] ^ data[13] ^ data[14];
            crc32_pair[19] = crc_in[0] ^ crc_in[2] ^ crc_in[3] ^ crc_in[6] ^ crc_in[9] ^ crc_in[10] ^ crc_in[11] ^ crc_in[13] ^ crc_in[14] ^ crc_in[15] ^ data[0] ^ data[2] ^ data[3] ^ data[6] ^ data[9] ^ data[10] ^ data[11] ^ data[13] ^ data[14] ^ data[15];
            crc32_pair[20] = crc_in[0] ^ crc_in[1] ^ crc_in[3] ^ crc_in[6] ^ crc_in[11] ^ crc_in[12] ^ crc_in[14] ^ crc_in[15] ^ data[0] ^ data[1] ^ data[3] ^ data[6] ^ data[11] ^ data[12] ^ data[14] ^ data[15];
            crc32_pair[21] = crc_in[1] ^ crc_in[2] ^ crc_in[6] ^ crc_in[10] ^ crc_in[12] ^ crc_in[13] ^ crc_in[15] ^ data[1] ^ data[2] ^ data[6] ^ data[10] ^ data[12] ^ data[13] ^ data[15];
            crc32_pair[22] = crc_in[2] ^ crc_in[3] ^ crc_in[4] ^ crc_in[6] ^ crc_in[10] ^ crc_in[11] ^ crc_in[13] ^ crc_in[14] ^ data[2] ^ data[3] ^ data[4] ^ data[6] ^ data[10] ^ data[11] ^ data[13] ^ data[14];
            crc32_pair[23] = crc_in[3] ^ crc_in[4] ^ crc_in[5] ^ crc_in[7] ^ crc_in[11] ^ crc_in[12] ^ crc_in[14] ^ crc_in[15] ^ data[3] ^ data[4] ^ data[5] ^ data[7] ^ data[11] ^ data[12] ^ data[14] ^ data[15];
            crc32_pair[24] = crc_in[0] ^ crc_in[5] ^ crc_in[7] ^ crc_in[8] ^ crc_in[10] ^ crc_in[12] ^ crc_in[13] ^ crc_in[15] ^ data[0] ^ data[5] ^ data[7] ^ data[8] ^ data[10] ^ data[12] ^ data[13] ^ data[15];
            crc32_pair[25] = crc_in[1] ^ crc_in[4] ^ crc_in[7] ^ crc_in[8] ^ crc_in[9] ^ crc_in[10] ^ crc_in[11] ^ crc_in[13] ^ crc_in[14] ^ data[1] ^ data[4] ^ data[7] ^ data[8] ^ data[9] ^ data[10] ^ data[11] ^ data[13] ^ data[14];
            crc32_pair[26] = crc_in[2] ^ crc_in[5] ^ crc_in[8] ^ crc_in[9] ^ crc_in[10] ^ crc_in[11] ^ crc_in[12] ^ crc_in[14] ^ crc_in[15] ^ data[2] ^ data[5] ^ data[8] ^ data[9] ^ data[10] ^ data[11] ^ data[12] ^ data[14] ^ data[15];
            crc32_pair[27] = crc_in[0] ^ crc_in[3] ^ crc_in[4] ^ crc_in[7] ^ crc_in[9] ^ crc_in[11] ^ crc_in[12] ^ crc_in[13] ^ crc_in[15] ^ data[0] ^ data[3] ^ data[4] ^ data[7] ^ data[9] ^ data[11] ^ data[12] ^ data[13] ^ data[15];
            crc32_pair[28] = crc_in[0] ^ crc_in[1] ^ crc_in[5] ^ crc_in[6] ^ crc_in[7] ^ crc_in[8] ^ crc_in[12] ^ crc_in[13] ^ crc_in[14] ^ data[0] ^ data[1] ^ data[5] ^ data[6] ^ data[7] ^ data[8] ^ data[12] ^ data[13] ^ data[14];
            crc32_pair[29] = crc_in[1] ^ crc_in[2] ^ crc_in[6] ^ crc_in[7] ^ crc_in[8] ^ crc_in[9] ^ crc_in[13] ^ crc_in[14] ^ crc_in[15] ^ data[1] ^ data[2] ^ data[6] ^ data[7] ^ data[8] ^ data[9] ^ data[13] ^ data[14] ^ data[15];
            crc32_pair[30] = crc_in[2] ^ crc_in[3] ^ crc_in[4] ^ crc_in[6] ^ crc_in[8] ^ crc_in[9] ^ crc_in[14] ^ crc_in[15] ^ data[2] ^ data[3] ^ data[4] ^ data[6] ^ data[8] ^ data[9] ^ data[14] ^ data[15];
            crc32_pair[31] = crc_in[3] ^ crc_in[5] ^ crc_in[6] ^ crc_in[9] ^ crc_in[15] ^ data[3] ^ data[5] ^ data[6] ^ data[9] ^ data[15];
        end
    endfunction

    wire [31:0] crc_folded = crc_single ? crc32_byte(crc, crc_data[7:0]) : crc32_pair(crc, crc_data);

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
        payload_meta = 31'd0;
        payload_sync = 31'd0;
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
        remaining = 10'd0;
        begin_argument_ok = 1'b0;
        chunk_match = 1'b1;
        snapshot_match = 1'b1;
        commit_ready = 1'b0;
        crc_good = 1'b0;
        crc_pending = 1'b0;
        crc_arm = 1'b0;
        crc_single = 1'b0;
        crc_data = 16'd0;
        bytes_left = 32'd0;
        snapshot_left = 32'd0;
    end

    always @(posedge clk) begin
        request_meta <= request_toggle;
        payload_meta <= gpo[30:0];
        request_sync <= request_meta;
        payload_sync <= payload_meta;
        remaining <= chunk_length - chunk_received;
        begin_argument_ok <= begin_in_range && (!GEOMETRY || begin_listed);
        chunk_match <= chunk_offset == received;
        snapshot_match <= snapshot_offset == snapshot_received;
        commit_ready <= active && !begin_staged && chunk_length == 10'd0 && received == total;
        bytes_left <= total - received;
        snapshot_left <= unit0_size - snapshot_received;
        if (crc_pending) begin
            crc <= crc_folded;
            crc_good <= (crc_folded ^ CRC_XOR) == expected_crc;
            crc_pending <= 1'b0;
        end else if (crc_arm) begin
            crc <= CRC_INIT;
            crc_good <= (CRC_INIT ^ CRC_XOR) == expected_crc;
            crc_arm <= 1'b0;
        end
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
                        if (!begin_argument_ok) reject(E_ARGUMENT);
                        else begin
                            active <= 1'b1;
                            total <= begin_total;
                            expected_crc <= {argument, begin2};
                            received <= 32'd0;
                            crc_pending <= 1'b0;
                            crc_arm <= 1'b1;
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
                        if (!chunk_match || argument == 16'd0 ||
                            {16'd0, argument} > CHUNK_MAX || {16'd0, argument} > bytes_left)
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
                        crc_data <= argument;
                        crc_single <= 1'b1;
                        crc_pending <= 1'b1;
                        received <= received + 32'd1;
                        chunk_length <= 10'd0;
                        chunk_received <= 10'd0;
                        ordinal <= 8'd0;
                    end else begin
                        crc_data <= argument;
                        crc_single <= 1'b0;
                        crc_pending <= 1'b1;
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
                    else if (!commit_ready) reject(E_STATE);
                    else if (!crc_good) reject(E_ARGUMENT);
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
                        if (!snapshot_match || argument == 16'd0 || argument[0] ||
                            argument > 16'd512 || {16'd0,argument} > snapshot_left) reject(E_ARGUMENT);
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
