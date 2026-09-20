module rom_port (
    input wire FPGA_CLK1_50,
    input wire [7:0] addr,
    output reg [7:0] rdata
);
    localparam [7:0] PATTERN = 8'hA5;
    (* ramstyle = "M10K" *) reg [7:0] stored [0:255];
    integer index;

    initial begin
        for (index = 0; index < 256; index = index + 1) begin
            stored[index] = index[7:0] ^ PATTERN;
        end
        rdata = 8'h00;
    end

    always @(posedge FPGA_CLK1_50) begin
        rdata <= stored[addr];
    end
endmodule

module product_unit (
    input wire FPGA_CLK1_50,
    input wire [7:0] left,
    input wire [7:0] right,
    input wire high_select,
    output reg [7:0] selected
);
    (* multstyle = "dsp" *) wire [15:0] wide_product;

    assign wide_product = left * right;

    initial selected = 8'h00;

    always @(posedge FPGA_CLK1_50) begin
        selected <= high_select ? wide_product[15:8] : wide_product[7:0];
    end
endmodule

module top (
    input wire FPGA_CLK1_50
);
    localparam [15:0] SIGNATURE = 16'hD910;

    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;
    wire [7:0] rdata;
    wire [7:0] selected;
    reg [1:0] beat = 2'b00;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    rom_port rom_port (
        .FPGA_CLK1_50(FPGA_CLK1_50),
        .addr(hps_to_fpga[15:8]),
        .rdata(rdata)
    );

    product_unit product (
        .FPGA_CLK1_50(FPGA_CLK1_50),
        .left(hps_to_fpga[7:0]),
        .right(rdata),
        .high_select(hps_to_fpga[16]),
        .selected(selected)
    );

    always @(posedge FPGA_CLK1_50) begin
        beat <= beat + 2'd1;
    end

    assign fpga_to_hps = {SIGNATURE, 6'b000000, beat, selected};
endmodule
