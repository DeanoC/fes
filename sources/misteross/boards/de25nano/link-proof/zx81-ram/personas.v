// SPDX-License-Identifier: GPL-2.0-or-later
// Board-local registered adapters; the memory is the actual ZX81 RAM-pack RTL.
`include "generated/fes_de25_pr_control.vh"
`include "zx81_bus_pack.vh"
module fes_link_zx81_persona #(
    parameter RAM = 0,
    parameter [31:0] ID = `FES_DE25_PR_CONTROL_PERSONA_A
)(
    input wire clk, reset, req,
    input wire [7:0] address,
    input wire [45:0] bus_request,
    output reg rsp_valid,
    output reg [31:0] rsp_data,
    (* preserve, dont_merge, noprune *) output reg [19:0] bus_response
);
    (* preserve, dont_merge *) reg request_q, reset_q;
    (* preserve, dont_merge *) reg [7:0] address_q;
    (* preserve, dont_merge *) reg [45:0] bus_q;
    reg valid_q, valid_r;
    reg [7:0] address_r;
    reg [15:0] checksum;
    reg [31:0] response_data_q;
    wire [7:0] read_data, peek_data;
    wire write_enable = !reset_q && bus_q[`ZX81_BUS_RESET_N] &&
        !bus_q[`ZX81_BUS_MREQ_N] && !bus_q[`ZX81_BUS_WR_N] &&
        bus_q[15:14] == 2'b01;
    generate if (RAM) begin : ram_pack
        zx81_ram_pack memory (
            .clock(clk), .address(bus_q[13:0]), .write_data(bus_q[`ZX81_BUS_DWR]),
            .write_enable(write_enable), .peek_address(bus_q[`ZX81_BUS_PEEK_A]),
            .read_data(read_data), .peek_data(peek_data));
    end else begin : empty_slot
        assign read_data = 8'd0;
        assign peek_data = 8'd0;
    end endgenerate
    // Read data and the presence flag describe the same registered request.
    // No combinational PR input or output reaches the other partition.
    always @(posedge clk) begin
        request_q <= req;
        reset_q <= reset;
        address_q <= address;
        bus_q <= bus_request;
        bus_response <= {RAM != 0, 3'b000, peek_data, read_data};
        checksum <= bus_q[15:0] ^ bus_q[31:16] ^ {2'b0,bus_q[45:32]};
        valid_q <= request_q;
        address_r <= address_q;
        valid_r <= valid_q;
        rsp_valid <= valid_r && !reset_q;
        response_data_q <= address_r == 0 ? ID : {checksum, checksum[7:0] ^ peek_data, checksum[15:8]};
        rsp_data <= response_data_q;
        if (reset_q) begin valid_q <= 0; valid_r <= 0; rsp_valid <= 0; end
    end
endmodule
module fes_link_persona_a (
    input wire clk, reset, req, input wire [7:0] address,
    input wire [45:0] bus_request,
    output wire rsp_valid, output wire [31:0] rsp_data,
    output wire [19:0] bus_response
);
    fes_link_zx81_persona core (
        .clk(clk), .reset(reset), .req(req), .address(address), .bus_request(bus_request),
        .rsp_valid(rsp_valid), .rsp_data(rsp_data), .bus_response(bus_response));
endmodule
module fes_link_persona_b (
    input wire clk, reset, req, input wire [7:0] address,
    input wire [45:0] bus_request,
    output wire rsp_valid, output wire [31:0] rsp_data,
    output wire [19:0] bus_response
);
    fes_link_zx81_persona #(.RAM(1), .ID(`FES_DE25_PR_CONTROL_PERSONA_B)) core (
        .clk(clk), .reset(reset), .req(req), .address(address), .bus_request(bus_request),
        .rsp_valid(rsp_valid), .rsp_data(rsp_data), .bus_response(bus_response));
endmodule
