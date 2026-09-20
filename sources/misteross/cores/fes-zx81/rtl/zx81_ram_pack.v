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
`ifdef SYNTHESIS
    // Native TDP read muxes require live clocks on both ports. Generic
    // inference drops read-only CLK2, so retain the physical clock contract.
    wire [7:0] bank_read [0:15];
    wire [7:0] bank_peek [0:15];
    genvar bank;
    generate for (bank=0; bank<16; bank=bank+1) begin : banks
        wire [9:0] read_a, read_b;
        MISTRAL_M10K_TDP #(.CFG_ABITS(10), .CFG_DBITS(10), .CFG_ASYNC_READ(1)) memory (
            .CLK1(clock), .CLK2(clock),
            .A1ADDR(address[9:0]), .B1ADDR(peek_address[9:0]),
            .A1DATA({2'b0,write_data}), .B1DATA(10'b0),
            .A1Q(read_a), .B1Q(read_b),
            .A1EN(1'b1), .B1EN(1'b1),
            .A1WE(write_enable && address[13:10]==bank), .B1WE(1'b0),
            .ACLR0(1'b0), .ACLR1(1'b0)
        );
        assign bank_read[bank] = read_a[7:0];
        assign bank_peek[bank] = read_b[7:0];
    end endgenerate
    assign read_data = bank_read[address[13:10]];
    assign peek_data = bank_peek[peek_address[13:10]];
`else
    zx81_dpram #(.ADDRWIDTH(14), .NUMWORDS(16384)) memory (
        .clock(clock), .address_a(address), .data_a(write_data),
        .wren_a(write_enable), .q_a(read_data),
        .address_b(peek_address), .data_b(8'b0), .wren_b(1'b0), .q_b(peek_data)
    );
`endif
endmodule
