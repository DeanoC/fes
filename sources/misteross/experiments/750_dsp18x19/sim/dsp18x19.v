// Simulation only: two unsigned 18x19 products packed into Y[73:0].
module dsp18x19 (
    input wire [17:0] A,
    input wire [18:0] B,
    input wire [17:0] C,
    input wire [18:0] D,
    output wire [73:0] Y
);
    wire [36:0] p0 = A * B;
    wire [36:0] p1 = C * D;
    assign Y = {p1, p0};
endmodule
