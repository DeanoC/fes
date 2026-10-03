// SPDX-License-Identifier: GPL-3.0-or-later
// Focused motherboard-I/O shell. Its clock remains above the real 2.4576 MHz
// MFP crystal enable; scaling the system clock does not change timer rates.
module st_io_sim_top (
    input wire clk, reset, req, write,
    input wire [23:1] addr,
    input wire [15:0] wdata,
    input wire [1:0] byte_enable,
    output wire selected, ack,
    output wire [15:0] rdata,
    output wire [2:0] irq,
    output wire irq_vectored,
    output wire [7:0] irq_vector,
    input wire irq_ack,
    input wire [2:0] irq_level,
    input wire [23:0] screen_base,
    input wire [1:0] resolution,
    input wire [7:0] sync_mode,
    input wire monochrome,
    output wire [23:0] video_counter,
    input wire [143:0] keyboard,
    input wire [15:0] controller_buttons,
    input wire mouse_valid,
    input wire signed [15:0] mouse_dx, mouse_dy,
    input wire [1:0] mouse_buttons,
    output wire mouse_ready,
    output wire signed [15:0] audio_pcm,
    output wire audio_valid,
    input wire media_ready,
    output wire media_req,
    output wire [19:0] media_addr,
    input wire [7:0] media_data,
    input wire media_valid,
    output wire dma_req,
    output wire [23:0] dma_addr,
    output wire [15:0] dma_wdata,
    output wire [1:0] dma_byte_enable,
    input wire dma_ready,
    output wire vblank, hblank,
    // Observe the physical timer-B pin, crystal enable and floppy selects
    // to check their connection to MMIO, without substituting peripherals.
    output wire timer_b_level, timer_ce_level,
    output wire [7:0] floppy_port_a
);
    st_io #(.SYSTEM_CLOCK_HZ(5_222_400)) io (
        .clk(clk), .reset(reset), .req(req), .addr(addr), .write(write),
        .wdata(wdata), .byte_enable(byte_enable), .selected(selected),
        .ack(ack), .rdata(rdata), .irq(irq), .irq_vectored(irq_vectored),
        .irq_vector(irq_vector), .irq_ack(irq_ack), .irq_level(irq_level),
        .screen_base(screen_base), .resolution(resolution), .sync_mode(sync_mode),
        .monochrome(monochrome), .video_counter(video_counter),
        .keyboard(keyboard), .controller_buttons(controller_buttons),
        .mouse_valid(mouse_valid), .mouse_dx(mouse_dx), .mouse_dy(mouse_dy),
        .mouse_buttons(mouse_buttons), .mouse_ready(mouse_ready),
        .audio_pcm(audio_pcm), .audio_valid(audio_valid), .media_ready(media_ready),
        .media_req(media_req), .media_addr(media_addr), .media_data(media_data),
        .media_valid(media_valid), .dma_req(dma_req), .dma_addr(dma_addr),
        .dma_wdata(dma_wdata), .dma_byte_enable(dma_byte_enable), .dma_ready(dma_ready),
        .vblank(vblank), .hblank(hblank)
    );
    assign timer_b_level = io.native_display;
    assign timer_ce_level = io.timer_ce;
    assign floppy_port_a = io.port_a;
endmodule
