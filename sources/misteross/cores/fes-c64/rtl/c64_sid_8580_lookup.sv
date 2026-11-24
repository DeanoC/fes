// SPDX-License-Identifier: GPL-2.0-or-later
// Interpolate between adjacent knots of one of the VICE 3.10 MOS8580
// transfer curves. Even and odd knots live in separate synchronous banks
// so a single request can read both endpoints. Five cycles to consumption.
module c64_sid_8580_lookup(input wire clk, input wire [2:0] curve_family,
    input wire [3:0] curve_selector, input wire [18:0] index, output reg [15:0] value);
    `include "c64_sid_8580_tables.vh"
    localparam BANKS = (SID8580_KNOTS + 1023) / 1024;
    localparam AW = $clog2(SID8580_KNOTS), BW = $clog2(BANKS);
    reg [AW-1:0] knot;
    reg [SID8580_STEP_BITS-1:0] fraction0, fraction1, fraction2;
    reg [2:0] zero_rev;
    always @(posedge clk) begin
        knot <= AW'(sid8580_base(curve_family,curve_selector)) + AW'(index >> SID8580_STEP_BITS);
        fraction0 <= index[SID8580_STEP_BITS-1:0];
        fraction1 <= fraction0;
        fraction2 <= fraction1;
        // The first VICE reverse-opamp entry is discontinuous.
        zero_rev <= {zero_rev[1:0], curve_family == 0 && index == 0};
    end
    function automatic [10239:0] lane_init(input [16383:0] data, input integer lane_number);
        integer i;
        for (i=0;i<512;i=i+1)
            lane_init[20*i+:20] = {4'd0,data[16*(2*i+lane_number)+:16]};
    endfunction
    wire [15:0] lane_q [0:1];
    genvar parity, bank;
    for (parity=0; parity<2; parity=parity+1) begin : lane
        wire [AW-2:0] address = knot[AW-1:1] + (parity == 0 && knot[0]);
        wire [15:0] bank_q [0:BANKS-1];
        reg [BW-1:0] selected;
        always @(posedge clk) selected <= BW'(address >> 9);
        assign lane_q[parity] = bank_q[selected];
        for (bank=0; bank<BANKS; bank=bank+1) begin : block_rom
            localparam [16383:0] KNOTS = {sid8580_bank(2*bank+1),sid8580_bank(2*bank)};
            localparam [10239:0] INIT = lane_init(KNOTS,parity);
`ifdef FES_SIMULATION
            reg [15:0] words [0:511];
            reg [15:0] q;
            integer i;
            initial for (i=0;i<512;i=i+1) words[i] = INIT[20*i+:16];
            always @(posedge clk) q <= words[address[8:0]];
            assign bank_q[bank] = q;
`else
            wire [19:0] q;
            MISTRAL_M10K #(.CFG_ABITS(9),.CFG_DBITS(20),.CFG_DUAL_CLOCK(0),
                .CFG_ASYNC_READ(0),.INIT(INIT)) rom(.CLK1(clk),.CLK2(),
                .A1ADDR(9'd0),.A1DATA(20'd0),.A1EN(1'b1),.A1BE(),
                .B1ADDR(address[8:0]),.B1DATA(q),.B1EN(1'b1),.ACLR0(1'b0),.ACLR1(1'b0));
            assign bank_q[bank] = q[15:0];
`endif
        end
    end
    reg odd;
    always @(posedge clk) odd <= knot[0];
    wire [15:0] a = odd ? lane_q[1] : lane_q[0];
    wire [15:0] b = odd ? lane_q[0] : lane_q[1];
    reg signed [16:0] delta;
    reg [15:0] base2, base3;
    reg signed [26:0] weighted;
    reg zero3;
    always @(posedge clk) begin
        base2 <= a;
        delta <= $signed({1'b0,b}) - $signed({1'b0,a});
        base3 <= base2;
        weighted <= delta * $signed({1'b0,fraction2});
        zero3 <= zero_rev[2];
        value <= zero3 ? 16'd0 : base3 +
            16'((weighted + (27'sd1 <<< (SID8580_STEP_BITS-1))) >>> SID8580_STEP_BITS);
    end
endmodule
