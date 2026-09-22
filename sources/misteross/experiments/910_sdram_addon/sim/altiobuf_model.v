// Simulation only. Does not model analog pad delay or bus hold.
module altiobuf_bidir #(
    parameter number_of_channels = 1,
    parameter enable_bus_hold = "OFF"
) (
    inout wire dataio,
    input wire oe,
    input wire datain,
    output wire dataout
);
    assign dataio = oe ? datain : 1'bz;
    assign dataout = dataio;
endmodule
