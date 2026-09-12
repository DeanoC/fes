// HPS peripheral I2C placed only by QSF HPS_LOCATION, not an RTL site
// attribute.
module top #(
    parameter [15:0] SIGNATURE = 16'hD850
) (
    input wire FPGA_CLK1_50
);
    wire [31:0] gp_in;
    wire [31:0] gp_out;
    wire i2c_clk_low;
    wire i2c_data_low;
    (* keep *) reg [1:0] ring = 2'b01;

    always @(posedge FPGA_CLK1_50)
        ring <= {ring[0], ring[1]};

    (* keep *)
    cyclonev_hps_interface_peripheral_i2c hdmi_i2c (
        .scl(1'b1),
        .sda(1'b1),
        .out_clk(i2c_clk_low),
        .out_data(i2c_data_low)
    );

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );

    assign gp_in = {SIGNATURE, 16'h00A6};
endmodule
