module product_unit (
    input wire FPGA_CLK1_50,
    input wire [8:0] left,
    input wire [8:0] right,
    input wire high_select,
    input wire [1:0] mode,
    output reg [15:0] selected
);
    // Full-width operands: a set bit 8 reads as negative when signed and as
    // 256..511 when unsigned, so each product distinguishes the DSP block's
    // signed and unsigned configurations.
    (* keep = "true", multstyle = "dsp" *) wire signed [17:0] signed9 = $signed(left) * $signed(right);
    (* keep = "true", multstyle = "dsp" *) wire [17:0] unsigned9 = left * right;

    wire signed [17:0] left18 = $signed({left, right});
    wire signed [17:0] right18 = $signed({right, left});
    (* keep = "true", multstyle = "dsp" *) wire signed [35:0] signed18 = left18 * right18;

    wire signed [26:0] left27 = $signed({left, right, left});
    wire signed [26:0] right27 = $signed({right, left, right});
    (* keep = "true", multstyle = "dsp" *) wire signed [53:0] signed27 = left27 * right27;

    // Every product is registered unconditionally so Yosys cannot share one
    // multiplier between the mutually exclusive views; each mode keeps its
    // own DSP cell. The selected half therefore appears two clocks after a
    // GPO write.
    reg signed [17:0] signed9_q;
    reg [17:0] unsigned9_q;
    reg signed [35:0] signed18_q;
    reg signed [53:0] signed27_q;

    wire [31:0] view = (mode == 2'd0) ? {14'd0, signed9_q} :
                       (mode == 2'd1) ? {14'd0, unsigned9_q} :
                       (mode == 2'd2) ? signed18_q[31:0] :
                                        signed27_q[31:0];

    initial begin
        signed9_q = 18'd0;
        unsigned9_q = 18'd0;
        signed18_q = 36'd0;
        signed27_q = 54'd0;
        selected = 16'h0000;
    end

    always @(posedge FPGA_CLK1_50) begin
        signed9_q <= signed9;
        unsigned9_q <= unsigned9;
        signed18_q <= signed18;
        signed27_q <= signed27;
        selected <= high_select ? view[31:16] : view[15:0];
    end
endmodule

module top (
    input wire FPGA_CLK1_50
);
    localparam [15:0] SIGNATURE = 16'hD61A;

    wire [31:0] fpga_to_hps;
    wire [31:0] hps_to_fpga;
    wire [15:0] selected;

    cyclonev_hps_interface_mpu_general_purpose hps_gp (
        .gp_in(fpga_to_hps),
        .gp_out(hps_to_fpga)
    );

    product_unit product (
        .FPGA_CLK1_50(FPGA_CLK1_50),
        .left(hps_to_fpga[8:0]),
        .right(hps_to_fpga[17:9]),
        .high_select(hps_to_fpga[18]),
        .mode(hps_to_fpga[20:19]),
        .selected(selected)
    );

    assign fpga_to_hps = {SIGNATURE, selected};
endmodule
