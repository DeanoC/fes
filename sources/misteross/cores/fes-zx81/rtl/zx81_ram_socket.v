// SPDX-License-Identifier: GPL-2.0-or-later
// ZX81 memory socket: the empty shell contains only the original 1 KiB RAM.
// The independent pack owns the complete 16 KiB window when present. Presence
// is a linked fabric signal, never a host command or live hot-plug control.
module zx81_ram_socket (
    input wire clock,
    input wire [13:0] address,
    input wire [7:0] write_data,
    input wire write_enable,
    input wire [13:0] peek_address,
    output wire [7:0] read_data,
    output wire [7:0] peek_data,
    input wire pack_present,
    input wire [7:0] pack_data,
    input wire [7:0] pack_peek_data,
    output wire [13:0] pack_address,
    output wire [7:0] pack_write_data,
    output wire pack_write_enable,
    output wire [13:0] pack_peek_address
);
    wire [7:0] internal_data, internal_peek;
    zx81_dpram #(.ADDRWIDTH(10), .NUMWORDS(1024)) internal_ram (
        .clock(clock), .address_a(address[9:0]), .data_a(write_data),
        .wren_a(write_enable && !present), .q_a(internal_data),
        .address_b(peek_address[9:0]), .data_b(8'b0), .wren_b(1'b0),
        .q_b(internal_peek)
    );
    // Fixed boundary registers are also the physical freeze-scaffold plugs.
    // A CPU bus phase lasts sixteen system clocks, leaving the two boundary
    // stages stable before sampling. One-cycle tape writes are delayed intact.
    wire [36:0] plug_addr_d = {peek_address, write_enable && present, write_data, address};
    wire [36:0] plug_addr;
    wire [16:0] plug_rdata_d = {pack_present, pack_peek_data, pack_data};
    wire [16:0] plug_rdata;
    wire present = plug_rdata[16];
