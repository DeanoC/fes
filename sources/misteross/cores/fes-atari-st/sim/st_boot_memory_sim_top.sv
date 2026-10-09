// SPDX-License-Identifier: GPL-3.0-or-later
// Stock-firmware boot through the real shared SDRAM controller and DDIO
// simulation path, concurrently with the independent-clock line-cache video.
// The physical command model is external; no C++ RAM callback serves the CPU.
module st_boot_memory_sim_top (
    input wire clk_sys, clk_pixel, cold_reset, reset_sys, reset_pixel,
    output wire initialized,
    output wire rom_req,
    output wire [17:1] rom_addr,
    input wire [15:0] rom_rdata,
    input wire rom_ready,
    output wire cpu_req, cpu_ready, cpu_write,
    output wire [18:1] cpu_addr,
    output wire [15:0] cpu_wdata,
    output wire [1:0] cpu_byte_enable,
    output wire video_req, video_ready,
    output wire [18:1] video_addr,
    output wire [23:0] screen_base,
    output wire [1:0] resolution,
    output wire [143:0] palette,
    output wire [23:0] debug_addr,
    output wire debug_bus_error, debug_halted,
    output wire [31:0] debug_pc,
    output wire irq_ack,
    output wire [2:0] irq_level,
    output wire vblank, hblank,
    output wire [31:0] video_request, debug_underruns, debug_frame,
    output wire [31:0] native_frames, native_underruns, native_skipped,
    output reg [27:0] video_response = 28'd0,
    output wire sdram_clk, sdram_cke, sdram_ncs, sdram_nras, sdram_ncas, sdram_nwe,
    output wire [1:0] sdram_ba,
    output wire [12:0] sdram_a,
    output wire sdram_dqml, sdram_dqmh, dq_oe,
    output wire [15:0] dq_out,
    input wire [15:0] dq_sample
);
    wire [7:0] sync_mode;
    wire native_display;
    wire [8:0] native_line;
    wire [15:0] cpu_rdata, video_rdata;
    wire media_req, media_valid, dma_req, dma_ready, dma_write;
    wire [15:0] dma_rdata;
    wire [19:0] media_addr;
    wire [7:0] media_data;
    wire [23:0] dma_addr;
    wire [15:0] dma_wdata;
    wire [1:0] dma_byte_enable;
    st_system system (
        .clk_sys(clk_sys), .reset(reset_sys), .cold_reset(cold_reset),
        .media_frozen(1'b0), .media_write_req(), .media_write_addr(), .media_write_data(),
        .media_write_ready(1'b0), .media_write_busy(), .media_changed(),
        .rom_req(rom_req), .rom_addr(rom_addr), .rom_rdata(rom_rdata), .rom_ready(rom_ready),
        .ram_req(cpu_req), .ram_addr(cpu_addr), .ram_wdata(cpu_wdata),
        .ram_byte_enable(cpu_byte_enable), .ram_write(cpu_write),
        .ram_rdata(cpu_rdata), .ram_ready(cpu_ready),
        .exp_req(), .exp_addr(), .exp_wdata(), .exp_byte_enable(), .exp_write(), .exp_fc(),
        .exp_reset(), .exp_phi1(), .exp_phi2(), .exp_ack(1'b0), .exp_berr(1'b0),
        .exp_present(1'b0), .exp_rdata(16'hffff), .exp_irq(3'd0),
        .irq_ack(irq_ack), .irq_level(irq_level),
        .keyboard(144'd0), .controller_buttons(16'd0), .monochrome(1'b0),
        .mouse_valid(1'b0), .mouse_dx(16'sd0), .mouse_dy(16'sd0), .mouse_buttons(2'd0),
        .mouse_ready(), .audio_pcm(), .audio_valid(), .media_size(32'd737280), .media_ready(1'b0),
        .media_req(media_req), .media_addr(media_addr), .media_data(media_data), .media_valid(media_valid),
        .dma_req(dma_req), .dma_addr(dma_addr), .dma_wdata(dma_wdata),
        .dma_byte_enable(dma_byte_enable), .dma_ready(dma_ready), .dma_write(dma_write), .dma_rdata(dma_rdata),
        .screen_base(screen_base), .resolution(resolution), .palette(palette), .sync_mode(sync_mode),
        .debug_addr(debug_addr), .debug_bus_error(debug_bus_error), .debug_overlay(),
        .debug_halted(debug_halted), .vblank(vblank), .hblank(hblank), .native_display(native_display), .native_line(native_line)
    );
    st_memory_sim_top memory (
        .clk(clk_sys), .cold_reset(cold_reset), .reset(reset_sys), .initialized(initialized),
        .cpu_req(cpu_req), .cpu_addr(cpu_addr), .cpu_write(cpu_write), .cpu_wdata(cpu_wdata),
        .cpu_byte_enable(cpu_byte_enable), .cpu_ready(cpu_ready), .cpu_rdata(cpu_rdata),
        .video_req(video_req), .video_addr(video_addr), .video_ready(video_ready), .video_rdata(video_rdata),
        .dma_req(dma_req), .dma_addr(dma_addr), .dma_write(dma_write), .dma_wdata(dma_wdata),
        .dma_byte_enable(dma_byte_enable), .dma_ready(dma_ready), .dma_rdata(dma_rdata),
        .media_write_req(1'b0), .media_write_addr(19'd0), .media_write_wdata(16'd0),
        .media_write_byte_enable(2'd0), .media_write_ready(),
        .media_read_req(media_req), .media_read_addr(media_addr),
        .media_read_ready(media_valid), .media_read_rdata(media_data),
        .sdram_clk(sdram_clk), .sdram_cke(sdram_cke), .sdram_ncs(sdram_ncs),
        .sdram_nras(sdram_nras), .sdram_ncas(sdram_ncas), .sdram_nwe(sdram_nwe),
        .sdram_ba(sdram_ba), .sdram_a(sdram_a), .sdram_dqml(sdram_dqml), .sdram_dqmh(sdram_dqmh),
        .dq_out(dq_out), .dq_oe(dq_oe), .dq_sample(dq_sample)
    );
    st_video_adapter video (
        .clk_sys(clk_sys), .clk_pixel(clk_pixel), .reset_sys(reset_sys), .reset_pixel(reset_pixel),
        .native_vblank(vblank), .native_display(native_display), .native_line(native_line), .sync_mode(sync_mode),
        .hold(reset_sys), .screen_base(screen_base), .resolution(resolution), .palette(palette),
        .video_req(video_req), .video_addr(video_addr), .video_ready(video_ready), .video_rdata(video_rdata),
        .video_request(video_request), .debug_underruns(debug_underruns), .debug_frame(debug_frame)
    );
    assign native_frames = video.native_capture.capture.debug_frames;
    assign native_underruns = video.native_capture.capture.debug_underruns;
    assign native_skipped = video.native_capture.capture.debug_skipped;
    reg [31:0] video_request_q = 32'd0;
    wire [27:0] direct_result;
    fes_video_part_direct direct (.video_request(video_request_q), .video_response(direct_result));
    always @(posedge clk_pixel) begin
        if (reset_pixel) begin
            video_request_q <= 32'd0;
            video_response <= 28'd0;
        end else begin
            video_request_q <= video_request;
            video_response <= direct_result;
        end
    end
    // Observability does not modify the pinned CPU or supply execution data.
    assign debug_pc = {system.machine.cpu.cpu.excUnit.PcH, system.machine.cpu.cpu.excUnit.PcL};
endmodule
