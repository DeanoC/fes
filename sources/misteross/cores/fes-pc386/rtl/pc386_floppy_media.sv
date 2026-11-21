// SPDX-License-Identifier: MIT
`include "fes_computer.vh"

// fes.media.pc-floppy's drives A: (unit 1) and B: (unit 2) on the mailbox's
// storage port (mister-packages docs/computer-io.md, "PC floppy"). DiskInfo,
// DiskControl and DiskCommit for those units are answered here; every other
// storage request, the ATA disk's on unit 0 included, passes through to
// fes_computer_storage unchanged.
//
// A drive's image sits in the DDR window, written by the runtime; this
// endpoint owns the unit's state, the staged DiskCommit words (indexed
// unit*8+word), the committed geometry and write-protect, the fence and
// Saved, the dirty and CRC error flags and the change epoch. Unlike the ATA
// disk every operation is valid while execution runs: the drive is absent
// from the controller (present low) while it loads or verifies.
//   - Commit makes the unit verifying; pc386_floppy_verify reads the image and
//     answers with verify_done and verify_match. The reply comes first.
//   - Prepare and Eject take the disk out at once and reply when the
//     controller holds no store (fdc_busy low), so a store it had queued
//     lands before the runtime writes the next image.
//   - The fence is shared: fdc_frozen while either drive is frozen. Freeze
//     replies once the controller reports fdc_fenced with nothing in flight.
//   - written[k] pulses as a store reaches drive k's image: the dirty flag
//     sets and the epoch advances.
module pc386_floppy_media (
    input wire clk,
    // The mailbox's storage port
    input  wire         request,
    input  wire [6:0]   opcode,
    input  wire [7:0]   index,
    input  wire [15:0]  argument,
    output wire         response_valid,
    output wire         response_error,
    output wire [15:0]  response_data,
    // fes_computer_storage, for everything else
    output wire         storage_request,
    input  wire         storage_response_valid,
    input  wire         storage_response_error,
    input  wire [15:0]  storage_response_data,
    // The drives, to pc386_fdc
    output wire [1:0] present,
    output reg  [1:0] write_protect,
    output wire [1:0] two_sided,
    output reg  [1:0][6:0] cylinders,
    output reg  [1:0][5:0] spt,
    output wire fdc_frozen,
    input  wire fdc_busy, fdc_fenced,
    input  wire [1:0] written,
    // The CRC pass, pc386_floppy_verify
    output wire [1:0] verify,
    output reg  [1:0][12:0] sectors,
    output reg  [1:0][31:0] crc,
    input  wire [1:0] verify_done,
    input  wire verify_match
);
    localparam [15:0] E_INDEX = 16'(`FES_COMPUTER_ERROR_INVALID_INDEX);
    localparam [15:0] E_ARGUMENT = 16'(`FES_COMPUTER_ERROR_INVALID_ARGUMENT);
    localparam [15:0] E_STATE = 16'(`FES_COMPUTER_ERROR_INVALID_STATE);
    localparam [2:0] EMPTY = 3'(`FES_COMPUTER_MEDIA_STATE_EMPTY);
    localparam [2:0] LOADING = 3'(`FES_COMPUTER_MEDIA_STATE_LOADING);
    localparam [2:0] READY = 3'(`FES_COMPUTER_MEDIA_STATE_READY);
    localparam [2:0] VERIFYING = 3'(`FES_COMPUTER_DISK_STATE_VERIFYING);
    localparam [31:0] MIN_BYTES = `FES_COMPUTER_PC_FLOPPY_MIN_BYTES;
    localparam [31:0] MAX_BYTES = `FES_COMPUTER_PC_FLOPPY_MAX_BYTES;
    localparam [6:0] OP_INFO = 7'(`FES_COMPUTER_OPCODE_DISK_INFO);
    localparam [6:0] OP_CONTROL = 7'(`FES_COMPUTER_OPCODE_DISK_CONTROL);
    localparam [6:0] OP_COMMIT = 7'(`FES_COMPUTER_OPCODE_DISK_COMMIT);

    // The request is this endpoint's when it is a Disk operation naming unit
    // 1 or 2; DiskDirty stays unit 0's.
    wire [7:0] unit_of = opcode == OP_CONTROL ? index : {3'd0, index[7:3]};
    wire disk_op = opcode == OP_INFO || opcode == OP_CONTROL || opcode == OP_COMMIT;
    wire mine = disk_op && (unit_of == 8'd1 || unit_of == 8'd2);
    wire k = unit_of == 8'd2;   // drive: 0 A:, 1 B:
    assign storage_request = request && !mine;

    reg local_valid, local_error;
    reg [15:0] local_data;
    assign response_valid = local_valid || storage_response_valid;
    assign response_error = local_valid ? local_error : storage_response_error;
    assign response_data = local_valid ? local_data : storage_response_data;

    // A deferred reply and what it waits for.
    localparam [1:0] W_NONE = 2'd0, W_DRAIN = 2'd1, W_FENCE = 2'd2;
    reg [1:0] waiting;
    reg replied;
    wire fresh = request && mine && !replied && waiting == W_NONE;

    reg [1:0][2:0] state;
    reg [1:0] frozen, saved, dirty, error;
    reg [1:0][15:0] epoch;
    reg [1:0][3:0] staged_count;
    reg [1:0][15:0] staged_size_lo;
    reg [1:0][1:0] staged_heads;
    assign present[0] = state[0] == READY;
    assign present[1] = state[1] == READY;
    assign verify[0] = state[0] == VERIFYING;
    assign verify[1] = state[1] == VERIFYING;
    reg [1:0] heads2;
    assign two_sided = heads2;
    assign fdc_frozen = |frozen;

    // The formats a PC controller reads: (cylinders, heads, sectors per track).
    function automatic format_ok(input [15:0] c, input [15:0] h, input [15:0] s);
        format_ok = (c == 16'd40 && h == 16'd1 && (s == 16'd8 || s == 16'd9)) ||
                    (c == 16'd40 && h == 16'd2 && (s == 16'd8 || s == 16'd9)) ||
                    (c == 16'd80 && h == 16'd2 &&
                     (s == 16'd9 || s == 16'd15 || s == 16'd18 || s == 16'd21 || s == 16'd36));
    endfunction
    reg [1:0][15:0] staged_cylinders;
    // cylinders x heads x sectors per track of the staged word 4
    wire [15:0] chs = staged_cylinders[k][6:0] * (staged_heads[k] == 2'd2 ? 16'd2 : 16'd1) * argument[5:0];

    task reply(input err, input [15:0] data);
        begin
            local_valid <= 1'b1;
            local_error <= err;
            local_data <= data;
            replied <= 1'b1;
            waiting <= W_NONE;
        end
    endtask

    initial begin
        local_valid = 0; local_error = 0; local_data = 0; waiting = W_NONE; replied = 0;
        state = {EMPTY, EMPTY}; frozen = 0; saved = 0; dirty = 0; error = 0; epoch = 0; staged_count = 0;
        staged_size_lo = 0; staged_heads = 0; staged_cylinders = 0; heads2 = 0;
        write_protect = 0; cylinders = 0; spt = 0; sectors = 0; crc = 0;
    end

    always @(posedge clk) begin
        local_valid <= 1'b0;
        if (!request) replied <= 1'b0;

        for (int d = 0; d < 2; d++) begin
            if (written[d]) begin
                dirty[d] <= 1'b1;
                epoch[d] <= epoch[d] + 16'd1;
            end
            if (state[d] == VERIFYING && verify_done[d]) begin
                state[d] <= verify_match ? READY : LOADING;
                error[d] <= !verify_match;
            end
        end

        case (waiting)
            W_DRAIN: if (!fdc_busy) reply(1'b0, 16'd0);
            W_FENCE: if (fdc_fenced && !fdc_busy) reply(1'b0, 16'd0);
            default: if (fresh) begin
                case (opcode)
                    OP_INFO: begin
                        if (argument != 16'd0) reply(1'b1, E_ARGUMENT);
                        else case (index[2:0])
                            3'd0: reply(1'b0, MIN_BYTES[15:0]);
                            3'd1: reply(1'b0, MIN_BYTES[31:16]);
                            3'd2: reply(1'b0, MAX_BYTES[15:0]);
                            3'd3: reply(1'b0, MAX_BYTES[31:16]);
                            3'd4: reply(1'b0, 16'd0);
                            3'd5: reply(1'b0, {13'd0, state[k]});
                            3'd6: reply(1'b0, {12'd0, error[k], 1'b0, frozen[k], dirty[k]});
                            default: reply(1'b0, epoch[k]);
                        endcase
                    end
                    OP_CONTROL: begin
                        if (argument > 16'(`FES_COMPUTER_DISK_CONTROL_SAVED)) reply(1'b1, E_ARGUMENT);
                        else case (argument[2:0])
                            3'(`FES_COMPUTER_DISK_CONTROL_PREPARE), 3'(`FES_COMPUTER_DISK_CONTROL_EJECT): begin
                                if (frozen[k] && !saved[k]) reply(1'b1, E_STATE);
                                else begin
                                    // The disk leaves the drive at once; the
                                    // reply waits for a store in flight.
                                    state[k] <= argument[1] ? EMPTY : LOADING;
                                    frozen[k] <= 1'b0; saved[k] <= 1'b0; error[k] <= 1'b0; dirty[k] <= 1'b0;
                                    staged_count[k] <= 4'd0;
                                    waiting <= W_DRAIN;
                                end
                            end
                            3'(`FES_COMPUTER_DISK_CONTROL_COMMIT): begin
                                if (state[k] != LOADING || staged_count[k] != 4'd8) reply(1'b1, E_STATE);
                                else begin
                                    state[k] <= VERIFYING;
                                    error[k] <= 1'b0;
                                    staged_count[k] <= 4'd0;
                                    reply(1'b0, 16'd0);
                                end
                            end
                            3'(`FES_COMPUTER_DISK_CONTROL_FREEZE): begin
                                if (state[k] != READY) reply(1'b1, E_STATE);
                                else begin
                                    frozen[k] <= 1'b1;
                                    saved[k] <= 1'b0;
                                    waiting <= W_FENCE;
                                end
                            end
                            3'(`FES_COMPUTER_DISK_CONTROL_RESUME): begin
                                frozen[k] <= 1'b0;
                                saved[k] <= 1'b0;
                                reply(1'b0, 16'd0);
                            end
                            default: begin  // Saved
                                if (!frozen[k]) reply(1'b1, E_STATE);
                                else begin
                                    dirty[k] <= 1'b0;
                                    saved[k] <= 1'b1;
                                    reply(1'b0, 16'd0);
                                end
                            end
                        endcase
                    end
                    default: begin  // DiskCommit: index = unit*8 + word
                        if (state[k] != LOADING) reply(1'b1, E_STATE);
                        else if ({1'b0, index[2:0]} != staged_count[k]) reply(1'b1, E_INDEX);
                        else case (index[2:0])
                            3'd0: begin staged_size_lo[k] <= argument; staged_count[k] <= 4'd1; reply(1'b0, 16'd0); end
                            3'd1: begin
                                // Size in sectors within 163,840..2,949,120 bytes.
                                if (argument != 16'd0 || staged_size_lo[k] < 16'(MIN_BYTES / 512) ||
                                    staged_size_lo[k] > 16'(MAX_BYTES / 512))
                                    reply(1'b1, E_ARGUMENT);
                                else begin staged_count[k] <= 4'd2; reply(1'b0, 16'd0); end
                            end
                            3'd2: begin staged_cylinders[k] <= argument; staged_count[k] <= 4'd3; reply(1'b0, 16'd0); end
                            3'd3: begin staged_heads[k] <= argument[1:0] == 2'd2 && argument[15:2] == 14'd0 ? 2'd2 :
                                                            argument == 16'd1 ? 2'd1 : 2'd0;
                                        staged_count[k] <= 4'd4; reply(1'b0, 16'd0); end
                            3'd4: begin
                                if (staged_heads[k] == 2'd0 ||
                                    !format_ok(staged_cylinders[k], {14'd0, staged_heads[k]}, argument) ||
                                    chs != staged_size_lo[k])
                                    reply(1'b1, E_ARGUMENT);
                                else begin
                                    cylinders[k] <= staged_cylinders[k][6:0];
                                    heads2[k] <= staged_heads[k] == 2'd2;
                                    spt[k] <= argument[5:0];
                                    sectors[k] <= staged_size_lo[k][12:0];
                                    staged_count[k] <= 4'd5;
                                    reply(1'b0, 16'd0);
                                end
                            end
                            3'd5: begin crc[k][15:0] <= argument; staged_count[k] <= 4'd6; reply(1'b0, 16'd0); end
                            3'd6: begin crc[k][31:16] <= argument; staged_count[k] <= 4'd7; reply(1'b0, 16'd0); end
                            default: begin
                                // Word 7: flags, only bit 0 (write-protect).
                                if (argument[15:1] != 15'd0) reply(1'b1, E_ARGUMENT);
                                else begin
                                    write_protect[k] <= argument[0];
                                    staged_count[k] <= 4'd8;
                                    reply(1'b0, 16'd0);
                                end
                            end
                        endcase
                    end
                endcase
            end
        endcase
    end
endmodule
