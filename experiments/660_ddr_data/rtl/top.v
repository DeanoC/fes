module top(input wire FPGA_CLK1_50, output wire DDR_OUT);
    wire [31:0] gpo;
    wire [31:0] gpi;
    wire high;
    wire low;
    reg [5:0] beat = 0;
    always @(posedge FPGA_CLK1_50)
        beat <= beat + 1'b1;
    assign high = beat[0];
    assign low = beat[1];
    cyclonev_hps_interface_mpu_general_purpose hps_gp (.gp_in(gpi), .gp_out(gpo));
    assign gpi = {16'hDD03, beat, high, low, beat, high, low};
    altddio_out #(
        .width(1),
        .intended_device_family("Cyclone V"),
        .power_up_high("OFF"),
        .oe_reg("UNREGISTERED"),
        .extend_oe_disable("OFF"),
        .invert_output("OFF")
    ) ddr (
        .datain_h(high),
        .datain_l(low),
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
