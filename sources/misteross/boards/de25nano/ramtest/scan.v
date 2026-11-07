// SPDX-License-Identifier: GPL-2.0-or-later
// One outstanding Avalon burst, checked write responses, and fail-closed drain.
// A timeout records a fault; it never cancels an accepted memory transaction.
module fes_de25_ram_scan #(
    parameter integer DATA_W = 128,
    parameter integer MAX_BURST = 16,
    parameter integer TIMEOUT = 1000000,
    parameter integer LATENCY_SAMPLES = 1024
) (
    input wire clk, reset_n, start, stop, clear, calibrated,
    input wire [31:0] base, length,
    output wire busy,
    output reg done = 0,
    output reg stopped = 0,
    output reg [31:0] faults = 0,
    output reg [2:0] pattern = 0,
    output reg [31:0] cursor = 0,
    output reg [31:0] errors = 0,
    output reg [31:0] first_addr = 0,
    output reg [DATA_W-1:0] first_expected = 0, first_actual = 0, bad_bits = 0,
    output reg [63:0] written_bytes = 0, read_bytes = 0,
    output reg [63:0] write_cycles = 0, read_cycles = 0,
    output reg [31:0] samples = 0,
    output reg [31:0] first_min = 32'hffffffff, first_max = 0,
    output reg [31:0] last_min = 32'hffffffff, last_max = 0,
    output reg [63:0] first_sum = 0, last_sum = 0,
    output wire [31:0] address,
    output wire [4:0] burstcount,
    output wire read, write,
    output wire [DATA_W-1:0] writedata,
    output wire [DATA_W/8-1:0] byteenable,
    input wire waitrequest, readdatavalid, writeresponsevalid,
    input wire [DATA_W-1:0] readdata,
    input wire [1:0] response
);
    localparam integer STEP = DATA_W / 8;
    localparam integer SHIFT = $clog2(STEP);
    localparam [3:0] IDLE=0, WRITE_SETUP=1, WRITE_DATA=2, WRITE_ACK=3,
        READ_COMMAND=4, READ_DATA=5, LAT_COMMAND=6, LAT_DATA=7;
    localparam [31:0] F_TIMEOUT=1, F_RESPONSE=2, F_PROTOCOL=4,
        F_CALIBRATION=8, F_ARGUMENT=16, F_RESET=32;
    reg [3:0] state = IDLE;
    reg [31:0] active_base = 0, active_length = 0;
    reg [4:0] beats = 1, beat = 0;
    reg cancel = 0;
    reg [31:0] watchdog = 0, latency = 0, random_word = 32'hd25a8101;
    reg first_seen = 0, healthy = 0;
    wire reset_fault = first_seen && !healthy;
    wire running = state != IDLE;
    wire [31:0] beat_address = cursor + ({{27{1'b0}}, beat} << SHIFT);
    assign busy = running;
    assign address = cursor;
    assign burstcount = beats;
    assign read = reset_n && !reset_fault && (state == READ_COMMAND || state == LAT_COMMAND);
    assign write = reset_n && !reset_fault && state == WRITE_DATA;

    always @(posedge clk) if (reset_n) first_seen <= 1;
    always @(posedge clk or negedge reset_n)
        if (!reset_n) healthy <= 0;
        else if (!first_seen) healthy <= 1;

    function [DATA_W-1:0] value;
        input [2:0] p;
        input [31:0] a;
        input expected;
        integer i;
        reg [31:0] lane;
        reg [63:0] word_value;
        reg [7:0] ordinary, inverted;
        begin
            value = 0;
            for (i=0; i<STEP; i=i+1) begin
                lane = (a & 32'hfffffff8) + ((i / 8) * 8);
                word_value = {~lane, lane};
                if (DATA_W == 16) ordinary = (a ^ (a >> 16)) >> (i*8);
                else ordinary = word_value[(i % 8)*8 +: 8];
                inverted = ~ordinary;
                case (p)
                    0: value[i*8 +: 8] = 0;
                    1: value[i*8 +: 8] = 8'hff;
                    2: value[i*8 +: 8] = lane[3] ? 8'haa : 8'h55;
                    3: value[i*8 +: 8] = (64'd1 << lane[8:3]) >> ((i % 8)*8);
                    4: value[i*8 +: 8] = ordinary;
                    5: value[i*8 +: 8] = inverted;
                    default: value[i*8 +: 8] = expected && i != ((a >> SHIFT) & (STEP-1)) ? inverted : ordinary;
                endcase
            end
        end
    endfunction
    function [STEP-1:0] enables;
        input [2:0] p;
        input [31:0] a;
        integer i;
        begin
            enables = {STEP{1'b1}};
            if (p == 6) begin
                enables = 0;
                for (i=0; i<STEP; i=i+1)
                    enables[i] = i == ((a >> SHIFT) & (STEP-1));
            end
        end
    endfunction
    function [4:0] burst_for;
        input [31:0] a, remaining;
        reg [31:0] n, page;
        begin
            n = remaining >> SHIFT;
            page = (32'd4096 - (a & 32'hfff)) >> SHIFT;
            if (n > MAX_BURST) n = MAX_BURST;
            if (n > page) n = page;
            burst_for = n[4:0];
        end
    endfunction
    assign writedata = value(pattern, beat_address, 0);
    assign byteenable = state == WRITE_DATA ? enables(pattern, beat_address) : {STEP{1'b1}};
    wire accepted_read = read && !waitrequest;
    wire accepted_write = write && !waitrequest;
    wire final_write = accepted_write && beat == beats-1;
    wire expected_b = state == WRITE_ACK || final_write;
    wire expected_r = state == READ_DATA || state == LAT_DATA || accepted_read;
    wire got_b = writeresponsevalid && expected_b;
    wire got_r = readdatavalid && expected_r;
    wire [DATA_W-1:0] expected_data = value(pattern, beat_address, 1);
    wire last_beat = beat == beats-1;
    wire pass_end = {1'b0,cursor} + ({{28{1'b0}},beats} << SHIFT) ==
                    {1'b0,active_base} + {1'b0,active_length};
    wire [31:0] elapsed = accepted_read ? 0 : latency + 1;
    wire progress = accepted_read || accepted_write || got_b || got_r;
    wire [31:0] fault_events =
        ((running && !calibrated) ? F_CALIBRATION : 32'd0) |
        ((running && !progress && watchdog >= TIMEOUT) ? F_TIMEOUT : 32'd0) |
        (((writeresponsevalid && !expected_b) || (readdatavalid && !expected_r)) ? F_PROTOCOL : 32'd0) |
        (((got_b || got_r) && response != 0) ? F_RESPONSE : 32'd0);
    wire aborting = cancel || stop || faults != 0 || fault_events != 0 || !calibrated;

    always @(posedge clk) begin
        if (reset_fault) begin
            faults <= faults | F_RESET;
            done <= 0;
            cancel <= 1;
        end else if (reset_n && healthy) begin
            if (stop && running) cancel <= 1;
            if (fault_events != 0) faults <= faults | fault_events;
            if (running) begin
                if (state == WRITE_SETUP || state == WRITE_DATA || state == WRITE_ACK)
                    write_cycles <= write_cycles + 1;
                else read_cycles <= read_cycles + 1;
                if (progress) watchdog <= 0;
                else if (watchdog < TIMEOUT) watchdog <= watchdog + 1;
                else cancel <= 1;
            end
            if ((writeresponsevalid && !expected_b) || (readdatavalid && !expected_r)) begin
                cancel <= 1;
            end
            case (state)
                IDLE: begin
                    watchdog <= 0;
                    // An unsolicited response destroys transaction ownership;
                    // local CLEAR cannot establish a new external epoch.
                    if (clear && (faults & (F_PROTOCOL|F_RESET)) == 0) begin
                        done <= 0; stopped <= 0; faults <= fault_events; cancel <= 0;
                        errors <= 0; bad_bits <= 0;
                    end
                    if (start) begin
                        if (!calibrated || faults != 0 || fault_events != 0 || length < 32 ||
                            (length & (length-1)) != 0 || base[4:0] != 0 ||
                            {1'b0,base}+{1'b0,length} > 33'h100000000) faults <= faults | fault_events | F_ARGUMENT;
                        else begin
                            active_base <= base; active_length <= length; cursor <= base;
                            state <= WRITE_SETUP; pattern <= 0; beat <= 0;
                            done <= 0; stopped <= 0; cancel <= 0;
                            errors <= 0; first_addr <= 0; first_expected <= 0; first_actual <= 0; bad_bits <= 0;
                            written_bytes <= 0; read_bytes <= 0; write_cycles <= 0; read_cycles <= 0;
                            samples <= 0; first_min <= 32'hffffffff; last_min <= 32'hffffffff;
                            first_max <= 0; last_max <= 0; first_sum <= 0; last_sum <= 0;
                            random_word <= 32'hd25a8101;
                        end
                    end
                end
                WRITE_SETUP: begin
                    if (aborting) begin state <= IDLE; stopped <= 1; end
                    else begin
                        beats <= burst_for(cursor, active_base+active_length-cursor);
                        beat <= 0; state <= WRITE_DATA;
                    end
                end
                WRITE_DATA: if (accepted_write) begin
                    written_bytes <= written_bytes + STEP;
                    if (last_beat) state <= WRITE_ACK;
                    else beat <= beat+1;
                end
                READ_COMMAND, LAT_COMMAND: if (accepted_read) begin
                    state <= state == LAT_COMMAND ? LAT_DATA : READ_DATA;
                    latency <= 0;
                end
                READ_DATA, LAT_DATA: latency <= latency+1;
                default: begin end
            endcase
            // B completion may coincide with acceptance of the last W beat.
            if (got_b) begin
                beat <= 0;
                if (response != 0) begin state <= IDLE; stopped <= 1; end
                else if (aborting) begin state <= IDLE; stopped <= 1; end
                else if (pass_end) begin
                    cursor <= active_base; beats <= burst_for(active_base,active_length); state <= READ_COMMAND;
                end else begin cursor <= cursor+(beats << SHIFT); state <= WRITE_SETUP; end
            end
            if (got_r) begin
                read_bytes <= read_bytes + STEP;
                if (response != 0) cancel <= 1;
                if (readdata != expected_data) begin
                    if (errors != 32'hffffffff) errors <= errors+1;
                    bad_bits <= bad_bits | (readdata ^ expected_data);
                    if (errors == 0) begin
                        first_addr <= beat_address; first_expected <= expected_data; first_actual <= readdata;
                    end
                end
                if (state == LAT_COMMAND || state == LAT_DATA) begin
                    if (beat == 0) begin
                        if (elapsed < first_min) first_min <= elapsed;
                        if (elapsed > first_max) first_max <= elapsed;
                        first_sum <= first_sum+elapsed;
                    end
                    if (last_beat) begin
                        if (elapsed < last_min) last_min <= elapsed;
                        if (elapsed > last_max) last_max <= elapsed;
                        last_sum <= last_sum+elapsed;
                        samples <= samples+1;
                    end
                end
                if (!last_beat) beat <= beat+1;
                else begin
                    beat <= 0;
                    if (aborting || response != 0) begin state <= IDLE; stopped <= 1; end
                    else if (state == LAT_COMMAND || state == LAT_DATA) begin
                        if (samples+1 == LATENCY_SAMPLES) begin state <= IDLE; done <= 1; end
                        else begin
                            random_word <= {random_word[30:0],random_word[31]^random_word[21]^random_word[1]^random_word[0]};
                            cursor <= active_base+((random_word & (active_length-1)) & 32'hffffffe0);
                            state <= LAT_COMMAND;
                        end
                    end else if (pass_end) begin
                        if (pattern == 6) begin
                            beats <= 32 / STEP; cursor <= active_base; state <= LAT_COMMAND;
                        end else begin pattern <= pattern+1; cursor <= active_base; state <= WRITE_SETUP; end
                    end else begin
                        cursor <= cursor+(beats << SHIFT);
                        beats <= burst_for(cursor+(beats << SHIFT), active_base+active_length-cursor-(beats << SHIFT));
                        state <= READ_COMMAND;
                    end
                end
            end
        end
    end
endmodule
