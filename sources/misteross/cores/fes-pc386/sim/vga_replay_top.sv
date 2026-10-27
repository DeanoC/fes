// SPDX-License-Identifier: MIT
// VGA trace replay bench top: the machine's I/O decoder and VGA, with VRAM
// on port 1 and the capture writes on their own port, as machine_tb drives
// them. Only VGA ports reach the decoder here, so a replayed I/O takes the
// timing it had in the machine.
module vga_replay_top (
    input wire clk, reset,
    input wire io_valid, io_write,
    input wire [15:0] io_port,
    input wire [1:0] io_size,
    input wire [31:0] io_wdata,
    output wire io_busy, io_done,
    output wire [31:0] io_rdata,
    input wire mem_valid, mem_write,
    input wire [16:2] mem_address,
    input wire [3:0] mem_byteenable,
    input wire [31:0] mem_wdata,
    output wire mem_ready, mem_rvalid,
    output wire [31:0] mem_rdata,
    output wire [28:0] p1_address,
    output wire [7:0] p1_burstcount,
    output wire p1_read, p1_write,
    output wire [63:0] p1_writedata,
    output wire [7:0] p1_byteenable,
    input wire p1_waitrequest,
    input wire [63:0] p1_readdata,
    input wire p1_readdatavalid,
    output wire [28:0] p2_address,
    output wire [7:0] p2_burstcount,
    output wire p2_write,
    output wire [63:0] p2_writedata,
    input wire p2_waitrequest,
    output wire frame_start, frame_done, start_latched,
    output wire [15:0] frame_count,
    output wire [1:0] frame_buffer,
    output wire [9:0] frame_width, frame_height,
    output wire [31:0] scanout_underflows, capture_drops
);
    wire vga_valid, vga_done;
    wire [15:0] vga_port;
    wire [7:0] vga_rdata;
    wire [15:0] cycle_port, cycle_wdata;
    wire cycle_write, cycle_word, cycle_inta, cycle_cs, cycle_strobe, cycle_access, inta_done;
    wire [7:0] inta_vector;
    pc386_io decoder (
        .clk(clk), .reset(reset),
        .io_valid(io_valid), .io_write(io_write), .io_port(io_port), .io_size(io_size), .io_wdata(io_wdata),
        .inta_valid(1'b0), .busy(io_busy), .io_done(io_done), .inta_done(inta_done),
        .io_rdata(io_rdata), .inta_vector(inta_vector),
        .cycle_port(cycle_port), .cycle_write(cycle_write), .cycle_word(cycle_word), .cycle_inta(cycle_inta),
        .cycle_wdata(cycle_wdata), .cycle_cs(cycle_cs), .cycle_strobe(cycle_strobe), .cycle_access(cycle_access),
        .cycle_rdata(16'hffff),
        .vga_valid(vga_valid), .vga_done(vga_done), .vga_rdata(vga_rdata)
    );
    wire [15:0] frame_sequence;
    pc386_vga vga (
        .clk(clk), .reset(reset),
        .io_valid(vga_valid), .io_write(cycle_write), .io_port(cycle_port), .io_wdata(cycle_wdata[7:0]),
        .io_done(vga_done), .io_rdata(vga_rdata),
        .mem_valid(mem_valid), .mem_write(mem_write), .mem_address(mem_address),
        .mem_byteenable(mem_byteenable), .mem_wdata(mem_wdata), .mem_ready(mem_ready),
        .mem_rvalid(mem_rvalid), .mem_rdata(mem_rdata),
        .p1_address(p1_address), .p1_burstcount(p1_burstcount), .p1_read(p1_read), .p1_write(p1_write),
        .p1_writedata(p1_writedata), .p1_byteenable(p1_byteenable), .p1_waitrequest(p1_waitrequest),
        .p1_readdata(p1_readdata), .p1_readdatavalid(p1_readdatavalid),
        .cap_address(p2_address), .cap_burstcount(p2_burstcount), .cap_write(p2_write),
        .cap_writedata(p2_writedata), .cap_waitrequest(p2_waitrequest),
        .frame_buffer(frame_buffer), .frame_width(frame_width), .frame_height(frame_height),
        .frame_sequence(frame_sequence), .present_buffer(2'd3),
        .frame_start(frame_start), .frame_done(frame_done), .start_latched(start_latched),
        .frame_count(frame_count), .scanout_underflows(scanout_underflows), .capture_drops(capture_drops)
    );
    wire unused = ^{cycle_word, cycle_inta, cycle_cs, cycle_strobe, cycle_access, inta_done, inta_vector, frame_sequence};
endmodule
