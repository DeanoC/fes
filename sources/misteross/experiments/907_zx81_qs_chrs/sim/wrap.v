// Simulation wrap: 904 mailbox plus one ZX81 cart.
module top #(
    parameter [15:0] SIGNATURE = 16'hD904
) (
    input wire FPGA_CLK1_50
);
    wire [31:0] gp_in;
    wire [31:0] gp_out;
    (* keep *) reg [15:0] plug_addr = 16'd0;
    (* keep *) reg [7:0] plug_wdata = 8'd0;
    (* keep *) reg plug_mem_we = 1'b0;
    (* keep *) reg plug_io_we = 1'b0;
    (* keep *) reg plug_io_rd = 1'b0;
    wire [9:0] plug_rdata;
    (* keep *) reg [1:0] ring = 2'b01;

    (* keep *) reg armed = 1'b0;
    always @(posedge FPGA_CLK1_50) begin
        ring <= {ring[0], ring[1]};
        if (gp_out == 32'h13579BDF)
            armed <= 1'b1;
        plug_addr <= gp_out[15:0];
        plug_wdata <= gp_out[23:16];
        plug_mem_we <= gp_out[24] & armed;
        plug_io_we <= gp_out[25] & armed;
        plug_io_rd <= gp_out[26];
    end

    cart u_cart (
        .FPGA_CLK1_50(FPGA_CLK1_50),
        .plug_addr(plug_addr),
        .plug_wdata(plug_wdata),
        .plug_mem_we(plug_mem_we),
        .plug_io_we(plug_io_we),
        .plug_io_rd(plug_io_rd),
        .plug_rdata(plug_rdata)
    );

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(gp_in),
        .gp_out(gp_out)
    );

    assign gp_in = {SIGNATURE, plug_addr[5:0], plug_rdata};
endmodule
