// SPDX-License-Identifier: GPL-2.0-or-later
// Pattern test for one fes.memory.hps-ddr port. Each pattern writes the
// port's span, then reads it back and compares every bit. Every 64-bit lane
// at byte address a holds a value of a alone, so the final ADDR pattern
// ({~a, a}, little-endian) can be checked from Linux through /dev/mem.
//
//   0 ZERO  all zeros                       burst 128
//   1 ONES  all ones                        burst 64
//   2 CHCK  5555/AAAA by lane               single beats
//   3 WALK  one bit, a[8:3] selects it      burst 16
//   4 INVR  {a, ~a}                         burst 64
//   5 BYTE  ADDR bytes written into INVR, one byte enable per lane  burst 32
//   6 ADDR  {~a, a}                         burst 128
//
// A data mismatch is counted and the scan continues; bad accumulates the
// failing bit positions. No progress for TIMEOUT cycles is NACK and ends the
// scan. stop ends it after the current burst. The last write and read
// passes report MB/s (decimal digits) for this port.
module ddr_channel #(
    parameter integer DATA_W = 64,
    parameter integer ADDR_W = 29,
    parameter [31:0] BASE = 32'h30000000,
    parameter [31:0] BYTES = 32'h04000000,
    parameter integer MHZ = 100,
    parameter [23:0] TIMEOUT = 24'd1000000
) (
    input  wire              clk,
    input  wire              reset,
    input  wire              stop,
    // Avalon-MM master.
    output reg  [ADDR_W-1:0] address,
    output reg  [7:0]        burstcount,
    input  wire              waitrequest,
    input  wire [DATA_W-1:0] readdata,
    input  wire              readdatavalid,
    output reg               read,
    output reg  [DATA_W-1:0] writedata,
    output reg  [DATA_W/8-1:0] byteenable,
    output reg               write,
    // Status for the display.
    output reg  [2:0]        phase,
    output reg               reading,
    output reg  [31:0]       shown_addr,
    output reg  [31:0]       errors,
    output reg  [31:0]       fault_addr,
    output reg  [31:0]       last_addr,
    output reg  [2:0]        fault_phase,
    output reg  [DATA_W-1:0] bad,
    output reg               done,
    output reg               nack,
    output reg               stopped,
    output wire [15:0]       write_rate,
    output wire [15:0]       read_rate
);
    localparam integer LANES = DATA_W / 64;
    localparam integer STEP = DATA_W / 8;
    localparam integer SHIFT = DATA_W == 128 ? 4 : 3;
    localparam [2:0] PHASES = 3'd7;
    localparam [2:0] P_BYTE = 3'd5;
    localparam [31:0] LAST = BASE + BYTES - STEP;
    // Read beats in flight. The controller holds 14 transactions itself.
    localparam [11:0] READ_LIMIT = 12'd512;

    localparam [2:0] ST_WRITE = 3'd0;
    localparam [2:0] ST_READ = 3'd1;
    localparam [2:0] ST_DRAIN = 3'd2;
    localparam [2:0] ST_DONE = 3'd3;
    localparam [2:0] ST_STOP = 3'd4;
    localparam [2:0] ST_NACK = 3'd5;

    function [7:0] burst_of;
        input [2:0] which;
        case (which)
            3'd0: burst_of = 8'd128;
            3'd1: burst_of = 8'd64;
            3'd2: burst_of = 8'd1;
            3'd3: burst_of = 8'd16;
            3'd4: burst_of = 8'd64;
            3'd5: burst_of = 8'd32;
            default: burst_of = 8'd128;
        endcase
    endfunction

    // One 64-bit lane at byte address a.
    function [63:0] lane_value;
        input [2:0] which;
        input [31:0] a;
        input readback;
        reg [63:0] addr_word;
        reg [63:0] invr_word;
        integer b;
        begin
            addr_word = {~a, a};
            invr_word = {a, ~a};
            case (which)
                3'd0: lane_value = 64'd0;
                3'd1: lane_value = {64{1'b1}};
                3'd2: lane_value = a[3] ? {32{2'b10}} : {32{2'b01}};
                3'd3: lane_value = 64'd1 << a[8:3];
                3'd4: lane_value = invr_word;
                3'd5: begin
                    lane_value = readback ? invr_word : addr_word;
                    if (readback)
                        for (b = 0; b < 8; b = b + 1)
                            if (a[5:3] == b[2:0])
                                lane_value[b*8 +: 8] = addr_word[b*8 +: 8];
                end
                default: lane_value = addr_word;
            endcase
        end
    endfunction

    function [DATA_W-1:0] word_value;
        input [2:0] which;
        input [31:0] a;
        input readback;
        integer lane;
        begin
            for (lane = 0; lane < LANES; lane = lane + 1)
                word_value[lane*64 +: 64] = lane_value(which, a | (lane * 8), readback);
        end
    endfunction

    function [DATA_W/8-1:0] word_enables;
        input [2:0] which;
        input [31:0] a;
        integer lane;
        begin
            for (lane = 0; lane < LANES; lane = lane + 1)
                word_enables[lane*8 +: 8] = which == P_BYTE ? (8'd1 << (a[5:3] + lane[2:0])) : 8'hFF;
        end
    endfunction

    reg [2:0]  state = ST_DONE;
    reg [31:0] beat_addr;     // byte address of the beat being written
    reg [31:0] next_addr;     // beat_addr + STEP
    reg [7:0]  beats_left;    // write beats left in this burst, this one included
    reg [31:0] cmd_addr;      // byte address of the next read command
    reg        cmds_done;     // every read command of the pass has been taken
    reg [11:0] in_flight;     // read beats requested and not yet returned
    reg [31:0] read_addr;     // byte address of the next returned beat
    reg [31:0] read_next;
    reg [DATA_W-1:0] expected;
    reg [23:0] idle;
    reg [31:0] pass_cycles;
    reg [31:0] write_cycles, read_cycles;
    reg        write_pass_end, read_pass_end;
    reg [2:0]  drain;

    // Compare pipeline: difference, grouped OR, then count.
    reg              diff_valid = 1'b0;
    reg [DATA_W-1:0] diff;
    reg [31:0]       diff_addr;
    reg              group_valid = 1'b0;
    reg [DATA_W/8-1:0] group_bad;
    reg [DATA_W-1:0] group_diff;
    reg [31:0]       group_addr;

    wire [7:0] burst = burst_of(phase);
    wire [31:0] burst_bytes = {24'd0, burst} << SHIFT;
    wire write_taken = write & ~waitrequest;
    wire read_taken = read & ~waitrequest;
    wire [11:0] in_flight_after = in_flight + (read_taken ? {4'd0, burstcount} : 12'd0) -
        (readdatavalid ? 12'd1 : 12'd0);
    wire room = in_flight_after + {4'd0, burst} <= READ_LIMIT;
    wire waiting = write | read | (in_flight != 12'd0);
    wire progress = write_taken | read_taken | readdatavalid;

    integer g;
    always @(posedge clk) begin
        write_pass_end <= 1'b0;
        read_pass_end <= 1'b0;
        if (reset) begin
            state <= ST_WRITE;
            phase <= 3'd0;
            reading <= 1'b0;
            write <= 1'b1;
            read <= 1'b0;
            address <= BASE[31:SHIFT];
            burstcount <= burst_of(3'd0);
            beat_addr <= BASE;
            next_addr <= BASE + STEP;
            beats_left <= burst_of(3'd0);
            writedata <= word_value(3'd0, BASE, 1'b0);
            byteenable <= word_enables(3'd0, BASE);
            shown_addr <= BASE;
            errors <= 32'd0;
            fault_addr <= 32'd0;
            last_addr <= 32'd0;
            fault_phase <= 3'd0;
            bad <= {DATA_W{1'b0}};
            done <= 1'b0;
            nack <= 1'b0;
            stopped <= 1'b0;
            in_flight <= 12'd0;
            idle <= 24'd0;
            pass_cycles <= 32'd0;
            diff_valid <= 1'b0;
            group_valid <= 1'b0;
        end else begin
            in_flight <= in_flight_after;
            pass_cycles <= pass_cycles + 32'd1;
            if (progress || !waiting)
                idle <= 24'd0;
            else if (idle != TIMEOUT)
                idle <= idle + 24'd1;

            // Returned beats: compare against the expected value, then
            // reduce the difference in groups of eight bits.
            diff_valid <= readdatavalid;
            if (readdatavalid) begin
                diff <= readdata ^ expected;
                diff_addr <= read_addr;
                read_addr <= read_next;
                read_next <= read_next + STEP;
                expected <= word_value(phase, read_next, 1'b1);
            end
            group_valid <= diff_valid;
            group_diff <= diff;
            group_addr <= diff_addr;
            for (g = 0; g < DATA_W / 8; g = g + 1)
                group_bad[g] <= |diff[g*8 +: 8];
            if (group_valid && group_bad != {DATA_W/8{1'b0}}) begin
                bad <= bad | group_diff;
                if (errors != 32'hFFFFFFFF)
                    errors <= errors + 32'd1;
                if (errors == 32'd0) begin
                    fault_addr <= group_addr;
                    fault_phase <= phase;
                end
                last_addr <= group_addr;
            end

            case (state)
                ST_WRITE: begin
                    if (idle == TIMEOUT) begin
                        write <= 1'b0;
                        nack <= 1'b1;
                        state <= ST_NACK;
                    end else if (write_taken) begin
                        beat_addr <= next_addr;
                        next_addr <= next_addr + STEP;
                        writedata <= word_value(phase, next_addr, 1'b0);
                        byteenable <= word_enables(phase, next_addr);
                        if (beats_left != 8'd1) begin
                            beats_left <= beats_left - 8'd1;
                        end else if (beat_addr == LAST) begin
                            // Pass written: read it back from the start.
                            write <= 1'b0;
                            write_pass_end <= 1'b1;
                            write_cycles <= pass_cycles;
                            reading <= 1'b1;
                            state <= ST_READ;
                            read <= 1'b1;
                            address <= BASE[31:SHIFT];
                            burstcount <= burst;
                            cmd_addr <= BASE + burst_bytes;
                            cmds_done <= 1'b0;
                            read_addr <= BASE;
                            read_next <= BASE + STEP;
                            expected <= word_value(phase, BASE, 1'b1);
                            shown_addr <= BASE;
                            pass_cycles <= 32'd0;
                        end else if (stop) begin
                            write <= 1'b0;
                            stopped <= 1'b1;
                            state <= ST_STOP;
                        end else begin
                            address <= next_addr[31:SHIFT];
                            burstcount <= burst;
                            beats_left <= burst;
                            shown_addr <= next_addr;
                        end
                    end
                end
                ST_READ: begin
                    if (idle == TIMEOUT) begin
                        read <= 1'b0;
                        nack <= 1'b1;
                        state <= ST_NACK;
                    end else begin
                        if (read_taken) begin
                            shown_addr <= cmd_addr;
                            if (cmd_addr == BASE + BYTES || stop) begin
                                read <= 1'b0;
                                cmds_done <= 1'b1;
                            end else begin
                                address <= cmd_addr[31:SHIFT];
                                cmd_addr <= cmd_addr + burst_bytes;
                                read <= room;
                            end
                        end else if (!read && !cmds_done) begin
                            if (stop)
                                cmds_done <= 1'b1;
                            else if (room)
                                read <= 1'b1;
                        end
                        if (cmds_done && in_flight_after == 12'd0 && !readdatavalid) begin
                            drain <= 3'd4;
                            state <= ST_DRAIN;
                        end
                    end
                end
                ST_DRAIN: begin
                    // Let the compare pipeline count the last beats.
                    if (drain != 3'd0) begin
                        drain <= drain - 3'd1;
                    end else if (stop || stopped) begin
                        stopped <= 1'b1;
                        state <= ST_STOP;
                    end else begin
                        read_pass_end <= 1'b1;
                        read_cycles <= pass_cycles;
                        reading <= 1'b0;
                        if (phase == PHASES - 3'd1) begin
                            done <= 1'b1;
                            state <= ST_DONE;
                        end else begin
                            phase <= phase + 3'd1;
                            state <= ST_WRITE;
                            write <= 1'b1;
                            address <= BASE[31:SHIFT];
                            burstcount <= burst_of(phase + 3'd1);
                            beats_left <= burst_of(phase + 3'd1);
                            beat_addr <= BASE;
                            next_addr <= BASE + STEP;
                            writedata <= word_value(phase + 3'd1, BASE, 1'b0);
                            byteenable <= word_enables(phase + 3'd1, BASE);
                            shown_addr <= BASE;
                            pass_cycles <= 32'd0;
                        end
                    end
                end
                default: begin
                    write <= 1'b0;
                    read <= 1'b0;
                    if (stop && state == ST_DONE)
                        stopped <= 1'b1;
                end
            endcase
            // A stop seen mid-read ends the pass once its data is back.
            if (state == ST_READ && stop)
                stopped <= 1'b1;
        end
    end

    ddr_rate #(.NUMERATOR({8'd0, BYTES} * MHZ)) write_speed (
        .clk(clk), .start(write_pass_end), .cycles(write_cycles), .digits(write_rate)
    );
    ddr_rate #(.NUMERATOR({8'd0, BYTES} * MHZ)) read_speed (
        .clk(clk), .start(read_pass_end), .cycles(read_cycles), .digits(read_rate)
    );
endmodule
