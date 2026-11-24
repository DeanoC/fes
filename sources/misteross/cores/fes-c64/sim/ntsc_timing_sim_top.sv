// SPDX-License-Identifier: MIT
module ntsc_timing_sim_top (
    input wire clk, reset, phi, we,
    input wire [5:0] phase, addr,
    input wire [7:0] din,
    output wire [8:0] raster, sprite_x,
    output wire [7:0] dout,
    output wire [6:0] cycle,
    output wire [2:0] dot,
    output wire dot_tick, ba_n, irq, sprite_fetch, matrix_fetch,
    output wire [15:0] address
);
    // $07f8..$07ff point to distinct, aligned sprite rows at $3000..$31c0.
    reg [7:0] memory_data;
    always @(posedge clk)
        memory_data <= address[15:3] == 13'h0ff ? 8'hc0 + {5'b0,address[2:0]} : 8'h00;

    c64_vic #(.NTSC(1)) vic (
        .clk_sys(clk), .phi(phi), .cycle_clock(phase), .cpu_bus_data(8'hff),
        .ba_n(ba_n), .pixel_clk(clk), .reset(reset), .cs(1'b1), .we(we),
        .reading(1'b0), .read_sample(1'b0), .addr(addr), .din(din), .dout(dout),
        .color_cs(1'b0), .color_addr(10'd0), .color_din(8'd0), .color_we(1'b0),
        .color_dout(), .bank(2'b00), .ram_addr(address), .ram_data(memory_data),
        .red(), .green(), .blue(), .de(), .hsync(), .vsync(), .raster(raster), .irq(irq)
    );
    assign cycle = vic.native_cycle;
    assign sprite_fetch = vic.sprite_request;
    assign matrix_fetch = vic.request && vic.matrix_request;
    assign dot = vic.subpixel;
    assign dot_tick = vic.pixel_strobe;
    assign sprite_x = vic.sprite_dot_x_q;
endmodule
