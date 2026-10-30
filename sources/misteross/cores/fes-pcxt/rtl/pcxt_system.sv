// SPDX-License-Identifier: GPL-2.0-or-later
// Reusable complete diagnostic: byte loader, real motherboard/storage and one
// selected video pipeline. Board pads and PLL/reset synchronizers are external.
module pcxt_system #(parameter bit MDA = 0, SCANLINES = 0, HALF_RATE_BOARD = 0, EXTERNAL_CS_REGISTER = 0, DIAGNOSTIC_PROBE = 1, FLOPPY = 0,
    parameter [7:0] SWITCHES = 8'h2d) (
    input wire clk, clk_cpu, clk_pixel, clk_pin, cold_reset, reset_pixel, warm_reset, hold,
    input wire media_present,
    input wire disk_load_valid,
    output wire disk_load_ready, disk_load_done,
    input wire [18:0] disk_load_address,
    input wire [7:0] disk_load_data,
    output reg disk_loaded,
    input wire keyboard_clock, keyboard_data,
    output wire keyboard_clock_low, keyboard_data_low, keyboard_clear,
    output wire [11:0] cga_font_address, mda_font_address,
    input wire [7:0] cga_font_data, mda_font_data,
    input wire firmware_valid,
    output wire firmware_ready, firmware_done,
    input wire [15:0] firmware_address,
    input wire [7:0] firmware_data,
    output reg firmware_loaded,
    output wire initialized,
    output reg machine_reset = 1'b1,
    output wire [1:0] fault,
    output wire halted, dma_owner, refresh_done,
    output wire [31:0] video_request,
    output wire [27:0] video_response,
    output wire audio_valid,
    output wire signed [15:0] audio_sample,
    output wire sdram_clk, sdram_cke, sdram_ncs, sdram_nras, sdram_ncas, sdram_nwe,
    output wire [1:0] sdram_ba,
    output wire [12:0] sdram_a,
    output wire sdram_dqml, sdram_dqmh,
    output wire [15:0] dq_out,
    output wire dq_oe,
    input wire [15:0] dq_rise, dq_fall
);
    wire memory_valid, memory_ready, memory_done, write_cycle;
    wire [19:0] address;
    wire [7:0] wdata, memory_rdata;
    wire media_read_valid, media_read_ready;
    wire [18:0] media_read_addr;
    wire [7:0] media_read_data;
    wire [1:0] pixel_ce, de, hs, vs, sof, eol;
    wire [7:0] rgbi;
    // Admit exactly one sequential 64 KiB image per cold start. Count completed
    // writes, never offers, and keep reset until the final physical DONE.
    reg [15:0] next_address;
    reg loading;
    wire loader_ready, loader_done;
    wire loader_valid = firmware_valid && !firmware_loaded && !loading &&
                        firmware_address == next_address;
    assign firmware_ready = loader_ready && !firmware_loaded && !loading &&
                            firmware_address == next_address;
    assign firmware_done = loader_done;
    // A disk is visible only after its entire 360 KiB image has completed
    // ordered physical writes. Cold reset invalidates the image; warm reset
    // retains it. Invalid or out-of-order requests are never acknowledged.
    localparam [18:0] DISK_LAST = 19'h59fff;
    reg [18:0] disk_next_address;
    reg disk_loading;
    wire disk_storage_ready, disk_storage_done;
    wire disk_storage_valid = FLOPPY && disk_load_valid && !disk_loaded && !disk_loading &&
                              disk_load_address == disk_next_address;
    assign disk_load_ready = FLOPPY && disk_storage_ready && !disk_loaded && !disk_loading &&
                             disk_load_address == disk_next_address;
    assign disk_load_done = disk_storage_done;
    // Keep the storage/firmware status decode out of the motherboard's reset
    // fanout and PIC priority logic. All execution participants share this
    // registered reset; storage cold reset remains immediate and independent.
    always @(posedge clk)
        machine_reset <= cold_reset || warm_reset || hold || !firmware_loaded || !initialized ||
                         (FLOPPY && !disk_loaded);
    always @(posedge clk) begin
        if (cold_reset) begin firmware_loaded <= 0; next_address <= 0; loading <= 0; end
        else begin
            if (loader_valid && loader_ready) loading <= 1;
            if (loader_done) begin
                loading <= 0;
                if (&next_address) firmware_loaded <= 1;
                else next_address <= next_address + 1'b1;
            end
        end
    end
    always @(posedge clk) begin
        if (cold_reset) begin disk_loaded <= 0; disk_next_address <= 0; disk_loading <= 0; end
        else begin
            if (disk_storage_valid && disk_storage_ready) disk_loading <= 1;
            if (disk_storage_done) begin
                disk_loading <= 0;
                if (disk_next_address == DISK_LAST) disk_loaded <= 1;
                else disk_next_address <= disk_next_address + 1'b1;
            end
        end
    end
    /* verilator lint_off PINCONNECTEMPTY */
    pcxt_machine #(.DIAGNOSTIC_PROBE(DIAGNOSTIC_PROBE), .SEPARATE_CPU_CLOCK(HALF_RATE_BOARD), .FLOPPY(FLOPPY),
                   .BOARD_CLOCK_HZ(HALF_RATE_BOARD ? 50000000 : 100000000), .SWITCHES(SWITCHES)) machine (
        .clk(clk), .clk_cpu(clk_cpu), .reset(machine_reset), .memory_valid(memory_valid),
        .media_present(media_present && disk_loaded), .media_read_valid(media_read_valid),
        .media_read_addr(media_read_addr), .media_read_ready(media_read_ready), .media_read_data(media_read_data),
        .memory_ready(memory_ready), .memory_done(memory_done), .memory_rdata(memory_rdata),
        .address(address), .wdata(wdata), .write_cycle(write_cycle), .fault(fault),
        .guest_ce(), .halted(halted), .accepted(), .completed(), .cycle_kind(), .cycle_rdata(),
        .keyboard_clock(keyboard_clock), .keyboard_data(keyboard_data),
        .keyboard_clock_low(keyboard_clock_low), .keyboard_data_low(keyboard_data_low),
        .keyboard_clear(keyboard_clear),
        .cga_font_address(cga_font_address), .mda_font_address(mda_font_address),
        .cga_font_data(cga_font_data), .mda_font_data(mda_font_data),
        .pixel_ce(pixel_ce), .de(de), .hs(hs), .vs(vs), .sof(sof), .eol(eol), .rgbi(rgbi),
        .dma_owner(dma_owner), .refresh_done(refresh_done), .audio_valid(audio_valid), .audio_sample(audio_sample)
    );
    /* verilator lint_on PINCONNECTEMPTY */
    pcxt_memory #(.HALF_RATE_CONTROLLER(HALF_RATE_BOARD), .EXTERNAL_CS_REGISTER(EXTERNAL_CS_REGISTER)) storage (
        .clk(clk), .clk_pin(clk_pin), .cold_reset(cold_reset), .reset(machine_reset),
        .initialized(initialized), .memory_valid(memory_valid), .memory_ready(memory_ready),
        .address(address), .wdata(wdata), .write_cycle(write_cycle), .memory_done(memory_done), .memory_rdata(memory_rdata),
        .firmware_valid(loader_valid), .firmware_ready(loader_ready), .firmware_address(firmware_address),
        .firmware_data(firmware_data), .firmware_done(loader_done),
        .disk_load_valid(disk_storage_valid), .disk_load_ready(disk_storage_ready),
        .disk_load_address(disk_load_address), .disk_load_data(disk_load_data), .disk_load_done(disk_storage_done),
        .media_read_valid(media_read_valid), .media_read_addr(media_read_addr),
        .media_read_ready(media_read_ready), .media_read_data(media_read_data),
        .sdram_clk(sdram_clk), .sdram_cke(sdram_cke), .sdram_ncs(sdram_ncs),
        .sdram_nras(sdram_nras), .sdram_ncas(sdram_ncas), .sdram_nwe(sdram_nwe),
        .sdram_ba(sdram_ba), .sdram_a(sdram_a), .sdram_dqml(sdram_dqml), .sdram_dqmh(sdram_dqmh),
        .dq_out(dq_out), .dq_oe(dq_oe), .dq_rise(dq_rise), .dq_fall(dq_fall)
    );
    pcxt_video_output #(.MDA(MDA), .SCANLINES(SCANLINES)) video (
        .clk_sys(clk), .clk_pixel(clk_pixel), .reset_sys(cold_reset), .reset_pixel(reset_pixel),
        .hold(machine_reset), .pixel_ce(pixel_ce[MDA]), .source_de(de[MDA]),
        .source_sof(sof[MDA]), .source_eol(eol[MDA]), .rgbi(MDA ? rgbi[7:4] : rgbi[3:0]),
        .video_request(video_request), .video_response(video_response)
    );
    wire unused_raster = ^{hs, vs, pixel_ce, de, sof, eol, rgbi};
endmodule
