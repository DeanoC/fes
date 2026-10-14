// SPDX-License-Identifier: GPL-3.0-or-later
// Exact normalized Hatari STF nonlinear mixer. Requests snapshot the three
// gated 5-bit levels; canonical sorting plus sparse order-rounding corrections
// avoids a full 32768-word ROM. One request at a time, ready pulses once.
module st_ym_mixer (
    input wire clk, reset, req,
    input wire [14:0] levels,
    output wire busy,
    output reg ready,
    output reg [14:0] pcm, result_levels
);
    localparam [3:0] IDLE=0, SORT_AB=1, SORT_BC=2, SORT_AB_AGAIN=3,
        RANK_PART=4, RANK_FINISH=5, BASE_WAIT=6, BASE_CAPTURE=7,
        SEARCH_ADDR=8, SEARCH_WAIT=9, SEARCH_COMPARE=10, FINISH=11;
    reg [3:0] state;
    reg [4:0] a,b,c;
    reg [14:0] original_levels, base_value;
    reg [12:0] rank_part, base_addr;
    reg [10:0] low, high, middle, correction_addr;
    wire [10:0] midpoint = 11'(({1'b0,low}+{1'b0,high}) >> 1);
    wire [14:0] base_data;
    wire [15:0] correction_data;
    assign busy = state != IDLE;
    st_ym_mix_rom rom (.clk(clk), .base_addr(base_addr), .correction_addr(correction_addr),
        .base_data(base_data), .correction_data(correction_data));
    function automatic [12:0] triangular(input [4:0] x);
        begin case(x)
            5'd0: triangular = 13'd0;
            5'd1: triangular = 13'd1;
            5'd2: triangular = 13'd3;
            5'd3: triangular = 13'd6;
            5'd4: triangular = 13'd10;
            5'd5: triangular = 13'd15;
            5'd6: triangular = 13'd21;
            5'd7: triangular = 13'd28;
            5'd8: triangular = 13'd36;
            5'd9: triangular = 13'd45;
            5'd10: triangular = 13'd55;
            5'd11: triangular = 13'd66;
            5'd12: triangular = 13'd78;
            5'd13: triangular = 13'd91;
            5'd14: triangular = 13'd105;
            5'd15: triangular = 13'd120;
            5'd16: triangular = 13'd136;
            5'd17: triangular = 13'd153;
            5'd18: triangular = 13'd171;
            5'd19: triangular = 13'd190;
            5'd20: triangular = 13'd210;
            5'd21: triangular = 13'd231;
            5'd22: triangular = 13'd253;
            5'd23: triangular = 13'd276;
            5'd24: triangular = 13'd300;
            5'd25: triangular = 13'd325;
            5'd26: triangular = 13'd351;
            5'd27: triangular = 13'd378;
            5'd28: triangular = 13'd406;
            5'd29: triangular = 13'd435;
            5'd30: triangular = 13'd465;
            5'd31: triangular = 13'd496;
        endcase end
    endfunction
    function automatic [12:0] tetrahedral(input [4:0] x);
        begin case(x)
            5'd0: tetrahedral = 13'd0;
            5'd1: tetrahedral = 13'd1;
            5'd2: tetrahedral = 13'd4;
            5'd3: tetrahedral = 13'd10;
            5'd4: tetrahedral = 13'd20;
            5'd5: tetrahedral = 13'd35;
            5'd6: tetrahedral = 13'd56;
            5'd7: tetrahedral = 13'd84;
            5'd8: tetrahedral = 13'd120;
            5'd9: tetrahedral = 13'd165;
            5'd10: tetrahedral = 13'd220;
            5'd11: tetrahedral = 13'd286;
            5'd12: tetrahedral = 13'd364;
            5'd13: tetrahedral = 13'd455;
            5'd14: tetrahedral = 13'd560;
            5'd15: tetrahedral = 13'd680;
            5'd16: tetrahedral = 13'd816;
            5'd17: tetrahedral = 13'd969;
            5'd18: tetrahedral = 13'd1140;
            5'd19: tetrahedral = 13'd1330;
            5'd20: tetrahedral = 13'd1540;
            5'd21: tetrahedral = 13'd1771;
            5'd22: tetrahedral = 13'd2024;
            5'd23: tetrahedral = 13'd2300;
            5'd24: tetrahedral = 13'd2600;
            5'd25: tetrahedral = 13'd2925;
            5'd26: tetrahedral = 13'd3276;
            5'd27: tetrahedral = 13'd3654;
            5'd28: tetrahedral = 13'd4060;
            5'd29: tetrahedral = 13'd4495;
            5'd30: tetrahedral = 13'd4960;
            5'd31: tetrahedral = 13'd5456;
        endcase end
    endfunction
    always @(posedge clk) begin
        if(reset) begin
            state<=IDLE; ready<=0; pcm<=0; result_levels<=0;
            a<=0;b<=0;c<=0;original_levels<=0;base_value<=0;
            rank_part<=0;base_addr<=0;low<=0;high<=0;middle<=0;correction_addr<=0;
        end else begin
            ready<=0;
            case(state)
                IDLE: if(req) begin
                    original_levels<=levels; a<=levels[4:0];b<=levels[9:5];c<=levels[14:10];
                    state<=SORT_AB;
                end
                SORT_AB: begin
                    if(a>b)begin a<=b;b<=a;end
                    state<=SORT_BC;
                end
                SORT_BC: begin
                    if(b>c)begin b<=c;c<=b;end
                    state<=SORT_AB_AGAIN;
                end
                SORT_AB_AGAIN: begin
                    if(a>b)begin a<=b;b<=a;end
                    state<=RANK_PART;
                end
                RANK_PART:begin rank_part<={8'd0,a}+triangular(b);state<=RANK_FINISH;end
                RANK_FINISH:begin base_addr<=rank_part+tetrahedral(c);state<=BASE_WAIT;end
                BASE_WAIT:state<=BASE_CAPTURE;
                BASE_CAPTURE:begin base_value<=base_data;low<=0;high<=11'd1159;state<=SEARCH_ADDR;end
                SEARCH_ADDR:begin
                    if(low>=high)state<=FINISH;
                    else begin middle<=midpoint;correction_addr<=midpoint;state<=SEARCH_WAIT;end
                end
                SEARCH_WAIT:state<=SEARCH_COMPARE;
                SEARCH_COMPARE:begin
                    if(correction_data[15:1]==original_levels) begin
                        base_value<=correction_data[0]?base_value+15'd1:base_value-15'd1;
                        state<=FINISH;
                    end else begin
                        if(correction_data[15:1]<original_levels)low<=middle+11'd1;
                        else high<=middle;
                        state<=SEARCH_ADDR;
                    end
                end
                FINISH:begin pcm<=base_value;result_levels<=original_levels;ready<=1;state<=IDLE;end
                default:state<=IDLE;
            endcase
        end
    end
endmodule
