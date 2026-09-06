module storage_port #(
    parameter integer ADDR_BITS = 5
) (
    input wire FPGA_CLK1_50,
    input wire we,
    input wire [ADDR_BITS-1:0] addr,
    input wire [7:0] wdata,
    output reg [7:0] rdata
);
    localparam integer DEPTH = 1 << ADDR_BITS;

    reg [7:0] stored [0:DEPTH-1];

    initial rdata = 8'h00;

    always @(posedge FPGA_CLK1_50) begin
        if (we) begin
            stored[addr] <= wdata;
        end
        rdata <= stored[addr];
    end
endmodule

module top (
    input wire FPGA_CLK1_50
);
    localparam [15:0] SIGNATURE = 16'hD410;

    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;
    wire [7:0] rdata;
    reg [1:0] beat = 2'b00;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    storage_port storage (
        .FPGA_CLK1_50(FPGA_CLK1_50),
        .we(hps_to_fpga[16]),
        .addr(hps_to_fpga[4:0]),
        .wdata(hps_to_fpga[15:8]),
        .rdata(rdata)
    );

    always @(posedge FPGA_CLK1_50) begin
        beat <= beat + 2'd1;
    end

    assign fpga_to_hps = {SIGNATURE, 6'b000000, beat, rdata};
endmodule
