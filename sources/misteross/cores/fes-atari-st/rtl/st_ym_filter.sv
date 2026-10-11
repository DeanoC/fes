// SPDX-License-Identifier: GPL-3.0-or-later
// STF C10 low-pass filter, exact-rational counter-to-PCM box average and
// Hatari-style DC removal. sample_ce is delayed at least 320 fabric clocks
// after reset so the four-entry result FIFO supplies causal output samples.
module st_ym_filter #(
    parameter integer CHIP_CLOCK_HZ = 2_005_311,
    parameter integer SAMPLE_RATE_HZ = 48_000
) (
    input wire clk, reset, input_valid, sample_ce,
    input wire [14:0] input_pcm,
    output reg signed [15:0] pcm,
    output reg sample_valid, fault,
    output wire busy
);
    localparam integer WEIGHT_BITS = $clog2(CHIP_CLOCK_HZ + 1);
    localparam integer TOTAL_BITS = WEIGHT_BITS + 15;
    localparam [WEIGHT_BITS-1:0] INTERVAL = WEIGHT_BITS'(CHIP_CLOCK_HZ);
    localparam [WEIGHT_BITS-1:0] STEP = WEIGHT_BITS'(8*SAMPLE_RATE_HZ);
    localparam [2:0] IDLE=0, WEIGHTS=1, ACCUMULATE=2, DIVIDE=3, PUSH=4;
    reg [2:0] state;
    reg [14:0] previous_input, filtered;
    wire [15:0] input_pair = {1'b0,input_pcm}+{1'b0,previous_input};
    // Hatari selects LowPassFilter for ST output at 40 kHz and above.
    // Both branches use the previous input; a rising edge also filters.
    wire [14:0] rise_value = 15'((18'(input_pair) + (18'(input_pair)<<1) +
                                 (18'(filtered)<<1))>>3);
    wire [14:0] fall_value = 15'((18'(input_pair) + (18'(filtered)<<2) +
                                 (18'(filtered)<<1))>>3);
    wire [14:0] low_pass_next = input_pcm>=filtered ? rise_value : fall_value;
    reg [WEIGHT_BITS-1:0] weight, tail_weight;
    wire [WEIGHT_BITS-1:0] remaining = INTERVAL-weight;
    wire boundary = remaining<=STEP;
    wire [WEIGHT_BITS-1:0] used_weight = boundary ? remaining : STEP;
    reg crosses_boundary;
    reg [TOTAL_BITS-1:0] total, product, tail_product, numerator;
    reg [WEIGHT_BITS-1:0] remainder;
    reg [14:0] quotient;
    reg [$clog2(TOTAL_BITS+1)-1:0] divide_count;
    wire [WEIGHT_BITS:0] shifted_remainder = {remainder,numerator[TOTAL_BITS-1]};
    wire quotient_bit = shifted_remainder >= {1'b0,INTERVAL};
    assign busy = state!=IDLE;
    always @(posedge clk) begin
        if(reset) begin
            state<=IDLE;previous_input<=0;filtered<=0;
            // The first counter period starts at reset with silent chip levels.
            weight<=STEP;tail_weight<=0;crosses_boundary<=0;
            total<=0;product<=0;tail_product<=0;numerator<=0;
            remainder<=0;quotient<=0;divide_count<=0;
        end else case(state)
            IDLE: if(input_valid) begin
                previous_input<=input_pcm;filtered<=low_pass_next;state<=WEIGHTS;
            end
            WEIGHTS: begin
                product<=TOTAL_BITS'(filtered)*TOTAL_BITS'(used_weight);
                tail_product<=TOTAL_BITS'(filtered)*TOTAL_BITS'(STEP-used_weight);
                tail_weight<=STEP-used_weight;crosses_boundary<=boundary;
                state<=ACCUMULATE;
            end
            ACCUMULATE: begin
                if(crosses_boundary) begin
                    numerator<=total+product;total<=tail_product;weight<=tail_weight;
                    remainder<=0;quotient<=0;divide_count<=$bits(divide_count)'(TOTAL_BITS);
                    state<=DIVIDE;
                end else begin total<=total+product;weight<=weight+STEP;state<=IDLE;end
            end
            DIVIDE: begin
                numerator<={numerator[TOTAL_BITS-2:0],1'b0};
                remainder<=quotient_bit ? WEIGHT_BITS'(shifted_remainder-{1'b0,INTERVAL}) :
                                         shifted_remainder[WEIGHT_BITS-1:0];
                quotient<={quotient[13:0],quotient_bit};divide_count<=divide_count-1'b1;
                if(divide_count==1)state<=PUSH;
            end
            PUSH:state<=IDLE;
            default:state<=IDLE;
        endcase
    end
    // Explicit registers keep this shallow FIFO out of scarce block RAM.
    reg [14:0] fifo0,fifo1,fifo2,fifo3;
    reg [1:0] read_pointer,write_pointer;
    reg [2:0] count;
    wire [14:0] head = read_pointer==0 ? fifo0 : read_pointer==1 ? fifo1 :
                       read_pointer==2 ? fifo2 : fifo3;
    wire pop = sample_ce && count!=0;
    wire push = state==PUSH && (count<4 || pop);
    reg signed [31:0] dc_state;
    reg [14:0] previous_average;
    wire signed [31:0] average_difference = $signed({17'd0,head})-$signed({17'd0,previous_average});
    wire signed [31:0] dc_output = dc_state>>>15;
    wire signed [31:0] next_dc_state = dc_state+(average_difference<<<15)-(dc_output<<<6);
    always @(posedge clk) begin
        if(reset) begin
            fifo0<=0;fifo1<=0;fifo2<=0;fifo3<=0;read_pointer<=0;write_pointer<=0;count<=0;
            dc_state<=0;previous_average<=0;pcm<=0;sample_valid<=0;fault<=0;
        end else begin
            sample_valid<=sample_ce;
            if((input_valid && busy)||(state==PUSH && !push)||(sample_ce && !pop))fault<=1;
            if(push) begin
                case(write_pointer)
                    0:fifo0<=quotient;1:fifo1<=quotient;2:fifo2<=quotient;3:fifo3<=quotient;
                endcase
                write_pointer<=write_pointer+1'b1;
            end
            if(pop) begin
                previous_average<=head;dc_state<=next_dc_state;
                pcm<=16'(next_dc_state>>>15);read_pointer<=read_pointer+1'b1;
            end else if(sample_ce)pcm<=0;
            case({push,pop})
                2'b10:count<=count+1'b1;
                2'b01:count<=count-1'b1;
                default:;
            endcase
        end
    end
endmodule
