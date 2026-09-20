module dsp18_reg (
    input wire [17:0] A,
    input wire [17:0] B,
    input wire CLK,
    output reg [35:0] Y
);
    reg [17:0] a_q, b_q;

    initial begin
        a_q = 18'd0;
        b_q = 18'd0;
        Y = 36'd0;
    end

    always @(posedge CLK) begin
        a_q <= A;
        b_q <= B;
        Y <= a_q * b_q;
    end
endmodule
