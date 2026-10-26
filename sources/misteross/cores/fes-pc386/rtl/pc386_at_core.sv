// SPDX-License-Identifier: MIT
// pc386 AT core (docs/pc386-machine-design.md, "Memory, I/O and DDR maps"):
// the on-board devices behind the CPU's I/O, INTA, A20, reset and DMA ports.
//
//   000-00F DMA 1     020-021 PIC 1    040-043 PIT (8254)   060, 064 8042
//   061 port B        070-071 RTC      080 POST             081-08F DMA pages
//   092 port A        0A0-0A1 PIC 2    0C0-0DF DMA 2        1F0-1F7, 3F6 ATA
//   220-22F or 240-24F, 388-389 sound port                  3B0-3DF VGA port
//   400-403, 500, E9 debug sink                             anything else FFh
//
// Every guest time base is a rational enable of the system clock; the
// defaults are for 100 MHz. A20M is asserted to the CPU when both the 8042
// output port bit 1 and port 92h bit 1 are clear. A CPU reset (8042 pulse,
// port 92h bit 0 edge, or SHUTDOWN) first asks the CPU to FLUSH its L2, then
// pulses cpu_reset; RAM, CMOS, A20 and every device keep their state.
module pc386_at_core #(
    parameter integer ISA_N = 1, ISA_D = 12,           // 8.33 MHz ISA clock
    parameter integer PIT_N = 21, PIT_D = 880,         // twice 1,193,181.8 Hz: the PIT clock's edges
    parameter integer RTC_N = 256, RTC_D = 781250,     // 32,768 Hz
    parameter integer US_N = 1, US_D = 100,            // 1 MHz: PS/2 pacing
    parameter integer DMA_N = 1, DMA_D = 24,           // 4.17 MHz DMA clock
    parameter integer ATA_RESET_CLOCKS = 1000
) (
    input wire clk, reset,
    // CPU I/O and interrupt acknowledge
    input wire io_valid, io_write,
    input wire [15:0] io_port,
    input wire [1:0] io_size,
    input wire [31:0] io_wdata,
    input wire inta_valid,
    output wire io_busy, io_done, inta_done,
    output wire [31:0] io_rdata,
    output wire [7:0] inta_vector,
    output wire intr,
    output wire a20m,
    input wire cpu_shutdown,
    output reg flush_request,
    input wire flush_done,
    output reg cpu_reset,
    // CPU DMA port, for the 8-bit channels' memory cycles
    output reg dma_valid, dma_write,
    output reg [23:0] dma_address,
    output reg [7:0] dma_wdata,
    input wire dma_done,
    input wire [7:0] dma_rdata,
    // On-board DMA devices on channels 1-3
    input wire [3:1] dma_drq,
    output reg dma_ack_valid, dma_ack_write, dma_ack_tc,
    output reg [1:0] dma_ack_channel,
    output reg [7:0] dma_ack_wdata,
    input wire dma_ack_done,
    input wire [7:0] dma_ack_rdata,
    // VGA ports
    output wire vga_valid, vga_write,
    output wire [15:0] vga_port,
    output wire [7:0] vga_wdata,
    input wire vga_done,
    input wire [7:0] vga_rdata,
    // Sound port: the Sound Blaster base range and 388h-389h, isa8 cycles
    output wire sound_access, sound_write, sound_adlib,
    output wire [3:0] sound_register,
    output wire [7:0] sound_wdata,
    input wire [7:0] sound_rdata,
    // Interrupts from other devices (Sound Blaster), ORed in
    input wire [15:0] device_irq,
    output wire speaker,
    output wire nmi_mask,
    // Trace
    output wire post_valid,
    output wire [7:0] post_code,
    output reg debug_valid,
    output reg [15:0] debug_port,
    output reg [7:0] debug_byte,
    // Keyboard and mouse
    input wire key_valid,
    input wire [7:0] key_code,
    output wire key_ready,
    input wire mouse_valid,
    input wire signed [7:0] mouse_dx, mouse_dy,
    input wire [1:0] mouse_buttons,
    // Configuration, written into CMOS after reset
    input wire [15:0] base_kib, extended_kib, above16m_64k,
    input wire [7:0] sb_base_high, sb_irq, sb_dma,
    output wire cmos_busy,
    input wire wall_valid,
    input wire [15:0] wall_year,
    input wire [7:0] wall_month, wall_day, wall_hour, wall_minute, wall_second, wall_weekday,
    // Disk
    input wire disk_present,
    input wire [31:0] disk_sectors,
    input wire [15:0] disk_cylinders,
    input wire [4:0] disk_heads,
    input wire [5:0] disk_spt,
    output wire disk_valid, disk_write,
    output wire [23:0] disk_address,
    output wire [63:0] disk_wdata,
    input wire disk_ready, disk_rvalid,
    input wire [63:0] disk_rdata,
    input wire [6:0] dirty_index,
    output wire [15:0] dirty_word
);
    // ---- I/O decoder ----------------------------------------------------
    wire [15:0] port;
    wire cycle_write, cycle_word, cycle_inta, cycle_cs, cycle_strobe, cycle_access;
    wire [15:0] cycle_wdata;
    reg [15:0] cycle_rdata;
    pc386_io #(.ISA_N(ISA_N), .ISA_D(ISA_D)) decoder (
        .clk(clk), .reset(reset),
        .io_valid(io_valid), .io_write(io_write), .io_port(io_port), .io_size(io_size), .io_wdata(io_wdata),
        .inta_valid(inta_valid), .busy(io_busy), .io_done(io_done), .inta_done(inta_done),
        .io_rdata(io_rdata), .inta_vector(inta_vector),
        .cycle_port(port), .cycle_write(cycle_write), .cycle_word(cycle_word), .cycle_inta(cycle_inta),
        .cycle_wdata(cycle_wdata), .cycle_cs(cycle_cs), .cycle_strobe(cycle_strobe), .cycle_access(cycle_access),
        .cycle_rdata(cycle_rdata),
        .vga_valid(vga_valid), .vga_done(vga_done), .vga_rdata(vga_rdata)
    );
    assign vga_write = cycle_write;
    assign vga_port = port;
    assign vga_wdata = cycle_wdata[7:0];

    wire io_cycle = !cycle_inta;
    wire select_dma1 = io_cycle && port[15:4] == 12'h000;
    wire select_pic1 = io_cycle && port[15:1] == 15'h0010;
    wire select_pit = io_cycle && port[15:2] == 14'h0010;
    wire select_kbc = io_cycle && (port == 16'h0060 || port == 16'h0064);
    wire select_port_b = io_cycle && port == 16'h0061;
    wire select_rtc = io_cycle && port[15:1] == 15'h0038;
    wire select_post = io_cycle && port == 16'h0080;
    wire select_page = io_cycle && port[15:4] == 12'h008 && port[3:0] != 0;
    wire select_port_a = io_cycle && port == 16'h0092;
    wire select_pic2 = io_cycle && port[15:1] == 15'h0050;
    wire select_dma2 = io_cycle && port[15:5] == 11'h006;
    wire select_ata_block = io_cycle && port[15:3] == 13'h003e;
    wire select_ata_control = io_cycle && port == 16'h03f6;
    wire select_sound = io_cycle && (port[15:4] == {4'h0, sb_base_high} || port[15:1] == 15'h01c4);
    wire select_debug = io_cycle && ((port >= 16'h0400 && port <= 16'h0403) || port == 16'h0500 || port == 16'h00e9);
    wire access = cycle_access && io_cycle;
    wire read_strobe = cycle_strobe && !cycle_write && io_cycle;
    wire write_strobe = cycle_strobe && cycle_write;
    wire inta_strobe = cycle_strobe && cycle_inta;

    assign sound_access = access && select_sound;
    assign sound_write = cycle_write;
    assign sound_adlib = port[15:4] != {4'h0, sb_base_high};
    assign sound_register = port[3:0];
    assign sound_wdata = cycle_wdata[7:0];

    // ---- Interrupts -------------------------------------------------------
    wire pit_out0, pit_out1, pit_out2, irq1, irq12, irq8, irq14;
    wire [7:0] pic_data;
    pc386_pic pic (
        .clk(clk), .reset(reset),
        .master_cs(cycle_cs && select_pic1), .slave_cs(cycle_cs && select_pic2),
        .read_strobe(read_strobe), .write_strobe(write_strobe), .inta_strobe(inta_strobe),
        .a0(port[0]), .wdata(cycle_wdata[7:0]), .rdata(pic_data),
        .irq(device_irq | {1'b0, irq14, 1'b0, irq12, 3'b000, irq8, 6'b000000, irq1, pit_out0}),
        .intr(intr)
    );

    // ---- PIT: an 8254 -----------------------------------------------------
    wire pit_tick;
    reg pit_clock;
    wire pit_gate2;
    wire [7:0] pit_data;
    pc386_rate #(.N(PIT_N), .D(PIT_D)) pit_rate (.clk(clk), .reset(reset), .tick(pit_tick));
    always @(posedge clk) begin
        if (reset) pit_clock <= 0;
        else if (pit_tick) pit_clock <= !pit_clock;
    end
    KF8253 #(.READ_BACK(1)) pit (
        .clock(clk), .reset(reset),
        .chip_select_n(!(cycle_cs && select_pit)),
        .read_enable_n(!read_strobe), .write_enable_n(!write_strobe), .address(port[1:0]),
        .data_bus_in(cycle_wdata[7:0]), .data_bus_out(pit_data),
        .counter_0_clock(pit_clock), .counter_0_gate(1'b1), .counter_0_out(pit_out0),
        .counter_1_clock(pit_clock), .counter_1_gate(1'b1), .counter_1_out(pit_out1),
        .counter_2_clock(pit_clock), .counter_2_gate(pit_gate2), .counter_2_out(pit_out2)
    );

    // ---- Ports 61h, 92h and 80h -------------------------------------------
    wire [7:0] sysctl_data;
    wire speaker_enable, port_a_a20, fast_reset;
    pc386_sysctl sysctl (
        .clk(clk), .reset(reset), .access(access), .write(cycle_write),
        .select_b(select_port_b), .select_a(select_port_a), .select_post(select_post),
        .wdata(cycle_wdata[7:0]), .rdata(sysctl_data),
        .pit_out1(pit_out1), .pit_out2(pit_out2), .pit_gate2(pit_gate2), .speaker_enable(speaker_enable),
        .port_a_a20(port_a_a20), .fast_reset(fast_reset),
        .post_valid(post_valid), .post_code(post_code)
    );
    assign speaker = pit_out2 && speaker_enable;

    // ---- DMA: controller 1 with a data path, controller 2 registers only ---
    // AT page registers 81h-8Fh are all readable; 87h, 83h, 81h and 82h page
    // channels 0-3 and 8Fh, 8Bh, 89h and 8Ah channels 4-7.
    reg [7:0] pages [1:15];
    reg [1:0] page_channel;
    reg page_first, page_second;
    always @* begin
        page_first = 1; page_second = 0; page_channel = 0;
        case (port[3:0])
            4'h7: page_channel = 0;
            4'h3: page_channel = 1;
            4'h1: page_channel = 2;
            4'h2: page_channel = 3;
            4'hf: begin page_first = 0; page_second = 1; page_channel = 0; end
            4'hb: begin page_first = 0; page_second = 1; page_channel = 1; end
            4'h9: begin page_first = 0; page_second = 1; page_channel = 2; end
            4'ha: begin page_first = 0; page_second = 1; page_channel = 3; end
            default: page_first = 0;
        endcase
    end
    always @(posedge clk) if (access && cycle_write && select_page) pages[port[3:0]] <= cycle_wdata[7:0];
    wire page_write = access && cycle_write && select_page;

    wire dma_tick;
    pc386_rate #(.N(DMA_N), .D(DMA_D)) dma_rate (.clk(clk), .reset(reset), .tick(dma_tick));
    wire [7:0] dma1_data, dma2_data;
    wire dma1_valid, dma1_write, dma1_tc, dma2_valid, dma1_hold, dma2_hold;
    wire [23:0] dma1_address;
    wire [7:0] dma1_wdata;
    wire [1:0] dma1_kind, dma1_channel;
    reg dma1_ready, dma1_done, dma2_done;
    reg [7:0] dma1_rdata;
    /* verilator lint_off PINCONNECTEMPTY */
    fes_dma8237 #(.ADDRESS_BITS(24), .AT_PAGES(1), .REFRESH(0)) dma1 (
        .clk(clk), .reset(reset), .guest_ce(dma_tick),
        .reg_read(access && !cycle_write && select_dma1), .reg_write(access && cycle_write && select_dma1),
        .page_write(page_write && page_first),
        .reg_address(select_page ? {2'b00, page_channel} : port[3:0]), .reg_wdata(cycle_wdata[7:0]),
        .reg_rdata(dma1_data),
        .drq(dma_drq), .refresh_clock(1'b0), .hold_request(dma1_hold), .granted(dma1_hold),
        .cpu_waiting(1'b0), .cpu_done(1'b0),
        .cycle_valid(dma1_valid), .cycle_ready(dma1_ready), .cycle_done(dma1_done),
        .cycle_address(dma1_address), .cycle_wdata(dma1_wdata), .cycle_kind(dma1_kind),
        .cycle_channel(dma1_channel), .cycle_write(dma1_write), .cycle_tc(dma1_tc),
        .cycle_rdata(dma1_rdata), .cycle_fault(2'b00), .fault(), .refresh_done()
    );
    fes_dma8237 #(.ADDRESS_BITS(24), .AT_PAGES(1), .REFRESH(0), .WORD_ADDRESS(1)) dma2 (
        .clk(clk), .reset(reset), .guest_ce(dma_tick),
        .reg_read(access && !cycle_write && select_dma2), .reg_write(access && cycle_write && select_dma2),
        .page_write(page_write && page_second),
        .reg_address(select_page ? {2'b00, page_channel} : port[4:1]), .reg_wdata(cycle_wdata[7:0]),
        .reg_rdata(dma2_data),
        .drq(3'b000), .refresh_clock(1'b0), .hold_request(dma2_hold), .granted(dma2_hold),
        .cpu_waiting(1'b0), .cpu_done(1'b0),
        .cycle_valid(dma2_valid), .cycle_ready(1'b1), .cycle_done(dma2_done),
        .cycle_address(), .cycle_wdata(), .cycle_kind(), .cycle_channel(), .cycle_write(), .cycle_tc(),
        .cycle_rdata(8'hff), .cycle_fault(2'b00), .fault(), .refresh_done()
    );
    /* verilator lint_on PINCONNECTEMPTY */

    // Controller 1's memory cycles go to the CPU's DMA port, its device cycles
    // to the on-board device's DACK. Controller 2 has no data path: a cycle it
    // starts (only a software request can) completes with no transfer.
    reg dma1_busy;
    always @(posedge clk) begin
        dma1_ready <= 0;
        dma1_done <= 0;
        dma2_done <= dma2_valid;
        if (reset) begin
            dma1_busy <= 0; dma1_rdata <= 8'hff;
            dma_valid <= 0; dma_write <= 0; dma_address <= 0; dma_wdata <= 0;
            dma_ack_valid <= 0; dma_ack_write <= 0; dma_ack_tc <= 0; dma_ack_channel <= 0; dma_ack_wdata <= 0;
        end else if (!dma1_busy) begin
            if (dma1_valid && !dma1_ready) begin
                dma1_ready <= 1;
                dma1_busy <= 1;
                if (dma1_kind == 0) begin
                    dma_valid <= 1; dma_write <= dma1_write; dma_address <= dma1_address; dma_wdata <= dma1_wdata;
                end else begin
                    dma_ack_valid <= 1; dma_ack_write <= dma1_write; dma_ack_tc <= dma1_tc;
                    dma_ack_channel <= dma1_channel; dma_ack_wdata <= dma1_wdata;
                end
            end
        end else if ((dma_valid && dma_done) || (dma_ack_valid && dma_ack_done)) begin
            dma1_rdata <= dma_valid ? dma_rdata : dma_ack_rdata;
            dma_valid <= 0;
            dma_ack_valid <= 0;
            dma1_done <= 1;
            dma1_busy <= 0;
        end
    end

    // ---- 8042 ---------------------------------------------------------------
    wire [7:0] kbc_data;
    wire kbc_a20, kbc_reset;
    pc386_kbc #(.US_N(US_N), .US_D(US_D)) kbc (
        .clk(clk), .reset(reset), .access(access && select_kbc), .write(cycle_write), .a2(port[2]),
        .wdata(cycle_wdata[7:0]), .rdata(kbc_data), .irq1(irq1), .irq12(irq12),
        .a20(kbc_a20), .cpu_reset(kbc_reset),
        .key_valid(key_valid), .key_code(key_code), .key_ready(key_ready),
        .mouse_valid(mouse_valid), .mouse_dx(mouse_dx), .mouse_dy(mouse_dy), .mouse_buttons(mouse_buttons)
    );
    assign a20m = !kbc_a20 && !port_a_a20;

    // ---- RTC and CMOS -------------------------------------------------------
    wire [7:0] rtc_data;
    pc386_rtc #(.RTC_N(RTC_N), .RTC_D(RTC_D)) rtc (
        .clk(clk), .reset(reset), .access(access && select_rtc), .write(cycle_write), .a0(port[0]),
        .wdata(cycle_wdata[7:0]), .rdata(rtc_data), .irq8(irq8), .nmi_mask(nmi_mask), .busy(cmos_busy),
        .base_kib(base_kib), .extended_kib(extended_kib), .above16m_64k(above16m_64k),
        .disk_present(disk_present), .disk_cylinders(disk_cylinders),
        .disk_heads({3'b000, disk_heads}), .disk_spt({2'b00, disk_spt}),
        .sb_base_high(sb_base_high), .sb_irq(sb_irq), .sb_dma(sb_dma),
        .wall_valid(wall_valid), .wall_year(wall_year), .wall_month(wall_month), .wall_day(wall_day),
        .wall_hour(wall_hour), .wall_minute(wall_minute), .wall_second(wall_second), .wall_weekday(wall_weekday)
    );

    // ---- ATA ----------------------------------------------------------------
    wire [15:0] ata_data;
    pc386_ata #(.RESET_CLOCKS(ATA_RESET_CLOCKS)) ata (
        .clk(clk), .reset(reset),
        .access(access && (select_ata_block || select_ata_control)), .write(cycle_write), .word(cycle_word),
        .command_block(select_ata_block), .register(port[2:0]), .wdata(cycle_wdata), .rdata(ata_data),
        .irq14(irq14),
        .present(disk_present), .sectors(disk_sectors), .cylinders(disk_cylinders), .heads(disk_heads), .spt(disk_spt),
        .disk_valid(disk_valid), .disk_write(disk_write), .disk_address(disk_address), .disk_wdata(disk_wdata),
        .disk_ready(disk_ready), .disk_rvalid(disk_rvalid), .disk_rdata(disk_rdata),
        .dirty_index(dirty_index), .dirty_word(dirty_word)
    );

    // ---- Read data ----------------------------------------------------------
    always @* begin
        cycle_rdata = 16'hffff;
        if (cycle_inta) cycle_rdata = {8'hff, pic_data};
        else if (select_dma1) cycle_rdata = {8'hff, dma1_data};
        else if (select_pic1 || select_pic2) cycle_rdata = {8'hff, pic_data};
        else if (select_pit) cycle_rdata = {8'hff, pit_data};
        else if (select_kbc) cycle_rdata = {8'hff, kbc_data};
        else if (select_port_b || select_port_a || select_post) cycle_rdata = {8'hff, sysctl_data};
        else if (select_rtc) cycle_rdata = {8'hff, rtc_data};
        else if (select_page) cycle_rdata = {8'hff, pages[port[3:0]]};
        else if (select_dma2) cycle_rdata = {8'hff, dma2_data};
        else if (select_ata_block || select_ata_control) cycle_rdata = ata_data;
        else if (select_sound) cycle_rdata = {8'hff, sound_rdata};
    end

    // ---- Debug sink, CPU reset and FLUSH -----------------------------------
    reg shutdown_previous;
    always @(posedge clk) begin
        debug_valid <= 0;
        cpu_reset <= 0;
        if (reset) begin
            debug_port <= 0; debug_byte <= 0;
            flush_request <= 0; shutdown_previous <= 0;
        end else begin
            if (access && cycle_write && select_debug) begin
                debug_valid <= 1;
                debug_port <= port;
                debug_byte <= cycle_wdata[7:0];
            end
            shutdown_previous <= cpu_shutdown;
            if (kbc_reset || fast_reset || (cpu_shutdown && !shutdown_previous)) flush_request <= 1;
            else if (flush_request && flush_done) begin
                flush_request <= 0;
                cpu_reset <= 1;
            end
        end
    end
    wire unused = ^{dma1_hold, dma2_hold, dma2_valid, cycle_word};
endmodule
