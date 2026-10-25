// SPDX-License-Identifier: GPL-2.0-or-later
// ZX81-family machine extracted from MiSTer-devel/ZX81_MiSTer ZX81.sv
// (Release 20260603). No model define is the Sinclair ZX81: PAL, 8 KiB ROM,
// 1 KiB internal RAM when the expansion socket is on, and NMI. ZX81_MODEL_ZX80,
// ZX81_MODEL_TS1000 and ZX81_MODEL_TS1500 select the other shells. The socket
// stays on for every model. Keyboard rows and tape come from the FES GP mailbox.
`include "zx81_model.vh"

module zx81_machine #(
    parameter EXTERNAL_RAM = 0,
    parameter FIRMWARE_INIT = ""
) (
    input  wire        clk_sys,
    input  wire        reset,
    input  wire [39:0] keyboard,
    input  wire        tape_ready,
    input  wire [14:0] tape_size,
    input  wire [7:0]  tape_data,
    output wire [13:0] tape_addr_out,
    // High while the cassette loader is copying mailbox bytes into RAM.
    output wire        tape_busy,
    // ROM SAVE at $02F7 captures the program image the $031E emitter reads.
    // save_q is the byte at save_addr in that .p, one clock after save_addr.
    output wire        save_ready,
    output wire        save_busy,
    output wire        save_changed,
    output wire [14:0] save_size,
    input  wire [13:0] save_addr,
    output wire [7:0]  save_q,
    output wire        ce_6m5,
    output wire        video_pixel,
    output wire        hblank,
    output wire        vblank,
    output wire        hsync_out,
    output wire        vsync_out,
    output wire        halt_n,
    output wire [15:0] cpu_addr,
    input  wire [15:0] peek_addr,
    output wire [7:0]  peek_data,
    output wire [13:0] ram_address,
    output wire [7:0]  ram_write_data,
    output wire        ram_write_enable,
    input  wire [7:0]  external_ram_data,
    input  wire [7:0]  external_peek_data,
    output wire [15:0] bus_addr,
    output wire [7:0]  bus_wdata,
    output wire        bus_mreq_n,
    output wire        bus_iorq_n,
    output wire        bus_rd_n,
    output wire        bus_wr_n,
    output wire        bus_m1_n,
    output wire        bus_rfsh_n,
    output wire        bus_cpu_clock,
    input  wire [7:0]  bus_rdata,
    input  wire [7:0]  bus_peek_data,
    input  wire        bus_dsel,
    input  wire        bus_romcs,
    input  wire        bus_wait,
    input  wire        bus_ram_present
);
    localparam [1:0] MEM_SIZE_16K = 2'd1;
`ifdef ZX81_MODEL_ZX80
    localparam ZX81 = 1'b0;
`else
    localparam ZX81 = 1'b1;
`endif
`ifdef ZX81_MODEL_TS1000
    localparam HZ50 = 1'b0;
`elsif ZX81_MODEL_TS1500
    localparam HZ50 = 1'b0;
