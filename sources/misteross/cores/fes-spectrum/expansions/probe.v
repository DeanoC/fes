// SPDX-License-Identifier: GPL-2.0-or-later
// Open FES ZX Spectrum probe card for one physical edge socket.
//
// The card is built separately and linked into one socket of the frozen
// shell. Its ports are the socket plugs: FPGA_CLK1_50 is spliced onto the
// shell system clock, plug_addr is the registered request word and
// plug_rdata the response word (spectrum_bus.vh).
//
// Socket N (1..4) owns four fully decoded ports at $E0+(N-1)*4:
//   +0  card id $F5
//   +1  scratch register
//   +2  access counter
//   +3  socket index
// The card does not claim ROMCS, NMI or WAIT.
`include "spectrum_bus.vh"

module cart #(
    parameter integer SOCKET = 1
) (
    input  wire FPGA_CLK1_50,
    input  wire [`SP_BUS_REQ-1:0] plug_addr,
    output wire [`SP_BUS_RSP-1:0] plug_rdata
);
    wire clk = FPGA_CLK1_50;
    wire [15:0] addr = plug_addr[`SP_BUS_A];
    wire [7:0] wdata = plug_addr[`SP_BUS_D];
    wire iorq = plug_addr[`SP_BUS_IORQ];
    wire rd = plug_addr[`SP_BUS_RD];
    wire wr = plug_addr[`SP_BUS_WR];
    wire strobe = plug_addr[`SP_BUS_STROBE];
    wire bus_reset = plug_addr[`SP_BUS_RESET];
    localparam [7:0] BASE = 8'hE0 + (SOCKET - 1) * 4;

    wire select = iorq && addr[7:2] == BASE[7:2];
    wire [1:0] offset = addr[1:0];
    reg [7:0] scratch = 8'h00;
    reg [7:0] counter = 8'h00;

    always @(posedge clk) begin
        if (bus_reset) begin
            scratch <= 8'h00;
            counter <= 8'h00;
        end else if (strobe && select) begin
            counter <= counter + 8'h01;
            if (wr && offset == 2'd1)
                scratch <= wdata;
        end
    end

    reg [7:0] rdata;
    always @* begin
        case (offset)
            2'd0: rdata = 8'hF5;
            2'd1: rdata = scratch;
            2'd2: rdata = counter;
            default: rdata = SOCKET[7:0];
        endcase
    end

    wire drive = select && rd;
    assign plug_rdata[`SP_BUS_RDATA] = drive ? rdata : 8'h00;
    assign plug_rdata[`SP_BUS_DRIVE] = drive;
    assign plug_rdata[`SP_BUS_ROMCS] = 1'b0;
    assign plug_rdata[`SP_BUS_NMI] = 1'b0;
    assign plug_rdata[`SP_BUS_WAIT] = 1'b0;
    assign plug_rdata[`SP_BUS_AUDIO] = 16'h0000;
endmodule
