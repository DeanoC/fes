// SPDX-License-Identifier: GPL-3.0-or-later
// Same-clock simulation assembly. A board shell needs external storage and
// coherent pixel-domain configuration transfer before this can be synthesized.
module st_sim_top (
    input wire clk_sys, reset,
    output wire rom_req,
    output wire [17:1] rom_addr,
    input wire [15:0] rom_rdata,
    input wire rom_ready,
    output wire ram_req,
    output wire [18:1] ram_addr,
    output wire [15:0] ram_wdata,
    output wire [1:0] ram_byte_enable,
    output wire ram_write,
    input wire [15:0] ram_rdata,
    input wire ram_ready,
    input wire probe_enable,
    input wire [7:0] probe_wait_cycles,
    output wire [15:0] probe_scratch,
    output wire [31:0] probe_long,
    output wire [31:0] probe_write_count,
    input wire [2:0] exp_irq,
    output wire exp_req,
    output wire [23:1] exp_addr,
    output wire [15:0] exp_wdata,
    output wire [1:0] exp_byte_enable,
    output wire exp_write,
    output wire [2:0] exp_fc,
    output wire exp_reset, exp_phi1, exp_phi2,
    output wire exp_ack, exp_berr,
    output wire [23:0] screen_base,
    output wire [1:0] resolution,
    output wire [143:0] palette,
    output wire [23:0] debug_addr,
    output wire debug_bus_error, debug_overlay,
    input wire video_hold, video_scanlines,
    output wire [18:1] video_addr,
    input wire [15:0] video_data,
    output wire [31:0] video_request,
    output reg [27:0] video_response
);
    wire [15:0] exp_rdata;
    wire irq_vectored = 1'b0;
    wire [7:0] irq_vector = 8'd0;
    wire irq_ack, debug_halted;
    wire [2:0] irq_level;
    wire [23:0] video_counter = 24'd0;
    wire [7:0] sync_mode;
    st_machine machine (.*);
    st_probe probe (
        .clk(clk_sys), .reset(exp_reset), .enable(probe_enable),
        .wait_cycles(probe_wait_cycles), .req(exp_req), .addr(exp_addr),
        .wdata(exp_wdata), .byte_enable(exp_byte_enable), .write(exp_write),
        .ack(exp_ack), .berr(exp_berr), .rdata(exp_rdata),
        .scratch(probe_scratch), .long_value(probe_long),
        .write_count(probe_write_count)
    );
    st_video video (
        .clk(clk_sys), .reset(reset), .hold(video_hold),
        .screen_base(screen_base), .resolution(resolution), .palette(palette),
        .mem_addr(video_addr), .mem_data(video_data),
        /* verilator lint_off PINCONNECTEMPTY */
        .fetch_valid(), .fetch_row(), .fetch_column(), .raster_row(), .raster_next_row(),
        /* verilator lint_on PINCONNECTEMPTY */
        .video_request(video_request)
    );
    reg [31:0] video_boundary = 32'd0;
    wire [27:0] direct_response, scanline_response;
    fes_video_part_direct direct (
        .video_request(video_boundary), .video_response(direct_response)
    );
    fes_video_part_scanlines scanlines (
        .clock(clk_sys), .video_request(video_boundary), .video_response(scanline_response)
    );
    always @(posedge clk_sys) begin
        if (reset) begin
            video_boundary <= 32'd0;
            video_response <= 28'd0;
        end else begin
            video_boundary <= video_request;
            video_response <= video_scanlines ? scanline_response : direct_response;
        end
    end
endmodule