`else
    localparam HZ50 = 1'b1;
`endif

    wire [15:0] addr;
    logic [7:0] cpu_din;
    wire [7:0] cpu_dout;
    wire nM1, nMREQ, nIORQ, nRD, nWR, nRFSH, nHALT;
    wire [15:0] debug_de;
    reg save_fast;
    wire nINT = addr[6];
    assign cpu_addr = addr;
    assign halt_n = nHALT;

    wire ce_cpu_p, ce_cpu_n, ce_6m5_r;
    wire ce_3m25 = ce_cpu_p;
    assign ce_6m5 = ce_6m5_r;
    zx81_machine_clock machine_clock (
        .clk_sys(clk_sys), .reset(1'b0), .ce_6m5(ce_6m5_r),
        .ce_cpu_p(ce_cpu_p), .ce_cpu_n(ce_cpu_n), .cpu_clock(bus_cpu_clock)
    );

    fes_z80_nmos cpu (
        .reset(reset),
        .clk(clk_sys),
        .ce_p(ce_cpu_p),
        .ce_n(ce_cpu_n),
        .wait_n(nWAIT),
        .int_n(nINT),
        .nmi_n(nNMI),
        .busrq_n(1'b1),
        .m1_n(nM1),
        .mreq_n(nMREQ),
        .iorq_n(nIORQ),
        .rd_n(nRD),
        .wr_n(nWR),
        .rfsh_n(nRFSH),
        .halt_n(nHALT),
        .busak_n(),
        .a(addr),
        .dout(cpu_dout),
        .din(cpu_din),
        .illegal(),
        .retired(),
        .retire_pc(),
        .debug_pc(),
        .debug_sp(),
        .debug_af(),
        .debug_bc(),
        .debug_de(debug_de),
        .debug_hl(),
        .debug_ix(),
        .debug_iy(),
        .debug_ir(),
        .debug_iff(),
        .cycle_present(),
        .cycle_kind(),
        .cycle_t()
    );

    always_comb begin
        if (bus_dsel)
            cpu_din = bus_rdata;
        else case ({nMREQ, ~nM1 | nIORQ | nRD})
            2'b01: cpu_din = (~nM1 & nopgen) ? 8'h00 : mem_out;
            2'b10: cpu_din = io_dout;
            default: cpu_din = 8'hFF;
        endcase
    end

    wire kbd_n = nIORQ | nRD | addr[0];
    wire [4:0] key_data =
        (!addr[8]  ? keyboard[4:0]   : 5'b11111) &
        (!addr[9]  ? keyboard[9:5]   : 5'b11111) &
        (!addr[10] ? keyboard[14:10] : 5'b11111) &
        (!addr[11] ? keyboard[19:15] : 5'b11111) &
        (!addr[12] ? keyboard[24:20] : 5'b11111) &
        (!addr[13] ? keyboard[29:25] : 5'b11111) &
        (!addr[14] ? keyboard[34:30] : 5'b11111) &
        (!addr[15] ? keyboard[39:35] : 5'b11111);

    wire [7:0] io_dout = ~kbd_n ? {1'b0, HZ50, 1'b0, key_data} : 8'hFF;

    logic [7:0] mem_out;
    // SAVE skips the leader. The runtime body is:
    //   $02F4 RST 08 / $02F5 class / $02F6 CALL NAME / $02F9 JR C
    //   $02FB EX DE,HL / $02FC LD DE,$12CB / delay loop through $0309
    //   $030B name bytes / $0313 LD HL,$4009 / $0316 CALL $031E
    //   $031E LD E,(HL) / $031F SCF bit-bang.
    // $02FC-$02FE become JP $0313 once NAME has returned, and $031F becomes
    // RET so each program byte is the RAM value LD E,(HL) already read.
    // The filename burst is not part of a .p.
    wire save_patch_ret = save_fast && addr == 16'h031f;
    wire save_patch_delay = save_fast && addr >= 16'h02fc && addr <= 16'h02fe;
    wire [7:0] save_delay_byte = addr == 16'h02fc ? 8'hc3 : addr == 16'h02fd ? 8'h13 : 8'h03;
    always_comb begin
        // CPU window reads use DSEL. Do not fold /RFSH into this mux:
        // nRFSH -> mem_out -> cpu_din is a combinational loop.
        if (bus_dsel)
            mem_out = bus_rdata;
        else if (tapeloader)
`ifdef ZX81_MODEL_ZX80
            mem_out = tape_loader_patch[addr - 13'h0207];
`else
            mem_out = tape_loader_patch[addr - 13'h0347];
`endif
        else if (save_patch_ret)
            mem_out = 8'hc9;
        else if (save_patch_delay)
            mem_out = save_delay_byte;
        else case ({rom_e, ram_e})
            2'b10, 2'b11: mem_out = rom_out;
            2'b01: mem_out = ram_out;
            default: mem_out = 8'hFF;
        endcase
    end

    wire low16k_e = ~addr[15] | ~MEM_SIZE_16K[1];
    wire ram_e = addr[14];
`ifdef ZX81_MODEL_ZX80
    // Raw image at $4000. The ZX81 .p loader adds eight bytes; ZX80 does not.
    // tapewrite_we is registered with tape_addr's increment, so writes use
    // the previous byte address.
    wire [15:0] ram_a = tapeloader ? {2'b01, tape_addr - 14'd1} : {2'b01, addr[13:0]};
`else
    wire [15:0] ram_a = tapeloader ? {2'b01, tape_addr + 4'd8} : {2'b01, addr[13:0]};
`endif

    wire [7:0] ram_out;
    assign ram_address = ram_a[13:0];
    assign ram_write_data = tapeloader ? tape_in_byte_r : cpu_dout;
    assign ram_write_enable = (~nWR & ~nMREQ & ram_e) | tapewrite_we;
`ifdef ZX81_MODEL_TS1500
    localparam integer INTERNAL_RAM_WORDS = 16384;
    localparam integer INTERNAL_RAM_WIDTH = 14;
`elsif ZX81_MODEL_TS1000
    localparam integer INTERNAL_RAM_WORDS = 2048;
    localparam integer INTERNAL_RAM_WIDTH = 11;
`else
    localparam integer INTERNAL_RAM_WORDS = 1024;
    localparam integer INTERNAL_RAM_WIDTH = 10;
`endif
    generate if (EXTERNAL_RAM) begin : expansion_ram
        wire [7:0] internal_data;
        wire [7:0] internal_peek;
        /* verilator lint_off UNUSEDSIGNAL */
        wire unused_external = ^{external_ram_data, external_peek_data};
        /* verilator lint_on UNUSEDSIGNAL */
        zx81_dpram #(.ADDRWIDTH(INTERNAL_RAM_WIDTH), .NUMWORDS(INTERNAL_RAM_WORDS)) internal_ram (
            .clock(clk_sys),
            .address_a(ram_address[INTERNAL_RAM_WIDTH-1:0]),
            .data_a(ram_write_data),
            .wren_a(ram_write_enable && !bus_ram_present),
            .q_a(internal_data),
            .address_b(peek_addr[INTERNAL_RAM_WIDTH-1:0]),
            .data_b(8'h00),
            .wren_b(1'b0),
            .q_b(internal_peek)
        );
        assign ram_out = bus_ram_present ? bus_rdata : internal_data;
        assign peek_data = bus_ram_present ? bus_peek_data : internal_peek;
    end else begin : builtin_ram
    zx81_dpram #(.ADDRWIDTH(14), .NUMWORDS(16384)) ram_block (
        .clock(clk_sys),
        .address_a(ram_address),
        .data_a(ram_write_data),
        .wren_a(ram_write_enable),
        .q_a(ram_out),
        .address_b(peek_addr[13:0]),
        .data_b(8'h00),
        .wren_b(1'b0),
        .q_b(peek_data)
    );
    end endgenerate

    wire [12:0] rom_a = nRFSH ? addr[12:0] :
        {addr[12:9] + (addr[13] & ram_data_latch[7] & addr[8]), ram_data_latch[5:0], row_counter};
    wire rom_e = ~addr[14] & ~addr[13] & (~addr[12] | ZX81) & low16k_e & ~bus_romcs;
    wire [7:0] rom_out;
`ifdef FES_ZX81_ROM_LINK
    zx81_rom_link rom (
        .clk(clk_sys),
`ifdef ZX81_MODEL_ZX80
        .address({1'b0, rom_a[11:0]}),
`else
        .address(rom_a),
`endif
        .data(rom_out)
    );
`else
`ifdef QUARTUS
    localparam ROM_INIT = FIRMWARE_INIT == "" ? "cores/fes-zx81/rtl/zx8x.mif" : FIRMWARE_INIT;
`else
    localparam ROM_INIT = FIRMWARE_INIT == "" ? "cores/fes-zx81/rtl/zx8x.hex" : FIRMWARE_INIT;
`endif
    zx81_dpram #(.ADDRWIDTH(14), .NUMWORDS(16384), .MEM_INIT_FILE(ROM_INIT)) rom (
        .clock(clk_sys),
`ifdef ZX81_MODEL_ZX80
        .address_a({2'b00, rom_a[11:0]}),
`else
        .address_a({1'b0, rom_a[12], rom_a[11:0]}),
`endif
        .data_a(8'h00),
        .wren_a(1'b0),
        .q_a(rom_out),
        .address_b(14'd0),
        .data_b(8'h00),
        .wren_b(1'b0),
        .q_b()
    );
`endif

    reg tapeloader, tapewrite_we;
    reg [13:0] tape_addr;
    reg [7:0] tape_in_byte, tape_in_byte_r;
    reg [7:0] tape_loader_patch[0:6];
    // nM1 stays low for many host clocks. The byte and completion events
    // latch once per opcode fetch; the address match itself stays level
    // sensitive so a fetch is not consumed before A is the opcode address.
    reg save_program, save_ready_r, save_changed_r, save_overflow;
    reg save_seen_byte, save_seen_done;
    reg save_we;
    reg [14:0] save_len;
    reg [13:0] save_waddr;
    reg [7:0] save_wdata;
    zx81_dpram #(.ADDRWIDTH(14), .NUMWORDS(16384)) save_ram (
        .clock(clk_sys),
        .address_a(save_waddr),
        .data_a(save_wdata),
        .wren_a(save_we),
        .q_a(),
        .address_b(save_addr),
        .data_b(8'h00),
        .wren_b(1'b0),
        .q_b(save_q)
    );
    assign tape_addr_out = tape_addr;
    assign tape_busy = tapeloader;
    assign save_ready = save_ready_r;
    assign save_busy = save_program;
    assign save_changed = save_changed_r;
    assign save_size = save_len;
    assign bus_wdata = tapeloader ? tape_in_byte_r : cpu_dout;
    assign bus_mreq_n = tapewrite_we ? 1'b0 : nMREQ;
    assign bus_iorq_n = tapewrite_we ? 1'b1 : nIORQ;
    assign bus_rd_n = tapewrite_we ? 1'b1 : nRD;
    assign bus_wr_n = tapewrite_we ? 1'b0 : nWR;
    assign bus_m1_n = tapewrite_we ? 1'b1 : nM1;
    assign bus_rfsh_n = tapewrite_we ? 1'b1 : nRFSH;

    always @(posedge clk_sys) tape_in_byte <= tape_data;

    always @(posedge clk_sys) begin
        tapewrite_we <= 0;
        save_changed_r <= 0;
        save_we <= 0;
        if (reset) begin
            tapeloader <= 0;
            tape_addr <= 0;
            save_fast <= 0;
            save_program <= 0;
            save_ready_r <= 0;
            save_overflow <= 0;
            save_seen_byte <= 0;
            save_seen_done <= 0;
            save_len <= 0;
            save_waddr <= 0;
            save_wdata <= 0;
            tape_loader_patch[0] <= 8'haf;
            tape_loader_patch[1] <= 8'h00;
            tape_loader_patch[2] <= 8'h30;
            tape_loader_patch[3] <= 8'hfd;
            tape_loader_patch[4] <= 8'hc3;
`ifdef ZX81_MODEL_ZX80
            tape_loader_patch[5] <= 8'h03;
`else
            tape_loader_patch[5] <= 8'h07;
`endif
            tape_loader_patch[6] <= 8'h02;
        end else begin
`ifdef ZX81_MODEL_ZX80
            // ZX80 cassette entry is $0207 and returns with JP $0203.
            // The image is stored at tape_addr with no +8 .p header.
            // save_fast stays clear: $0207 is the ZX81 SAVE completion fetch.
            if (~nM1 && addr == 16'h0207 && !tapeloader) begin
                tape_loader_patch[1] <= (tape_ready && tape_size != 0) ? 8'h00 : 8'h37;
                tape_loader_patch[5] <= 8'h03;
                tape_addr <= 14'h0;
                tapeloader <= 1;
            end
            if (tapeloader && ~nM1 && (addr >= 16'h024d || addr < 16'h0207))
                tapeloader <= 0;
`else
            if (nM1) begin
                save_seen_byte <= 0;
                save_seen_done <= 0;
            end
            // Arm on M1 of EX DE,HL, after NAME accepts the string and before
            // the leader's LD DE is fetched. $02F7 is an operand of CALL NAME,
            // so it is not an M1 address.
            if (~nM1 && addr == 16'h02fb && !tapeloader)
                save_fast <= 1;
            // M1 of LD HL,$4009. $0314 is the immediate 09, not the opcode.
            if (~nM1 && save_fast && addr == 16'h0313) begin
                save_program <= 1;
                save_ready_r <= 0;
                save_overflow <= 0;
                save_len <= 0;
            end
            // M1 of the patched RET: LD E,(HL) has committed the emitted byte.
            if (~nM1 && save_program && addr == 16'h031f && !save_seen_byte) begin
                save_seen_byte <= 1;
                if (save_len < 15'd16384) begin
                    save_we <= 1;
                    save_waddr <= save_len[13:0];
                    save_wdata <= debug_de[7:0];
                    save_len <= save_len + 15'd1;
                end else
                    save_overflow <= 1;
            end
            if (~nM1 && save_program && addr == 16'h0207 && !save_seen_done) begin
                save_seen_done <= 1;
                save_program <= 0;
                save_fast <= 0;
                if (save_len != 0 && !save_overflow) begin
                    save_ready_r <= 1;
                    save_changed_r <= 1;
                end
            end
            // Always intercept LOAD at $0347. A committed mailbox blob is
            // copied into RAM; with no blob, SCF immediately so BASIC
            // returns 0/0 instead of the original cassette waiter (black
            // screen until BREAK).
            if (~nM1 && addr == 16'h0347 && !tapeloader) begin
                tape_loader_patch[1] <= (tape_ready && tape_size != 0) ? 8'h00 : 8'h37;
                tape_loader_patch[5] <= 8'h07;
                tape_addr <= 14'h0;
                tapeloader <= 1;
            end
            if (tapeloader && ~nM1 && (addr >= 16'h03c3 || addr < 16'h0347))
                tapeloader <= 0;
`endif
            if (tapeloader & ce_cpu_p) begin
                if (tape_ready && {1'b0, tape_addr} < tape_size) begin
                    tape_addr <= tape_addr + 1'h1;
                    tape_in_byte_r <= tape_in_byte;
                    tapewrite_we <= 1;
                end else
                    tape_loader_patch[1] <= 8'h37;
            end
        end
    end

    wire nopgen = addr[15] & ~mem_out[6] & nHALT;
    wire data_latch_enable = nRFSH & ce_cpu_p & ~nMREQ;
    reg [7:0] ram_data_latch;
    reg nopgen_store;
    reg [2:0] row_counter;
    // Present the ULA character-ROM address as the QS 1 KiB window during
    // /RFSH. CPU A during refresh is I+R, which the board does not use.
    wire [15:0] chr_fetch = {6'h21, ram_data_latch[6:0], row_counter};
    assign bus_addr = tapewrite_we ? ram_a : (~nRFSH ? chr_fetch : addr);
    // Hold the first settled cart byte. ROMCS is a short pulse through the
    // socket FFs; later /RFSH clocks must not replace it with onboard ROM.
    reg rfsh_from_cart;
    reg [7:0] rfsh_chr;
    always @(posedge clk_sys) begin
        if (reset) begin
            rfsh_from_cart <= 1'b0;
            rfsh_chr <= 8'h00;
        end else if (nRFSH) begin
            rfsh_from_cart <= 1'b0;
        end else if (bus_romcs) begin
            rfsh_from_cart <= 1'b1;
            rfsh_chr <= bus_rdata;
        end else if (!rfsh_from_cart) begin
            rfsh_chr <= mem_out;
        end
    end
    wire shifter_start = nMREQ & nopgen_store & ce_cpu_p & shifter_en & ~NMIlatch;
    reg [7:0] shifter_reg;
    reg inverse;
    wire video_out = shifter_reg[7] ^ inverse;
    reg [7:0] paper_reg;
    wire border = ~paper_reg[7];
    reg shifter_en;
    assign video_pixel = border ? 1'b0 : video_out;

    always @(posedge clk_sys) begin
        reg old_hsync, old_hblank;
        reg old_shifter_start;
        if (reset) begin
            shifter_reg <= 0;
            paper_reg <= 0;
            row_counter <= 0;
            shifter_en <= 0;
            inverse <= 0;
        end else if (ce_6m5_r) begin
            old_hsync <= hsync;
            if (data_latch_enable) begin
                ram_data_latch <= mem_out;
                nopgen_store <= nopgen;
            end
            if (nMREQ & ce_cpu_p)
                inverse <= 0;
            old_shifter_start <= shifter_start;
            shifter_reg <= {shifter_reg[6:0], 1'b0};
            paper_reg <= {paper_reg[6:0], 1'b0};
            if (~old_shifter_start & shifter_start) begin
                shifter_reg <= (~nM1 & nopgen) ? 8'h0 : rfsh_chr;
                inverse <= ram_data_latch[7];
                paper_reg <= 8'hFF;
            end
            if (~old_hsync & hsync)
                row_counter <= row_counter + 1'd1;
            if (vs)
                row_counter <= 0;
            old_hblank <= hblank;
            if (~old_hblank & hblank)
                shifter_en <= 0;
            if (old_hblank & ~hblank)
                shifter_en <= ~NMIlatch;
        end
    end

    reg vsync, vs;
    always @(posedge clk_sys) begin
        if (reset) begin
            vs <= 0;
            vsync <= 0;
        end else begin
            if (~nIORQ & ~nWR & ~NMIlatch) vs <= 0;
            if (~kbd_n & ~NMIlatch) vs <= 1;
            if (!hsync) vsync <= vs;
        end
    end

    wire nWAIT = (~nHALT | nNMI) & ~bus_wait;
    wire nNMI = ~NMIlatch | ~hsync;

    reg slow_mode;
    always @(posedge clk_sys) begin
        reg [6:0] fcnt;
        reg old_halt, old_latch;
        old_latch <= NMIlatch;
        old_halt <= nHALT;
        if (~old_latch & NMIlatch) begin
            if (&fcnt) slow_mode <= 1;
            else fcnt <= fcnt + 1'd1;
        end
        if (old_halt & ~nHALT)
            slow_mode <= 0;
        if (reset)
            {fcnt, slow_mode} <= 0;
    end

    /* verilator lint_off UNUSEDSIGNAL */
    wire unused_slow = slow_mode;
    /* verilator lint_on UNUSEDSIGNAL */

    reg [7:0] sync_counter;
    reg NMIlatch;
    reg hsync;
    always @(posedge clk_sys) begin
        if (reset) begin
            sync_counter <= 0;
            NMIlatch <= 0;
            hsync <= 0;
        end else begin
            if (ce_3m25) begin
                sync_counter <= sync_counter + 1'd1;
                if (sync_counter == 206) sync_counter <= 0;
                if (sync_counter == 15) hsync <= 1;
                if (sync_counter == 31) hsync <= 0;
            end
            if (~nM1 & ~nIORQ)
                {hsync, sync_counter} <= 0;
`ifdef ZX81_MODEL_ZX80
            NMIlatch <= 0;
`else
            if (~nIORQ & ~nWR & (addr[0] ^ addr[1]))
                NMIlatch <= addr[1];
`endif
        end
    end

    reg hsync2, vsync2, hblank_r, vblank_r;
    assign hblank = hblank_r;
    assign vblank = vblank_r;
    assign hsync_out = hsync2;
    assign vsync_out = vsync2;

    always @(posedge clk_sys) begin
        reg [8:0] cnt;
        reg [4:0] vreg;
        reg old_hsync;
        if (reset) begin
            cnt <= 0;
            hsync2 <= 0;
            vsync2 <= 0;
            hblank_r <= 1;
            vblank_r <= 1;
        end else if (ce_6m5_r) begin
            cnt <= cnt + 1'd1;
            if (cnt == 413) cnt <= 0;
            if (cnt == 0) hsync2 <= 1;
            if (cnt == 32) hsync2 <= 0;
            if (cnt == 400) hblank_r <= 1;
            if (cnt == 72) hblank_r <= 0;
            old_hsync <= hsync;
            if (~old_hsync & hsync) begin
                vreg <= {vreg[3:0], vsync};
                vblank_r <= |{vreg, vsync};
                vsync2 <= vreg[2];
                if (&vreg[3:2]) cnt <= 0;
            end
        end
    end
endmodule
