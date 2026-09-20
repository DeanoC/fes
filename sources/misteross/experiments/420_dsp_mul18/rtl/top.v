module product_unit (
    input wire FPGA_CLK1_50,
    input wire [15:0] left,
    input wire [15:0] right,
    output reg [15:0] selected
);
    (* multstyle = "dsp" *) wire [31:0] wide_product;
    (* keep = "true" *) reg [1:0] beat;

    assign wide_product = left * right;

    initial begin
        selected = 16'h0000;
        beat = 2'b00;
    end

    always @(posedge FPGA_CLK1_50) begin
        beat <= beat + 2'd1;
        selected <= wide_product[15:0];
    end
endmodule

module top (
    input wire FPGA_CLK1_50
);
    localparam [15:0] SIGNATURE = 16'hD612;

    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;
    wire [15:0] selected;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    product_unit product (
        .FPGA_CLK1_50(FPGA_CLK1_50),
        .left(hps_to_fpga[15:0]),
        .right(hps_to_fpga[31:16]),
        .selected(selected)
    );

    assign fpga_to_hps = {SIGNATURE, selected};
endmodule
