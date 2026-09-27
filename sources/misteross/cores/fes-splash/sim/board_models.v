// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"
// Controllable boundaries for simulating splash top.v. The test drives
// outclk_0 independently; this is not a 74.25 MHz, PLL-lock, or hardware model.
/* verilator lint_off DECLFILENAME */
/* verilator lint_off UNUSEDSIGNAL */
// Digital wired-AND boundary: external_low models another device pulling the
// pad low; released lines have a pull-up. Drive intent is exposed separately
// so the test rejects active-high output as well as incorrect pad feedback.
module MISTRAL_IO (
    input wire I,
    input wire OE,
    output wire O,
    inout wire PAD
);
    reg external_low /* verilator public_flat_rw */ = 1'b0;
    wire drive_low /* verilator public_flat_rd */ = OE && !I;
    wire drive_high /* verilator public_flat_rd */ = OE && I;
    assign PAD = OE ? I : 1'bz;
    assign O = (OE ? I : 1'b1) && !external_low;
endmodule

module cyclonev_hps_interface_peripheral_i2c (
    input wire scl,
    input wire sda,
    output reg out_clk,
    output reg out_data
);
    wire observed_scl /* verilator public_flat_rd */ = scl;
    wire observed_sda /* verilator public_flat_rd */ = sda;
    initial begin
        out_clk = 1'b0;
        out_data = 1'b0;
    end
endmodule

module pixel_pll (
    input wire refclk,
    input wire rst,
    output reg outclk_0,
    output wire locked
);
    assign locked = 1'b1;
    initial outclk_0 = 1'b0;
endmodule
/* verilator lint_on UNUSEDSIGNAL */
/* verilator lint_on DECLFILENAME */

// fpga2sdram stand-in. layout_ok compares the configuration inputs with the
// generated fes.memory.hps-ddr constants; idle says no command is presented.
module cyclonev_hps_interface_fpga2sdram (
    input wire [5:0] cfg_axi_mm_select,
    input wire [17:0] cfg_cport_rfifo_map,
    input wire [11:0] cfg_cport_type,
    input wire [17:0] cfg_cport_wfifo_map,
    input wire [11:0] cfg_port_width,
    input wire [15:0] cfg_rfifo_cport_map,
    input wire [15:0] cfg_wfifo_cport_map,
    input wire cmd_port_clk_0, input wire cmd_port_clk_1, input wire cmd_port_clk_2,
    input wire cmd_port_clk_3, input wire cmd_port_clk_4, input wire cmd_port_clk_5,
    input wire cmd_valid_0, input wire cmd_valid_1, input wire cmd_valid_2,
    input wire cmd_valid_3, input wire cmd_valid_4, input wire cmd_valid_5,
    input wire wr_clk_0, input wire wr_clk_1, input wire wr_clk_2, input wire wr_clk_3,
    input wire wr_valid_0, input wire wr_valid_1, input wire wr_valid_2, input wire wr_valid_3,
    input wire rd_clk_0, input wire rd_clk_1, input wire rd_clk_2, input wire rd_clk_3,
    input wire rd_ready_0, input wire rd_ready_1, input wire rd_ready_2, input wire rd_ready_3,
    input wire wrack_ready_0, input wire wrack_ready_1, input wire wrack_ready_2,
    input wire wrack_ready_3, input wire wrack_ready_4, input wire wrack_ready_5
);
    localparam [31:0] PORT_WIDTH = `FES_APPLICATION_HPS_DDR_CFG_PORT_WIDTH;
    localparam [31:0] CPORT_TYPE = `FES_APPLICATION_HPS_DDR_CFG_CPORT_TYPE;
    localparam [31:0] CPORT_WFIFO_MAP = `FES_APPLICATION_HPS_DDR_CFG_CPORT_WFIFO_MAP;
    localparam [31:0] CPORT_RFIFO_MAP = `FES_APPLICATION_HPS_DDR_CFG_CPORT_RFIFO_MAP;
    localparam [31:0] WFIFO_CPORT_MAP = `FES_APPLICATION_HPS_DDR_CFG_WFIFO_CPORT_MAP;
    localparam [31:0] RFIFO_CPORT_MAP = `FES_APPLICATION_HPS_DDR_CFG_RFIFO_CPORT_MAP;
    localparam [31:0] AXI_MM_SELECT = `FES_APPLICATION_HPS_DDR_CFG_AXI_MM_SELECT;
    wire layout_ok = cfg_port_width == PORT_WIDTH[11:0] && cfg_cport_type == CPORT_TYPE[11:0]
        && cfg_cport_wfifo_map == CPORT_WFIFO_MAP[17:0] && cfg_cport_rfifo_map == CPORT_RFIFO_MAP[17:0]
        && cfg_wfifo_cport_map == WFIFO_CPORT_MAP[15:0] && cfg_rfifo_cport_map == RFIFO_CPORT_MAP[15:0]
        && cfg_axi_mm_select == AXI_MM_SELECT[5:0];
    wire idle = !(cmd_valid_0 | cmd_valid_1 | cmd_valid_2 | cmd_valid_3 | cmd_valid_4 | cmd_valid_5
        | wr_valid_0 | wr_valid_1 | wr_valid_2 | wr_valid_3);
endmodule
