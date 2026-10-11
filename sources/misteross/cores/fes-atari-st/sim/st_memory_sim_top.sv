// SPDX-License-Identifier: GPL-3.0-or-later
// Simulation shell with actual DDR input-register timing, no analog pad model.
module st_memory_sim_top #(
    parameter [13:0] REFRESH_WAIT_CYCLES = 14'd4,
    parameter EARLY_COMPLETION = 1,
    parameter REGISTERED_READ_INPUT = 0,
    parameter POSTED_CPU_WRITES = 0,
    parameter PHASE_SLOTS = 0,
    parameter [1:0] CPU_SLOT_PHASE = 2'd0,
    parameter SINGLE_RANK_REFRESH = 0,
    parameter SLOT_REFRESH = 0
) (
    input wire clk,
    input wire [1:0] memory_phase,
    input wire raster_reset,
    input wire cold_reset,
    input wire reset,
    output wire initialized,
    input wire cpu_req,
    input wire [18:1] cpu_addr,
    input wire cpu_write,
    input wire [15:0] cpu_wdata,
    input wire [1:0] cpu_byte_enable,
    output wire cpu_ready,
    output wire [15:0] cpu_rdata,
    input wire video_req,
    input wire [18:1] video_addr,
    output wire video_ready,
    output wire [15:0] video_rdata,
    input wire dma_req,
    input wire [23:0] dma_addr,
    input wire dma_write,
    input wire [15:0] dma_wdata,
    input wire [1:0] dma_byte_enable,
    output wire dma_ready,
    output wire [15:0] dma_rdata,
    input wire media_write_req,
    input wire [19:1] media_write_addr,
    input wire [15:0] media_write_wdata,
    input wire [1:0] media_write_byte_enable,
    output wire media_write_ready,
    input wire media_read_req,
    input wire [19:0] media_read_addr,
    output wire media_read_ready,
    output wire [7:0] media_read_rdata,
    output wire sdram_clk,
    output wire sdram_cke,
    output wire sdram_ncs,
    output wire sdram_nras,
    output wire sdram_ncas,
    output wire sdram_nwe,
    output wire [1:0] sdram_ba,
    output wire [12:0] sdram_a,
    output wire sdram_dqml,
    output wire sdram_dqmh,
    output wire [15:0] dq_out,
    output wire dq_oe,
    input wire [15:0] dq_sample

);
    reg [15:0] dq_rise, dq_fall;
    always @(posedge clk) dq_rise <= dq_sample;
    always @(negedge clk) dq_fall <= dq_sample;
    st_memory #(.REFRESH_WAIT_CYCLES(REFRESH_WAIT_CYCLES),
                .EARLY_COMPLETION(EARLY_COMPLETION),
                .REGISTERED_READ_INPUT(REGISTERED_READ_INPUT),
                .POSTED_CPU_WRITES(POSTED_CPU_WRITES),
                .PHASE_SLOTS(PHASE_SLOTS), .CPU_SLOT_PHASE(CPU_SLOT_PHASE),
                .SINGLE_RANK_REFRESH(SINGLE_RANK_REFRESH), .SLOT_REFRESH(SLOT_REFRESH)) memory (.clk_pin(clk), .*);
endmodule

// Only the clock pin is needed; the stand-in retains high/low DDR values.
/* verilator lint_off DECLFILENAME */
/* verilator lint_off UNUSEDSIGNAL */
/* verilator lint_off UNUSEDPARAM */
module altddio_out #(
    parameter width = 1,
    parameter intended_device_family = "Cyclone V",
    parameter power_up_high = "OFF",
    parameter oe_reg = "UNREGISTERED",
    parameter extend_oe_disable = "OFF",
    parameter invert_output = "OFF"
) (
    input wire [width-1:0] datain_h, datain_l,
    input wire outclock, outclocken, aset, aclr, sset, sclr, oe,
    output wire [width-1:0] dataout,
    output wire oe_out
);
    reg [width-1:0] high_data = 0, low_data = 0;
    always @(posedge outclock) if (outclocken) high_data <= datain_h;
    always @(negedge outclock) if (outclocken) low_data <= datain_l;
    assign dataout = outclock ? high_data : low_data;
    assign oe_out = 1'b0;
endmodule
/* verilator lint_on UNUSEDPARAM */
/* verilator lint_on UNUSEDSIGNAL */
/* verilator lint_on DECLFILENAME */
