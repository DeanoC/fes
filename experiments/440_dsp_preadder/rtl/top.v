`ifndef VERILATOR
(* keep, blackbox *)
module dsp9_preadder (
    input wire [8:0] A,
    input wire [8:0] B,
    input wire [8:0] Z,
    output wire [17:0] Y
);
endmodule
`endif

module product_unit (
    input wire FPGA_CLK1_50,
    input wire [7:0] left,
    input wire [7:0] right,
    input wire [7:0] preadd,
    input wire high_select,
    output reg [7:0] selected,
    output reg [1:0] beat
);
    wire [17:0] wide_product;

    (* keep *)
    dsp9_preadder preadder (
        .A({1'b0, left}),
        .B({1'b0, right}),
        .Z({1'b0, preadd}),
        .Y(wide_product)
    );

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
    localparam [15:0] SIGNATURE = 16'hD614;

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
        .preadd(hps_to_fpga[23:16]),
        .high_select(hps_to_fpga[24]),
        .selected(selected),
        .beat(beat)
    );

    assign fpga_to_hps = {SIGNATURE, 6'b000000, beat, selected};
endmodule
