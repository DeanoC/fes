// SPDX-License-Identifier: GPL-2.0-or-later
// 48 KiB ZX Spectrum RAM at CPU $4000-$FFFF. The array is 64 KiB so the
// index is the power-of-two shape Apple II RAM uses; offsets $C000-$FFFF
// are unused. Port A is the CPU (system clock). Port B is the HDMI scanner.
// Both reads are registered. The hit flops match those reads, so addresses
// below $4000 return zero and do not write. ramstyle=M10K, not the same-clock
// m10k_tdp tag: these ports have independent clocks.
module spectrum_ram (
    input  wire        clk_sys,
    input  wire        cpu_we,
    input  wire [15:0] cpu_addr,
    input  wire [7:0]  cpu_wdata,
    output wire [7:0]  cpu_rdata,
    input  wire        pixel_clk,
    input  wire [15:0] video_addr,
    output wire [7:0]  video_rdata,
    output wire [7:0]  sig8000,
    output wire [7:0]  sig8001,
    output wire [7:0]  sig8002,
    output wire [7:0]  sig8003,
    output wire [7:0]  sig8004
);
    (* ramstyle = "M10K" *) reg [7:0] mem [0:65535];
    wire cpu_hit = cpu_addr[15] | cpu_addr[14];
    wire video_hit = video_addr[15] | video_addr[14];
    wire [15:0] cpu_offset = cpu_addr - 16'h4000;
    wire [15:0] video_offset = video_addr - 16'h4000;
    reg [7:0] cpu_raw = 8'h00;
    reg [7:0] video_raw = 8'h00;
    reg cpu_hit_q = 1'b0;
    reg video_hit_q = 1'b0;

`ifdef VERILATOR
    integer init_i;
    initial begin
        for (init_i = 0; init_i < 65536; init_i = init_i + 1)
            mem[init_i] = 8'h00;
    end
`endif

    always @(posedge clk_sys) begin
        if (cpu_we && cpu_hit)
            mem[cpu_offset] <= cpu_wdata;
        cpu_raw <= mem[cpu_offset];
        cpu_hit_q <= cpu_hit;
    end

    always @(posedge pixel_clk) begin
        video_raw <= mem[video_offset];
        video_hit_q <= video_hit;
    end

    assign cpu_rdata = cpu_hit_q ? cpu_raw : 8'h00;
    assign video_rdata = video_hit_q ? video_raw : 8'h00;

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
