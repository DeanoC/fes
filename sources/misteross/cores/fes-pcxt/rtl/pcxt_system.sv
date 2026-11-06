// SPDX-License-Identifier: GPL-2.0-or-later
// Reusable complete diagnostic: byte loader, real motherboard/storage and one
// selected video pipeline. Board pads and PLL/reset synchronizers are external.
module pcxt_system #(parameter bit MDA = 0, SCANLINES = 0, HALF_RATE_BOARD = 0, EXTERNAL_CS_REGISTER = 0, DIAGNOSTIC_PROBE = 1, FLOPPY = 0, FLOPPY_WRITE = 0,
    parameter [7:0] SWITCHES = 8'h2d) (
    input wire clk, clk_cpu, clk_pixel, clk_pin, cold_reset, reset_pixel, warm_reset, hold,
    input wire media_present, media_frozen,
    output wire media_write_busy, media_changed,
    input wire snapshot_read_valid,
    input wire [19:0] snapshot_read_address,
    output wire snapshot_read_ready,
    output wire [7:0] snapshot_read_data,
    input wire disk_restart,
    input wire disk_load_valid,
    output wire disk_load_ready, disk_load_done,
    input wire [18:0] disk_load_address,
    input wire [7:0] disk_load_data,
    output wire disk_loaded,
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
    wire fdc_write_busy, fdc_write_valid, fdc_write_ready, fdc_write_error;
    wire [9:0] fdc_write_lba;
    wire [8:0] fdc_write_index;
    wire [7:0] fdc_write_data;
    wire writer_busy, writer_valid, writer_ready, writer_done;
    wire [18:0] writer_address;
    wire [7:0] writer_data;
    wire storage_valid, storage_ready, storage_done;
    wire [18:0] storage_address;
    wire [7:0] storage_data;
    wire shared_read_valid, shared_read_ready, shared_read_reset_safe;
    wire [18:0] shared_read_address;
    wire [7:0] shared_read_data;
    assign media_write_busy = fdc_write_busy || writer_busy;
    pcxt_sector_writer writer (
        .clk(clk), .cold_reset(cold_reset), .sector_valid(fdc_write_valid),
        .sector_lba(fdc_write_lba), .sector_index(fdc_write_index), .sector_data(fdc_write_data),
        .sector_ready(fdc_write_ready), .sector_error(fdc_write_error), .busy(writer_busy),
        .media_changed(media_changed), .storage_valid(writer_valid), .storage_ready(writer_ready),
        .storage_done(writer_done), .storage_address(writer_address), .storage_data(writer_data));
    pcxt_media_arbiter media_arbiter (
        .clk(clk), .cold_reset(cold_reset), .machine_reset(machine_reset),
        .host_valid(disk_storage_valid), .host_ready(disk_storage_ready), .host_done(disk_storage_done),
        .host_address(disk_load_address), .host_data(disk_load_data),
        .guest_valid(writer_valid), .guest_ready(writer_ready), .guest_done(writer_done),
        .guest_address(writer_address), .guest_data(writer_data),
        .storage_valid(storage_valid), .storage_ready(storage_ready), .storage_done(storage_done),
        .storage_address(storage_address), .storage_data(storage_data),
        .fdc_valid(media_read_valid && !writer_busy), .fdc_address(media_read_addr),
        .fdc_ready(media_read_ready), .fdc_data(media_read_data),
        .snapshot_valid(snapshot_read_valid && !snapshot_read_address[19]),
        .snapshot_address(snapshot_read_address[18:0]),
        .snapshot_ready(snapshot_read_ready), .snapshot_data(snapshot_read_data),
        .read_valid(shared_read_valid), .read_reset_safe(shared_read_reset_safe), .read_address(shared_read_address),
        .read_ready(shared_read_ready), .read_data(shared_read_data));
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
    wire disk_storage_ready, disk_storage_done;
    wire disk_storage_valid;
    generate if (FLOPPY) begin: floppy_loader
        pcxt_disk_loader loader (
            .clk(clk), .cold_reset(cold_reset), .restart(disk_restart),
            .load_valid(disk_load_valid), .load_ready(disk_load_ready), .load_done(disk_load_done),
            .load_address(disk_load_address), .loaded(disk_loaded),
            .storage_valid(disk_storage_valid), .storage_ready(disk_storage_ready), .storage_done(disk_storage_done)
        );
    end else begin: no_floppy_loader
        assign disk_loaded = 1'b0;
        assign disk_load_ready = 1'b0;
        assign disk_load_done = 1'b0;
        assign disk_storage_valid = 1'b0;
        wire unused_disk = &{1'b0, disk_restart, disk_load_valid, disk_storage_ready, disk_storage_done};
    end endgenerate
    // Keep the storage/firmware status decode out of the motherboard's reset
    // fanout and PIC priority logic. All execution participants share this
    // registered reset; storage cold reset remains immediate and independent.
    always @(posedge clk)
        machine_reset <= cold_reset || warm_reset || hold || !firmware_loaded || !initialized;
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
    /* verilator lint_off PINCONNECTEMPTY */
    pcxt_machine #(.DIAGNOSTIC_PROBE(DIAGNOSTIC_PROBE), .SEPARATE_CPU_CLOCK(HALF_RATE_BOARD), .FLOPPY(FLOPPY), .FLOPPY_WRITE(FLOPPY_WRITE),
                   .BOARD_CLOCK_HZ(HALF_RATE_BOARD ? 50000000 : 100000000), .SWITCHES(SWITCHES)) machine (
        .clk(clk), .clk_cpu(clk_cpu), .reset(machine_reset), .memory_valid(memory_valid),
        .media_present(media_present && disk_loaded), .cold_reset(cold_reset), .media_frozen(media_frozen),
        .media_write_busy(fdc_write_busy), .media_write_valid(fdc_write_valid),
        .media_write_lba(fdc_write_lba), .media_write_index(fdc_write_index),
        .media_write_data(fdc_write_data), .media_write_ready(fdc_write_ready),
        .media_write_error(fdc_write_error), .media_read_valid(media_read_valid),
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
        .disk_load_valid(storage_valid), .disk_load_ready(storage_ready),
        .disk_load_address(storage_address), .disk_load_data(storage_data), .disk_load_done(storage_done),
        .media_read_valid(shared_read_valid), .media_read_reset_safe(shared_read_reset_safe),
        .media_read_addr(shared_read_address),
        .media_read_ready(shared_read_ready), .media_read_data(shared_read_data),
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
