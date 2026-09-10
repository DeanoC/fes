// Simulation only: direct-buffer stand-ins. They do not model analog pad
// delay, bus hold or electrical timing.
module altiobuf_in #(
    parameter number_of_channels = 1,
    parameter enable_bus_hold = "FALSE",
    parameter use_differential_mode = "FALSE"
) (
    input wire datain,
    output wire dataout
);
    assign dataout = datain;
endmodule

module altiobuf_out #(
    parameter number_of_channels = 1,
    parameter enable_bus_hold = "FALSE",
    parameter use_differential_mode = "FALSE",
    parameter use_oe = "FALSE"
) (
    input wire datain,
    output wire dataout
);
    assign dataout = datain;
endmodule

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
