module bench (
    input wire FPGA_CLK1_50
);
    wire sdram_clk;
    wire sdram_cke;
    wire sdram_ncs;
    wire sdram_nras;
    wire sdram_ncas;
    wire sdram_nwe;
    wire sdram_dqml;
    wire sdram_dqmh;
    wire [1:0] sdram_ba;
    wire [12:0] sdram_a;
    wire [15:0] sdram_dq;

    top dut (
        .FPGA_CLK1_50(FPGA_CLK1_50),
        .SDRAM_CLK(sdram_clk),
        .SDRAM_CKE(sdram_cke),
        .SDRAM_nCS(sdram_ncs),
        .SDRAM_nRAS(sdram_nras),
        .SDRAM_nCAS(sdram_ncas),
        .SDRAM_nWE(sdram_nwe),
        .SDRAM_DQML(sdram_dqml),
        .SDRAM_DQMH(sdram_dqmh),
        .SDRAM_BA(sdram_ba),
        .SDRAM_A(sdram_a),
        .SDRAM_DQ(sdram_dq)
    );

    sdram_model chip (
        .clk(sdram_clk),
        .cke(sdram_cke),
        .ncs(sdram_ncs),
        .nras(sdram_nras),
        .ncas(sdram_ncas),
        .nwe(sdram_nwe),
        .ba(sdram_ba),
        .a(sdram_a),
        .dqml(sdram_dqml),
        .dqmh(sdram_dqmh),
        .dq(sdram_dq)
    );
endmodule
