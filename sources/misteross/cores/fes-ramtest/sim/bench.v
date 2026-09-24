// SPDX-License-Identifier: GPL-2.0-or-later
// Connects the utility core to a behavioral SDRAM chip. HPS DDR uses the
// cyclonev_hps_interface_fpga2sdram stand-in compiled beside this bench.
module bench (
    input wire FPGA_CLK1_50,
    output wire HDMI_TX_DE,
    output wire [23:0] HDMI_TX_D
);
    wire sdram_clk, sdram_cke, sdram_ncs, sdram_nras, sdram_ncas, sdram_nwe;
    wire sdram_dqml, sdram_dqmh;
    wire [1:0] sdram_ba;
    wire [12:0] sdram_a;
    wire [15:0] sdram_dq;
    wire hdmi_clk, hdmi_hs, hdmi_vs;
    wire hdmi_scl, hdmi_sda;

    top dut (
        .FPGA_CLK1_50(FPGA_CLK1_50),
        .HDMI_TX_CLK(hdmi_clk),
        .HDMI_TX_DE(HDMI_TX_DE),
        .HDMI_TX_D(HDMI_TX_D),
        .HDMI_TX_HS(hdmi_hs),
        .HDMI_TX_VS(hdmi_vs),
        .HDMI_I2C_SCL(hdmi_scl),
        .HDMI_I2C_SDA(hdmi_sda),
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
