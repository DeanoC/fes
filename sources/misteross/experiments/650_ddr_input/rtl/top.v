module top(input wire FPGA_CLK1_50, input wire DDR_IN);
    wire [31:0] gpo;
    wire [31:0] gpi;
    wire high;
    wire low;
    reg [5:0] beat = 0;
    always @(posedge FPGA_CLK1_50)
        beat <= beat + 1'b1;
    cyclonev_hps_interface_mpu_general_purpose hps_gp (.gp_in(gpi), .gp_out(gpo));
    assign gpi = {16'hDD02, beat, high, low, beat, high, low};
    altddio_in #(
        .width(1),
        .intended_device_family("Cyclone V"),
        .power_up_high("OFF"),
        .invert_input_clocks("OFF")
    ) ddr (
        .datain(DDR_IN),
        .inclock(FPGA_CLK1_50),
        .inclocken(1'b1),
        .aset(1'b0),
        .aclr(1'b0),
        .sset(1'b0),
        .sclr(1'b0),
        .dataout_h(high),
        .dataout_l(low)
    );
endmodule
