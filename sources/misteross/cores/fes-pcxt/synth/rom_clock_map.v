// SPDX-License-Identifier: GPL-2.0-or-later
// The locked memory mapper leaves a ROM's unused write clock undefined. Native
// M10K packing requires that pin to be connected. Only a provably disabled
// write port may share its read clock; INIT and registered read timing survive.
module MISTRAL_M10K (CLK1, CLK2, A1ADDR, A1DATA, A1EN, B1ADDR, B1DATA, B1EN);
    parameter INIT = 0;
    parameter CFG_ABITS = 10, CFG_DBITS = 10;
    parameter CFG_RD_ABITS = CFG_ABITS, CFG_RD_DBITS = CFG_DBITS;
    parameter CFG_MIXED_WIDTH = 0, CFG_DUAL_CLOCK = 0, CFG_BYTE_ENABLE = 0, CFG_ASYNC_READ = 0;
    parameter _TECHMAP_CONSTMSK_CLK1_ = 0;
    parameter _TECHMAP_CONSTMSK_A1EN_ = 0, _TECHMAP_CONSTVAL_A1EN_ = 0;
    input CLK1, CLK2, A1EN, B1EN;
    input [CFG_ABITS-1:0] A1ADDR;
    input [CFG_RD_ABITS-1:0] B1ADDR;
    input [CFG_DBITS-1:0] A1DATA;
    output [CFG_RD_DBITS-1:0] B1DATA;
    // Ordinary narrow M10K writes are active low; byte/40-bit writes are high.
    localparam DISABLED = (CFG_BYTE_ENABLE || CFG_DBITS == 40) ? 0 : 1;
    wire _TECHMAP_FAIL_ = !CFG_DUAL_CLOCK || CFG_ASYNC_READ || CFG_BYTE_ENABLE || CFG_MIXED_WIDTH ||
        !_TECHMAP_CONSTMSK_CLK1_ || !_TECHMAP_CONSTMSK_A1EN_ ||
        _TECHMAP_CONSTVAL_A1EN_ !== DISABLED;
    MISTRAL_M10K #(.INIT(INIT), .CFG_ABITS(CFG_ABITS), .CFG_DBITS(CFG_DBITS),
        .CFG_RD_ABITS(CFG_RD_ABITS), .CFG_RD_DBITS(CFG_RD_DBITS),
        .CFG_MIXED_WIDTH(CFG_MIXED_WIDTH), .CFG_DUAL_CLOCK(CFG_DUAL_CLOCK),
        .CFG_BYTE_ENABLE(CFG_BYTE_ENABLE), .CFG_ASYNC_READ(CFG_ASYNC_READ)) _TECHMAP_REPLACE_ (
        .CLK1(CLK2), .CLK2(CLK2), .A1ADDR(A1ADDR), .A1DATA(A1DATA), .A1EN(A1EN),
        .B1ADDR(B1ADDR), .B1DATA(B1DATA), .B1EN(B1EN));
endmodule
