// SPDX-License-Identifier: GPL-2.0-or-later
// Freeze-scaffold cart boundary. Port names match the existing static linker.
// FPGA_CLK1_50 is mapped to the shell's explicit 52 MHz socket clock at build.
module cart (
    input wire FPGA_CLK1_50,
    input wire [36:0] plug_addr,
    output wire [16:0] plug_rdata
);
    zx81_ram_pack memory (
        .clock(FPGA_CLK1_50), .address(plug_addr[13:0]),
        .write_data(plug_addr[21:14]), .write_enable(plug_addr[22]),
        .peek_address(plug_addr[36:23]),
        .read_data(plug_rdata[7:0]), .peek_data(plug_rdata[15:8])
    );
    // A real retained register crosses the boundary; a top-level constant
    // would disappear during independent synthesis and could not be linked.
    (* keep *) MISTRAL_FF presence (
        .CLK(FPGA_CLK1_50), .DATAIN(1'b1), .Q(plug_rdata[16]),
        .ACLR(1'b1), .ENA(1'b1), .SCLR(1'b0), .SLOAD(1'b0), .SDATA(1'b0)
    );
endmodule
