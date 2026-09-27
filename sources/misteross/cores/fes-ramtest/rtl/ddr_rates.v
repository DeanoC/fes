// SPDX-License-Identifier: GPL-2.0-or-later
// MB/s of each DDR port's last write and read pass, as four decimal digits.
// One divider takes the six pass lengths in turn: bytes per microsecond is
// pass bytes times the clock in MHz, divided by the pass cycles. A slot with
// no finished pass shows zero; the digits saturate at 9999.
module ddr_rates #(
    parameter [39:0] WIDE_NUMERATOR = 40'd0,
    parameter [39:0] NARROW_NUMERATOR = 40'd0
) (
    input  wire         clk,
    // Port 0, 1, 2; write then read cycles for each.
    input  wire [191:0] cycles,
    output reg  [95:0]  digits
);
    localparam [1:0] ST_LOAD = 2'd0;
    localparam [1:0] ST_DIVIDE = 2'd1;
    localparam [1:0] ST_BINARY = 2'd2;
    localparam [1:0] ST_DIGITS = 2'd3;

    reg [1:0]  state = ST_LOAD;
    reg [2:0]  slot = 3'd0;
    reg [5:0]  step;
    reg [31:0] divisor;
    reg [31:0] remainder;
    reg [39:0] quotient;
    reg [13:0] binary;
    reg [15:0] bcd;

    initial digits = 96'd0;

    // One subtraction per step: the borrow says whether the divisor fits.
    wire [32:0] shifted = {remainder, quotient[39]};
    wire [33:0] trial = {1'b0, shifted} - {2'b00, divisor};
    wire fits = ~trial[33];
    // Double dabble: add three to every digit of five or more, then shift.
    wire [15:0] adjusted = {
        bcd[15:12] >= 4'd5 ? bcd[15:12] + 4'd3 : bcd[15:12],
        bcd[11:8] >= 4'd5 ? bcd[11:8] + 4'd3 : bcd[11:8],
        bcd[7:4] >= 4'd5 ? bcd[7:4] + 4'd3 : bcd[7:4],
        bcd[3:0] >= 4'd5 ? bcd[3:0] + 4'd3 : bcd[3:0]
    };

    always @(posedge clk) begin
        case (state)
            ST_LOAD: begin
                divisor <= cycles[slot*32 +: 32];
                remainder <= 32'd0;
                quotient <= slot < 3'd2 ? WIDE_NUMERATOR : NARROW_NUMERATOR;
                step <= 6'd40;
                state <= ST_DIVIDE;
            end
            ST_DIVIDE: begin
                remainder <= fits ? trial[31:0] : shifted[31:0];
                quotient <= {quotient[38:0], fits};
                step <= step - 6'd1;
                if (step == 6'd1)
                    state <= ST_BINARY;
            end
            ST_BINARY: begin
                binary <= divisor == 32'd0 ? 14'd0 :
                    (quotient > 40'd9999 ? 14'd9999 : quotient[13:0]);
                bcd <= 16'h0000;
                step <= 6'd14;
                state <= ST_DIGITS;
            end
            default: begin
                {bcd, binary} <= {adjusted, binary} << 1;
                step <= step - 6'd1;
                if (step == 6'd1) begin
                    digits[slot*16 +: 16] <= {adjusted[14:0], binary[13]};
                    slot <= slot == 3'd5 ? 3'd0 : slot + 3'd1;
                    state <= ST_LOAD;
                end
            end
        endcase
    end
endmodule
