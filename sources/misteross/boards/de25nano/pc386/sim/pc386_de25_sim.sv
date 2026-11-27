// SPDX-License-Identifier: GPL-2.0-or-later
`include "fes_application.vh"
// The DE25 pc386 machine for system_tb: the derived pc386_system (no display
// plane) behind the board's window path -- fes_de25_hps_window's guards,
// rebase and bounds, then fes_de25_window_arbiter -- with the port list
// system_tb drives, so the unchanged bench boots FreeDOS through it.
//
// The arbiter's one 128-bit master comes out as port 0, in word addresses;
// ports 1 and 2 are idle. The grant is the DE10 window base, so window
// offsets reach the bench's window model unchanged. The board's fabric
// beyond the arbiter is never held (the guards are inside), so the bench in
// FES_PC386_DE25 mode never holds port 0. A write burst's response follows
// its last beat, as the board's bridge returns it. The session display is the
// board's, so its opcodes answer as no plane: no response, quiesced.
// dbg_p2_written and dbg_p2_owed give the bench pc386_system's own port 2,
// where a hold now cuts a write burst.
module pc386_de25_sim #(
    parameter integer ISA_N = 1, ISA_D = 12,
    parameter integer PIT_N = 21, PIT_D = 880,
    parameter integer RTC_N = 256, RTC_D = 781250,
    parameter integer US_N = 1, US_D = 100,
    parameter integer DMA_N = 1, DMA_D = 24,
    parameter integer DOT0_N = 1007, DOT0_D = 4000,
    parameter integer DOT1_N = 14161, DOT1_D = 50000,
    parameter integer OPL2_N = 7, OPL2_D = 14080,
    parameter integer AUDIO_N = 3, AUDIO_D = 6250,
    parameter integer GOV_N = 1, GOV_D = 1000,
    parameter integer ATA_RESET_CLOCKS = 1000,
    parameter ROM_HEX = "build/firmware/pc386/pc386-rom.hex",
    // The CPU identity and the 387, as the DE25 producer elaborates them
    // (pc386/build.py I486 and X87_PRESENT), both passed on to pc386_system
    parameter bit I486 = 1'b1,
    parameter integer X87_PRESENT = 1
) (
    input wire clk, pixel_clk, pixel_locked,
    input wire [31:0] gpo,
    input wire [127:0] build_id,
    output wire [31:0] gpi,
    output wire exec_reset,
    output wire ddr_hold,
    input wire p0_reset, p1_reset, p2_reset,
    output wire [27:0] video,
    output wire signed [15:0] sample,
    output wire [6*64-1:0] status,
    output wire debug_valid,
    output wire [7:0] debug_byte,
    output wire [27:0] p0_address,
    output wire [7:0] p0_burstcount,
    output wire p0_read, p0_write,
    output wire [127:0] p0_writedata,
    output wire [15:0] p0_byteenable,
    input wire p0_waitrequest,
    input wire [127:0] p0_readdata,
    input wire p0_readdatavalid,
    output wire [28:0] p1_address,
    output wire [7:0] p1_burstcount,
    output wire p1_read, p1_write,
    output wire [63:0] p1_writedata,
    output wire [7:0] p1_byteenable,
    input wire p1_waitrequest,
    input wire [63:0] p1_readdata,
    input wire p1_readdatavalid,
    output wire [28:0] p2_address,
    output wire [7:0] p2_burstcount,
    output wire p2_read, p2_write,
    output wire [63:0] p2_writedata,
    output wire [7:0] p2_byteenable,
    input wire p2_waitrequest,
    input wire [63:0] p2_readdata,
    input wire p2_readdatavalid,
    output reg [63:0] dbg_p2_written = 64'd0,
    output reg [7:0] dbg_p2_owed = 8'd0,
    // For the bench's failure report: the window's fault record, the arbiter's
    // idle, and each machine port's command and waitrequest.
    output wire [31:0] dbg_window,
    output wire window_drained,
    // For the bench's status-ring report: the writer's busy and own, the
    // disk's aux read and write, and the CRC pass.
    output wire [4:0] dbg_status
);
    assign dbg_status = {system.verifying, system.disk_aux_write, system.disk_aux_read,
                         system.status_ring.own, system.status_ring.busy};
    wire q0_reset, q1_reset, q2_reset, q0_drained, q1_drained, q2_drained;
    wire q0_guard_reset, q1_guard_reset, q2_guard_reset, startup_hold;
    fes_de25_pc386_reset_hold reset_hold (
        .clk(clk), .machine_reset_n(1'b1), .grant_valid(1'b1),
        .execution_held(exec_reset), .ports_held(ddr_hold), .held(startup_hold)
    );
    assign q0_reset = q0_guard_reset || startup_hold;
    assign q1_reset = q1_guard_reset || startup_hold;
    assign q2_reset = q2_guard_reset || startup_hold;
    wire q0_read, q0_write, q0_waitrequest, q0_readdatavalid;
    wire [27:0] q0_address;
    wire [7:0] q0_burstcount;
    wire [127:0] q0_writedata, q0_readdata;
    wire [15:0] q0_byteenable;
    wire q1_read, q1_write, q1_waitrequest, q1_readdatavalid, q2_read, q2_write, q2_waitrequest, q2_readdatavalid;
    wire [28:0] q1_address, q2_address;
    wire [7:0] q1_burstcount, q2_burstcount, q1_byteenable, q2_byteenable;
    wire [63:0] q1_writedata, q1_readdata, q2_writedata, q2_readdata;
    /* verilator lint_off PINCONNECTEMPTY */
    pc386_system #(
        .ISA_N(ISA_N), .ISA_D(ISA_D), .PIT_N(PIT_N), .PIT_D(PIT_D), .RTC_N(RTC_N), .RTC_D(RTC_D),
        .US_N(US_N), .US_D(US_D), .DMA_N(DMA_N), .DMA_D(DMA_D), .DOT0_N(DOT0_N), .DOT0_D(DOT0_D),
        .DOT1_N(DOT1_N), .DOT1_D(DOT1_D), .OPL2_N(OPL2_N), .OPL2_D(OPL2_D),
        .AUDIO_N(AUDIO_N), .AUDIO_D(AUDIO_D), .GOV_N(GOV_N), .GOV_D(GOV_D),
        .ATA_RESET_CLOCKS(ATA_RESET_CLOCKS), .ROM_HEX(ROM_HEX), .I486(I486), .X87_PRESENT(X87_PRESENT)
    ) system (
        .clk(clk), .pixel_clk(pixel_clk), .pixel_locked(pixel_locked),
        .gpo(gpo), .build_id(build_id), .gpi(gpi), .exec_reset(exec_reset), .ddr_hold(ddr_hold),
        .p0_reset(q0_reset), .p1_reset(q1_reset), .p2_reset(q2_reset),
        .video(video), .display_request(), .display_opcode(), .display_index(), .display_argument(),
        .display_response_valid(1'b0), .display_response_error(1'b0), .display_response_data(16'd0),
        .display_quiesced(1'b1),
        .sample(sample), .status(status), .debug_valid(debug_valid), .debug_byte(debug_byte),
        .p0_address(q0_address), .p0_burstcount(q0_burstcount), .p0_read(q0_read), .p0_write(q0_write),
        .p0_writedata(q0_writedata), .p0_byteenable(q0_byteenable), .p0_waitrequest(q0_waitrequest),
        .p0_readdata(q0_readdata), .p0_readdatavalid(q0_readdatavalid),
        .p1_address(q1_address), .p1_burstcount(q1_burstcount), .p1_read(q1_read), .p1_write(q1_write),
        .p1_writedata(q1_writedata), .p1_byteenable(q1_byteenable), .p1_waitrequest(q1_waitrequest),
        .p1_readdata(q1_readdata), .p1_readdatavalid(q1_readdatavalid),
        .p2_address(q2_address), .p2_burstcount(q2_burstcount), .p2_read(q2_read), .p2_write(q2_write),
        .p2_writedata(q2_writedata), .p2_byteenable(q2_byteenable), .p2_waitrequest(q2_waitrequest),
        .p2_readdata(q2_readdata), .p2_readdatavalid(q2_readdatavalid)
    );
    /* verilator lint_on PINCONNECTEMPTY */

    wire [31:0] w0_address, w1_address, w2_address, m_address;
    wire [7:0] w0_burstcount, w1_burstcount, w2_burstcount, m_burstcount;
    wire w0_read, w0_write, w1_read, w1_write, w2_read, w2_write, m_read, m_write;
    wire [127:0] w0_writedata, w0_readdata, m_writedata;
    wire [15:0] w0_byteenable, m_byteenable;
    wire [63:0] w1_writedata, w1_readdata, w2_writedata, w2_readdata;
    wire [7:0] w1_byteenable, w2_byteenable;
    wire w0_waitrequest, w0_readdatavalid, w1_waitrequest, w1_readdatavalid, w2_waitrequest, w2_readdatavalid;
    wire idle, fault_no_grant, fault_outside;
    wire [2:0] fault_port;
    fes_de25_hps_window window (
        .reset_n(1'b1), .hold(ddr_hold || startup_hold),
        .grant_valid(1'b1), .grant_base(`FES_APPLICATION_HPS_DDR_WINDOW_BASE),
        .fault_clear(1'b0), .fault_port(fault_port), .fault_no_grant(fault_no_grant), .fault_outside(fault_outside),
        .p0_clk(clk), .p0_reset(q0_guard_reset), .p0_drained(q0_drained), .p0_address(q0_address),
        .p0_burstcount(q0_burstcount), .p0_waitrequest(q0_waitrequest), .p0_readdata(q0_readdata),
        .p0_readdatavalid(q0_readdatavalid), .p0_read(q0_read), .p0_writedata(q0_writedata),
        .p0_byteenable(q0_byteenable), .p0_write(q0_write),
        .p1_clk(clk), .p1_reset(q1_guard_reset), .p1_drained(q1_drained), .p1_address(q1_address),
        .p1_burstcount(q1_burstcount), .p1_waitrequest(q1_waitrequest), .p1_readdata(q1_readdata),
        .p1_readdatavalid(q1_readdatavalid), .p1_read(q1_read), .p1_writedata(q1_writedata),
        .p1_byteenable(q1_byteenable), .p1_write(q1_write),
        .p2_clk(clk), .p2_reset(q2_guard_reset), .p2_drained(q2_drained), .p2_address(q2_address),
        .p2_burstcount(q2_burstcount), .p2_waitrequest(q2_waitrequest), .p2_readdata(q2_readdata),
        .p2_readdatavalid(q2_readdatavalid), .p2_read(q2_read), .p2_writedata(q2_writedata),
        .p2_byteenable(q2_byteenable), .p2_write(q2_write),
        .m0_address(w0_address), .m0_burstcount(w0_burstcount), .m0_read(w0_read), .m0_write(w0_write),
        .m0_writedata(w0_writedata), .m0_byteenable(w0_byteenable), .m0_waitrequest(w0_waitrequest),
        .m0_readdata(w0_readdata), .m0_readdatavalid(w0_readdatavalid),
        .m1_address(w1_address), .m1_burstcount(w1_burstcount), .m1_read(w1_read), .m1_write(w1_write),
        .m1_writedata(w1_writedata), .m1_byteenable(w1_byteenable), .m1_waitrequest(w1_waitrequest),
        .m1_readdata(w1_readdata), .m1_readdatavalid(w1_readdatavalid),
        .m2_address(w2_address), .m2_burstcount(w2_burstcount), .m2_read(w2_read), .m2_write(w2_write),
        .m2_writedata(w2_writedata), .m2_byteenable(w2_byteenable), .m2_waitrequest(w2_waitrequest),
        .m2_readdata(w2_readdata), .m2_readdatavalid(w2_readdatavalid)
    );
    // Model write replies that remain in flight after the last beat.
    reg [7:0] m_write_left = 8'd0;
    reg [31:0] write_responses = 32'd0;
    wire m_write_beat = m_write && !p0_waitrequest;
    wire m_write_last = m_write_beat && (m_write_left == 8'd0 ? m_burstcount == 8'd1 : m_write_left == 8'd1);
    always @(posedge clk) begin
        write_responses <= {write_responses[30:0], m_write_last};
        if (m_write_beat) m_write_left <= m_write_left == 8'd0 ? m_burstcount - 8'd1 : m_write_left - 8'd1;
    end
    fes_de25_window_arbiter arbiter (
        .clk(clk), .reset(startup_hold),
        .s0_address(w0_address), .s0_burstcount(w0_burstcount), .s0_read(w0_read), .s0_write(w0_write),
        .s0_writedata(w0_writedata), .s0_byteenable(w0_byteenable), .s0_waitrequest(w0_waitrequest),
        .s0_readdata(w0_readdata), .s0_readdatavalid(w0_readdatavalid),
        .s1_address(w1_address), .s1_burstcount(w1_burstcount), .s1_read(w1_read), .s1_write(w1_write),
        .s1_writedata(w1_writedata), .s1_byteenable(w1_byteenable), .s1_waitrequest(w1_waitrequest),
        .s1_readdata(w1_readdata), .s1_readdatavalid(w1_readdatavalid),
        .s2_address(w2_address), .s2_burstcount(w2_burstcount), .s2_read(w2_read), .s2_write(w2_write),
        .s2_writedata(w2_writedata), .s2_byteenable(w2_byteenable), .s2_waitrequest(w2_waitrequest),
        .s2_readdata(w2_readdata), .s2_readdatavalid(w2_readdatavalid),
        .m_address(m_address), .m_burstcount(m_burstcount), .m_read(m_read), .m_write(m_write),
        .m_writedata(m_writedata), .m_byteenable(m_byteenable), .m_waitrequest(p0_waitrequest),
        .m_readdata(p0_readdata), .m_readdatavalid(p0_readdatavalid),
        .m_writeresponsevalid(write_responses[31]), .idle(idle)
    );
    assign window_drained = idle && q0_drained && q1_drained && q2_drained;
    assign p0_address = m_address[31:4];
    assign p0_burstcount = m_burstcount;
    assign p0_read = m_read;
    assign p0_write = m_write;
    assign p0_writedata = m_writedata;
    assign p0_byteenable = m_byteenable;
    assign {p1_address, p1_burstcount, p1_read, p1_write, p1_writedata, p1_byteenable} = 111'd0;
    assign {p2_address, p2_burstcount, p2_read, p2_write, p2_writedata, p2_byteenable} = 111'd0;

    // pc386_system's own port 2 write beats, and those still owed in its open burst.
    // A hold resets the master mid-burst; the count clears a clock after the
    // port's reset rises, once the bench has sampled it.
    wire q2_beat = q2_write && !q2_waitrequest;
    reg q2_reset_seen = 1'b0;
    always @(posedge clk) begin
        q2_reset_seen <= q2_reset;
        if (q2_reset && q2_reset_seen) dbg_p2_owed <= 8'd0;
        else if (q2_beat) begin
            dbg_p2_written <= dbg_p2_written + 64'd1;
            dbg_p2_owed <= dbg_p2_owed == 8'd0 ? q2_burstcount - 8'd1 : dbg_p2_owed - 8'd1;
        end
    end
    // [31:16]: arbiter state and the machine-side drain prerequisites.
    assign dbg_window = {arbiter.busy, arbiter.stalled, arbiter.order_count, arbiter.owed1 != 10'd0,
                         arbiter.owed2 != 10'd0, arbiter.writes_owed != 8'd0, arbiter.sel,
                         system.cpu_mem_idle, system.rom_idle, system.disk_writer_busy,
                         system.fdc_busy, system.verifying, fault_port, fault_no_grant, fault_outside,
                         idle, q0_read, q0_write, q0_waitrequest,
                         q1_read || q1_write, q1_waitrequest, q2_read || q2_write, q2_waitrequest, q0_drained && q1_drained && q2_drained};
    wire unused = ^{p0_reset, p1_reset, p2_reset, p1_waitrequest, p1_readdata, p1_readdatavalid, p2_waitrequest,
                    p2_readdata, p2_readdatavalid, m_address[3:0]};
endmodule
