// SPDX-License-Identifier: GPL-3.0-or-later
// Reusable 520ST system: storage and video/audio sockets remain external.
module st_system #(
    // PAL STF master 32,084,988 / 4; sync-mode writes do not switch crystals.
    // https://github.com/hatari/hatari/blob/v2.5.0/src/clocks_timings.c
    parameter integer CPU_CLOCK_HZ = 8_021_247,
    parameter integer ENABLE_FLOPPY_WRITE = 0,
    parameter integer MFP_WAIT_STATES = 4,
    parameter integer PSG_WAIT_STATES = 4,
    // MFP CLK is CPU/2, distinct from its timer crystal. Use the board
    // phase by default; -1 bypasses timing and 0..3 permit clock diagnostics.
    parameter integer MFP_IACK_PHASE = 1,
    parameter RAW_MFP_IACK = 1,
    parameter EARLY_RAM_WRITE_COMPLETION = 0
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
    output wire native_short_line, native_blank,
    output wire [8:0] native_word_loss,
    output wire [8:0] native_line, native_cycle,
    output wire native_pixel_ce
);
    wire bus_req, io_selected, io_ack;
    wire [15:0] io_rdata;
    wire [2:0] io_irq;
    wire io_vectored;
    wire [7:0] irq_vector;
    wire irq_vector_ready, irq_data_strobe, irq_iack_pin;
    generate if (MFP_IACK_PHASE < 0) begin
        assign irq_vector_ready = 1'b1;
    end else begin
        st_mfp_iack_timing #(.PHASE(MFP_IACK_PHASE)) iack_timing (
            .clk(clk_sys), .reset(reset), .phi1(exp_phi1), .phi2(exp_phi2),
            .iack(RAW_MFP_IACK ? irq_iack_pin : irq_ack && irq_level == 3'd6), .ds(irq_data_strobe),
            .ready(irq_vector_ready));
    end endgenerate
    wire [23:0] video_counter;
    wire [2:0] machine_irq = io_irq >= exp_irq ? io_irq : exp_irq;
    wire irq_vectored = io_vectored && io_irq >= exp_irq;
    // The motherboard decodes the cartridge window even when the connector
    // is empty. A plugged part owns the response and may insert wait states.
    wire empty_cartridge = !exp_present && exp_addr >= 23'h7d0000 && exp_addr < 23'h7e0000;
    assign exp_req = bus_req && !io_selected;
    st_machine #(.CPU_CLOCK_HZ(CPU_CLOCK_HZ), .EARLY_RAM_WRITE_COMPLETION(EARLY_RAM_WRITE_COMPLETION), .TIMED_MFP_IACK(MFP_IACK_PHASE >= 0)) machine (
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
        .irq_data_strobe(irq_data_strobe), .irq_iack_pin(irq_iack_pin), .irq_vector_ready(irq_vector_ready),
        // Native cycle labels advance on CPU phase 2. Phase 1 is the
        // palette acceptance window in this counter's coordinate system.
        .palette_ready(native_cycle[1:0] == 2'd1),
        .video_counter(video_counter), .screen_base(screen_base), .resolution(resolution),
        .palette(palette), .sync_mode(sync_mode), .debug_addr(debug_addr),
        .debug_bus_error(debug_bus_error), .debug_overlay(debug_overlay), .debug_halted(debug_halted)
    );
    st_io #(.CPU_CLOCK_HZ(CPU_CLOCK_HZ), .ENABLE_FLOPPY_WRITE(ENABLE_FLOPPY_WRITE), .MFP_WAIT_STATES(MFP_WAIT_STATES), .PSG_WAIT_STATES(PSG_WAIT_STATES)) io (
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
        .native_display(native_display), .native_line(native_line),
        .native_short_line(native_short_line), .native_blank(native_blank), .native_word_loss(native_word_loss),
        .native_cycle(native_cycle), .native_pixel_ce(native_pixel_ce)
    );
endmodule

// MC68901 bus-clock handshake, separate from the timer crystal.
// Motorola MC68901 Fig9: qualify IACK on a falling CLK; DTACK is launched
// 3.5 CLK periods later. CLK is CPU/2, so seven half-period edges follow.
// Table AC row4 requires50ns setup. Three SYS periods conservatively meet it
// at52.224MHz. Registered inputs rise after a SYS edge: age2 at the
// third subsequent sampling edge means three elapsed periods. Output
// propagation is ideal here; physical max is180ns.
module st_mfp_iack_timing #(parameter integer PHASE = 1) (
    input wire clk, reset, phi1, phi2, iack, ds,
    output reg ready
);
    reg clock_level, started;
    reg [2:0] elapsed_half_periods;
    reg [1:0] setup_age;
    wire clock_edge = PHASE >= 2 ? phi1 : phi2;
    always @(posedge clk) begin
        if (reset) begin
            clock_level <= (PHASE % 2) != 0;
            started <= 0; ready <= 0; setup_age <= 0;
            elapsed_half_periods <= 0;
        end else begin
            if (clock_edge) clock_level <= !clock_level;
            if (!iack || !ds) begin
                started <= 0; ready <= 0; setup_age <= 0;
                elapsed_half_periods <= 0;
            end else begin
                if (setup_age != 3) setup_age <= setup_age + 1'b1;
                if (clock_edge && !started && clock_level && setup_age >= 2) begin
                    started <= 1;
                    elapsed_half_periods <= 0;
                end else if (clock_edge && started && !ready) begin
                    elapsed_half_periods <= elapsed_half_periods + 1'b1;
                    if (elapsed_half_periods == 6) ready <= 1;
                end
            end
        end
    end
endmodule