`define ZX81_SOCKET_FF(NAME, SITE, D, QOUT) \
    (* keep, BEL = SITE *) MISTRAL_FF NAME ( \
        .CLK(clock), .DATAIN(D), .Q(QOUT), .ACLR(1'b1), .ENA(1'b1), \
        .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0));
    `ZX81_SOCKET_FF(plug_addr_ff_0, "MISTRAL_FF.24.1.2", plug_addr_d[0], plug_addr[0])
    `ZX81_SOCKET_FF(plug_addr_ff_1, "MISTRAL_FF.24.1.4", plug_addr_d[1], plug_addr[1])
    `ZX81_SOCKET_FF(plug_addr_ff_2, "MISTRAL_FF.24.1.8", plug_addr_d[2], plug_addr[2])
    `ZX81_SOCKET_FF(plug_addr_ff_3, "MISTRAL_FF.24.1.10", plug_addr_d[3], plug_addr[3])
    `ZX81_SOCKET_FF(plug_addr_ff_4, "MISTRAL_FF.24.1.14", plug_addr_d[4], plug_addr[4])
    `ZX81_SOCKET_FF(plug_addr_ff_5, "MISTRAL_FF.24.1.16", plug_addr_d[5], plug_addr[5])
    `ZX81_SOCKET_FF(plug_addr_ff_6, "MISTRAL_FF.24.1.20", plug_addr_d[6], plug_addr[6])
    `ZX81_SOCKET_FF(plug_addr_ff_7, "MISTRAL_FF.24.1.22", plug_addr_d[7], plug_addr[7])
    `ZX81_SOCKET_FF(plug_addr_ff_8, "MISTRAL_FF.24.1.26", plug_addr_d[8], plug_addr[8])
    `ZX81_SOCKET_FF(plug_addr_ff_9, "MISTRAL_FF.24.1.28", plug_addr_d[9], plug_addr[9])
    `ZX81_SOCKET_FF(plug_addr_ff_10, "MISTRAL_FF.24.1.32", plug_addr_d[10], plug_addr[10])
    `ZX81_SOCKET_FF(plug_addr_ff_11, "MISTRAL_FF.24.1.34", plug_addr_d[11], plug_addr[11])
    `ZX81_SOCKET_FF(plug_addr_ff_12, "MISTRAL_FF.24.1.38", plug_addr_d[12], plug_addr[12])
    `ZX81_SOCKET_FF(plug_addr_ff_13, "MISTRAL_FF.24.1.40", plug_addr_d[13], plug_addr[13])
    `ZX81_SOCKET_FF(plug_addr_ff_14, "MISTRAL_FF.24.1.44", plug_addr_d[14], plug_addr[14])
    `ZX81_SOCKET_FF(plug_addr_ff_15, "MISTRAL_FF.24.1.46", plug_addr_d[15], plug_addr[15])
    `ZX81_SOCKET_FF(plug_addr_ff_16, "MISTRAL_FF.24.1.50", plug_addr_d[16], plug_addr[16])
    `ZX81_SOCKET_FF(plug_addr_ff_17, "MISTRAL_FF.24.1.52", plug_addr_d[17], plug_addr[17])
    `ZX81_SOCKET_FF(plug_addr_ff_18, "MISTRAL_FF.24.1.56", plug_addr_d[18], plug_addr[18])
    `ZX81_SOCKET_FF(plug_addr_ff_19, "MISTRAL_FF.24.1.58", plug_addr_d[19], plug_addr[19])
    `ZX81_SOCKET_FF(plug_addr_ff_20, "MISTRAL_FF.24.2.2", plug_addr_d[20], plug_addr[20])
    `ZX81_SOCKET_FF(plug_addr_ff_21, "MISTRAL_FF.24.2.4", plug_addr_d[21], plug_addr[21])
    `ZX81_SOCKET_FF(plug_addr_ff_22, "MISTRAL_FF.24.2.8", plug_addr_d[22], plug_addr[22])
    `ZX81_SOCKET_FF(plug_addr_ff_23, "MISTRAL_FF.24.2.10", plug_addr_d[23], plug_addr[23])
    `ZX81_SOCKET_FF(plug_addr_ff_24, "MISTRAL_FF.24.2.14", plug_addr_d[24], plug_addr[24])
    `ZX81_SOCKET_FF(plug_addr_ff_25, "MISTRAL_FF.24.2.16", plug_addr_d[25], plug_addr[25])
    `ZX81_SOCKET_FF(plug_addr_ff_26, "MISTRAL_FF.24.2.20", plug_addr_d[26], plug_addr[26])
    `ZX81_SOCKET_FF(plug_addr_ff_27, "MISTRAL_FF.24.2.22", plug_addr_d[27], plug_addr[27])
    `ZX81_SOCKET_FF(plug_addr_ff_28, "MISTRAL_FF.24.2.26", plug_addr_d[28], plug_addr[28])
    `ZX81_SOCKET_FF(plug_addr_ff_29, "MISTRAL_FF.24.2.28", plug_addr_d[29], plug_addr[29])
    `ZX81_SOCKET_FF(plug_addr_ff_30, "MISTRAL_FF.24.2.32", plug_addr_d[30], plug_addr[30])
    `ZX81_SOCKET_FF(plug_addr_ff_31, "MISTRAL_FF.24.2.34", plug_addr_d[31], plug_addr[31])
    `ZX81_SOCKET_FF(plug_addr_ff_32, "MISTRAL_FF.24.2.38", plug_addr_d[32], plug_addr[32])
    `ZX81_SOCKET_FF(plug_addr_ff_33, "MISTRAL_FF.24.2.40", plug_addr_d[33], plug_addr[33])
    `ZX81_SOCKET_FF(plug_addr_ff_34, "MISTRAL_FF.24.2.44", plug_addr_d[34], plug_addr[34])
    `ZX81_SOCKET_FF(plug_addr_ff_35, "MISTRAL_FF.24.2.46", plug_addr_d[35], plug_addr[35])
    `ZX81_SOCKET_FF(plug_addr_ff_36, "MISTRAL_FF.24.2.50", plug_addr_d[36], plug_addr[36])
    `ZX81_SOCKET_FF(plug_rdata_ff_0, "MISTRAL_FF.28.1.2", plug_rdata_d[0], plug_rdata[0])
    `ZX81_SOCKET_FF(plug_rdata_ff_1, "MISTRAL_FF.28.2.2", plug_rdata_d[1], plug_rdata[1])
    `ZX81_SOCKET_FF(plug_rdata_ff_2, "MISTRAL_FF.28.3.2", plug_rdata_d[2], plug_rdata[2])
    `ZX81_SOCKET_FF(plug_rdata_ff_3, "MISTRAL_FF.28.4.2", plug_rdata_d[3], plug_rdata[3])
    `ZX81_SOCKET_FF(plug_rdata_ff_4, "MISTRAL_FF.28.5.2", plug_rdata_d[4], plug_rdata[4])
    `ZX81_SOCKET_FF(plug_rdata_ff_5, "MISTRAL_FF.28.6.2", plug_rdata_d[5], plug_rdata[5])
    `ZX81_SOCKET_FF(plug_rdata_ff_6, "MISTRAL_FF.28.7.2", plug_rdata_d[6], plug_rdata[6])
    `ZX81_SOCKET_FF(plug_rdata_ff_7, "MISTRAL_FF.28.8.2", plug_rdata_d[7], plug_rdata[7])
    `ZX81_SOCKET_FF(plug_rdata_ff_8, "MISTRAL_FF.28.9.2", plug_rdata_d[8], plug_rdata[8])
    `ZX81_SOCKET_FF(plug_rdata_ff_9, "MISTRAL_FF.28.10.2", plug_rdata_d[9], plug_rdata[9])
    `ZX81_SOCKET_FF(plug_rdata_ff_10, "MISTRAL_FF.28.11.2", plug_rdata_d[10], plug_rdata[10])
    `ZX81_SOCKET_FF(plug_rdata_ff_11, "MISTRAL_FF.28.12.2", plug_rdata_d[11], plug_rdata[11])
    `ZX81_SOCKET_FF(plug_rdata_ff_12, "MISTRAL_FF.28.13.2", plug_rdata_d[12], plug_rdata[12])
    `ZX81_SOCKET_FF(plug_rdata_ff_13, "MISTRAL_FF.28.14.2", plug_rdata_d[13], plug_rdata[13])
    `ZX81_SOCKET_FF(plug_rdata_ff_14, "MISTRAL_FF.28.15.2", plug_rdata_d[14], plug_rdata[14])
    `ZX81_SOCKET_FF(plug_rdata_ff_15, "MISTRAL_FF.28.16.2", plug_rdata_d[15], plug_rdata[15])
    `ZX81_SOCKET_FF(plug_rdata_ff_16, "MISTRAL_FF.28.17.2", plug_rdata_d[16], plug_rdata[16])
`undef ZX81_SOCKET_FF
    assign pack_address = plug_addr[13:0];
    assign pack_write_data = plug_addr[21:14];
    assign pack_write_enable = plug_addr[22];
    assign pack_peek_address = plug_addr[36:23];
    assign read_data = present ? plug_rdata[7:0] : internal_data;
    assign peek_data = present ? plug_rdata[15:8] : internal_peek;
endmodule
