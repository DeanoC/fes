// SPDX-License-Identifier: GPL-3.0-or-later
// Reusable 520ST system: storage and video/audio sockets remain external.
module st_system #(
    parameter integer ENABLE_FLOPPY_WRITE = 0,
    parameter integer MFP_WAIT_STATES = 0
) (
    input wire clk_sys, reset, cold_reset,
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
    input wire media_ready, media_frozen,
    input wire [31:0] media_size,
    output wire media_write_req,
    output wire [19:1] media_write_addr,
    output wire [15:0] media_write_data,
    input wire media_write_ready,
    output wire media_write_busy, media_changed,
    output wire media_req,
    output wire [19:0] media_addr,
    input wire [7:0] media_data,
    input wire media_valid,
    output wire dma_req, dma_write,
    output wire [23:0] dma_addr,
    output wire [15:0] dma_wdata,
    output wire [1:0] dma_byte_enable,
    input wire dma_ready,
    input wire [15:0] dma_rdata,
    output wire [23:0] screen_base,
    output wire [1:0] resolution,
    output wire [143:0] palette,
    output wire [7:0] sync_mode,
    output wire [23:0] debug_addr,
    output wire debug_bus_error, debug_overlay, debug_halted,
    output wire vblank, hblank, native_display,
    output wire [8:0] native_line
);
    wire bus_req, io_selected, io_ack;
    wire [15:0] io_rdata;
    wire [2:0] io_irq;
    wire io_vectored;
    wire [7:0] irq_vector;
    wire [23:0] video_counter;
    wire [2:0] machine_irq = io_irq >= exp_irq ? io_irq : exp_irq;
    wire irq_vectored = io_vectored && io_irq >= exp_irq;
    // The motherboard decodes the cartridge window even when the connector
    // is empty. A plugged part owns the response and may insert wait states.
    wire empty_cartridge = !exp_present && exp_addr >= 23'h7d0000 && exp_addr < 23'h7e0000;
    assign exp_req = bus_req && !io_selected;
    st_machine machine (
        .clk_sys(clk_sys), .reset(reset),
        .rom_req(rom_req), .rom_addr(rom_addr), .rom_rdata(rom_rdata), .rom_ready(rom_ready),
        .ram_req(ram_req), .ram_addr(ram_addr), .ram_wdata(ram_wdata),
        .ram_byte_enable(ram_byte_enable), .ram_write(ram_write),
        .ram_rdata(ram_rdata), .ram_ready(ram_ready),
        .exp_req(bus_req), .exp_addr(exp_addr), .exp_wdata(exp_wdata),
        .exp_byte_enable(exp_byte_enable), .exp_write(exp_write), .exp_fc(exp_fc),
        .exp_reset(exp_reset), .exp_phi1(exp_phi1), .exp_phi2(exp_phi2),
        .exp_ack(io_selected ? io_ack : exp_ack || empty_cartridge), .exp_berr(!io_selected && exp_berr),
        .exp_rdata(io_selected ? io_rdata : empty_cartridge ? 16'hffff : exp_rdata), .exp_irq(machine_irq),
        .irq_vectored(irq_vectored), .irq_vector(irq_vector), .irq_ack(irq_ack), .irq_level(irq_level),
        .video_counter(video_counter), .screen_base(screen_base), .resolution(resolution),
        .palette(palette), .sync_mode(sync_mode), .debug_addr(debug_addr),
        .debug_bus_error(debug_bus_error), .debug_overlay(debug_overlay), .debug_halted(debug_halted)
    );
    st_io #(.ENABLE_FLOPPY_WRITE(ENABLE_FLOPPY_WRITE), .MFP_WAIT_STATES(MFP_WAIT_STATES)) io (
        .clk(clk_sys), .reset(exp_reset), .cold_reset(cold_reset), .cpu_cycle_ce(exp_phi2), .req(bus_req), .addr(exp_addr),
        .write(exp_write), .wdata(exp_wdata), .byte_enable(exp_byte_enable),
        .selected(io_selected), .ack(io_ack), .rdata(io_rdata),
        .irq(io_irq), .irq_vectored(io_vectored), .irq_vector(irq_vector),
        .irq_ack(irq_ack), .irq_level(irq_level), .screen_base(screen_base),
        .resolution(resolution), .sync_mode(sync_mode), .monochrome(monochrome),
        .video_counter(video_counter), .keyboard(keyboard), .controller_buttons(controller_buttons),
        .mouse_valid(mouse_valid), .mouse_dx(mouse_dx), .mouse_dy(mouse_dy),
        .mouse_buttons(mouse_buttons), .mouse_ready(mouse_ready),
        .audio_pcm(audio_pcm), .audio_valid(audio_valid), .media_ready(media_ready), .media_size(media_size),
        .media_frozen(media_frozen), .media_write_req(media_write_req), .media_write_addr(media_write_addr),
        .media_write_data(media_write_data), .media_write_ready(media_write_ready),
        .media_write_busy(media_write_busy), .media_changed(media_changed),
        .media_req(media_req), .media_addr(media_addr), .media_data(media_data), .media_valid(media_valid),
        .dma_req(dma_req), .dma_write(dma_write), .dma_rdata(dma_rdata), .dma_addr(dma_addr), .dma_wdata(dma_wdata),
        .dma_byte_enable(dma_byte_enable), .dma_ready(dma_ready), .vblank(vblank), .hblank(hblank),
        .native_display(native_display), .native_line(native_line)
    );
endmodule
