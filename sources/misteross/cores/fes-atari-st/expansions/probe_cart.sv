// SPDX-License-Identifier: GPL-3.0-or-later
// Independent probe card for the registered ST expansion connector. The
// compiler's cart clock port is reconnected to the shell socket clock.
`include "fes_atari_st_bus.vh"
module cart (
    input wire FPGA_CLK1_50,
    input wire [`FES_ATARI_ST_BUS_REQUEST_BITS-1:0] plug_addr,
    output wire [`FES_ATARI_ST_BUS_RESPONSE_BITS-1:0] plug_rdata
);
    wire [15:0] rdata;
    wire ack, berr;
    st_probe probe (
        .clk(FPGA_CLK1_50), .reset(plug_addr[`FES_ATARI_ST_BUS_RESET_BIT]),
        .enable(1'b1), .wait_cycles(8'd4),
        .req(plug_addr[`FES_ATARI_ST_BUS_REQUEST_BIT]),
        .addr(plug_addr[`FES_ATARI_ST_BUS_ADDRESS_LOW +: `FES_ATARI_ST_BUS_ADDRESS_BITS]),
        .wdata(plug_addr[`FES_ATARI_ST_BUS_WRITE_DATA_LOW +: 16]),
        .byte_enable(plug_addr[`FES_ATARI_ST_BUS_BYTE_ENABLE_LOW +: 2]),
        .write(plug_addr[`FES_ATARI_ST_BUS_WRITE_BIT]),
        .rdata(rdata), .ack(ack), .berr(berr),
        .scratch(), .long_value(), .write_count()
    );
    // Always present, no asserted interrupt, and reserved response bits zero.
    assign plug_rdata = (32'd1 << `FES_ATARI_ST_BUS_PRESENT_BIT) |
                       (32'(berr) << `FES_ATARI_ST_BUS_BUS_ERROR_BIT) |
                       (32'(ack) << `FES_ATARI_ST_BUS_ACK_BIT) |
                       (32'(rdata) << `FES_ATARI_ST_BUS_READ_DATA_LOW);
endmodule
