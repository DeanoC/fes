`ifndef VERILATOR
(* keep, blackbox *)
module dsp18_mac (
    input wire [17:0] A,
    input wire [17:0] B,
    input wire [35:0] C,
    output wire [35:0] Y
);
endmodule
`endif

module product_unit (
    input wire FPGA_CLK1_50,
    input wire [7:0] left,
    input wire [7:0] right,
    input wire [15:0] addend,
    output reg [15:0] selected
);
    wire [35:0] wide_product;
    (* keep = "true" *) reg [1:0] beat;

    (* keep *)
    dsp18_mac mac (
        .A({10'b0, left}),
        .B({10'b0, right}),
        .C({20'b0, addend}),
        .Y(wide_product)
    );

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
    localparam [15:0] SIGNATURE = 16'hD615;

    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;
    wire [15:0] selected;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    product_unit product (
        .FPGA_CLK1_50(FPGA_CLK1_50),
        .left(hps_to_fpga[7:0]),
        .right(hps_to_fpga[15:8]),
        .addend(hps_to_fpga[31:16]),
        .selected(selected)
    );

    assign fpga_to_hps = {SIGNATURE, selected};
endmodule
