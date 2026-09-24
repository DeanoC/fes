// SPDX-License-Identifier: GPL-2.0-or-later
// Original Coleco CPU-bus validation module. The clock pad is spliced to the
// selected shell system clock by the frozen-scaffold linker.
`include "coleco_bus_pack.vh"
module cart (
    input wire FPGA_CLK1_50,
    input wire [`COLECO_BUS_REQ-1:0] plug_addr,
    output wire [`COLECO_BUS_RSP-1:0] plug_rdata
);
    wire [15:0] cpu_addr = plug_addr[`COLECO_BUS_A];
    wire [7:0] cpu_dout = plug_addr[`COLECO_BUS_DWR];
    wire reset = plug_addr[`COLECO_BUS_RESET];
    wire mem_read = !plug_addr[`COLECO_BUS_MREQ_N] && !plug_addr[`COLECO_BUS_RD_N];
    wire mem_write = !plug_addr[`COLECO_BUS_MREQ_N] && !plug_addr[`COLECO_BUS_WR_N];
    wire io_read = !plug_addr[`COLECO_BUS_IORQ_N] && !plug_addr[`COLECO_BUS_RD_N];
    wire selected_mem = cpu_addr == 16'h2000 || cpu_addr == 16'h2001 || cpu_addr == 16'h2002;
    wire selected_io = cpu_addr[7:0] == 8'h40;

    reg [7:0] data_reg = 8'h5a;
    reg [5:0] wait_mask = 0;
    // Sixteen phases from a four-bit LFSR plus its zero state; no carry chain
    // is needed in the small routed cart rectangle.
    reg [3:0] wait_phase = 0;
    reg irq_enable = 0;
    wire wait_read = mem_read && cpu_addr == 16'h2000;
    always @(posedge FPGA_CLK1_50) begin
        if (reset) begin
            data_reg <= 8'h5a;
            wait_mask <= 0;
            wait_phase <= 0;
            irq_enable <= 0;
        end else begin
            if (mem_write && cpu_addr == 16'h2000)
                data_reg <= cpu_dout;
            if (mem_write && cpu_addr == 16'h2001) begin
                wait_mask <= cpu_dout[5:0];
                wait_phase <= 0;
            end else if (wait_read && wait_mask != 0) begin
                // The request is registered at the socket and the CPU only
                // samples WAIT on its slower enable. Keep the delay armed
                // until a read starts, then hold each mask bit for 16 system
                // clocks so even a one-bit request crosses a CPU enable.
                wait_phase <= wait_phase == 4'h0 ? 4'h1 :
                              wait_phase == 4'h8 ? 4'h0 :
                              {wait_phase[2:0], wait_phase[3] ^ wait_phase[2]};
                if (wait_phase == 4'h8)
                    wait_mask <= {wait_mask[4:0], 1'b0};
            end
            if (mem_write && cpu_addr == 16'h2002)
                irq_enable <= cpu_dout[0];
        end
    end

    wire claim = (mem_read && selected_mem) || (io_read && selected_io);
    wire [7:0] read_data = selected_io ? {7'b0, irq_enable} :
                           cpu_addr == 16'h2000 ? data_reg :
                           cpu_addr == 16'h2001 ? {2'b0, wait_mask} :
                           {7'b0, irq_enable};
    wire wait_request = wait_read && wait_mask != 0;
    assign plug_rdata = {irq_enable, wait_request, claim, read_data};
endmodule
