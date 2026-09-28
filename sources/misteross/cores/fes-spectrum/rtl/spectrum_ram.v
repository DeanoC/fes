// SPDX-License-Identifier: GPL-2.0-or-later
// 48 KiB ZX Spectrum RAM at CPU $4000-$FFFF. Port A is the CPU (system
// clock). Port B is the HDMI scanner. Both reads are registered. Addresses
// are the CPU address; accesses below $4000 read zero and do not write.
module spectrum_ram (
    input  wire        clk_sys,
    input  wire        cpu_we,
    input  wire [15:0] cpu_addr,
    input  wire [7:0]  cpu_wdata,
    output reg  [7:0]  cpu_rdata,
    input  wire        pixel_clk,
    input  wire [15:0] video_addr,
    output reg  [7:0]  video_rdata,
    output wire [7:0]  sig8000,
    output wire [7:0]  sig8001,
    output wire [7:0]  sig8002,
    output wire [7:0]  sig8003,
    output wire [7:0]  sig8004
);
    (* ram_style = "m10k_tdp" *) reg [7:0] mem [0:49151];
    wire cpu_hit = cpu_addr[15] | cpu_addr[14];
    wire video_hit = video_addr[15] | video_addr[14];
    wire [15:0] cpu_offset = cpu_addr - 16'h4000;
    wire [15:0] video_offset = video_addr - 16'h4000;

`ifdef VERILATOR
    integer init_i;
    initial begin
        for (init_i = 0; init_i < 49152; init_i = init_i + 1)
            mem[init_i] = 8'h00;
    end
`endif

    always @(posedge clk_sys) begin
        if (cpu_we && cpu_hit)
            mem[cpu_offset] <= cpu_wdata;
        cpu_rdata <= cpu_hit ? mem[cpu_offset] : 8'h00;
    end

    always @(posedge pixel_clk)
        video_rdata <= video_hit ? mem[video_offset] : 8'h00;

`ifdef VERILATOR
    assign sig8000 = mem[16'h4000];
    assign sig8001 = mem[16'h4001];
    assign sig8002 = mem[16'h4002];
    assign sig8003 = mem[16'h4003];
    assign sig8004 = mem[16'h4004];
`else
    assign sig8000 = 8'h00;
    assign sig8001 = 8'h00;
    assign sig8002 = 8'h00;
    assign sig8003 = 8'h00;
    assign sig8004 = 8'h00;
`endif
endmodule
