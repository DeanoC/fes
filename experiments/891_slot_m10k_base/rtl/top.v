// Base fabric probe with an empty reserved M10K slot. GPI signature 0xD891.
module top #(
    parameter [15:0] SIGNATURE = 16'hD891
) (
    input wire FPGA_CLK1_50
);
    wire [31:0] gp_in;
    wire [31:0] gp_out;
    (* keep *) reg [1:0] ring = 2'b01;
    reg [15:0] sampled = 16'h00A6;

    always @(posedge FPGA_CLK1_50)
        ring <= {ring[0], ring[1] ^ gp_out[0]};

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );

    assign gp_in = {SIGNATURE, sampled};
endmodule
