// SPDX-License-Identifier: GPL-2.0-or-later
// Static registered boundary for the standard core's public expansion bus.
module fes_de25_zx81_pr_bus (
    input wire clk, machine_reset, cpu_clock,
    input wire [15:0] address,
    input wire [7:0] write_data,
    input wire mreq_n, iorq_n, rd_n, wr_n, m1_n, rfsh_n,
    input wire [13:0] peek_address,
    input wire [19:0] bus_response,
    (* preserve, dont_merge, noprune *) output reg [45:0] bus_request = 0,
    output wire [19:0] response
);
    (* preserve, dont_merge *) reg [19:0] response_q = 0;
    always @(posedge clk) begin
        response_q <= bus_response;
        // The exported full machine reset fences writes before the CPU's
        // enable-scheduled reset reaches its internal bus state.
        bus_request <= {!machine_reset, cpu_clock, peek_address,
            !machine_reset ? {rfsh_n, m1_n, wr_n, rd_n, iorq_n, mreq_n,
                               write_data, address} : {6'b111111, 24'd0}};
    end
    assign response = response_q;
endmodule
