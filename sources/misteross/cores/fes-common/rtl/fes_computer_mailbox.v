// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"

// One-request-at-a-time HPS GPO/GPI mailbox for fes.computer 1.0 and 1.1
// (mister-packages docs/computer-io.md). The endpoint implements identity,
// execution, USB HID key rows, two controller ports and media unit 0. The
// core owns media storage: accepted data words appear on the write port for
// exactly the clock they are acknowledged (low byte at addr, high byte at
// addr+1), and unit0_state tells the core whether the unit's image may be
// presented to the machine. Media requests are valid while execution runs.
// ENABLE_APPLE2_FLOPPY, ENABLE_C64_DISK, ENABLE_SPECTRUM_TAPE and
// ENABLE_ZX81_TAPE each occupy unit 0; a core sets one of them. UNIT0_MIN/MAX must match that interface.
// ENABLE_SPECTRUM_DISK adds unit 1 beside a unit-0 medium. One transfer runs
// at a time: media_write_unit names the unit its writes belong to, and
// unit1_state/unit1_size report unit 1 as unit0_* report unit 0.
// Shells that leave the session-display plane off instantiate fes_computer_mailbox,
// which ties these response inputs idle. Verilator rejects ranged input defaults,
// so the idle tie lives in that wrapper. ZX81 instantiates this module and
// connects the plane. Verilator -Wall rejects a first module whose name
// differs from the filename, so that check is off for the core only.
// ENABLE_ATA_DISK, ENABLE_PERSISTENCE, ENABLE_PC386_MACHINE and
// ENABLE_WALL_CLOCK are the 1.1 interfaces on capability bits 16..19. Any of
// them makes the endpoint minor 1, answering identity word 16; their opcodes
// go to fes_computer_storage over the storage port, held until it replies,
// as the menu opcodes go to the display plane. The persistence opcodes need
// both of its bits. execution_accepted pulses for every accepted Execution
// request, which closes that endpoint's restore staging.
/* verilator lint_off DECLFILENAME */
module fes_computer_mailbox_core #(
    parameter bit ENABLE_KEYBOARD = 0,
    parameter bit ENABLE_PORTS = 0,
    parameter bit ENABLE_MOUSE = 0,
    parameter bit ENABLE_AUDIO = 0,
    parameter bit ENABLE_APPLE2_FLOPPY = 0,
    parameter bit ENABLE_C64_DISK = 0,
    parameter bit ENABLE_SPECTRUM_TAPE = 0,
    parameter bit ENABLE_ZX81_TAPE = 0,
    parameter bit ENABLE_SESSION_DISPLAY = 0,
    parameter bit ENABLE_ATARI_ST_FLOPPY = 0,
    parameter bit ENABLE_ATARI_ST_FLOPPY_WRITE = 0,
    parameter bit ENABLE_ATARI_ST_FLOPPY_GEOMETRY = 0,
    parameter bit ENABLE_ZX81_TAPE_WRITE = 0,
    parameter bit ENABLE_SPECTRUM_DISK = 0,
    parameter bit ENABLE_ATA_DISK = 0,
    parameter bit ENABLE_PERSISTENCE = 0,
    parameter bit ENABLE_PC386_MACHINE = 0,
    parameter bit ENABLE_WALL_CLOCK = 0,
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
    output wire         media_write_unit,
    input  wire        media_write_ready,
    input  wire        media_write_busy,
    input  wire        media_changed,
    output reg         media_frozen,
    output wire        media_read_req,
    output wire [19:0] media_read_addr,
    input  wire        media_read_ready,
    input  wire [7:0]  media_read_data,
    // BASIC SAVE image. Unused unless ENABLE_ZX81_TAPE_WRITE. Tied off by
    // fes_computer_mailbox; the ZX81 shell connects the machine save buffer.
    input  wire        save_ready,
    input  wire        save_busy,
    input  wire [14:0] save_size,
    output reg  [1:0]   unit0_state,
    output reg  [31:0]  unit0_size,
    output reg  [1:0]   unit1_state,
    output reg  [31:0]  unit1_size,
    // In-session launcher plane (fes.memory.hps-ddr and
    // fes.video.session-display). A request is held until the pixel-domain reply.
    output reg          display_request,
    output reg  [6:0]   display_opcode,
    output reg  [7:0]   display_index,
    output reg  [15:0]  display_argument,
    input  wire         display_response_valid,
    input  wire         display_response_error,
    input  wire [15:0]  display_response_data,
    input  wire         display_quiesced,
    // fes.computer 1.1 storage requests, held until the reply.
    output reg          storage_request,
    output reg  [6:0]   storage_opcode,
    output reg  [7:0]   storage_index,
    output reg  [15:0]  storage_argument,
    input  wire         storage_response_valid,
    input  wire         storage_response_error,
    input  wire [15:0]  storage_response_data,
    output reg          execution_accepted
);
    // Unit 0 is one drive. A shell enables one of the three media parameters.
    localparam bit MEDIA = ENABLE_APPLE2_FLOPPY | ENABLE_C64_DISK | ENABLE_SPECTRUM_TAPE | ENABLE_ZX81_TAPE | ENABLE_ATARI_ST_FLOPPY;
    localparam bit WRITABLE = ENABLE_ATARI_ST_FLOPPY_WRITE && ENABLE_ATARI_ST_FLOPPY;
    localparam bit ZX81_WRITE = ENABLE_ZX81_TAPE && ENABLE_ZX81_TAPE_WRITE;
    localparam bit SNAPSHOT = WRITABLE || ZX81_WRITE;
    localparam bit GEOMETRY = ENABLE_ATARI_ST_FLOPPY && ENABLE_ATARI_ST_FLOPPY_GEOMETRY;
    localparam bit UNIT1 = MEDIA && ENABLE_SPECTRUM_DISK;
    localparam [31:0] UNIT1_MIN = `FES_COMPUTER_SPECTRUM_DISK_MIN_BYTES;
    localparam [31:0] UNIT1_MAX = `FES_COMPUTER_SPECTRUM_DISK_MAX_BYTES;
    localparam [15:0] CAPABILITIES =
        16'(`FES_COMPUTER_INTERFACE_VIDEO_FIXED_720P60_CAPABILITY_MASK) |
        (ENABLE_KEYBOARD ? 16'(`FES_COMPUTER_INTERFACE_KEYBOARD_HID_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_PORTS ? 16'(`FES_COMPUTER_INTERFACE_GAMEPAD_PORTS_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_MOUSE ? 16'(`FES_COMPUTER_INTERFACE_MOUSE_RELATIVE_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_AUDIO ? 16'(`FES_COMPUTER_INTERFACE_AUDIO_PCM_S16_STEREO_48K_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_APPLE2_FLOPPY ? 16'(`FES_COMPUTER_INTERFACE_MEDIA_APPLE2_FLOPPY_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_C64_DISK ? 16'(`FES_COMPUTER_INTERFACE_MEDIA_C64_DISK_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_SPECTRUM_TAPE ? 16'(`FES_COMPUTER_INTERFACE_MEDIA_SPECTRUM_TAPE_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_ZX81_TAPE ? 16'(`FES_COMPUTER_INTERFACE_MEDIA_ZX81_TAPE_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_ATARI_ST_FLOPPY ? 16'(`FES_COMPUTER_INTERFACE_MEDIA_ATARI_ST_FLOPPY_CAPABILITY_MASK) : 16'd0) |
        (GEOMETRY ? 16'(`FES_COMPUTER_INTERFACE_MEDIA_ATARI_ST_FLOPPY_GEOMETRY_CAPABILITY_MASK) : 16'd0) |
        (WRITABLE ? 16'(`FES_COMPUTER_INTERFACE_MEDIA_ATARI_ST_FLOPPY_WRITE_CAPABILITY_MASK) : 16'd0) |
        (ZX81_WRITE ? 16'(`FES_COMPUTER_INTERFACE_MEDIA_ZX81_TAPE_WRITE_CAPABILITY_MASK) : 16'd0) |
        (UNIT1 ? 16'(`FES_COMPUTER_INTERFACE_MEDIA_SPECTRUM_DISK_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_SESSION_DISPLAY ? 16'(`FES_COMPUTER_INTERFACE_MEMORY_HPS_DDR_CAPABILITY_MASK) : 16'd0) |
        (ENABLE_SESSION_DISPLAY ? 16'(`FES_COMPUTER_INTERFACE_VIDEO_SESSION_DISPLAY_CAPABILITY_MASK) : 16'd0);
    // Identity word 16: capabilities 16..31.
    localparam [31:0] CAPABILITIES_1_1 =
        (ENABLE_ATA_DISK ? `FES_COMPUTER_INTERFACE_MEDIA_ATA_DISK_CAPABILITY_MASK : 32'd0) |
        (ENABLE_PERSISTENCE ? `FES_COMPUTER_INTERFACE_PERSISTENCE_WORDS_CAPABILITY_MASK : 32'd0) |
        (ENABLE_WALL_CLOCK ? `FES_COMPUTER_INTERFACE_RTC_WALL_CLOCK_CAPABILITY_MASK : 32'd0) |
        (ENABLE_PC386_MACHINE ? `FES_COMPUTER_INTERFACE_PC386_MACHINE_CAPABILITY_MASK : 32'd0);
    localparam [15:0] CAPABILITIES_HIGH = CAPABILITIES_1_1[31:16];
    localparam bit MINOR_1 = CAPABILITIES_HIGH != 16'd0;
    localparam bit DATA = ENABLE_PERSISTENCE && ENABLE_PC386_MACHINE;
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
    // ZX81 SAVE is ready without a loaded cassette. ST still keys off unit 0.
    wire snap_ready = ZX81_WRITE ? save_ready : unit0_state == READY;
    wire snap_busy = ZX81_WRITE ? (save_busy || media_write_busy) : media_write_busy;
    wire [31:0] snap_size = ZX81_WRITE ? {17'd0, save_size} : unit0_size;
    wire snapshot_one = snapshot_length == 10'd1;
    wire freeze_ok = request && SNAPSHOT && opcode == 7'(`FES_COMPUTER_OPCODE_MEDIA_SNAPSHOT_CONTROL) &&
                     index == 8'd0 && argument == 16'd0 && snap_ready && !(ZX81_WRITE && save_busy);
    wire snapshot_data_ok = request && SNAPSHOT && opcode == 7'(`FES_COMPUTER_OPCODE_MEDIA_SNAPSHOT_DATA) &&
                            argument == 16'd0 && index == snapshot_ordinal && media_frozen &&
                            snap_ready && !(ZX81_WRITE && save_busy) && snapshot_length != 10'd0;
    wire snapshot_complete = snapshot_data_ok &&
                             (snapshot_one ? read_phase == 2'd3 : read_phase == 2'd1 && media_read_ready);
    assign media_read_req = snapshot_data_ok && (snapshot_one ? read_phase == 2'd0 : read_phase != 2'd2);
    wire [19:0] snapshot_address = snapshot_received[19:0] + {10'd0,snapshot_cursor} + (read_phase == 2'd1 ? 20'd1 : 20'd0);
    assign media_read_addr = snapshot_address;
    wire [31:0] snapshot_offset = {snapshot1,snapshot0};
    wire [15:0] snapshot_flags = {12'd0,snap_busy,media_frozen,snapshot_dirty,snap_ready};

    // Transfer state, for unit 0 or (UNIT1) unit 1.
    reg begin_staged;
    reg begin_unit, active_unit;
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
    reg display_wait;

    wire [4:0] unit8 = index[7:3];
    wire [2:0] field = index[2:0];
    wire [5:0] unit4 = index[7:2];
    wire [1:0] word = index[1:0];
    // unit4 names a unit this endpoint implements.
    wire unit4_ok = unit4 == 6'd0 || (UNIT1 && unit4 == 6'd1);
    wire unit_index_ok = index == 8'd0 || (UNIT1 && index == 8'd1);
    wire [31:0] begin_total = {begin1, begin0};
    wire [31:0] chunk_offset = {chunk1, chunk0};
    wire [9:0] remaining = chunk_length - chunk_received;
    wire data_ok = request && opcode == 7'(`FES_COMPUTER_OPCODE_MEDIA_DATA) && MEDIA &&
                   active && chunk_length != 10'd0 && index == ordinal &&
                   !(remaining == 10'd1 && argument[15:8] != 8'h00);

    assign media_write_addr = received[MEDIA_AW-1:0];
    assign media_write_unit = UNIT1 && active_unit;
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
                8'd6: identity_word = {15'd0, MINOR_1};
                8'd7: identity_word = CAPABILITIES;
                8'd16: identity_word = CAPABILITIES_HIGH;
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
        unit1_state = EMPTY;
        unit1_size = 32'd0;
        begin_unit = 1'b0;
        active_unit = 1'b0;
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
        display_request = 1'b0;
        display_opcode = 7'd0;
        display_index = 8'd0;
        display_argument = 16'd0;
        display_wait = 1'b0;
        storage_request = 1'b0;
        storage_opcode = 7'd0;
        storage_index = 8'd0;
        storage_argument = 16'd0;
        execution_accepted = 1'b0;
    end

    always @(posedge clk) begin
        request_meta <= request_toggle;
        request_sync <= request_meta;
        execution_accepted <= 1'b0;
        if (SNAPSHOT && media_changed) begin
            snapshot_dirty <= 1'b1;
            saved_authorized <= 1'b0;
            snapshot_epoch <= snapshot_epoch + 16'd1;
        end
        if (freeze_ok) media_frozen <= 1'b1;
        if (snapshot_data_ok && snapshot_one && read_phase == 2'd0 && media_read_ready) begin
            read_low <= media_read_data;
            read_phase <= 2'd3; // one-byte tail: no second read
        end else if (snapshot_data_ok && !snapshot_one && read_phase == 2'd0 && media_read_ready) begin
            read_low <= media_read_data;
            read_phase <= 2'd2; // electrical gap before the second byte request
        end else if (snapshot_data_ok && read_phase == 2'd2) read_phase <= 2'd1;
        if (display_wait) begin
            if (display_response_valid) begin
                acknowledged_toggle <= request_sync;
                response_error <= display_response_error;
                response_data <= display_response_data;
                display_request <= 1'b0;
                display_wait <= 1'b0;
            end
        end else if (storage_request) begin
            if (storage_response_valid) begin
                acknowledged_toggle <= request_sync;
                response_error <= storage_response_error;
                response_data <= storage_response_data;
                storage_request <= 1'b0;
            end
        end else if (request && (!ENABLE_MEDIA_BACKPRESSURE || !data_ok || media_write_ready) &&
            (!mouse_ok || mouse_ready) && (!freeze_ok || (media_frozen && !snap_busy)) &&
            (!snapshot_data_ok || snapshot_complete)) begin
            response_error <= 1'b0;
            response_data <= 16'h0000;
            begin : acknowledge
                reg defer_ack;
                defer_ack = 1'b0;
            case (opcode)
                7'(`FES_COMPUTER_OPCODE_IDENTITY): begin
                    if (index > (MINOR_1 ? 8'd16 : 8'd15)) reject(E_INDEX);
                    else if (argument != 16'd0) reject(E_ARGUMENT);
                    else response_data <= identity_word(index, build_id);
                end
                7'(`FES_COMPUTER_OPCODE_EXECUTION): begin
                    if (index != 8'd0) reject(E_INDEX);
                    else if (argument > 16'd1) reject(E_ARGUMENT);
                    else if (argument == 16'd0 && ENABLE_SESSION_DISPLAY && !display_quiesced)
                        reject(E_STATE);
                    else if (argument == 16'd0) begin
                        exec_reset <= 1'b1;
                        keyboard_rows <= 144'd0;
                        controller_buttons <= 16'd0;
                        execution_accepted <= 1'b1;
                    end else begin
                        exec_reset <= 1'b0;
                        execution_accepted <= 1'b1;
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
                    end else if (UNIT1 && unit8 == 5'd1) begin
                        case (field)
                            3'd0: response_data <= UNIT1_MIN[15:0];
                            3'd1: response_data <= UNIT1_MIN[31:16];
                            3'd2: response_data <= UNIT1_MAX[15:0];
                            3'd3: response_data <= UNIT1_MAX[31:16];
                            3'd4: response_data <= CHUNK_MAX[15:0];
                            default: response_data <= {14'd0, unit1_state};
                        endcase
                    end
                end
                7'(`FES_COMPUTER_OPCODE_MEDIA_BEGIN): begin
                    if (!MEDIA) reject(E_OPCODE);
                    else if (!unit4_ok || (begin_staged && unit4[0] != begin_unit)) reject(E_INDEX);
                    // Collection and accepted sector commits retain the old
                    // image even for volatile loads. Never overlap a new host
                    // upload with a writer draining through job withdrawal.
                    // Writable disks and an active ZX81 LOAD both hold this input.
                    else if (unit4 == 6'd0 && (WRITABLE || ENABLE_ZX81_TAPE) && media_write_busy) reject(E_STATE);
                    else if (unit4 == 6'd0 && ZX81_WRITE && save_busy) reject(E_STATE);
                    else if (unit4 == 6'd0 && media_frozen && !saved_authorized) reject(E_STATE);
                    else if (active) reject(E_STATE);
                    else if (!begin_staged ? word != 2'd0 : word != begin_next) reject(E_INDEX);
                    else if (word == 2'd3 && begin_unit) begin
                        if (begin_total < UNIT1_MIN || begin_total > UNIT1_MAX) reject(E_ARGUMENT);
                        else begin
                            active <= 1'b1;
                            active_unit <= 1'b1;
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
                    end else if (word == 2'd3) begin
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
                            active_unit <= 1'b0;
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
                                begin_unit <= UNIT1 && unit4[0];
                                if (UNIT1 && unit4[0]) unit1_state <= LOADING;
                                else begin
                                    unit0_state <= LOADING;
                                    media_frozen <= 1'b0;
                                    saved_authorized <= 1'b0;
                                    if (!ZX81_WRITE) snapshot_dirty <= 1'b0;
                                    snapshot_length <= 10'd0;
                                end
                            end
                            2'd1: begin1 <= argument;
                            default: begin2 <= argument;
                        endcase
                    end
                end
                7'(`FES_COMPUTER_OPCODE_MEDIA_CHUNK): begin
                    if (!MEDIA) reject(E_OPCODE);
                    else if (!active) reject(E_STATE);
                    else if (unit4 != {5'd0, UNIT1 && active_unit} || word != chunk_next || word == 2'd3) reject(E_INDEX);
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
                    else if (!unit_index_ok) reject(E_INDEX);
                    else if (argument != 16'd0) reject(E_ARGUMENT);
                    else if (!active || begin_staged || index[0] != (UNIT1 && active_unit)) reject(E_STATE);
                    else if (chunk_length != 10'd0 || received != total) reject(E_STATE);
                    else if ((crc ^ CRC_XOR) != expected_crc) reject(E_ARGUMENT);
                    else begin
                        if (UNIT1 && active_unit) begin
                            unit1_state <= READY;
                            unit1_size <= total;
                        end else begin
                            unit0_state <= READY;
                            unit0_size <= total;
                        end
                        cancel_transfer;
                    end
                end
                7'(`FES_COMPUTER_OPCODE_MEDIA_EJECT): begin
                    if (!MEDIA) reject(E_OPCODE);
                    else if (!unit_index_ok) reject(E_INDEX);
                    else if (argument != 16'd0) reject(E_ARGUMENT);
                    else if (UNIT1 && index[0]) begin
                        // Unit 1 ejects without touching unit 0's transfer.
                        if ((begin_staged && begin_unit) || (active && active_unit)) cancel_transfer;
                        unit1_state <= EMPTY;
                        unit1_size <= 32'd0;
                    end
                    else if ((WRITABLE || ENABLE_ZX81_TAPE) && media_write_busy) reject(E_STATE);
                    else if (ZX81_WRITE && save_busy) reject(E_STATE);
                    else if (media_frozen && !saved_authorized) reject(E_STATE);
                    else begin
                        if (!(UNIT1 && ((begin_staged && begin_unit) || (active && active_unit))))
                            cancel_transfer;
                        unit0_state <= EMPTY;
                        media_frozen <= 1'b0;
                        saved_authorized <= 1'b0;
                        if (!ZX81_WRITE) snapshot_dirty <= 1'b0;
                        snapshot_length <= 10'd0;
                        unit0_size <= 32'd0;
                    end
                end
                7'(`FES_COMPUTER_OPCODE_MEDIA_SNAPSHOT_INFO): begin
                    if (!SNAPSHOT) reject(E_OPCODE);
                    else if (unit8 != 5'd0) reject(E_INDEX);
                    else if (argument != 16'd0) reject(E_ARGUMENT);
                    else case (field)
                        3'd0: response_data <= snapshot_flags;
                        3'd1: response_data <= ZX81_WRITE ? 16'(`FES_COMPUTER_ZX81_TAPE_LAYOUT_TAG) : 16'd1;
                        3'd2: response_data <= 16'd1;
                        3'd3: response_data <= ZX81_WRITE ? 16'd0 :
                            (GEOMETRY && unit0_size != 32'd737280 ? 16'd1 : 16'd0);
                        3'd4: response_data <= snap_size[15:0];
                        3'd5: response_data <= snap_size[31:16];
                        3'd6: response_data <= 16'd512;
                        default: response_data <= snapshot_epoch;
                    endcase
                end
                7'(`FES_COMPUTER_OPCODE_MEDIA_SNAPSHOT_CONTROL): begin
                    if (!SNAPSHOT) reject(E_OPCODE);
                    else if (index != 8'd0) reject(E_INDEX);
                    else if (argument > 16'd2) reject(E_ARGUMENT);
                    else if (argument == 16'd0) begin
                        if (!snap_ready || (ZX81_WRITE && save_busy)) reject(E_STATE);
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
                    end else if (!media_frozen || snap_busy || media_changed) reject(E_STATE);
                    else begin
                        snapshot_dirty <= 1'b0;
                        saved_authorized <= 1'b1;
                    end
                end
                7'(`FES_COMPUTER_OPCODE_MEDIA_SNAPSHOT_CHUNK): begin
                    if (!SNAPSHOT) reject(E_OPCODE);
                    else if (!media_frozen || !snap_ready || (ZX81_WRITE && save_busy) || snapshot_length != 10'd0) reject(E_STATE);
                    else if (unit4 != 6'd0 || word != snapshot_next || word == 2'd3) reject(E_INDEX);
                    else if (word == 2'd2) begin
                        if (snapshot_offset != snapshot_received || argument == 16'd0 ||
                            (argument[0] && !(ZX81_WRITE && argument == 16'd1)) ||
                            argument > 16'd512 || {16'd0,argument} > snap_size - snapshot_received) reject(E_ARGUMENT);
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
                    if (!SNAPSHOT) reject(E_OPCODE);
                    else if (!media_frozen || !snap_ready || (ZX81_WRITE && save_busy) || snapshot_length == 10'd0) reject(E_STATE);
                    else if (index != snapshot_ordinal) reject(E_INDEX);
                    else if (argument != 16'd0) reject(E_ARGUMENT);
                    else begin
                        response_data <= snapshot_one ? {8'h00, read_low} : {media_read_data, read_low};
                        read_phase <= 2'd0;
                        if (snapshot_one || snapshot_cursor + 10'd2 == snapshot_length) begin
                            snapshot_received <= snapshot_received + (snapshot_one ? 32'd1 : {22'd0,snapshot_length});
                            snapshot_length <= 10'd0;
                            snapshot_cursor <= 10'd0;
                            snapshot_ordinal <= 8'd0;
                        end else begin
                            snapshot_cursor <= snapshot_cursor + 10'd2;
                            snapshot_ordinal <= snapshot_ordinal + 8'd1;
                        end
                    end
                end
                7'(`FES_COMPUTER_OPCODE_MENU_INFO),
                7'(`FES_COMPUTER_OPCODE_MENU_CONFIGURE),
                7'(`FES_COMPUTER_OPCODE_MENU_CONTROL),
                7'(`FES_COMPUTER_OPCODE_MENU_SUBMIT): begin
                    if (!ENABLE_SESSION_DISPLAY) reject(E_OPCODE);
                    else begin
                        // Hold the request until the pixel-domain plane replies.
                        defer_ack = 1'b1;
                        display_request <= 1'b1;
                        display_opcode <= opcode;
                        display_index <= index;
                        display_argument <= argument;
                        display_wait <= 1'b1;
                    end
                end
                7'(`FES_COMPUTER_OPCODE_DISK_INFO),
                7'(`FES_COMPUTER_OPCODE_DISK_CONTROL),
                7'(`FES_COMPUTER_OPCODE_DISK_COMMIT),
                7'(`FES_COMPUTER_OPCODE_DISK_DIRTY),
                7'(`FES_COMPUTER_OPCODE_DATA_CONTROL),
                7'(`FES_COMPUTER_OPCODE_DATA_READ),
                7'(`FES_COMPUTER_OPCODE_DATA_WRITE),
                7'(`FES_COMPUTER_OPCODE_DATA_INFO),
                7'(`FES_COMPUTER_OPCODE_WALL_CLOCK): begin
                    if (opcode >= 7'(`FES_COMPUTER_OPCODE_DATA_CONTROL) &&
                        opcode <= 7'(`FES_COMPUTER_OPCODE_DATA_INFO) ? !DATA :
                        opcode == 7'(`FES_COMPUTER_OPCODE_WALL_CLOCK) ? !ENABLE_WALL_CLOCK : !ENABLE_ATA_DISK)
                        reject(E_OPCODE);
                    else begin
                        defer_ack = 1'b1;
                        storage_request <= 1'b1;
                        storage_opcode <= opcode;
                        storage_index <= index;
                        storage_argument <= argument;
                    end
                end
                default: reject(E_OPCODE);
            endcase
                if (!defer_ack)
                    acknowledged_toggle <= request_sync;
            end
        end
    end
endmodule
/* verilator lint_on DECLFILENAME */

// Port list used by shells that leave the session-display plane off.
// Response inputs are tied idle here because Verilator rejects ranged defaults.
module fes_computer_mailbox #(
    parameter bit ENABLE_KEYBOARD = 0,
    parameter bit ENABLE_PORTS = 0,
    parameter bit ENABLE_MOUSE = 0,
    parameter bit ENABLE_AUDIO = 0,
    parameter bit ENABLE_APPLE2_FLOPPY = 0,
    parameter bit ENABLE_C64_DISK = 0,
    parameter bit ENABLE_SPECTRUM_TAPE = 0,
    parameter bit ENABLE_ZX81_TAPE = 0,
    parameter bit ENABLE_SESSION_DISPLAY = 0,
    parameter bit ENABLE_ATARI_ST_FLOPPY = 0,
    parameter bit ENABLE_ATARI_ST_FLOPPY_WRITE = 0,
    parameter bit ENABLE_ATARI_ST_FLOPPY_GEOMETRY = 0,
    parameter bit ENABLE_ZX81_TAPE_WRITE = 0,
    parameter bit ENABLE_MEDIA_BACKPRESSURE = 0,
    parameter integer MEDIA_AW = 18,
    parameter [31:0] UNIT0_MIN = `FES_COMPUTER_APPLE2_FLOPPY_BYTES,
    parameter [31:0] UNIT0_MAX = `FES_COMPUTER_APPLE2_FLOPPY_BYTES
) (
    input  wire         clk,
    input  wire [31:0]  gpo,
    input  wire [127:0] build_id,
    output wire [31:0]  gpi,
    output wire         exec_reset,
    output wire [143:0] keyboard_rows,
    output wire [15:0]  controller_buttons,
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
    output wire        media_frozen,
    output wire        media_read_req,
    output wire [19:0] media_read_addr,
    input  wire        media_read_ready,
    input  wire [7:0]  media_read_data,
    output wire [1:0]   unit0_state,
    output wire [31:0]  unit0_size
);
    /* verilator lint_off UNUSEDSIGNAL */
    wire unused_display_request;
    wire [6:0] unused_display_opcode;
    wire [7:0] unused_display_index;
    wire [15:0] unused_display_argument;
    wire unused_write_unit;
    wire [1:0] unused_unit1_state;
    wire [31:0] unused_unit1_size;
    wire unused_storage_request, unused_execution_accepted;
    wire [6:0] unused_storage_opcode;
    wire [7:0] unused_storage_index;
    wire [15:0] unused_storage_argument;
    /* verilator lint_on UNUSEDSIGNAL */
    fes_computer_mailbox_core #(
        .ENABLE_KEYBOARD(ENABLE_KEYBOARD),
        .ENABLE_PORTS(ENABLE_PORTS),
        .ENABLE_MOUSE(ENABLE_MOUSE),
        .ENABLE_AUDIO(ENABLE_AUDIO),
        .ENABLE_APPLE2_FLOPPY(ENABLE_APPLE2_FLOPPY),
        .ENABLE_C64_DISK(ENABLE_C64_DISK),
        .ENABLE_SPECTRUM_TAPE(ENABLE_SPECTRUM_TAPE),
        .ENABLE_ZX81_TAPE(ENABLE_ZX81_TAPE),
        .ENABLE_SESSION_DISPLAY(ENABLE_SESSION_DISPLAY),
        .ENABLE_ATARI_ST_FLOPPY(ENABLE_ATARI_ST_FLOPPY),
        .ENABLE_ATARI_ST_FLOPPY_WRITE(ENABLE_ATARI_ST_FLOPPY_WRITE),
        .ENABLE_ATARI_ST_FLOPPY_GEOMETRY(ENABLE_ATARI_ST_FLOPPY_GEOMETRY),
        .ENABLE_ZX81_TAPE_WRITE(ENABLE_ZX81_TAPE_WRITE),
        .ENABLE_MEDIA_BACKPRESSURE(ENABLE_MEDIA_BACKPRESSURE),
        .MEDIA_AW(MEDIA_AW),
        .UNIT0_MIN(UNIT0_MIN),
        .UNIT0_MAX(UNIT0_MAX)
    ) core (
        .clk(clk), .gpo(gpo), .build_id(build_id), .gpi(gpi),
        .exec_reset(exec_reset), .keyboard_rows(keyboard_rows),
        .controller_buttons(controller_buttons),
        .mouse_valid(mouse_valid), .mouse_dx(mouse_dx), .mouse_dy(mouse_dy),
        .mouse_buttons(mouse_buttons), .mouse_ready(mouse_ready),
        .media_write_addr(media_write_addr), .media_write_data(media_write_data),
        .media_write_enable(media_write_enable), .media_write_unit(unused_write_unit),
        .media_write_ready(media_write_ready),
        .media_write_busy(media_write_busy), .media_changed(media_changed),
        .media_frozen(media_frozen),
        .media_read_req(media_read_req), .media_read_addr(media_read_addr),
        .media_read_ready(media_read_ready), .media_read_data(media_read_data),
        .save_ready(1'b0), .save_busy(1'b0), .save_size(15'd0),
        .unit0_state(unit0_state), .unit0_size(unit0_size),
        .unit1_state(unused_unit1_state), .unit1_size(unused_unit1_size),
        .display_request(unused_display_request), .display_opcode(unused_display_opcode),
        .display_index(unused_display_index), .display_argument(unused_display_argument),
        .display_response_valid(1'b0), .display_response_error(1'b0),
        .display_response_data(16'd0), .display_quiesced(1'b1),
        .storage_request(unused_storage_request), .storage_opcode(unused_storage_opcode),
        .storage_index(unused_storage_index), .storage_argument(unused_storage_argument),
        .storage_response_valid(1'b0), .storage_response_error(1'b0), .storage_response_data(16'd0),
        .execution_accepted(unused_execution_accepted)
    );
endmodule
