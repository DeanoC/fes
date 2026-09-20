// SPDX-License-Identifier: GPL-2.0-or-later
// Independently synthesized writable 16 KiB expansion memory. The fixed
// socket wiring supplies clock and bus; no CPU, host mailbox or video here.
module zx81_ram_pack (
    input wire clock,
    input wire [13:0] address,
    input wire [7:0] write_data,
    input wire write_enable,
    input wire [13:0] peek_address,
    output wire [7:0] read_data,
    output wire [7:0] peek_data
);
    zx81_dpram #(.ADDRWIDTH(14), .NUMWORDS(16384)) memory (
        .clock(clock), .address_a(address), .data_a(write_data),
        .wren_a(write_enable), .q_a(read_data),
        .address_b(peek_address), .data_b(8'b0), .wren_b(1'b0), .q_b(peek_data)
    );
endmodule
