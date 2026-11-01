// SPDX-License-Identifier: GPL-2.0-or-later
// Adapt unit 0's mailbox transfer to the PC/XT SDRAM byte loader. Loading and
// ejecting immediately hide the drive; writes remain held until SDRAM DONE.
module pcxt_media (
    input wire clk, cold_reset,
    input wire [1:0] unit_state,
    input wire [18:0] source_addr,
    input wire [15:0] source_data,
    input wire [1:0] source_enable,
    output wire source_ready,
    output wire disk_restart,
    output wire media_present,
    input wire disk_loaded,
    output wire disk_valid,
    output wire [18:0] disk_address,
    output wire [7:0] disk_data,
    input wire disk_ready, disk_done
);
    localparam [1:0] EMPTY=2'd1, READY=2'd3;
    reg [1:0] previous_state = EMPTY;
    always @(posedge clk)
        if (cold_reset) previous_state <= EMPTY;
        else previous_state <= unit_state;
    assign disk_restart = !cold_reset && unit_state != previous_state && unit_state != READY;
    assign media_present = unit_state == READY && disk_loaded;
    pcxt_media_writer writer (
        .clk(clk), .cold_reset(cold_reset), .source_addr(source_addr), .source_data(source_data),
        .source_enable(source_enable), .source_ready(source_ready), .disk_valid(disk_valid),
        .disk_address(disk_address), .disk_data(disk_data), .disk_ready(disk_ready), .disk_done(disk_done)
    );
endmodule
