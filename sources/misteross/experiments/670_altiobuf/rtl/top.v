module top(
    input wire FPGA_CLK1_50,
    input wire ALTI_IN,
    output wire ALTI_OUT,
    inout wire ALTI_BIDIR
);
    wire [31:0] gpo;
    wire [31:0] gpi;
    wire in_data;
    wire bidir_data;
    reg [7:0] beat = 0;
    always @(posedge FPGA_CLK1_50)
        beat <= beat + 1'b1;
    cyclonev_hps_interface_mpu_general_purpose hps_gp (.gp_in(gpi), .gp_out(gpo));
    assign gpi = {16'hAB01, beat, beat};
    altiobuf_in #(
        .number_of_channels(1),
        .enable_bus_hold("FALSE"),
        .use_differential_mode("FALSE")
    ) in_buf (
        .datain(ALTI_IN),
        .dataout(in_data)
    );
    altiobuf_out #(
        .number_of_channels(1),
        .enable_bus_hold("FALSE"),
        .use_differential_mode("FALSE"),
        .use_oe("FALSE")
    ) out_buf (
        .datain(beat[0] ^ in_data ^ bidir_data),
        .dataout(ALTI_OUT)
    );
    altiobuf_bidir #(
        .number_of_channels(1),
        .enable_bus_hold("OFF")
    ) bidir_buf (
        .dataio(ALTI_BIDIR),
        .oe(1'b1),
        .datain(beat[1]),
        .dataout(bidir_data)
    );
endmodule
