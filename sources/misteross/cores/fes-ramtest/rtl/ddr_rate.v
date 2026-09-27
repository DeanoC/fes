// SPDX-License-Identifier: GPL-2.0-or-later
// MB/s of one completed pass, as four decimal digits. NUMERATOR is the pass
// size in bytes times the clock in MHz, so bytes per microsecond is
// NUMERATOR / cycles. A sequential divide and a double-dabble conversion run
// once per pass; the digits saturate at 9999.
module ddr_rate #(
    parameter [39:0] NUMERATOR = 40'd0
) (
    input  wire        clk,
    input  wire        start,
    input  wire [31:0] cycles,
    output reg  [15:0] digits
);
    localparam [1:0] ST_IDLE = 2'd0;
    localparam [1:0] ST_DIVIDE = 2'd1;
    localparam [1:0] ST_LOAD = 2'd2;
    localparam [1:0] ST_DIGITS = 2'd3;

    reg [1:0]  state = ST_IDLE;
    reg [5:0]  step;
    reg [31:0] divisor;
    reg [31:0] remainder;
    reg [39:0] quotient;
    reg [13:0] binary;
    reg [15:0] bcd;

    initial digits = 16'h0000;

    wire [32:0] shifted = {remainder, quotient[39]};
    wire fits = shifted >= {1'b0, divisor};
    // Double dabble: add three to every digit of five or more, then shift.
    wire [15:0] adjusted = {
        bcd[15:12] >= 4'd5 ? bcd[15:12] + 4'd3 : bcd[15:12],
        bcd[11:8] >= 4'd5 ? bcd[11:8] + 4'd3 : bcd[11:8],
        bcd[7:4] >= 4'd5 ? bcd[7:4] + 4'd3 : bcd[7:4],
        bcd[3:0] >= 4'd5 ? bcd[3:0] + 4'd3 : bcd[3:0]
    };

    always @(posedge clk) begin
        if (start && cycles != 32'd0) begin
            state <= ST_DIVIDE;
            divisor <= cycles;
            remainder <= 32'd0;
            quotient <= NUMERATOR;
            step <= 6'd40;
        end else begin
            case (state)
                ST_DIVIDE: begin
                    remainder <= fits ? shifted[31:0] - divisor : shifted[31:0];
                    quotient <= {quotient[38:0], fits};
                    step <= step - 6'd1;
                    if (step == 6'd1)
                        state <= ST_LOAD;
                end
                ST_LOAD: begin
                    binary <= quotient > 40'd9999 ? 14'd9999 : quotient[13:0];
                    bcd <= 16'h0000;
                    step <= 6'd14;
                    state <= ST_DIGITS;
                end
                ST_DIGITS: begin
                    {bcd, binary} <= {adjusted, binary} << 1;
                    step <= step - 6'd1;
                    if (step == 6'd1) begin
                        digits <= {adjusted[14:0], binary[13]};
                        state <= ST_IDLE;
                    end
                end
                default: ;
            endcase
        end
    end
endmodule
