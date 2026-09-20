module top(input wire FPGA_CLK1_50, input wire SDR_IN);
    wire [31:0] gpo;
    wire [31:0] gpi;
    reg [6:0] beat = 0;
    reg captured = 0;
    always @(posedge FPGA_CLK1_50) begin
        beat <= beat + 1'b1;
        captured <= SDR_IN;
    end
    cyclonev_hps_interface_mpu_general_purpose hps_gp (.gp_in(gpi), .gp_out(gpo));
    assign gpi = {16'h5E01, beat, captured, beat, captured};
endmodule
