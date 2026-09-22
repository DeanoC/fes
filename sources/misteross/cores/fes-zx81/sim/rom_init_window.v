// SPDX-License-Identifier: GPL-2.0-or-later
// The ZX81 machine ROM port from zx81_machine.sv: zx81_dpram 16384x8,
// address {1'b0, rom_a[12], rom_a[11:0]}, non-Quartus init zx8x.hex.
// The low 8 KiB is BASIC.
module rom_init_window (
    input wire clk_sys,
    input wire [12:0] addr,
    output wire [7:0] data
);
    localparam ROM_INIT = "cores/fes-zx81/rtl/zx8x.hex";

    zx81_dpram #(
        .ADDRWIDTH(14),
        .NUMWORDS(16384),
        .MEM_INIT_FILE(ROM_INIT)
    ) rom (
        .clock(clk_sys),
        .address_a({1'b0, addr[12], addr[11:0]}),
        .data_a(8'h00),
        .wren_a(1'b0),
        .q_a(data),
        .address_b(14'd0),
        .data_b(8'h00),
        .wren_b(1'b0),
        .q_b()
    );
endmodule
