// SPDX-License-Identifier: GPL-2.0-or-later
// Original Bi-Pak Zon X-81: A0..A3 high, A4 low, A7 selects address/data.
// Write-only board. The AY8912 clock is the real edge CPU clock divided by
// two, not the FPGA transport clock. Reset follows the edge /RESET pin.
`include "zx81_bus_pack.vh"
module cart (
    input wire FPGA_CLK1_50,
    input wire [`ZX81_BUS_REQ-1:0] plug_addr,
    output wire [`ZX81_BUS_RSP-1:0] plug_rdata
);
    wire [15:0] cpu_a = plug_addr[`ZX81_BUS_A];
    wire [7:0] cpu_d = plug_addr[`ZX81_BUS_DWR];
    wire reset_n = plug_addr[`ZX81_BUS_RESET_N];
    wire cpu_clock = plug_addr[`ZX81_BUS_CPU_CLK];
    wire io_we = !plug_addr[`ZX81_BUS_IORQ_N] && !plug_addr[`ZX81_BUS_WR_N];
    wire decoded = (cpu_a[4:0] == 5'h0f);
    reg previous_write = 0, previous_clock = 0, divider = 0;
    wire write_event = io_we && !previous_write && decoded;
    // The card's 7472 toggles on the falling bus-clock edge.
    wire chip_ce = !cpu_clock && previous_clock && !divider;
    always @(posedge FPGA_CLK1_50) begin
        previous_clock <= cpu_clock;
        if (!reset_n) begin
            previous_write <= 0;
            divider <= 0;
        end else begin
            previous_write <= io_we;
            if (!cpu_clock && previous_clock) divider <= ~divider;
        end
    end
    wire [7:0] pcm;
    wire [14:0] unused_ym_levels;
    zonx_ay sound (
        .clk(FPGA_CLK1_50), .reset_n(reset_n), .chip_ce(chip_ce),
        .address_write(write_event && cpu_a[7]),
        .data_write(write_event && !cpu_a[7]), .data(cpu_d),
        .read_data(), .ym_levels(unused_ym_levels), .pcm(pcm)
    );
    // No CPU data readback on the physical board. peek_d is the shell's
    // digital replacement for the card's summed analog audio output.
    assign plug_rdata = {4'b0, pcm, 8'b0};
endmodule
