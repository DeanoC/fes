// SPDX-License-Identifier: MIT
// pc386 Sound Blaster 2.0 DSP, version 2.01 (docs/pc386-machine-design.md,
// "Sound Blaster 2.0 DSP"): an original model from Creative's hardware
// programming guide, with DOSBox-X's sblaster.cpp as the reference for what
// the guide leaves open.
//
// Registers, relative to the base; odd offsets alias the even one below, as
// on an SB 2.0 (base+8-9, the OPL2, are decoded by pc386_audio):
//   6  write: bit 0 = 1 holds the DSP in reset, 1 then 0 releases it and AAh
//      reaches the read buffer RESET_US later
//   A  read data; an empty buffer returns the last byte again
//   C  write command or data; read write status: AAh busy, 2Ah ready, FFh in
//      reset (the values DOSBox-X records from an SB 2.0)
//   E  read status: AAh data available, 2Ah empty; the read acknowledges IRQ
// anything else reads FFh.
//
// Commands (argument bytes in brackets, lengths as length - 1):
//   10 [d]   direct DAC             14 [lo hi] single-cycle DMA playback
//   1C       auto-init playback     48 [lo hi] block size for 1C, 2C, 90-99
//   91, 90   high-speed single-cycle and auto-init playback
//   24 [lo hi], 2C, 99, 98  recording: DMA writes 80h (silence)
//   20       direct ADC: 80h        40 [tc]    rate = 1,000,000 / (256 - tc)
//   80 [lo hi] silence for that many samples, then IRQ
//   D0, D4   halt and continue DMA  DA        exit auto-init after the block
//   D1, D3   speaker on, off        D8        speaker status: FFh on, 00h off
//   E0 [d]   returns ~d             E1        returns 02h, 01h
//   E2 [d]   DMA identification: one byte written by DMA (DOSBox-X's algorithm)
//   E4 [d], E8  write and read the test register
//   F2       IRQ 10 us later
//   ADPCM DMA playback, single-cycle [lo hi] and auto-init (48h's block):
//            4-bit 74, 75, 7D   2.6-bit 76, 77, 7F   2-bit 16, 17, 1F
//            75, 77, 17 and every auto-init form start with a reference byte
// Every other command is consumed with its argument bytes and ignored. A
// write during the 2 us write-busy interval or a high-speed transfer is
// dropped; only reset stops high-speed auto-init.
//
// DMA: each sample period raises DREQ for one byte while a transfer runs and
// is not halted; the acknowledge takes or supplies the byte and drops DREQ.
// The DSP counts its own block: the byte that ends it raises the IRQ and
// either reloads the count (auto-init) or ends the transfer. The DAC holds the
// last byte played or written; the output is silent (80h) while the speaker
// is off.
//
// ADPCM: a byte holds 2, 3 or 4 codes, high bits first (2.6-bit: 3, 3, then
// the low 2 bits doubled), decoded as DOSBox-X's decode_ADPCM_4/3/2_sample:
// each code steps an 8-bit reference by a signed delta from its magnitude and
// the step row, and moves the row. The first code plays when its byte
// arrives and the others on the following sample periods, so the card takes
// one byte per 2, 3 or 4 periods and the IRQ comes with the block's last
// byte, length + 1 bytes in. That byte's later codes still play, in its
// encoding: a transfer started meanwhile (the next block, at that IRQ) takes
// its first byte after them, so a block without a reference continues from
// them. A reference byte resets the step, goes to the DAC and holds it for
// its byte's periods (DOSBox-X plays no sample for it). Auto-init takes its reference only at
// the start. Forms without one continue the previous reference and step,
// which a DSP reset sets to 80h and 0. Creative's ADPCM rate limits (12 kHz
// for 4-bit) are not applied, as for PCM.
module pc386_sb #(
    parameter integer RESET_US = 20
) (
    input wire clk, reset,
    input wire us_tick,               // 1 MHz enable
    input wire access, write,         // access: one clock per guest byte
    input wire [3:0] address,
    input wire [7:0] wdata,
    output reg [7:0] rdata,
    output reg irq,
    // DMA channel 1
    output reg drq,
    input wire dack_valid, dack_write,
    input wire [7:0] dack_wdata,
    output reg dack_done,
    output reg [7:0] dack_rdata,
    output wire [7:0] dac             // unsigned, 80h is silence
);
    localparam [1:0] RUNNING = 2'd0, IN_RESET = 2'd1, RESET_WAIT = 2'd2;
    localparam [2:0] NONE = 3'd0, PLAY = 3'd1, RECORD = 3'd2, SILENCE = 3'd3, IDENTIFY = 3'd4;

    reg [1:0] state;
    reg [7:0] reset_us;
    reg [7:0] command, argument0;
    reg [1:0] needed, received;
    reg command_pending, execute;
    reg [7:0] out_fifo [0:3];
    reg [2:0] out_head, out_tail;      // the buffer holds out_tail - out_head bytes
    reg [7:0] out_last;
    reg [1:0] busy_us;
    reg [7:0] time_constant, test_register, dac_value;
    reg [15:0] block_length, remaining;
    reg [2:0] mode;
    reg autoinit, high_speed, halted, speaker;
    reg [8:0] period_us;
    reg [7:0] e2_add, e2_xor;
    reg [3:0] irq_us;
    reg [15:0] last_length;            // a two-byte argument, when its command executes
    localparam [1:0] PCM = 2'd0, ADPCM4 = 2'd1, ADPCM3 = 2'd2, ADPCM2 = 2'd3;
    reg [1:0] encoding;                // the transfer's encoding
    reg adpcm_ref;                     // the next byte is a reference
    reg [1:0] codes_left;              // sample periods left in the byte in hand
    reg [1:0] codes_encoding;          // ... and its encoding
    reg [7:0] adpcm_bits;              // its undecoded codes, high-aligned
    reg [7:0] adpcm_reference, adpcm_step;
    wire [2:0] out_count = out_tail - out_head;

    assign dac = speaker ? dac_value : 8'h80;

    wire [2:0] pair = address[3:1];
    wire write_busy = busy_us != 0 || high_speed;
    always @* begin
        rdata = 8'hff;
        case (pair)
            3'd5: rdata = out_count != 0 ? out_fifo[out_head[1:0]] : out_last;
            3'd6: rdata = state != RUNNING ? 8'hff : write_busy ? 8'haa : 8'h2a;
            3'd7: rdata = out_count != 0 ? 8'haa : 8'h2a;
            default: ;
        endcase
    end

    // Argument bytes each command takes (DOSBox-X DSP_cmd_len_sb).
    function automatic [1:0] arguments(input [7:0] c);
        case (c)
            8'h10, 8'h38, 8'h40, 8'he0, 8'he2, 8'he4: arguments = 2'd1;
            8'h14, 8'h15, 8'h16, 8'h17, 8'h24, 8'h41, 8'h42, 8'h48,
            8'h74, 8'h75, 8'h76, 8'h77, 8'h80: arguments = 2'd2;
            default: arguments = 2'd0;
        endcase
    endfunction

    wire command_write = access && write && pair == 3'd6 && state == RUNNING && !write_busy;
    wire data_read = access && !write && pair == 3'd5;
    wire status_read = access && !write && pair == 3'd7;
    wire sample_event = us_tick && period_us == 9'd1;
    wire dack = dack_valid && !dack_done;
    wire [15:0] argument_length = {wdata, argument0};    // with the last argument byte

    // One ADPCM code (the high bits of `code`): {next step, next reference}.
    // Rows of the step: 4-bit 0-3 by 16, 2.6-bit 0-4 by 8, 2-bit 0-5 by 4.
    function automatic [15:0] adpcm_decode(input [1:0] kind, input [3:0] code,
                                           input [7:0] reference, input [7:0] step);
        reg [8:0] index;
        reg [2:0] row, magnitude;
        reg negative;
        reg [7:0] delta, adjust;
        reg [9:0] sum;
        begin
            case (kind)
                ADPCM4: begin
                    index = {5'd0, code} + {1'b0, step};
                    if (index > 9'd63) index = 9'd63;
                    row = {1'b0, index[5:4]}; magnitude = index[2:0]; negative = index[3];
                    adjust = magnitude >= 3'd5 && row != 3'd3 ? 8'd16 : magnitude == 0 && row != 0 ? 8'hf0 : 8'd0;
                end
                ADPCM3: begin
                    index = {6'd0, code[3:1]} + {1'b0, step};
                    if (index > 9'd39) index = 9'd39;
                    row = index[5:3]; magnitude = {1'b0, index[1:0]}; negative = index[2];
                    adjust = magnitude == 3'd3 && row != 3'd4 ? 8'd8 : magnitude == 0 && row != 0 ? 8'hf8 : 8'd0;
                end
                default: begin
                    index = {7'd0, code[3:2]} + {1'b0, step};
                    if (index > 9'd23) index = 9'd23;
                    row = index[4:2]; magnitude = {2'd0, index[0]}; negative = index[1];
                    adjust = magnitude == 3'd1 && row != 3'd5 ? 8'd4 : magnitude == 0 && row != 0 ? 8'hfc : 8'd0;
                end
            endcase
            // Row 0 steps by the magnitude, row r by (2m + 1) << (r - 1); the
            // 2.6-bit top row by (2m + 1) * 5.
            if (row == 0) delta = {5'd0, magnitude};
            else if (kind == ADPCM3 && row == 3'd4) delta = {4'd0, magnitude, 1'b1} * 8'd5;
            else delta = {4'd0, magnitude, 1'b1} << (row - 1'b1);
            sum = negative ? {2'd0, reference} - {2'd0, delta} : {2'd0, reference} + {2'd0, delta};
            adpcm_decode = {step + adjust, sum[9] ? 8'h00 : sum[8] ? 8'hff : sum[7:0]};
        end
    endfunction
    // The byte in hand's later codes, or a new byte's first.
    wire [7:0] adpcm_in = codes_left != 0 ? adpcm_bits : dack_wdata;
    wire [1:0] adpcm_kind = codes_left != 0 ? codes_encoding : encoding;
    wire [15:0] adpcm_next = adpcm_decode(adpcm_kind, adpcm_in[7:4], adpcm_reference, adpcm_step);
    wire [1:0] adpcm_more = encoding == ADPCM4 ? 2'd1 : encoding == ADPCM3 ? 2'd2 : 2'd3;
    wire [7:0] adpcm_shifted = adpcm_kind == ADPCM4 ? {adpcm_in[3:0], 4'd0}
                             : adpcm_kind == ADPCM3 ? {adpcm_in[4:0], 3'd0} : {adpcm_in[5:0], 2'd0};
    task automatic adpcm_play;
        adpcm_step <= adpcm_next[15:8];
        adpcm_reference <= adpcm_next[7:0];
        dac_value <= adpcm_next[7:0];
        adpcm_bits <= adpcm_shifted;
    endtask

    task automatic push(input [7:0] value);
        out_fifo[out_tail[1:0]] <= value;
        out_tail <= out_tail + 1'b1;
    endtask
    // Replaces the buffer's contents with one or two bytes.
    task automatic reply(input [7:0] first, input [7:0] second, input two);
        out_head <= out_tail;
        out_fifo[out_tail[1:0]] <= first;
        out_fifo[2'(out_tail[1:0] + 2'd1)] <= second;
        out_tail <= out_tail + (two ? 3'd2 : 3'd1);
    endtask
    // One byte (or silent sample) of the block is done.
    task automatic block_done;
        remaining <= remaining - 1'b1;
        if (remaining == 0) begin
            irq <= 1;
            if (autoinit) remaining <= block_length;
            else begin
                mode <= NONE;
                high_speed <= 0;
            end
        end
    endtask
    task automatic start(input [2:0] kind, input [15:0] length, input auto, input fast,
                         input [1:0] code, input with_ref);
        mode <= kind;
        encoding <= code;
        adpcm_ref <= with_ref;
        remaining <= length;
        autoinit <= auto;
        high_speed <= fast;
        halted <= 0;
        period_us <= 9'd256 - {1'b0, time_constant};
    endtask

    always @(posedge clk) begin
        dack_done <= 0;
        if (reset || (access && write && pair == 3'd3 && wdata[0])) begin
            // Reset, or held in reset by base+6 bit 0.
            state <= reset ? RUNNING : IN_RESET;
            reset_us <= 0;
            command_pending <= 0; execute <= 0; needed <= 0; received <= 0;
            command <= 0; argument0 <= 0;
            out_head <= reset ? 3'd0 : out_tail;
            if (reset) out_tail <= 0;
            out_last <= 8'haa;
            busy_us <= 0;
            time_constant <= 8'hd3; test_register <= 0; dac_value <= 8'h80;
            block_length <= 16'h07ff; remaining <= 0;
            mode <= NONE; autoinit <= 0; high_speed <= 0; halted <= 0; speaker <= 0;
            period_us <= 9'd45;
            e2_add <= 8'haa; e2_xor <= 8'h96;
            encoding <= PCM; adpcm_ref <= 0; codes_left <= 0; codes_encoding <= PCM;
            adpcm_bits <= 0; adpcm_reference <= 8'h80; adpcm_step <= 0;
            irq <= 0; irq_us <= 0; drq <= 0;
            if (reset) dack_rdata <= 8'h80;
        end else begin
            if (access && write && pair == 3'd3 && state == IN_RESET) begin
                state <= RESET_WAIT;
                reset_us <= 0;
            end
            if (state == RESET_WAIT && us_tick) begin
                reset_us <= reset_us + 1'b1;
                if (reset_us == 8'(RESET_US - 1)) begin
                    state <= RUNNING;
                    push(8'haa);
                end
            end

            if (data_read && out_count != 0) begin
                out_last <= out_fifo[out_head[1:0]];
                out_head <= out_head + 1'b1;
            end
            if (status_read) irq <= 0;
            if (us_tick && busy_us != 0) busy_us <= busy_us - 1'b1;
            if (us_tick && irq_us != 0) begin
                irq_us <= irq_us - 1'b1;
                if (irq_us == 1) irq <= 1;
            end

            // ---- Command bytes ------------------------------------------------
            execute <= 0;
            if (command_write) begin
                busy_us <= 2'd2;
                if (!command_pending) begin
                    command <= wdata;
                    needed <= arguments(wdata);
                    received <= 0;
                    command_pending <= arguments(wdata) != 0;
                    execute <= arguments(wdata) == 0;
                end else begin
                    if (received == 0) argument0 <= wdata;
                    received <= received + 1'b1;
                    if (received + 1'b1 == needed) begin
                        command_pending <= 0;
                        execute <= 1;
                    end
                end
            end
            // A command executes in the clock after its last byte; its last
            // argument is still on wdata for a two-byte length.
            if (execute) case (command)
                8'h10: dac_value <= argument0;
                8'h14, 8'h15: start(PLAY, last_length, 0, 0, PCM, 0);
                8'h1c: start(PLAY, block_length, 1, 0, PCM, 0);
                8'h91: start(PLAY, block_length, 0, 1, PCM, 0);
                8'h90: start(PLAY, block_length, 1, 1, PCM, 0);
                8'h74, 8'h75: start(PLAY, last_length, 0, 0, ADPCM4, command[0]);
                8'h76, 8'h77: start(PLAY, last_length, 0, 0, ADPCM3, command[0]);
                8'h16, 8'h17: start(PLAY, last_length, 0, 0, ADPCM2, command[0]);
                8'h7d: start(PLAY, block_length, 1, 0, ADPCM4, 1);
                8'h7f: start(PLAY, block_length, 1, 0, ADPCM3, 1);
                8'h1f: start(PLAY, block_length, 1, 0, ADPCM2, 1);
                8'h24: start(RECORD, last_length, 0, 0, PCM, 0);
                8'h2c: start(RECORD, block_length, 1, 0, PCM, 0);
                8'h99: start(RECORD, block_length, 0, 1, PCM, 0);
                8'h98: start(RECORD, block_length, 1, 1, PCM, 0);
                8'h80: start(SILENCE, last_length, 0, 0, PCM, 0);
                8'h20: push(8'h80);
                8'h40: time_constant <= argument0;
                8'h48: block_length <= last_length;
                8'hd0: begin halted <= 1; drq <= 0; end
                8'hd4: halted <= 0;
                8'hda: autoinit <= 0;
                8'hd1: speaker <= 1;
                8'hd3: speaker <= 0;
                8'hd8: reply(speaker ? 8'hff : 8'h00, 8'h00, 0);
                8'he0: reply(~argument0, 8'h00, 0);
                8'he1: reply(8'h02, 8'h01, 1);
                8'he2: begin
                    e2_add <= e2_add + (argument0 ^ e2_xor);
                    e2_xor <= {e2_xor[1:0], e2_xor[7:2]};
                    mode <= IDENTIFY;
                    drq <= 1;
                end
                8'he4: test_register <= argument0;
                8'he8: push(test_register);
                8'hf2: irq_us <= 4'd10;
                default: ;
            endcase

            // ---- Transfers ------------------------------------------------------
            // An ADPCM byte's later codes play out after its block ends.
            if ((mode != NONE && mode != IDENTIFY || codes_left != 0) && !halted && us_tick) begin
                period_us <= period_us == 9'd1 ? 9'd256 - {1'b0, time_constant} : period_us - 1'b1;
                if (sample_event) begin
                    if (codes_left != 0) begin
                        codes_left <= codes_left - 1'b1;
                        adpcm_play();
                    end else if (mode == SILENCE) block_done();
                    else if (mode != NONE && mode != IDENTIFY) drq <= 1;
                end
            end
            if (dack && drq) begin
                dack_done <= 1;
                drq <= 0;
                if (mode == IDENTIFY) begin
                    dack_rdata <= e2_add;
                    mode <= NONE;
                end else begin
                    dack_rdata <= 8'h80;
                    if (mode == PLAY && dack_write) begin
                        if (encoding == PCM) dac_value <= dack_wdata;
                        else begin
                            codes_left <= adpcm_more;
                            codes_encoding <= encoding;
                            if (adpcm_ref) begin
                                // Code 0 at step 0 changes nothing, so the
                                // reference holds for its byte's periods.
                                adpcm_ref <= 0;
                                adpcm_reference <= dack_wdata;
                                adpcm_step <= 0;
                                adpcm_bits <= 0;
                                dac_value <= dack_wdata;
                            end else adpcm_play();
                        end
                    end
                    block_done();
                end
            end else if (dack) dack_done <= 1;
        end
    end

    always @(posedge clk) if (command_write && command_pending && received == needed - 1'b1) last_length <= argument_length;
endmodule
