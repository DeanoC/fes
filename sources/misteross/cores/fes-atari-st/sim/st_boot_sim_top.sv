// SPDX-License-Identifier: GPL-3.0-or-later
// Reusable 520ST system: storage and video/audio sockets remain external.
module st_boot_sim_top (
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
    output wire exp_req,
    output wire [23:1] exp_addr,
    output wire [15:0] exp_wdata,
    output wire [1:0] exp_byte_enable,
    output wire exp_write,
    output wire [2:0] exp_fc,
    output wire exp_reset, exp_phi1, exp_phi2,
    input wire exp_ack, exp_berr,
    input wire exp_present,
    input wire [15:0] exp_rdata,
    input wire [2:0] exp_irq,
    output wire irq_ack,
    output wire [2:0] irq_level,
    input wire [143:0] keyboard,
    input wire [15:0] controller_buttons,
    input wire monochrome,
    input wire mouse_valid,
    input wire signed [15:0] mouse_dx, mouse_dy,
    input wire [1:0] mouse_buttons,
    output wire mouse_ready,
    output wire signed [15:0] audio_pcm,
    output wire audio_valid,
    input wire media_ready,
    input wire [31:0] media_size,
    output wire media_req,
    output wire [19:0] media_addr,
    input wire [7:0] media_data,
    input wire media_valid,
    output wire dma_req,
    output wire [23:0] dma_addr,
    output wire [15:0] dma_wdata,
    output wire [1:0] dma_byte_enable,
    input wire dma_ready,
    output wire [23:0] screen_base,
    output wire [1:0] resolution,
    output wire [143:0] palette,
    output wire [7:0] sync_mode,
    output wire [23:0] debug_addr,
    output wire debug_bus_error, debug_overlay, debug_halted,
    output wire [31:0] debug_pc,
    output wire [7:0] debug_fdc_status, debug_fdc_track, debug_fdc_sector, debug_fdc_head,
    output wire vblank, hblank
);
    st_system system (
        .cold_reset(reset), .media_frozen(1'b0),
        .media_write_req(), .media_write_addr(), .media_write_data(),
        .media_write_ready(1'b0), .media_write_busy(), .media_changed(),
        .dma_write(), .dma_rdata(16'd0), .*
    );
    assign debug_fdc_status = system.io.floppy.fdc_status;
    assign debug_fdc_track = system.io.floppy.track_reg;
    assign debug_fdc_sector = system.io.floppy.sector_reg;
    assign debug_fdc_head = system.io.floppy.head_track;
    // Simulation-only observability; no upstream CPU bytes are changed.
    assign debug_pc = {system.machine.cpu.cpu.excUnit.PcH,
                       system.machine.cpu.cpu.excUnit.PcL};
endmodule
