module dsp18_mac (
    input wire [17:0] A,
    input wire [17:0] B,
    input wire [35:0] C,
    output wire [35:0] Y
);
    assign Y = (A * B) + C;
endmodule
