// SPDX-License-Identifier: MIT
module c64_video_sim_top (
 input wire clk_sys,pixel_clk,reset,we,color_we,phi,reading,read_sample,
 input wire [5:0] cycle_clock,
 input wire [5:0] addr,input wire [7:0] din,
 input wire [9:0] color_addr,input wire [7:0] color_din,
 input wire [1:0] bank,
 output wire [7:0] red,green,blue,output wire de,vsync,irq,output wire [7:0] vic_dout
);
 wire [15:0] video_addr;wire [7:0] video_data;
 c64_ram memory(.clk_a(clk_sys),.addr_a(16'd0),.wdata_a(8'd0),.we_a(1'b0),.q_a(),
 .clk_b(clk_sys),.addr_b(video_addr),.q_b(video_data));
 c64_vic vic(.clk_sys(clk_sys),.phi(phi),.pixel_clk(pixel_clk),.reset(reset),
 .cs(1'b1),.we(we),.reading(reading),.read_sample(read_sample),.addr(addr),.din(din),.dout(vic_dout),.color_cs(1'b1),
 .color_addr(color_addr),.color_din(color_din),.color_we(color_we),.color_dout(),
 .cycle_clock(cycle_clock),.cpu_bus_data(8'd0),.ba_n(),
 .bank(bank),.ram_addr(video_addr),.ram_data(video_data),.red(red),.green(green),.blue(blue),
 .de(de),.vsync(vsync),.hsync(),.irq(irq),.raster());
endmodule
