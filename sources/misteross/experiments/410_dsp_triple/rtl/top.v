(* keep_hierarchy *)
module packed_product (
    input wire [7:0] left,
    input wire [7:0] right,
    output wire [15:0] wide_product
);
    (* keep = "true", multstyle = "dsp" *) wire [15:0] product;

    assign product = left * right;
    assign wide_product = product;
endmodule

module product_unit (
    input wire FPGA_CLK1_50,
    input wire [7:0] left,
    input wire [7:0] right,
    input wire high_select,
    input wire [1:0] lane,
    output reg [7:0] selected,
    output reg [1:0] beat
);
    wire [15:0] product_ab;
    wire [15:0] product_anotb;
    wire [15:0] product_xor;
    wire [15:0] wide_product;

    packed_product lane0 (
        .left(left),
        .right(right),
        .wide_product(product_ab)
    );
    packed_product lane1 (
        .left(left),
        .right(~right),
        .wide_product(product_anotb)
    );
    packed_product lane2 (
        .left(left),
        .right(right ^ 8'h01),
        .wide_product(product_xor)
    );

    assign wide_product = (lane == 2'd1) ? product_anotb :
                          (lane == 2'd2) ? product_xor :
                          product_ab;

    initial begin
        selected = 8'h00;
        beat = 2'b00;
    end

    always @(posedge FPGA_CLK1_50) begin
        beat <= beat + 2'd1;
        selected <= high_select ? wide_product[15:8] : wide_product[7:0];
    end
endmodule

module top (
    input wire FPGA_CLK1_50
);
    localparam [15:0] SIGNATURE = 16'hD611;

    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;
    wire [7:0] selected;
    wire [1:0] beat;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    product_unit product (
        .FPGA_CLK1_50(FPGA_CLK1_50),
        .left(hps_to_fpga[7:0]),
        .right(hps_to_fpga[15:8]),
        .high_select(hps_to_fpga[16]),
        .lane(hps_to_fpga[18:17]),
        .selected(selected),
        .beat(beat)
    );

    assign fpga_to_hps = {SIGNATURE, 6'b000000, beat, selected};
endmodule
