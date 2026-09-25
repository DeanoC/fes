// SPDX-License-Identifier: GPL-2.0-or-later
// Opcode SGM memory-window and AY-port decode. Physical RAM is in the shell.
`include "coleco_bus_v2_pack.vh"
module sgm_control (
    input wire clk,
    input wire [`COLECO_V2_BUS_REQ-1:0] request,
    output wire ram_claim,
    output wire ay_address_write,
    output wire ay_data_write,
    output wire ay_data_read
);
    wire [15:0] address = request[`COLECO_V2_BUS_A];
    wire [7:0] data = request[`COLECO_V2_BUS_DWR];
    wire reset = request[`COLECO_V2_BUS_RESET];
    wire memory_cycle = !request[`COLECO_V2_BUS_MREQ_N] &&
                        (!request[`COLECO_V2_BUS_RD_N] || !request[`COLECO_V2_BUS_WR_N]);
    wire io_write = !request[`COLECO_V2_BUS_IORQ_N] && !request[`COLECO_V2_BUS_WR_N];
    wire io_read = !request[`COLECO_V2_BUS_IORQ_N] && !request[`COLECO_V2_BUS_RD_N];
    wire lower_selected = address[15:13] == 3'b000;
    wire upper_selected = !address[15] && !lower_selected;
    reg lower_enabled = 0;
    reg upper_enabled = 0;
    reg io_write_seen = 0;
    wire io_write_pulse = io_write && !io_write_seen && !reset;

    always @(posedge clk) begin
        if (reset) begin
            lower_enabled <= 0;
            upper_enabled <= 0;
            io_write_seen <= 0;
        end else begin
            if (!io_write) io_write_seen <= 0;
            else if (io_write_pulse) io_write_seen <= 1;
            if (io_write_pulse && address[7:0] == 8'h53)
                upper_enabled <= data[0];
            if (io_write_pulse && address[7:0] == 8'h7f)
                lower_enabled <= !data[1];
        end
    end
    assign ram_claim = !reset && memory_cycle &&
                       ((lower_selected && lower_enabled) ||
                        (upper_selected && upper_enabled));
    assign ay_address_write = io_write_pulse && address[7:0] == 8'h50;
    assign ay_data_write = io_write_pulse && address[7:0] == 8'h51;
    assign ay_data_read = !reset && io_read && address[7:0] == 8'h52;
endmodule
