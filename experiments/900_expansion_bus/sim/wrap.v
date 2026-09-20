// Simulation wrap: 901 mailbox + cart A. GPI signature stays 0xD901.
module top #(
    parameter [15:0] SIGNATURE = 16'hD901
) (
    input wire FPGA_CLK1_50
);
    wire [31:0] gp_in;
    wire [31:0] gp_out;
    (* keep *) reg [15:0] plug_addr = 16'd0;
    wire [9:0] plug_rdata;
    (* keep *) reg [1:0] ring = 2'b01;

    always @(posedge FPGA_CLK1_50) begin
        ring <= {ring[0], ring[1]};
        plug_addr <= {6'd0, gp_out[9:0]};
    end

    cart u_cart (
        .FPGA_CLK1_50(FPGA_CLK1_50),
        .plug_addr(plug_addr),
        .plug_rdata(plug_rdata)
    );

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );

    assign gp_in = {SIGNATURE, 6'd0, plug_rdata};
endmodule
