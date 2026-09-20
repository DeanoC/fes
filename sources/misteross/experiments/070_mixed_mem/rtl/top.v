module storage_port (
    input wire FPGA_CLK1_50,
    input wire we,
    input wire bank,
    input wire [7:0] addr,
    input wire [7:0] wdata,
    output wire [7:0] rdata
);
    (* ramstyle = "mlab" *) reg [7:0] lab_store [0:31];
    (* ramstyle = "M10K" *) reg [7:0] block_store [0:255];
    reg [7:0] lab_q = 8'h00;
    reg [7:0] block_q = 8'h00;

    always @(posedge FPGA_CLK1_50) begin
        if (we && !bank) begin
            lab_store[addr[4:0]] <= wdata;
        end
        lab_q <= lab_store[addr[4:0]];
    end

    always @(posedge FPGA_CLK1_50) begin
        if (we && bank) begin
            block_store[addr] <= wdata;
        end
        block_q <= block_store[addr];
    end

    assign rdata = bank ? block_q : lab_q;
endmodule

module top (
    input wire FPGA_CLK1_50
);
    localparam [15:0] SIGNATURE = 16'hD710;

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
        .bank(hps_to_fpga[17]),
        .addr(hps_to_fpga[7:0]),
        .wdata(hps_to_fpga[15:8]),
        .rdata(rdata)
    );

    always @(posedge FPGA_CLK1_50) begin
        beat <= beat + 2'd1;
    end

    assign fpga_to_hps = {SIGNATURE, 6'b000000, beat, rdata};
endmodule
