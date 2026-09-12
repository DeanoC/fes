// Simulation only: idle HPS I2C cell with constant released outputs.
module cyclonev_hps_interface_peripheral_i2c (
    input wire scl,
    input wire sda,
    output reg out_clk,
    output reg out_data
);
    initial begin
        out_clk = 1'b0;
        out_data = 1'b0;
    end
endmodule
