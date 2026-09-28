// SPDX-License-Identifier: GPL-2.0-or-later
// ZX Spectrum 48K pathfinder.
//
// Z80 at a 3.5 MHz average (875/13056 of the 52.224 MHz system clock).
// 16 KiB linked ROM at $0000, 48 KiB RAM at $4000. Port $FE is the ULA
// (border, beeper, keyboard, EAR). Port $1F is a built-in Kempston joystick.
// Four edge sockets share one request word. There is no ULA contention and
// no floating bus: unclaimed I/O reads $FF. Maskable interrupt is asserted
// for 32 T-states every 69888 T-states, independent of the 60 Hz HDMI scan.
`include "spectrum_bus.vh"

module spectrum_machine (
    input  wire        clk_sys,
    input  wire        reset,
    input  wire [39:0] matrix,
    input  wire [4:0]  kempston,
    input  wire [1:0]  unit_state,
    input  wire [31:0] unit_size,
    input  wire [15:0] media_write_addr,
    input  wire [15:0] media_write_data,
    input  wire [1:0]  media_write_enable,
    output wire [`SP_BUS_REQ-1:0] bus_request,
    input  wire [`SP_BUS_RSP-1:0] response1,
    input  wire [`SP_BUS_RSP-1:0] response2,
    input  wire [`SP_BUS_RSP-1:0] response3,
    input  wire [`SP_BUS_RSP-1:0] response4,
    output wire [2:0]  border,
    output wire        flash_on,
    output wire        speaker,
    output wire        ear,
    output wire        cpu_cycle,
    output wire signed [15:0] slot_audio,
    input  wire        video_clk,
    input  wire [15:0] video_addr,
    output wire [7:0]  video_data,
    output wire [7:0]  sig8000,
    output wire [7:0]  sig8001,
    output wire [7:0]  sig8002,
    output wire [7:0]  sig8003,
    output wire [7:0]  sig8004
);
    reg cen_p = 1'b0;
    reg cen_n = 1'b0;
    reg [13:0] phase = 14'd0;
    reg [3:0] tcount = 4'd0;
    localparam [13:0] PHASE_ADD = 14'd875;
    localparam [13:0] PHASE_MOD = 14'd13056;

    always @(posedge clk_sys) begin
        cen_p <= 1'b0;
        cen_n <= 1'b0;
        if (reset) begin
            phase <= 14'd0;
            tcount <= 4'd0;
        end else if (phase + PHASE_ADD >= PHASE_MOD) begin
            phase <= phase + PHASE_ADD - PHASE_MOD;
            tcount <= 4'd0;
            cen_p <= 1'b1;
        end else begin
            phase <= phase + PHASE_ADD;
            if (tcount == 4'd7)
                cen_n <= 1'b1;
            tcount <= tcount + 4'd1;
        end
    end
    assign cpu_cycle = cen_p;

    reg [16:0] frame_t = 17'd0;
    reg int_n = 1'b1;
    reg flash = 1'b0;
    reg [3:0] flash_div = 4'd0;
    always @(posedge clk_sys) begin
        if (reset) begin
            frame_t <= 17'd0;
            int_n <= 1'b1;
            flash <= 1'b0;
            flash_div <= 4'd0;
        end else if (cen_p) begin
            if (frame_t == 17'd69887) begin
                frame_t <= 17'd0;
                if (flash_div == 4'd15) begin
                    flash <= ~flash;
                    flash_div <= 4'd0;
                end else begin
                    flash_div <= flash_div + 4'd1;
                end
            end else begin
                frame_t <= frame_t + 17'd1;
            end
            int_n <= frame_t >= 17'd32;
        end
    end
    assign flash_on = flash;

    wire cpu_m1_n, cpu_mreq_n, cpu_iorq_n, cpu_rd_n, cpu_wr_n, cpu_rfsh_n, cpu_halt_n;
    wire [15:0] cpu_a;
    wire [7:0] cpu_do;
    wire [7:0] cpu_di;
    wire nmi = response1[`SP_BUS_NMI] | response2[`SP_BUS_NMI] |
               response3[`SP_BUS_NMI] | response4[`SP_BUS_NMI];
    wire card_wait = response1[`SP_BUS_WAIT] | response2[`SP_BUS_WAIT] |
                     response3[`SP_BUS_WAIT] | response4[`SP_BUS_WAIT];

    T80pa cpu (
        .RESET_n(~reset),
        .CLK(clk_sys),
        .CEN_p(cen_p),
        .CEN_n(cen_n),
        .WAIT_n(~card_wait),
        .INT_n(int_n),
        .NMI_n(~nmi),
        .BUSRQ_n(1'b1),
        .M1_n(cpu_m1_n),
        .MREQ_n(cpu_mreq_n),
        .IORQ_n(cpu_iorq_n),
        .RD_n(cpu_rd_n),
        .WR_n(cpu_wr_n),
        .RFSH_n(cpu_rfsh_n),
        .HALT_n(cpu_halt_n),
        .BUSAK_n(),
        .A(cpu_a),
        .DO(cpu_do),
        .DI(cpu_di)
    );

    reg rd_q = 1'b1;
    reg wr_q = 1'b1;
    always @(posedge clk_sys) begin
        rd_q <= cpu_rd_n;
        wr_q <= cpu_wr_n;
    end
    wire strobe = (rd_q & ~cpu_rd_n) | (wr_q & ~cpu_wr_n);
    assign bus_request[`SP_BUS_A] = cpu_a;
    assign bus_request[`SP_BUS_D] = cpu_do;
    assign bus_request[`SP_BUS_MREQ] = ~cpu_mreq_n;
    assign bus_request[`SP_BUS_IORQ] = ~cpu_iorq_n;
    assign bus_request[`SP_BUS_RD] = ~cpu_rd_n;
    assign bus_request[`SP_BUS_WR] = ~cpu_wr_n;
    assign bus_request[`SP_BUS_M1] = ~cpu_m1_n;
    assign bus_request[`SP_BUS_STROBE] = strobe;
    assign bus_request[`SP_BUS_RESET] = reset;
    assign bus_request[`SP_BUS_RESERVED] = 1'b0;

    wire [27:0] chosen =
        response1[`SP_BUS_DRIVE] ? response1 :
        response2[`SP_BUS_DRIVE] ? response2 :
        response3[`SP_BUS_DRIVE] ? response3 :
        response4[`SP_BUS_DRIVE] ? response4 : 28'd0;
    wire romcs = response1[`SP_BUS_ROMCS] | response2[`SP_BUS_ROMCS] |
                 response3[`SP_BUS_ROMCS] | response4[`SP_BUS_ROMCS];
    assign slot_audio = chosen[`SP_BUS_DRIVE] ? chosen[`SP_BUS_AUDIO] : 16'sd0;

    wire [7:0] rom_data;
    spectrum_rom rom (
        .clk(clk_sys),
        .address(cpu_a[13:0]),
        .data(rom_data)
    );

    wire ram_hit = cpu_a[15] | cpu_a[14];
    wire writing = ~cpu_mreq_n & ~cpu_wr_n & ram_hit;
    reg writing_q = 1'b0;
    always @(posedge clk_sys) writing_q <= writing;
    wire [7:0] ram_data;
    spectrum_ram ram (
        .clk_sys(clk_sys),
        .cpu_we(writing & ~writing_q),
        .cpu_addr(cpu_a),
        .cpu_wdata(cpu_do),
        .cpu_rdata(ram_data),
        .pixel_clk(video_clk),
        .video_addr(video_addr),
        .video_rdata(video_data),
        .sig8000(sig8000),
        .sig8001(sig8001),
        .sig8002(sig8002),
        .sig8003(sig8003),
        .sig8004(sig8004)
    );

    wire tape_ear;
    spectrum_tape tape (
        .clk(clk_sys),
        .cen(cen_p),
        .reset(reset),
        .unit_state(unit_state),
        .unit_size(unit_size),
        .write_addr(media_write_addr),
        .write_data(media_write_data),
        .write_enable(media_write_enable),
        .ear(tape_ear)
    );
    assign ear = tape_ear;

    reg [2:0] border_q = 3'd7;
    reg speaker_q = 1'b0;
    wire ula_access = ~cpu_iorq_n & cpu_m1_n & ~cpu_a[0];
    wire ula_write = ula_access & ~cpu_wr_n;
    reg ula_write_q = 1'b0;
    always @(posedge clk_sys) begin
        ula_write_q <= ula_write;
        if (reset) begin
            border_q <= 3'd7;
            speaker_q <= 1'b0;
        end else if (ula_write & ~ula_write_q) begin
            border_q <= cpu_do[2:0];
            speaker_q <= cpu_do[4];
        end
    end
    assign border = border_q;
    assign speaker = speaker_q;

    wire [7:0] row_select = ~cpu_a[15:8];
    wire [4:0] keys =
        (row_select[0] ? matrix[4:0] : 5'h1f) &
        (row_select[1] ? matrix[9:5] : 5'h1f) &
        (row_select[2] ? matrix[14:10] : 5'h1f) &
        (row_select[3] ? matrix[19:15] : 5'h1f) &
        (row_select[4] ? matrix[24:20] : 5'h1f) &
        (row_select[5] ? matrix[29:25] : 5'h1f) &
        (row_select[6] ? matrix[34:30] : 5'h1f) &
        (row_select[7] ? matrix[39:35] : 5'h1f);
    wire [7:0] ula_read = {1'b1, tape_ear, 1'b1, keys};
    wire kempston_hit = ~cpu_iorq_n & cpu_m1_n & ~cpu_rd_n & cpu_a[7:0] == 8'h1f;
    wire io_read = ~cpu_iorq_n & cpu_m1_n & ~cpu_rd_n;
    wire mem_read = ~cpu_mreq_n & ~cpu_rd_n;
    wire rom_region = ~cpu_a[15] & ~cpu_a[14];
    wire [7:0] rom_mux = (romcs && chosen[`SP_BUS_DRIVE]) ? chosen[`SP_BUS_RDATA] :
                         romcs ? 8'hff : rom_data;
    wire [7:0] io_mux = chosen[`SP_BUS_DRIVE] ? chosen[`SP_BUS_RDATA] :
                        kempston_hit ? {3'b000, kempston} :
                        ula_access ? ula_read : 8'hff;
    assign cpu_di = mem_read ? (rom_region ? rom_mux : ram_data) : io_read ? io_mux : 8'hff;
endmodule
