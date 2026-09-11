module top(input wire FPGA_CLK1_50, output reg SDR_OUT);
    wire [31:0] gpo;
    wire [31:0] gpi;
    reg [7:0] beat = 0;
    always @(posedge FPGA_CLK1_50)
        beat <= beat + 1'b1;
    cyclonev_hps_interface_mpu_general_purpose hps_gp (.gp_in(gpi), .gp_out(gpo));
    assign gpi = {16'h5D01, beat, beat};
    always @(posedge FPGA_CLK1_50)
        SDR_OUT <= beat[7];
endmodule
