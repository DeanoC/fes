// SPDX-License-Identifier: GPL-2.0-or-later
// Cyclone V hard-atom declaration for the locked ZX81 Yosys lane. Its native
// library predates the DDR atom; nextpnr already implements this exact atom.
// Ports match the complete declaration introduced in DeanoC/yosys commit
// 54ea7109f (techlibs/intel_alm/common/megafunction_bb.v). This source is read
// with -lib only by ZX81; other producers retain their compiler declarations.
// No behavior, addresses, allocation or physical control are implemented here.
(* blackbox, keep *)
module cyclonev_hps_interface_fpga2sdram(
    cfg_axi_mm_select, cfg_cport_rfifo_map, cfg_cport_type, cfg_cport_wfifo_map,
    cfg_port_width, cfg_rfifo_cport_map, cfg_wfifo_cport_map,
    cmd_port_clk_0, cmd_port_clk_1, cmd_port_clk_2, cmd_port_clk_3, cmd_port_clk_4, cmd_port_clk_5,
    cmd_valid_0, cmd_valid_1, cmd_valid_2, cmd_valid_3, cmd_valid_4, cmd_valid_5,
    cmd_data_0, cmd_data_1, cmd_data_2, cmd_data_3, cmd_data_4, cmd_data_5,
    cmd_ready_0, cmd_ready_1, cmd_ready_2, cmd_ready_3, cmd_ready_4, cmd_ready_5,
    wrack_ready_0, wrack_ready_1, wrack_ready_2, wrack_ready_3, wrack_ready_4, wrack_ready_5,
    wrack_data_0, wrack_data_1, wrack_data_2, wrack_data_3, wrack_data_4, wrack_data_5,
    wrack_valid_0, wrack_valid_1, wrack_valid_2, wrack_valid_3, wrack_valid_4, wrack_valid_5,
    wr_clk_0, wr_clk_1, wr_clk_2, wr_clk_3,
    wr_valid_0, wr_valid_1, wr_valid_2, wr_valid_3,
    wr_data_0, wr_data_1, wr_data_2, wr_data_3,
    wr_ready_0, wr_ready_1, wr_ready_2, wr_ready_3,
    rd_clk_0, rd_clk_1, rd_clk_2, rd_clk_3,
    rd_ready_0, rd_ready_1, rd_ready_2, rd_ready_3,
    rd_data_0, rd_data_1, rd_data_2, rd_data_3,
    rd_valid_0, rd_valid_1, rd_valid_2, rd_valid_3
);

input [5:0] cfg_axi_mm_select;
input [17:0] cfg_cport_rfifo_map;
input [11:0] cfg_cport_type;
input [17:0] cfg_cport_wfifo_map;
input [11:0] cfg_port_width;
input [15:0] cfg_rfifo_cport_map;
input [15:0] cfg_wfifo_cport_map;
input cmd_port_clk_0, cmd_port_clk_1, cmd_port_clk_2, cmd_port_clk_3, cmd_port_clk_4, cmd_port_clk_5;
input cmd_valid_0, cmd_valid_1, cmd_valid_2, cmd_valid_3, cmd_valid_4, cmd_valid_5;
input [59:0] cmd_data_0, cmd_data_1, cmd_data_2, cmd_data_3, cmd_data_4, cmd_data_5;
output cmd_ready_0, cmd_ready_1, cmd_ready_2, cmd_ready_3, cmd_ready_4, cmd_ready_5;
input wrack_ready_0, wrack_ready_1, wrack_ready_2, wrack_ready_3, wrack_ready_4, wrack_ready_5;
output [9:0] wrack_data_0, wrack_data_1, wrack_data_2, wrack_data_3, wrack_data_4, wrack_data_5;
output wrack_valid_0, wrack_valid_1, wrack_valid_2, wrack_valid_3, wrack_valid_4, wrack_valid_5;
input wr_clk_0, wr_clk_1, wr_clk_2, wr_clk_3;
input wr_valid_0, wr_valid_1, wr_valid_2, wr_valid_3;
input [89:0] wr_data_0, wr_data_1, wr_data_2, wr_data_3;
output wr_ready_0, wr_ready_1, wr_ready_2, wr_ready_3;
input rd_clk_0, rd_clk_1, rd_clk_2, rd_clk_3;
input rd_ready_0, rd_ready_1, rd_ready_2, rd_ready_3;
output [79:0] rd_data_0, rd_data_1, rd_data_2, rd_data_3;
output rd_valid_0, rd_valid_1, rd_valid_2, rd_valid_3;

endmodule
