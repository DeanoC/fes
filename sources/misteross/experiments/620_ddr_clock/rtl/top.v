module top(input wire FPGA_CLK1_50, output wire DDR_OUT);
    wire [31:0] gpo;
    wire [31:0] gpi;
    reg [7:0] beat = 0;
    always @(posedge FPGA_CLK1_50)
        beat <= beat + 1'b1;
    cyclonev_hps_interface_mpu_general_purpose hps_gp (.gp_in(gpi), .gp_out(gpo));
    assign gpi = {16'hDD01, beat, beat};
    altddio_out #(
        .width(1),
        .intended_device_family("Cyclone V"),
        .power_up_high("OFF"),
        .oe_reg("UNREGISTERED"),
        .extend_oe_disable("OFF"),
        .invert_output("OFF")
    ) ddr (
        .datain_h(1'b1),
        .datain_l(1'b0),
        .outclock(FPGA_CLK1_50),
        .outclocken(1'b1),
        .aset(1'b0),
        .aclr(1'b0),
        .sset(1'b0),
        .sclr(1'b0),
        .oe(1'b1),
        .dataout(DDR_OUT),
        .oe_out()
    );
endmodule
