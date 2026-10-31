// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_computer.vh"

// The fes.computer 1.1 storage interfaces (mister-packages
// docs/computer-io.md): the ATA hard disk's control (fes.media.ata-disk), the
// persistence words of the fes.pc386.machine layout and the wall clock.
// fes_computer_mailbox_core hands over each request of their opcodes, having
// rejected those whose interface is absent, and waits for this endpoint's
// reply: response_valid for one clock, once per request. exec_reset is the
// mailbox's held execution; execution_accepted pulses for every accepted
// Execution request, after which exec_reset is the new state.
//
// ATA disk, unit 0. The machine owns the image in the DDR window, the CRC pass
// and the dirty map; this endpoint owns the unit's state, the staged DiskCommit
// words, the committed geometry, the fence and Saved, the dirty and CRC error
// flags and the change epoch.
//   - Commit makes the unit verifying with disk_sectors and disk_crc; the
//     machine reads that many sectors from the window and answers with
//     disk_verify_done and disk_verify_match. The reply comes first.
//   - disk_frozen is the fence: the machine holds back new guest sector
//     writes. Freeze replies once disk_fenced shows that it does and that no
//     write it accepted is still on its way to the image (disk_busy).
//   - disk_written pulses as a guest sector write reaches the image, with its
//     dirty mark; it sets the dirty flag and advances the epoch.
//   - disk_clear pulses for Prepare, Eject and Saved, once the writer is idle;
//     the machine clears its map while disk_clearing, and the reply waits.
//   - DiskDirty replies with disk_dirty_word, three clocks after
//     disk_dirty_index names its word.
// Persistence. Words 0..7, the settings, are registers (machine_settings,
// word n in bits 16n+15:16n). Words 8..71 hold CMOS 00h..7Fh, two bytes a
// word, low byte first, in a RAM the machine writes a byte at a time
// (cmos_write) and reads while cmos_load is high (the word at cmos_load_pair
// a clock later). A 128-word buffer holds the Freeze snapshot while released
// and the restore staging while held: Freeze copies the 72 words into it, and
// Commit copies the staged CMOS words back while execution is held. A copy
// that meets cmos_load starts again.
// Wall clock. Words 0..2 are staged; word 3 sets the wall_* outputs from all
// four and pulses wall_apply.
module fes_computer_storage #(
    parameter bit ENABLE_ATA_DISK = 0,
    parameter bit ENABLE_DATA = 0,
    parameter bit ENABLE_WALL_CLOCK = 0
) (
    input  wire         clk,
    input  wire         request,
    input  wire [6:0]   opcode,
    input  wire [7:0]   index,
    input  wire [15:0]  argument,
    output reg          response_valid,
    output reg          response_error,
    output reg  [15:0]  response_data,
    input  wire         exec_reset,
    input  wire         execution_accepted,
    // ATA disk
    output reg  [2:0]   disk_state,
    output reg  [31:0]  disk_sectors,
    output reg  [15:0]  disk_cylinders,
    output reg  [4:0]   disk_heads,
    output reg  [5:0]   disk_spt,
    output reg  [31:0]  disk_crc,
    input  wire         disk_verify_done,
    input  wire         disk_verify_match,
    output reg          disk_frozen,
    input  wire         disk_fenced,
    input  wire         disk_busy,
    input  wire         disk_written,
    output reg          disk_clear,
    input  wire         disk_clearing,
    output wire [6:0]   disk_dirty_index,
    input  wire [15:0]  disk_dirty_word,
    // Persistence words
    output reg  [127:0] machine_settings,
    input  wire         cmos_write,
    input  wire [6:0]   cmos_byte_index,
    input  wire [7:0]   cmos_byte,
    input  wire         cmos_load,
    input  wire [5:0]   cmos_load_pair,
    output wire [15:0]  cmos_load_word,
    // Wall clock
    output reg          wall_apply,
    output reg  [15:0]  wall_year,
    output reg  [7:0]   wall_month,
    output reg  [7:0]   wall_day,
    output reg  [7:0]   wall_hour,
    output reg  [7:0]   wall_minute,
    output reg  [7:0]   wall_second,
    output reg  [7:0]   wall_weekday
);
    localparam [15:0] E_INDEX = 16'(`FES_COMPUTER_ERROR_INVALID_INDEX);
    localparam [15:0] E_ARGUMENT = 16'(`FES_COMPUTER_ERROR_INVALID_ARGUMENT);
    localparam [15:0] E_STATE = 16'(`FES_COMPUTER_ERROR_INVALID_STATE);
    localparam [2:0] EMPTY = 3'(`FES_COMPUTER_MEDIA_STATE_EMPTY);
    localparam [2:0] LOADING = 3'(`FES_COMPUTER_MEDIA_STATE_LOADING);
    localparam [2:0] READY = 3'(`FES_COMPUTER_MEDIA_STATE_READY);
    localparam [2:0] VERIFYING = 3'(`FES_COMPUTER_DISK_STATE_VERIFYING);
    localparam [31:0] MIN_BYTES = `FES_COMPUTER_ATA_DISK_MIN_BYTES;
    localparam [31:0] MAX_BYTES = `FES_COMPUTER_ATA_DISK_MAX_BYTES;
    localparam [31:0] MIN_SECTORS = `FES_COMPUTER_ATA_DISK_MIN_SECTORS;
    localparam [31:0] MAX_SECTORS = `FES_COMPUTER_ATA_DISK_MAX_SECTORS;
    localparam [7:0] WORDS = 8'(`FES_COMPUTER_PC386_MACHINE_WORD_COUNT);
    localparam [15:0] SB_IRQ_MASK = 16'(`FES_COMPUTER_PC386_SB_IRQ_MASK);
    localparam [127:0] DEFAULTS = {16'd0, 16'(`FES_COMPUTER_PC386_SB_DMA),
        16'(`FES_COMPUTER_PC386_SB_IRQ_DEFAULT), 16'(`FES_COMPUTER_PC386_SB_BASE220), 64'd0};

    // A deferred reply, and what it waits for.
    localparam [3:0] W_NONE = 4'd0, W_DRAIN = 4'd1, W_CLEAR = 4'd2, W_FENCE = 4'd3, W_DIRTY = 4'd4,
                     W_CHS = 4'd5, W_SNAPSHOT = 4'd6, W_RESTORE = 4'd7, W_READ = 4'd8;
    reg [3:0] waiting;
    reg [6:0] step;
    reg replied;  // this request has had its reply
    wire fresh = request && !replied && waiting == W_NONE;

    task reply;
        input error;
        input [15:0] data;
        begin
            response_valid <= 1'b1;
            response_error <= error;
            response_data <= data;
            replied <= 1'b1;
            waiting <= W_NONE;
        end
    endtask

    // ---- ATA disk
    reg frozen_saved, disk_dirty, disk_error;
    reg [15:0] disk_epoch;
    reg [3:0] staged_count;  // DiskCommit words staged, 0..8
    reg [15:0] staged_size_lo, staged_size_hi, staged_cylinders, staged_heads, staged_spt, staged_crc_lo;
    reg [24:0] chs;
    wire [31:0] staged_size = {argument, staged_size_lo};
    wire [31:0] staged_sectors = {staged_size_hi, staged_size_lo};
    wire [15:0] flags = {12'd0, disk_error, disk_busy, disk_frozen, disk_dirty};
    assign disk_dirty_index = index[6:0];

    // ---- Persistence
    // The CMOS words: one write port (machine bytes, or Commit's copy) and one
    // registered read port (the machine's load, or Freeze's copy).
    (* ramstyle = "M10K" *) reg [15:0] cmos_words [0:63];
    reg [15:0] cmos_q;
    reg [15:0] buffer_q;
    // Commit's copy writes word n a clock after its buffer read, at step n+1.
    wire cmos_we_copy = waiting == W_RESTORE && step != 7'd0;
    wire [5:0] cmos_waddr_copy = 6'(step - 7'd1);
    wire [1:0] cmos_lanes = cmos_we_copy ? 2'b11 : cmos_write ? (cmos_byte_index[0] ? 2'b10 : 2'b01) : 2'b00;
    wire [5:0] cmos_waddr = cmos_we_copy ? cmos_waddr_copy : cmos_byte_index[6:1];
    wire [15:0] cmos_wdata = cmos_we_copy ? buffer_q : {2{cmos_byte}};
    wire [5:0] cmos_raddr = cmos_load ? cmos_load_pair : 6'(step - 7'd7);
    integer lane;
    always @(posedge clk) begin
        for (lane = 0; lane < 2; lane = lane + 1)
            if (cmos_lanes[lane]) cmos_words[cmos_waddr][lane * 8 +: 8] <= cmos_wdata[lane * 8 +: 8];
        cmos_q <= cmos_words[cmos_raddr];
    end
    assign cmos_load_word = cmos_q;

    // Snapshot or staging: Freeze's copy and DataWrite write it, DataRead and
    // Commit's copy read it.
    (* ramstyle = "M10K" *) reg [15:0] buffer [0:127];
    reg buffer_we;
    reg [6:0] buffer_waddr;
    reg [15:0] buffer_wdata;
    wire [6:0] buffer_raddr = waiting == W_RESTORE ? 7'(step + 7'd8) : index[6:0];
    always @(posedge clk) begin
        if (buffer_we) buffer[buffer_waddr] <= buffer_wdata;
        buffer_q <= buffer[buffer_raddr];
    end

    reg snapshot, staging;
    reg [71:0] staged;
    reg [127:0] staged_settings;
    // M10K leaves undefined a read of the word being written: when the
    // machine writes the CMOS word Freeze's copy is reading, the copy starts
    // again, as it does when the machine takes the read port.
    reg copy_restart;
    wire [15:0] w0 = staged_settings[15:0], w1 = staged_settings[31:16], w2 = staged_settings[47:32],
                w3 = staged_settings[63:48], w4 = staged_settings[79:64], w5 = staged_settings[95:80],
                w6 = staged_settings[111:96], w7 = staged_settings[127:112];
    wire settings_valid =
        w0 < 16'(`FES_COMPUTER_PC386_PRESET_COUNT) && w2 <= 16'd1 && w3 <= 16'd1 && w7 == 16'd0 &&
        (w1 == 16'd0 || (w0 != 16'd0 && w1 >= 16'(`FES_COMPUTER_PC386_FREQUENCY_MIN) &&
                         w1 <= 16'(`FES_COMPUTER_PC386_FREQUENCY_MAX))) &&
        (w4 == 16'(`FES_COMPUTER_PC386_SB_BASE220) || w4 == 16'(`FES_COMPUTER_PC386_SB_BASE240)) &&
        w5 < 16'd8 && SB_IRQ_MASK[w5[3:0]] && w6 == 16'(`FES_COMPUTER_PC386_SB_DMA);

    // ---- Wall clock
    reg [1:0] clock_next;
    reg [15:0] clock_year, clock_month_day, clock_hour_minute;
    wire [7:0] low = argument[7:0], high = argument[15:8];
    // February's length from the year's low bits: in 1980..2099 every fourth year leaps.
    function automatic [4:0] month_days(input [1:0] year, input [7:0] month);
        case (month)
            8'd2: month_days = year == 2'd0 ? 5'd29 : 5'd28;
            8'd4, 8'd6, 8'd9, 8'd11: month_days = 5'd30;
            default: month_days = 5'd31;
        endcase
    endfunction

    initial begin
        response_valid = 1'b0; response_error = 1'b0; response_data = 16'd0;
        waiting = W_NONE; step = 7'd0; replied = 1'b0;
        disk_state = EMPTY; disk_sectors = 32'd0; disk_cylinders = 16'd0; disk_heads = 5'd0; disk_spt = 6'd0;
        disk_crc = 32'd0; disk_frozen = 1'b0; disk_clear = 1'b0;
        frozen_saved = 1'b0; disk_dirty = 1'b0; disk_error = 1'b0; disk_epoch = 16'd0;
        staged_count = 4'd0; staged_size_lo = 16'd0; staged_size_hi = 16'd0; staged_cylinders = 16'd0;
        staged_heads = 16'd0; staged_spt = 16'd0; staged_crc_lo = 16'd0; chs = 25'd0;
        machine_settings = DEFAULTS; cmos_q = 16'd0;
        buffer_q = 16'd0; buffer_we = 1'b0; buffer_waddr = 7'd0; buffer_wdata = 16'd0;
        snapshot = 1'b0; staging = 1'b0; staged = 72'd0; staged_settings = DEFAULTS; copy_restart = 1'b0;
        clock_next = 2'd0; clock_year = 16'd0; clock_month_day = 16'd0; clock_hour_minute = 16'd0;
        wall_apply = 1'b0; wall_year = 16'd0; wall_month = 8'd0; wall_day = 8'd0; wall_hour = 8'd0;
        wall_minute = 8'd0; wall_second = 8'd0; wall_weekday = 8'd0;
    end

    always @(posedge clk) begin
        response_valid <= 1'b0;
        disk_clear <= 1'b0;
        wall_apply <= 1'b0;
        buffer_we <= 1'b0;
        if (!request) replied <= 1'b0;

        // The machine's side of the disk.
        if (ENABLE_ATA_DISK && disk_written) begin
            disk_dirty <= 1'b1;
            disk_epoch <= disk_epoch + 16'd1;
        end
        if (ENABLE_ATA_DISK && disk_state == VERIFYING && disk_verify_done) begin
            disk_state <= disk_verify_match ? READY : LOADING;
            disk_error <= !disk_verify_match;
            if (!disk_verify_match) begin
                disk_sectors <= 32'd0; disk_cylinders <= 16'd0; disk_heads <= 5'd0; disk_spt <= 6'd0;
            end
        end
        // Every accepted Execution request closes restore staging and drops a
        // partly staged wall clock; Hold also drops the snapshot.
        if (execution_accepted) begin
            staging <= 1'b0;
            clock_next <= 2'd0;
            if (exec_reset) snapshot <= 1'b0;
        end

        case (waiting)
            W_DRAIN: if (!disk_busy) begin
                // Prepare and Eject: the writer has landed its last sector.
                disk_clear <= 1'b1;
                step <= 7'd0;
                waiting <= W_CLEAR;
            end
            W_CLEAR: begin
                // Saved too: the reply waits for the map to clear.
                if (step != 7'd2) step <= step + 7'd1;
                else if (!disk_clearing) reply(1'b0, 16'd0);
            end
            W_FENCE: begin
                if (step != 7'd2) step <= step + 7'd1;
                else if (disk_fenced && !disk_busy) reply(1'b0, 16'd0);
            end
            W_DIRTY: begin
                if (step != 7'd3) step <= step + 7'd1;
                else reply(1'b0, disk_dirty_word);
            end
            W_CHS: begin
                // Word 4: cylinders x heads x sectors per track fit the size.
                if (chs > staged_sectors[24:0]) reply(1'b1, E_ARGUMENT);
                else begin
                    staged_spt <= argument;
                    staged_count <= 4'd5;
                    reply(1'b0, 16'd0);
                end
            end
            W_SNAPSHOT: begin
                // Words 0..7 from the settings, then word n from the CMOS
                // word read at step n-1.
                if (cmos_load || copy_restart) begin
                    copy_restart <= 1'b0;
                    step <= 7'd0;
                end else begin
                    copy_restart <= cmos_write && step >= 7'd7 && step < 7'd71 &&
                                    cmos_byte_index[6:1] == 6'(step - 7'd7);
                    buffer_we <= 1'b1;
                    buffer_waddr <= step;
                    buffer_wdata <= step < 7'd8 ? machine_settings[step[2:0] * 16 +: 16] : cmos_q;
                    if (step == 7'd71) begin
                        snapshot <= 1'b1;
                        reply(1'b0, 16'd0);
                    end else step <= step + 7'd1;
                end
            end
            W_RESTORE: begin
                // Staged words 8..71 into the CMOS words (cmos_we_copy), then
                // the settings.
                if (step == 7'd64) begin
                    machine_settings <= staged_settings;
                    staging <= 1'b0;
                    reply(1'b0, 16'd0);
                end else step <= step + 7'd1;
            end
            W_READ: begin
                if (step != 7'd1) step <= step + 7'd1;
                else reply(1'b0, buffer_q);
            end
            default: if (fresh) begin
                step <= 7'd0;
                case (opcode)
                    7'(`FES_COMPUTER_OPCODE_DISK_INFO): begin
                        if (index >= 8'd64) reply(1'b1, E_INDEX);
                        else if (argument != 16'd0) reply(1'b1, E_ARGUMENT);
                        else if (index[5:3] != 3'd0) reply(1'b0, 16'd0);
                        else case (index[2:0])
                            3'd0: reply(1'b0, MIN_BYTES[15:0]);
                            3'd1: reply(1'b0, MIN_BYTES[31:16]);
                            3'd2: reply(1'b0, MAX_BYTES[15:0]);
                            3'd3: reply(1'b0, MAX_BYTES[31:16]);
                            3'd4: reply(1'b0, 16'(`FES_COMPUTER_ATA_DISK_CHUNK_SECTORS));
                            3'd5: reply(1'b0, {13'd0, disk_state});
                            3'd6: reply(1'b0, flags);
                            default: reply(1'b0, disk_epoch);
                        endcase
                    end
                    7'(`FES_COMPUTER_OPCODE_DISK_CONTROL): begin
                        if (index != 8'd0) reply(1'b1, E_INDEX);
                        else if (argument > 16'(`FES_COMPUTER_DISK_CONTROL_SAVED)) reply(1'b1, E_ARGUMENT);
                        else case (argument[2:0])
                            3'(`FES_COMPUTER_DISK_CONTROL_PREPARE), 3'(`FES_COMPUTER_DISK_CONTROL_EJECT): begin
                                if (!exec_reset || (disk_frozen && !frozen_saved)) reply(1'b1, E_STATE);
                                else begin
                                    // The drive leaves the machine at once; the
                                    // reply waits for the writer and the map.
                                    disk_state <= argument[1] ? EMPTY : LOADING;
                                    disk_frozen <= 1'b0; frozen_saved <= 1'b0; disk_error <= 1'b0;
                                    disk_dirty <= 1'b0; staged_count <= 4'd0;
                                    disk_sectors <= 32'd0; disk_cylinders <= 16'd0; disk_heads <= 5'd0;
                                    disk_spt <= 6'd0;
                                    waiting <= W_DRAIN;
                                end
                            end
                            3'(`FES_COMPUTER_DISK_CONTROL_COMMIT): begin
                                if (!exec_reset || disk_state != LOADING || staged_count != 4'd8)
                                    reply(1'b1, E_STATE);
                                else begin
                                    disk_state <= VERIFYING;
                                    disk_error <= 1'b0;
                                    staged_count <= 4'd0;
                                    disk_sectors <= staged_sectors;
                                    disk_cylinders <= staged_cylinders;
                                    disk_heads <= staged_heads[4:0];
                                    disk_spt <= staged_spt[5:0];
                                    reply(1'b0, 16'd0);
                                end
                            end
                            3'(`FES_COMPUTER_DISK_CONTROL_FREEZE): begin
                                if (disk_state != READY) reply(1'b1, E_STATE);
                                else begin
                                    disk_frozen <= 1'b1;
                                    frozen_saved <= 1'b0;
                                    waiting <= W_FENCE;
                                end
                            end
                            3'(`FES_COMPUTER_DISK_CONTROL_RESUME): begin
                                disk_frozen <= 1'b0;
                                frozen_saved <= 1'b0;
                                reply(1'b0, 16'd0);
                            end
                            default: begin  // Saved
                                if (!disk_frozen || disk_busy) reply(1'b1, E_STATE);
                                else begin
                                    disk_dirty <= 1'b0;
                                    frozen_saved <= 1'b1;
                                    disk_clear <= 1'b1;
                                    waiting <= W_CLEAR;
                                end
                            end
                        endcase
                    end
                    7'(`FES_COMPUTER_OPCODE_DISK_COMMIT): begin
                        if (index >= 8'd8) reply(1'b1, E_INDEX);
                        else if (!exec_reset || disk_state != LOADING) reply(1'b1, E_STATE);
                        else if ({1'b0, index[2:0]} != staged_count) reply(1'b1, E_INDEX);
                        else case (index[2:0])
                            3'd0: begin staged_size_lo <= argument; staged_count <= 4'd1; reply(1'b0, 16'd0); end
                            3'd1: begin
                                if (staged_size < MIN_SECTORS || staged_size > MAX_SECTORS) reply(1'b1, E_ARGUMENT);
                                else begin
                                    staged_size_hi <= argument;
                                    staged_count <= 4'd2;
                                    reply(1'b0, 16'd0);
                                end
                            end
                            3'd2: begin
                                if (argument == 16'd0 || argument > 16'(`FES_COMPUTER_ATA_DISK_MAX_CYLINDERS))
                                    reply(1'b1, E_ARGUMENT);
                                else begin staged_cylinders <= argument; staged_count <= 4'd3; reply(1'b0, 16'd0); end
                            end
                            3'd3: begin
                                if (argument == 16'd0 || argument > 16'(`FES_COMPUTER_ATA_DISK_MAX_HEADS))
                                    reply(1'b1, E_ARGUMENT);
                                else begin staged_heads <= argument; staged_count <= 4'd4; reply(1'b0, 16'd0); end
                            end
                            3'd4: begin
                                if (argument == 16'd0 || argument > 16'(`FES_COMPUTER_ATA_DISK_MAX_SECTORS_PER_TRACK))
                                    reply(1'b1, E_ARGUMENT);
                                else begin
                                    chs <= 25'(staged_cylinders[13:0] * staged_heads[4:0]) * 25'(argument[5:0]);
                                    waiting <= W_CHS;
                                end
                            end
                            3'd5: begin staged_crc_lo <= argument; staged_count <= 4'd6; reply(1'b0, 16'd0); end
                            3'd6: begin
                                disk_crc <= {argument, staged_crc_lo};
                                staged_count <= 4'd7;
                                reply(1'b0, 16'd0);
                            end
                            default: begin
                                // Word 7, reserved; Commit takes all eight.
                                if (argument != 16'd0) reply(1'b1, E_ARGUMENT);
                                else begin staged_count <= 4'd8; reply(1'b0, 16'd0); end
                            end
                        endcase
                    end
                    7'(`FES_COMPUTER_OPCODE_DISK_DIRTY): begin
                        if (index >= 8'd128) reply(1'b1, E_INDEX);
                        else if (argument != 16'd0) reply(1'b1, E_ARGUMENT);
                        else if (!disk_frozen) reply(1'b1, E_STATE);
                        else waiting <= W_DIRTY;
                    end
                    7'(`FES_COMPUTER_OPCODE_DATA_CONTROL): begin
                        if (index != 8'd0) reply(1'b1, E_INDEX);
                        else case (argument)
                            16'(`FES_COMPUTER_DATA_FREEZE): begin
                                if (exec_reset) reply(1'b1, E_STATE);
                                else if (snapshot) reply(1'b0, 16'd0);
                                else begin
                                    copy_restart <= 1'b0;
                                    waiting <= W_SNAPSHOT;
                                end
                            end
                            16'(`FES_COMPUTER_DATA_BEGIN): begin
                                if (!exec_reset) reply(1'b1, E_STATE);
                                else begin
                                    staging <= 1'b1;
                                    staged <= 72'd0;
                                    reply(1'b0, 16'd0);
                                end
                            end
                            16'(`FES_COMPUTER_DATA_COMMIT): begin
                                if (!exec_reset || !staging || staged != {72{1'b1}}) reply(1'b1, E_STATE);
                                else if (!settings_valid) reply(1'b1, E_ARGUMENT);
                                else waiting <= W_RESTORE;
                            end
                            16'(`FES_COMPUTER_DATA_RESUME): begin
                                if (!snapshot) reply(1'b1, E_STATE);
                                else begin
                                    snapshot <= 1'b0;
                                    reply(1'b0, 16'd0);
                                end
                            end
                            default: reply(1'b1, E_ARGUMENT);
                        endcase
                    end
                    7'(`FES_COMPUTER_OPCODE_DATA_READ): begin
                        if (index >= WORDS) reply(1'b1, E_INDEX);
                        else if (argument != 16'd0) reply(1'b1, E_ARGUMENT);
                        else if (!snapshot) reply(1'b1, E_STATE);
                        else waiting <= W_READ;
                    end
                    7'(`FES_COMPUTER_OPCODE_DATA_WRITE): begin
                        if (index >= WORDS) reply(1'b1, E_INDEX);
                        else if (!exec_reset || !staging) reply(1'b1, E_STATE);
                        else begin
                            buffer_we <= 1'b1;
                            buffer_waddr <= index[6:0];
                            buffer_wdata <= argument;
                            staged[index[6:0]] <= 1'b1;
                            if (index < 8'd8) staged_settings[index[2:0] * 16 +: 16] <= argument;
                            reply(1'b0, 16'd0);
                        end
                    end
                    7'(`FES_COMPUTER_OPCODE_DATA_INFO): begin
                        if (index > 8'd3) reply(1'b1, E_INDEX);
                        else if (argument != 16'd0) reply(1'b1, E_ARGUMENT);
                        else case (index[1:0])
                            2'd0: reply(1'b0, {8'd0, WORDS});
                            2'd1: reply(1'b0, 16'(`FES_COMPUTER_PC386_MACHINE_TAG));
                            2'd2: reply(1'b0, 16'd1);
                            default: reply(1'b0, 16'd0);
                        endcase
                    end
                    7'(`FES_COMPUTER_OPCODE_WALL_CLOCK): begin
                        if (index > 8'd3) reply(1'b1, E_INDEX);
                        else if (!exec_reset) reply(1'b1, E_STATE);
                        else if (index != 8'd0 && index[1:0] != clock_next) reply(1'b1, E_INDEX);
                        else case (index[1:0])
                            2'd0: begin
                                if (argument < 16'(`FES_COMPUTER_WALL_CLOCK_MIN_YEAR) ||
                                    argument > 16'(`FES_COMPUTER_WALL_CLOCK_MAX_YEAR)) reply(1'b1, E_ARGUMENT);
                                else begin clock_year <= argument; clock_next <= 2'd1; reply(1'b0, 16'd0); end
                            end
                            2'd1: begin
                                if (low < 8'd1 || low > 8'd12 || high < 8'd1 ||
                                    high > {3'd0, month_days(clock_year[1:0], low)}) reply(1'b1, E_ARGUMENT);
                                else begin clock_month_day <= argument; clock_next <= 2'd2; reply(1'b0, 16'd0); end
                            end
                            2'd2: begin
                                if (low > 8'd23 || high > 8'd59) reply(1'b1, E_ARGUMENT);
                                else begin clock_hour_minute <= argument; clock_next <= 2'd3; reply(1'b0, 16'd0); end
                            end
                            default: begin
                                if (low > 8'd59 || high < 8'd1 || high > 8'd7) reply(1'b1, E_ARGUMENT);
                                else begin
                                    wall_year <= clock_year;
                                    {wall_day, wall_month} <= clock_month_day;
                                    {wall_minute, wall_hour} <= clock_hour_minute;
                                    {wall_weekday, wall_second} <= argument;
                                    wall_apply <= 1'b1;
                                    clock_next <= 2'd0;
                                    reply(1'b0, 16'd0);
                                end
                            end
                        endcase
                    end
                    default: reply(1'b1, 16'(`FES_COMPUTER_ERROR_INVALID_OPCODE));
                endcase
            end
        endcase
    end
    wire unused = ^{staged_heads[15:5], staged_spt[15:6], ENABLE_DATA, ENABLE_WALL_CLOCK};
endmodule
