// SPDX-License-Identifier: GPL-2.0-or-later
// Independently placed Opcode SGM. The v2 shell owns the 32 KiB M10K store.
`include "coleco_bus_v2_pack.vh"
module cart (
    input wire FPGA_CLK1_50,
    input wire [`COLECO_V2_BUS_REQ-1:0] plug_addr,
    output wire [`COLECO_V2_BUS_RSP-1:0] plug_rdata
);
    wire reset = plug_addr[`COLECO_V2_BUS_RESET];
    wire ram_claim;
    wire ay_address_write, ay_data_write, ay_data_read;
    wire [7:0] ay_read_data;
    wire signed [15:0] ay_sample;
    sgm_control control (
        .clk(FPGA_CLK1_50), .request(plug_addr),
        .ram_claim(ram_claim), .ay_address_write(ay_address_write),
        .ay_data_write(ay_data_write), .ay_data_read(ay_data_read)
    );
    sgm_ay ay (
        .clk(FPGA_CLK1_50), .reset(reset),
        .address_write(ay_address_write), .data_write(ay_data_write),
        .data_read(ay_data_read), .write_data(plug_addr[`COLECO_V2_BUS_DWR]),
        .read_data(ay_read_data), .sample_signed(ay_sample)
    );
    assign plug_rdata = reset ? 28'b0 :
                        {ay_sample, ram_claim, 2'b00, ay_data_read, ay_read_data};
endmodule
