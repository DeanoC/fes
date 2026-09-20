`ifndef VERILATOR
(* keep, blackbox *)
module dsp18x19 (
    input wire [17:0] A,
    input wire [18:0] B,
    input wire [17:0] C,
    input wire [18:0] D,
    output wire [73:0] Y
);
endmodule
`endif

module product_unit (
    input wire FPGA_CLK1_50,
    input wire [7:0] left,
    input wire [7:0] right,
    input wire [7:0] other,
    input wire select_second,
    output reg [15:0] selected
);
    wire [73:0] wide;
    (* keep = "true" *) reg [1:0] beat;

    (* keep *)
    dsp18x19 mul (
        .A({10'b0, left}),
        .B({11'b0, right}),
        .C({10'b0, other}),
        .D(19'd3),
        .Y(wide)
    );

    initial begin
        selected = 16'h0000;
        beat = 2'b00;
    end

    always @(posedge FPGA_CLK1_50) begin
        beat <= beat + 2'd1;
        selected <= select_second ? wide[52:37] : wide[15:0];
    end
endmodule

module top (
    input wire FPGA_CLK1_50
);
    localparam [15:0] SIGNATURE = 16'hD619;

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
        .other(hps_to_fpga[23:16]),
        .select_second(hps_to_fpga[24]),
        .selected(selected)
    );

    assign fpga_to_hps = {SIGNATURE, selected};
endmodule
