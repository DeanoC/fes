module dsp9_preadder (
    input wire [8:0] A,
    input wire [8:0] B,
    input wire [8:0] Z,
    output wire [17:0] Y
);
    // PREADDER_SUB: AY minus AZ, then multiply by AX: A * (B - Z).
    wire [8:0] preadd;
    assign preadd = B - Z;
    assign Y = A * preadd;
endmodule
