// SPDX-License-Identifier: GPL-2.0-or-later
// ZX81 machine extracted from MiSTer-devel/ZX81_MiSTer ZX81.sv (Release 20260603).
// Fixed first slice: ZX81, 16 KB RAM, PAL, no CHROMA/QS/YM2149/joystick.
// Keyboard rows and .p tape come from the FES GP mailbox.

module zx81_machine #(
    parameter EXTERNAL_RAM = 0
) (
    input  wire        clk_sys,
    input  wire        reset,
    input  wire [39:0] keyboard,
    input  wire        tape_ready,
    input  wire [14:0] tape_size,
    input  wire [7:0]  tape_data,
    output wire [13:0] tape_addr_out,
    // High while the $0347 loader is copying mailbox bytes into RAM.
    output wire        tape_busy,
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
    input  wire [7:0]  bus_rdata,
    input  wire [7:0]  bus_peek_data,
    input  wire        bus_dsel,
    input  wire        bus_romcs,
    input  wire        bus_wait,
    input  wire        bus_ram_present
);
    localparam [1:0] MEM_SIZE_16K = 2'd1;
    localparam ZX81 = 1'b1;
    localparam HZ50 = 1'b1;

    wire [15:0] addr;
    logic [7:0] cpu_din;
    wire [7:0] cpu_dout;
    wire nM1, nMREQ, nIORQ, nRD, nWR, nRFSH, nHALT;
    wire nINT = addr[6];
    assign cpu_addr = addr;
    assign halt_n = nHALT;

    reg ce_cpu_p, ce_cpu_n, ce_3m25, ce_6m5_r;
    reg [4:0] ce_counter = 0;
    assign ce_6m5 = ce_6m5_r;

    always @(negedge clk_sys) begin
        ce_counter <= ce_counter + 1'd1;
        ce_cpu_p <= !ce_counter[3] & !ce_counter[2:0];
        ce_cpu_n <= ce_counter[3] & !ce_counter[2:0];
        ce_3m25  <= !ce_counter[3:0];
        ce_6m5_r <= !ce_counter[2:0];
    end

    T80pa cpu (
        .RESET_n(~reset),
        .CLK(clk_sys),
        .CEN_p(ce_cpu_p),
        .CEN_n(ce_cpu_n),
        .WAIT_n(nWAIT),
        .INT_n(nINT),
        .NMI_n(nNMI),
        .BUSRQ_n(1'b1),
        .M1_n(nM1),
        .MREQ_n(nMREQ),
        .IORQ_n(nIORQ),
        .RD_n(nRD),
        .WR_n(nWR),
        .RFSH_n(nRFSH),
        .HALT_n(nHALT),
        .BUSAK_n(),
        .A(addr),
        .DO(cpu_dout),
        .DI(cpu_din)
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
    always_comb begin
        // CPU window reads use DSEL. Do not fold /RFSH into this mux:
        // nRFSH -> mem_out -> cpu_din -> T80 is a combinational loop and
        // the FES GP mailbox stops acknowledging.
        if (bus_dsel)
            mem_out = bus_rdata;
        else casex ({tapeloader, rom_e, ram_e})
            3'b1xx: mem_out = tape_loader_patch[addr - 13'h0347];
            3'b01x: mem_out = rom_out;
            3'b001: mem_out = ram_out;
            default: mem_out = 8'hFF;
        endcase
    end

    wire low16k_e = ~addr[15] | ~MEM_SIZE_16K[1];
    wire ram_e = addr[14];
    wire [15:0] ram_a = tapeloader ? {2'b01, tape_addr + 4'd8} : {2'b01, addr[13:0]};

    wire [7:0] ram_out;
    assign ram_address = ram_a[13:0];
    assign ram_write_data = tapeloader ? tape_in_byte_r : cpu_dout;
    assign ram_write_enable = (~nWR & ~nMREQ & ram_e) | tapewrite_we;
    generate if (EXTERNAL_RAM) begin : expansion_ram
        wire [7:0] internal_data;
        wire [7:0] internal_peek;
        /* verilator lint_off UNUSEDSIGNAL */
        wire unused_external = ^{external_ram_data, external_peek_data};
        /* verilator lint_on UNUSEDSIGNAL */
        zx81_dpram #(.ADDRWIDTH(10), .NUMWORDS(1024)) internal_ram (
            .clock(clk_sys),
            .address_a(ram_address[9:0]),
            .data_a(ram_write_data),
            .wren_a(ram_write_enable && !bus_ram_present),
            .q_a(internal_data),
            .address_b(peek_addr[9:0]),
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
        .address(rom_a),
        .data(rom_out)
    );
`else
`ifdef QUARTUS
    localparam ROM_INIT = "cores/fes-zx81/rtl/zx8x.mif";
`else
    localparam ROM_INIT = "cores/fes-zx81/rtl/zx8x.hex";
`endif
    zx81_dpram #(.ADDRWIDTH(14), .NUMWORDS(16384), .MEM_INIT_FILE(ROM_INIT)) rom (
        .clock(clk_sys),
        .address_a({1'b0, rom_a[12], rom_a[11:0]}),
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
    assign tape_addr_out = tape_addr;
    assign tape_busy = tapeloader;
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
        if (reset) begin
            tapeloader <= 0;
            tape_addr <= 0;
            tape_loader_patch[0] <= 8'haf;
            tape_loader_patch[1] <= 8'h00;
            tape_loader_patch[2] <= 8'h30;
            tape_loader_patch[3] <= 8'hfd;
            tape_loader_patch[4] <= 8'hc3;
            tape_loader_patch[5] <= 8'h07;
            tape_loader_patch[6] <= 8'h02;
        end else begin
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
            if (~nIORQ & ~nWR & (addr[0] ^ addr[1]))
                NMIlatch <= addr[1];
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
