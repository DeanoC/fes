module top(input wire FPGA_CLK1_50, inout wire DDR_IO);
    wire [31:0] gpo;
    wire [31:0] gpi;
    wire dataout_h;
    wire dataout_l;
    wire combout;
    wire high;
    wire low;
    reg [7:0] beat = 0;
    always @(posedge FPGA_CLK1_50)
        beat <= beat + 1'b1;
    assign high = beat[0];
    assign low = beat[1];
    cyclonev_hps_interface_mpu_general_purpose hps_gp (.gp_in(gpi), .gp_out(gpo));
    (* keep *)
    reg captured = 1'b0;
    always @(posedge FPGA_CLK1_50)
        captured <= dataout_h ^ dataout_l ^ combout;
    assign gpi = {16'hDD04, beat, beat};
    altddio_bidir #(
        .width(1),
        .intended_device_family("Cyclone V"),
        .power_up_high("OFF"),
        .oe_reg("UNREGISTERED"),
        .extend_oe_disable("OFF"),
        .implement_input_in_lcell("UNUSED"),
        .invert_output("OFF")
    ) ddr (
        .datain_h(high),
        .datain_l(low),
        .inclock(FPGA_CLK1_50),
        .inclocken(1'b1),
        .outclock(FPGA_CLK1_50),
        .outclocken(1'b1),
        .aset(1'b0),
        .aclr(1'b0),
        .sset(1'b0),
        .sclr(1'b0),
        .oe(1'b1),
        .dataout_h(dataout_h),
        .dataout_l(dataout_l),
        .combout(combout),
        .oe_out(),
        .dqsundelayedout(),
        .padio(DDR_IO)
    );
endmodule
