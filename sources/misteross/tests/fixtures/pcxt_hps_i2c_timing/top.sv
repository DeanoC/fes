module top(inout wire HDMI_I2C_SCL, HDMI_I2C_SDA);
 wire scl_low, sda_low, scl_in, sda_in;
 assign HDMI_I2C_SCL = scl_low ? 1'b0 : 1'bz;
 assign HDMI_I2C_SDA = sda_low ? 1'b0 : 1'bz;
 assign scl_in = HDMI_I2C_SCL;
 assign sda_in = HDMI_I2C_SDA;
 cyclonev_hps_interface_peripheral_i2c hdmi_i2c(
  .scl(scl_in), .sda(sda_in), .out_clk(scl_low), .out_data(sda_low));
endmodule
